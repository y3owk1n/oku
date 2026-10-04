package cli_test

import (
	"context"
	"crypto"
	"crypto/ecdsa"
	"crypto/elliptic"
	"crypto/rand"
	"crypto/sha256"
	"crypto/x509"
	"crypto/x509/pkix"
	"encoding/asn1"
	"encoding/base64"
	"encoding/hex"
	"encoding/json"
	"encoding/pem"
	"fmt"
	"math/big"
	"net/http"
	"net/http/httptest"
	"net/url"
	"strconv"
	"strings"
	"sync"
	"testing"
	"time"

	"github.com/cyberphone/json-canonicalization/go/src/webpki.org/jsoncanonicalizer"
	"github.com/secure-systems-lab/go-securesystemslib/dsse"
	protobundle "github.com/sigstore/protobuf-specs/gen/pb-go/bundle/v1"
	protocommon "github.com/sigstore/protobuf-specs/gen/pb-go/common/v1"
	protodsse "github.com/sigstore/protobuf-specs/gen/pb-go/dsse"
	protorekor "github.com/sigstore/protobuf-specs/gen/pb-go/rekor/v1"
	"github.com/sigstore/rekor/pkg/pki"
	"github.com/sigstore/rekor/pkg/types"
	dssekind "github.com/sigstore/rekor/pkg/types/dsse"
	_ "github.com/sigstore/rekor/pkg/types/dsse/v0.0.1"
	"github.com/sigstore/rekor/pkg/types/hashedrekord"
	_ "github.com/sigstore/rekor/pkg/types/hashedrekord/v0.0.1"
	"github.com/sigstore/sigstore-go/pkg/bundle"
	"github.com/sigstore/sigstore-go/pkg/root"
	"github.com/sigstore/sigstore-go/pkg/testing/ca"
	"github.com/sigstore/sigstore-go/pkg/tlog"
	"github.com/sigstore/sigstore-go/pkg/verify"

	"github.com/y3owk1n/oku/internal/sigstore"
)

// fakeSigstore is a Fulcio and a Rekor of the tests' own. Its certificates
// carry what GitHub Actions puts in them: the workflow, the issuer, and the
// repo and ref of the run.
type fakeSigstore struct {
	fulcio    *x509.Certificate
	fulcioKey *ecdsa.PrivateKey
	rekorKey  *ecdsa.PrivateKey
	logID     string

	// logged holds the entries of the fake Rekor by the sha256 of the file they
	// sign.
	mu     sync.Mutex
	logged map[string][]*protorekor.TransparencyLogEntry
}

// run is a GitHub Actions run that signs: the workflow file at the ref it was
// called at, and the repo and ref the run is for.
type run struct {
	workflow, repo, ref string
}

// newFakeSigstore makes m trust a fake Sigstore, and returns it.
func newFakeSigstore(t *testing.T, m *machine) *fakeSigstore {
	t.Helper()

	rootCert, rootKey, err := ca.GenerateRootCa()
	must(t, err)

	fulcio, fulcioKey, err := ca.GenerateFulcioIntermediate(rootCert, rootKey)
	must(t, err)

	rekorKey, err := ecdsa.GenerateKey(elliptic.P256(), rand.Reader)
	must(t, err)

	der, err := x509.MarshalPKIXPublicKey(rekorKey.Public())
	must(t, err)

	id := sha256.Sum256(der)
	logID := hex.EncodeToString(id[:])
	now := time.Now()

	material, err := root.NewTrustedRoot(
		root.TrustedRootMediaType01,
		[]root.CertificateAuthority{&root.FulcioCertificateAuthority{
			Root: rootCert, Intermediates: []*x509.Certificate{fulcio}, URI: "https://fulcio.test",
			ValidityPeriodStart: now.Add(-time.Hour), ValidityPeriodEnd: now.Add(time.Hour),
		}},
		nil, nil,
		map[string]*root.TransparencyLog{logID: {
			BaseURL: "https://rekor.test", ID: id[:], HashFunc: crypto.SHA256, PublicKey: rekorKey.Public(),
			ValidityPeriodStart: now.Add(-time.Hour), ValidityPeriodEnd: now.Add(time.Hour),
			SignatureHashFunc: crypto.SHA256,
		}},
	)
	must(t, err)

	f := &fakeSigstore{
		fulcio: fulcio, fulcioKey: fulcioKey, rekorKey: rekorKey, logID: logID,
		logged: map[string][]*protorekor.TransparencyLogEntry{},
	}

	rekor := httptest.NewServer(http.HandlerFunc(f.serveRekor))
	t.Cleanup(rekor.Close)

	m.opts.Sigstore = sigstore.New(material, rekor.URL, rekor.Client(),
		verify.WithTransparencyLog(1), verify.WithObserverTimestamps(1))

	return f
}

// serveRekor answers the two calls of Rekor's API that oku makes: the
// entries for a file's sha256, and one entry.
func (f *fakeSigstore) serveRekor(w http.ResponseWriter, r *http.Request) {
	f.mu.Lock()
	defer f.mu.Unlock()

	switch id, ok := strings.CutPrefix(r.URL.Path, "/api/v1/log/entries/"); {
	case r.URL.Path == "/api/v1/index/retrieve":
		var query struct {
			Hash string `json:"hash"`
		}
		_ = json.NewDecoder(r.Body).Decode(&query)

		ids := []string{}
		for i := range f.logged[strings.TrimPrefix(query.Hash, "sha256:")] {
			ids = append(ids, fmt.Sprintf("%s-%d", strings.TrimPrefix(query.Hash, "sha256:"), i))
		}

		_ = json.NewEncoder(w).Encode(ids)
	case ok:
		digest, index, _ := strings.Cut(id, "-")
		i, _ := strconv.Atoi(index)
		e := f.logged[digest][i]

		_ = json.NewEncoder(w).Encode(map[string]any{id: map[string]any{
			"body":           base64.StdEncoding.EncodeToString(e.CanonicalizedBody),
			"integratedTime": e.IntegratedTime,
			"logIndex":       e.LogIndex,
			"logID":          hex.EncodeToString(e.LogId.KeyId),
			"verification": map[string]any{
				"signedEntryTimestamp": base64.StdEncoding.EncodeToString(e.InclusionPromise.SignedEntryTimestamp),
			},
		}})
	default:
		http.NotFound(w, r)
	}
}

// leaf issues a certificate for r and returns it with its key.
func (f *fakeSigstore) leaf(t *testing.T, r run) (*x509.Certificate, *ecdsa.PrivateKey) {
	t.Helper()

	key, err := ecdsa.GenerateKey(elliptic.P256(), rand.Reader)
	must(t, err)

	subject, err := url.Parse("https://github.com/" + r.workflow)
	must(t, err)

	utf8 := func(oid asn1.ObjectIdentifier, value string) pkix.Extension {
		data, err := asn1.MarshalWithParams(value, "utf8")
		must(t, err)

		return pkix.Extension{Id: oid, Value: data}
	}

	der, err := x509.CreateCertificate(rand.Reader, &x509.Certificate{
		SerialNumber: big.NewInt(time.Now().UnixNano()),
		URIs:         []*url.URL{subject},
		NotBefore:    time.Now().Add(-time.Minute),
		NotAfter:     time.Now().Add(10 * time.Minute),
		KeyUsage:     x509.KeyUsageDigitalSignature,
		ExtKeyUsage:  []x509.ExtKeyUsage{x509.ExtKeyUsageCodeSigning},
		ExtraExtensions: []pkix.Extension{
			{Id: asn1.ObjectIdentifier{1, 3, 6, 1, 4, 1, 57264, 1, 1}, Value: []byte(sigstore.GitHubIssuer)},
			utf8(asn1.ObjectIdentifier{1, 3, 6, 1, 4, 1, 57264, 1, 8}, sigstore.GitHubIssuer),
			utf8(asn1.ObjectIdentifier{1, 3, 6, 1, 4, 1, 57264, 1, 12}, "https://github.com/"+r.repo),
			utf8(asn1.ObjectIdentifier{1, 3, 6, 1, 4, 1, 57264, 1, 14}, r.ref),
		},
	}, f.fulcio, key.Public(), f.fulcioKey)
	must(t, err)

	cert, err := x509.ParseCertificate(der)
	must(t, err)

	return cert, key
}

// logEntry records an entry of kind in the fake Rekor, with its promise to
// include it.
func (f *fakeSigstore) logEntry(t *testing.T, kind, version string, props types.ArtifactProperties) *protorekor.TransparencyLogEntry {
	t.Helper()

	ctx := context.Background()

	proposed, err := types.NewProposedEntry(ctx, kind, version, props)
	must(t, err)

	entry, err := types.CreateVersionedEntry(proposed)
	must(t, err)

	body, err := types.CanonicalizeEntry(ctx, entry)
	must(t, err)

	now := time.Now().Unix()

	payload, err := json.Marshal(tlog.RekorPayload{
		Body: base64.StdEncoding.EncodeToString(body), IntegratedTime: now, LogIndex: 1, LogID: f.logID,
	})
	must(t, err)

	canonical, err := jsoncanonicalizer.Transform(payload)
	must(t, err)

	digest := sha256.Sum256(canonical)

	set, err := ecdsa.SignASN1(rand.Reader, f.rekorKey, digest[:])
	must(t, err)

	id, err := hex.DecodeString(f.logID)
	must(t, err)

	return &protorekor.TransparencyLogEntry{
		LogIndex:          1,
		LogId:             &protocommon.LogId{KeyId: id},
		KindVersion:       &protorekor.KindVersion{Kind: kind, Version: version},
		IntegratedTime:    now,
		InclusionPromise:  &protorekor.InclusionPromise{SignedEntryTimestamp: set},
		CanonicalizedBody: body,
	}
}

// cosign signs data as r, records the signature in the fake Rekor, and returns
// the signature and the certificate as cosign writes them beside a file: in
// base64, and PEM in base64.
func (f *fakeSigstore) cosign(t *testing.T, r run, data []byte) (signature, cert []byte) {
	t.Helper()

	c, sig, _, entry := f.sign(t, r, data)

	f.mu.Lock()
	defer f.mu.Unlock()

	digest := digestOf(data)
	f.logged[digest] = append(f.logged[digest], entry)

	return []byte(base64.StdEncoding.EncodeToString(sig)), []byte(base64.StdEncoding.EncodeToString(certPEM(c)))
}

// legacyBundle returns a bundle in which r signs data, in the format cosign
// wrote before Sigstore's own.
func (f *fakeSigstore) legacyBundle(t *testing.T, r run, data []byte) []byte {
	t.Helper()

	c, sig, _, entry := f.sign(t, r, data)

	out, err := json.Marshal(map[string]any{
		"base64Signature": base64.StdEncoding.EncodeToString(sig),
		"cert":            base64.StdEncoding.EncodeToString(certPEM(c)),
		"rekorBundle": map[string]any{
			"SignedEntryTimestamp": base64.StdEncoding.EncodeToString(entry.InclusionPromise.SignedEntryTimestamp),
			"Payload": map[string]any{
				"body":           base64.StdEncoding.EncodeToString(entry.CanonicalizedBody),
				"integratedTime": entry.IntegratedTime,
				"logIndex":       entry.LogIndex,
				"logID":          hex.EncodeToString(entry.LogId.KeyId),
			},
		},
	})
	must(t, err)

	return out
}

// sign signs data as r and returns the certificate, the signature, the
// sha256 of data and the log's entry.
func (f *fakeSigstore) sign(t *testing.T, r run, data []byte) (*x509.Certificate, []byte, [32]byte, *protorekor.TransparencyLogEntry) {
	t.Helper()

	cert, key := f.leaf(t, r)
	digest := sha256.Sum256(data)

	sig, err := ecdsa.SignASN1(rand.Reader, key, digest[:])
	must(t, err)

	entry := f.logEntry(t, hashedrekord.KIND, hashedrekord.New().DefaultVersion(), types.ArtifactProperties{
		ArtifactHash:   hex.EncodeToString(digest[:]),
		SignatureBytes: sig,
		PublicKeyBytes: [][]byte{certPEM(cert)},
		PKIFormat:      string(pki.X509),
	})

	return cert, sig, digest, entry
}

// signBlob returns a bundle in which r signs data, as cosign sign-blob writes.
func (f *fakeSigstore) signBlob(t *testing.T, r run, data []byte) []byte {
	t.Helper()

	cert, sig, digest, entry := f.sign(t, r, data)

	return f.bundle(t, cert, entry, &protobundle.Bundle_MessageSignature{
		MessageSignature: &protocommon.MessageSignature{
			MessageDigest: &protocommon.HashOutput{Algorithm: protocommon.HashAlgorithm_SHA2_256, Digest: digest[:]},
			Signature:     sig,
		},
	})
}

// attest returns a bundle in which r attests that it built the file whose
// sha256 is digest, as GitHub's attest action writes.
func (f *fakeSigstore) attest(t *testing.T, r run, digest string) []byte {
	t.Helper()

	cert, statement, sig, entry := f.statement(t, r, digest)

	return f.bundle(t, cert, entry, &protobundle.Bundle_DsseEnvelope{DsseEnvelope: &protodsse.Envelope{
		Payload: statement, PayloadType: inToto, Signatures: []*protodsse.Signature{{Sig: sig}},
	}})
}

// envelope returns the same statement as attest, as a signed envelope with
// its certificate, the way slsa-github-generator wrote before it wrote
// bundles, and records it in the fake Rekor.
func (f *fakeSigstore) envelope(t *testing.T, r run, digest string) []byte {
	t.Helper()

	cert, statement, sig, entry := f.statement(t, r, digest)

	f.mu.Lock()
	f.logged[digest] = append(f.logged[digest], entry)
	f.mu.Unlock()

	out, err := json.Marshal(map[string]any{
		"payloadType": inToto,
		"payload":     base64.StdEncoding.EncodeToString(statement),
		"signatures": []map[string]string{{
			"keyid": "", "sig": base64.StdEncoding.EncodeToString(sig), "cert": string(certPEM(cert)),
		}},
	})
	must(t, err)

	return out
}

const inToto = "application/vnd.in-toto+json"

// statement signs, as r, an in-toto statement that names the file whose
// sha256 is digest, and returns the certificate, the statement, the signature
// and the log's entry.
func (f *fakeSigstore) statement(t *testing.T, r run, digest string) (*x509.Certificate, []byte, []byte, *protorekor.TransparencyLogEntry) {
	t.Helper()

	cert, key := f.leaf(t, r)

	statement := []byte(`{"_type": "https://in-toto.io/Statement/v1", ` +
		`"subject": [{"name": "tool.tar.gz", "digest": {"sha256": "` + digest + `"}}], ` +
		`"predicateType": "https://slsa.dev/provenance/v1", "predicate": {}}`)

	pae := sha256.Sum256(dsse.PAE(inToto, statement))

	sig, err := ecdsa.SignASN1(rand.Reader, key, pae[:])
	must(t, err)

	envelope, err := json.Marshal(dsse.Envelope{
		PayloadType: inToto,
		Payload:     base64.StdEncoding.EncodeToString(statement),
		Signatures:  []dsse.Signature{{Sig: base64.StdEncoding.EncodeToString(sig)}},
	})
	must(t, err)

	entry := f.logEntry(t, dssekind.KIND, dssekind.New().DefaultVersion(), types.ArtifactProperties{
		ArtifactBytes:  envelope,
		PublicKeyBytes: [][]byte{certPEM(cert)},
		PKIFormat:      string(pki.X509),
	})

	return cert, statement, sig, entry
}

// bundle writes a version 0.1 bundle, which takes a log's promise of
// inclusion in place of a proof.
func (f *fakeSigstore) bundle(
	t *testing.T,
	cert *x509.Certificate,
	entry *protorekor.TransparencyLogEntry,
	content any,
) []byte {
	t.Helper()

	pb := &protobundle.Bundle{
		MediaType: "application/vnd.dev.sigstore.bundle+json;version=0.1",
		VerificationMaterial: &protobundle.VerificationMaterial{
			Content: &protobundle.VerificationMaterial_X509CertificateChain{
				X509CertificateChain: &protocommon.X509CertificateChain{
					Certificates: []*protocommon.X509Certificate{{RawBytes: cert.Raw}},
				},
			},
			TlogEntries: []*protorekor.TransparencyLogEntry{entry},
		},
	}

	switch c := content.(type) {
	case *protobundle.Bundle_MessageSignature:
		pb.Content = c
	case *protobundle.Bundle_DsseEnvelope:
		pb.Content = c
	}

	b, err := bundle.NewBundle(pb)
	must(t, err)

	data, err := b.MarshalJSON()
	must(t, err)

	return data
}

// keySign signs data with key, as cosign sign-blob --key does, records the
// signature in the fake Rekor when logged, and returns it in base64.
func (f *fakeSigstore) keySign(t *testing.T, key *ecdsa.PrivateKey, data []byte, logged bool) []byte {
	t.Helper()

	sig, entry := f.keyEntry(t, key, data)

	if logged {
		f.mu.Lock()
		defer f.mu.Unlock()

		digest := digestOf(data)
		f.logged[digest] = append(f.logged[digest], entry)
	}

	return []byte(base64.StdEncoding.EncodeToString(sig))
}

// keyBundle returns a bundle in which key signs data.
func (f *fakeSigstore) keyBundle(t *testing.T, key *ecdsa.PrivateKey, data []byte) []byte {
	t.Helper()

	sig, entry := f.keyEntry(t, key, data)
	digest := sha256.Sum256(data)

	b, err := bundle.NewBundle(&protobundle.Bundle{
		MediaType: "application/vnd.dev.sigstore.bundle+json;version=0.1",
		VerificationMaterial: &protobundle.VerificationMaterial{
			Content:     &protobundle.VerificationMaterial_PublicKey{PublicKey: &protocommon.PublicKeyIdentifier{}},
			TlogEntries: []*protorekor.TransparencyLogEntry{entry},
		},
		Content: &protobundle.Bundle_MessageSignature{
			MessageSignature: &protocommon.MessageSignature{
				MessageDigest: &protocommon.HashOutput{Algorithm: protocommon.HashAlgorithm_SHA2_256, Digest: digest[:]},
				Signature:     sig,
			},
		},
	})
	must(t, err)

	out, err := b.MarshalJSON()
	must(t, err)

	return out
}

// keyEntry signs data with key and returns the signature and the log's entry
// of it.
func (f *fakeSigstore) keyEntry(t *testing.T, key *ecdsa.PrivateKey, data []byte) ([]byte, *protorekor.TransparencyLogEntry) {
	t.Helper()

	digest := sha256.Sum256(data)

	sig, err := ecdsa.SignASN1(rand.Reader, key, digest[:])
	must(t, err)

	der, err := x509.MarshalPKIXPublicKey(key.Public())
	must(t, err)

	entry := f.logEntry(t, hashedrekord.KIND, hashedrekord.New().DefaultVersion(), types.ArtifactProperties{
		ArtifactHash:   hex.EncodeToString(digest[:]),
		SignatureBytes: sig,
		PublicKeyBytes: [][]byte{pem.EncodeToMemory(&pem.Block{Type: "PUBLIC KEY", Bytes: der})},
		PKIFormat:      string(pki.X509),
	})

	return sig, entry
}

// cosignKey returns a new cosign key and its public half as a manifest names
// it, and as PEM.
func cosignKey(t *testing.T) (key *ecdsa.PrivateKey, text string, public []byte) {
	t.Helper()

	key, err := ecdsa.GenerateKey(elliptic.P256(), rand.Reader)
	must(t, err)

	der, err := x509.MarshalPKIXPublicKey(key.Public())
	must(t, err)

	return key, base64.StdEncoding.EncodeToString(der), pem.EncodeToMemory(&pem.Block{Type: "PUBLIC KEY", Bytes: der})
}

func certPEM(cert *x509.Certificate) []byte {
	return pem.EncodeToMemory(&pem.Block{Type: "CERTIFICATE", Bytes: cert.Raw})
}

// digestOf is the sha256 of data in hex.
func digestOf(data []byte) string {
	sum := sha256.Sum256(data)

	return hex.EncodeToString(sum[:])
}
