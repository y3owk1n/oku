package sigstore

import (
	"bytes"
	"context"
	"encoding/base64"
	"encoding/hex"
	"encoding/json"
	"encoding/pem"
	"errors"
	"fmt"
	"io"
	"net/http"
	"strings"

	protobundle "github.com/sigstore/protobuf-specs/gen/pb-go/bundle/v1"
	protocommon "github.com/sigstore/protobuf-specs/gen/pb-go/common/v1"
	protodsse "github.com/sigstore/protobuf-specs/gen/pb-go/dsse"
	protorekor "github.com/sigstore/protobuf-specs/gen/pb-go/rekor/v1"
	"github.com/sigstore/sigstore-go/pkg/bundle"
)

// PublicRekor is the transparency log of Sigstore's public instance.
const PublicRekor = "https://rekor.sigstore.dev"

// legacyBundle is the bundle that cosign wrote before it wrote Sigstore's own
// format: the signature, the certificate, and the log's promise to include them.
type legacyBundle struct {
	Signature   string `json:"base64Signature"`
	Certificate string `json:"cert"`
	Rekor       struct {
		SET     string `json:"SignedEntryTimestamp"`
		Payload struct {
			Body           string `json:"body"`
			IntegratedTime int64  `json:"integratedTime"`
			LogIndex       int64  `json:"logIndex"`
			LogID          string `json:"logID"`
		} `json:"Payload"`
	} `json:"rekorBundle"`
}

// readBundle reads data as a Sigstore bundle, or as cosign's older bundle of
// the file whose sha256 is digest.
func readBundle(data, digest []byte) (*bundle.Bundle, error) {
	var legacy legacyBundle
	if json.Unmarshal(data, &legacy) == nil && legacy.Signature != "" {
		p := legacy.Rekor.Payload

		entry, err := logEntry(p.Body, p.IntegratedTime, p.LogIndex, p.LogID, legacy.Rekor.SET)
		if err != nil {
			return nil, err
		}

		return signatureBundle(legacy.Signature, legacy.Certificate, digest, entry)
	}

	var b bundle.Bundle
	if err := b.UnmarshalJSON(data); err != nil {
		return nil, err
	}

	return &b, nil
}

// VerifySignature checks a signature and its certificate, as cosign writes
// them beside a file, of the file whose sha256 is digest, and that id signed
// it. The signature is in base64, and the certificate is PEM or PEM in base64.
// oku finds the log's entry for them in Rekor.
func (v *Verifier) VerifySignature(ctx context.Context, signature, cert, digest []byte, id Identity) error {
	if v == nil {
		return fmt.Errorf("%w: this oku has no Sigstore trust root here", ErrVerify)
	}

	sig, certPEM := strings.TrimSpace(string(signature)), pemOf(cert)

	entries, err := v.rekorEntries(ctx, digest)
	if err != nil {
		return fmt.Errorf("%w: ask %s for the signature: %w", ErrVerify, v.rekor, err)
	}

	for _, e := range entries {
		var body struct {
			Spec struct {
				Signature struct {
					Content   string `json:"content"`
					PublicKey struct {
						Content string `json:"content"`
					} `json:"publicKey"`
				} `json:"signature"`
			} `json:"spec"`
		}

		raw, err := base64.StdEncoding.DecodeString(e.Body)
		if err != nil || json.Unmarshal(raw, &body) != nil {
			continue
		}

		key, err := base64.StdEncoding.DecodeString(body.Spec.Signature.PublicKey.Content)
		if err != nil || body.Spec.Signature.Content != sig || !bytes.Equal(bytes.TrimSpace(key), bytes.TrimSpace(certPEM)) {
			continue
		}

		entry, err := logEntry(e.Body, e.IntegratedTime, e.LogIndex, e.LogID, e.Verification.SET)
		if err != nil {
			return fmt.Errorf("%w: %w", ErrVerify, err)
		}

		b, err := signatureBundle(sig, base64.StdEncoding.EncodeToString(certPEM), digest, entry)
		if err != nil {
			return fmt.Errorf("%w: %w", ErrVerify, err)
		}

		return v.verify(b, digest, id)
	}

	return fmt.Errorf("%w: %s holds no entry of this signature", ErrVerify, v.rekor)
}

// rekorEntry is an entry of the log as Rekor's API answers it.
type rekorEntry struct {
	Body           string `json:"body"`
	IntegratedTime int64  `json:"integratedTime"`
	LogIndex       int64  `json:"logIndex"`
	LogID          string `json:"logID"`
	Verification   struct {
		SET string `json:"signedEntryTimestamp"`
	} `json:"verification"`
}

// rekorEntries returns the entries of the log for the file whose sha256 is
// digest.
func (v *Verifier) rekorEntries(ctx context.Context, digest []byte) ([]rekorEntry, error) {
	query, err := json.Marshal(map[string]string{"hash": "sha256:" + hex.EncodeToString(digest)})
	if err != nil {
		return nil, err
	}

	var ids []string
	if err := v.rekorJSON(ctx, http.MethodPost, "/api/v1/index/retrieve", query, &ids); err != nil {
		return nil, err
	}

	var entries []rekorEntry

	for _, id := range ids {
		var found map[string]rekorEntry
		if err := v.rekorJSON(ctx, http.MethodGet, "/api/v1/log/entries/"+id, nil, &found); err != nil {
			return nil, err
		}

		for _, e := range found {
			entries = append(entries, e)
		}
	}

	return entries, nil
}

func (v *Verifier) rekorJSON(ctx context.Context, method, path string, body []byte, into any) error {
	req, err := http.NewRequestWithContext(ctx, method, v.rekor+path, bytes.NewReader(body))
	if err != nil {
		return err
	}

	req.Header.Set("Content-Type", "application/json")
	req.Header.Set("User-Agent", "oku")

	resp, err := v.client.Do(req)
	if err != nil {
		return err
	}
	defer resp.Body.Close()

	data, err := io.ReadAll(io.LimitReader(resp.Body, 4<<20))
	if err != nil {
		return err
	}

	if resp.StatusCode != http.StatusOK {
		return fmt.Errorf("%s %s: %s", method, path, resp.Status)
	}

	return json.Unmarshal(data, into)
}

// logEntry returns the log entry whose body is body, in base64, with the log's
// promise set, also in base64, to include it.
func logEntry(body string, integratedTime, logIndex int64, logID, set string) (*protorekor.TransparencyLogEntry, error) {
	raw, err := base64.StdEncoding.DecodeString(body)
	if err != nil {
		return nil, fmt.Errorf("read the log entry: %w", err)
	}

	var kind struct {
		APIVersion string `json:"apiVersion"`
		Kind       string `json:"kind"`
	}
	if err := json.Unmarshal(raw, &kind); err != nil {
		return nil, fmt.Errorf("read the log entry: %w", err)
	}

	id, err := hex.DecodeString(logID)
	if err != nil {
		return nil, fmt.Errorf("read the log entry's log: %w", err)
	}

	promise, err := base64.StdEncoding.DecodeString(set)
	if err != nil {
		return nil, fmt.Errorf("read the log's promise: %w", err)
	}

	return &protorekor.TransparencyLogEntry{
		LogIndex:          logIndex,
		LogId:             &protocommon.LogId{KeyId: id},
		KindVersion:       &protorekor.KindVersion{Kind: kind.Kind, Version: kind.APIVersion},
		IntegratedTime:    integratedTime,
		InclusionPromise:  &protorekor.InclusionPromise{SignedEntryTimestamp: promise},
		CanonicalizedBody: raw,
	}, nil
}

// signatureBundle puts a signature, in base64, and its certificate, PEM in
// base64, of the file whose sha256 is digest into a bundle with entry.
func signatureBundle(signature, cert string, digest []byte, entry *protorekor.TransparencyLogEntry) (*bundle.Bundle, error) {
	sig, err := base64.StdEncoding.DecodeString(signature)
	if err != nil {
		return nil, fmt.Errorf("read the signature: %w", err)
	}

	certPEM, err := base64.StdEncoding.DecodeString(cert)
	if err != nil {
		return nil, fmt.Errorf("read the certificate: %w", err)
	}

	return certificateBundle(certPEM, entry, &protobundle.Bundle_MessageSignature{
		MessageSignature: &protocommon.MessageSignature{
			MessageDigest: &protocommon.HashOutput{Algorithm: protocommon.HashAlgorithm_SHA2_256, Digest: digest},
			Signature:     sig,
		},
	})
}

// certificateBundle puts content, a message signature or an envelope, its
// certificate in PEM and its log entry into a bundle. Version 0.1 of the
// bundle takes the log's promise in place of a proof.
func certificateBundle(certPEM []byte, entry *protorekor.TransparencyLogEntry, content any) (*bundle.Bundle, error) {
	block, _ := pem.Decode(certPEM)
	if block == nil {
		return nil, errors.New("read the certificate: it is not PEM")
	}

	pb := &protobundle.Bundle{
		MediaType: "application/vnd.dev.sigstore.bundle+json;version=0.1",
		VerificationMaterial: &protobundle.VerificationMaterial{
			Content: &protobundle.VerificationMaterial_X509CertificateChain{
				X509CertificateChain: &protocommon.X509CertificateChain{
					Certificates: []*protocommon.X509Certificate{{RawBytes: block.Bytes}},
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

	return bundle.NewBundle(pb)
}

// pemOf returns cert as PEM. cosign writes a certificate as PEM, or as PEM in
// base64.
func pemOf(cert []byte) []byte {
	cert = bytes.TrimSpace(cert)
	if bytes.HasPrefix(cert, []byte("-----BEGIN")) {
		return cert
	}

	decoded, err := base64.StdEncoding.DecodeString(string(cert))
	if err != nil {
		return cert
	}

	return decoded
}

// VerifyStatement checks a line of a provenance file, a statement that names
// the file whose sha256 is digest, and that id signed it. The line is a
// Sigstore bundle, or a signed envelope as slsa-github-generator wrote before
// it wrote bundles, whose log entry oku finds in Rekor.
func (v *Verifier) VerifyStatement(ctx context.Context, line, digest []byte, id Identity) error {
	var envelope struct {
		PayloadType string `json:"payloadType"`
		Payload     string `json:"payload"`
		Signatures  []struct {
			KeyID string `json:"keyid"`
			Sig   string `json:"sig"`
			Cert  string `json:"cert"`
		} `json:"signatures"`
	}

	if json.Unmarshal(line, &envelope) != nil || envelope.PayloadType == "" {
		return v.Verify(line, digest, id)
	}

	if v == nil {
		return fmt.Errorf("%w: this oku has no Sigstore trust root here", ErrVerify)
	}

	if len(envelope.Signatures) != 1 {
		return fmt.Errorf("%w: the envelope has %d signatures, not one", ErrVerify, len(envelope.Signatures))
	}

	signed := envelope.Signatures[0]
	certPEM := pemOf([]byte(signed.Cert))

	entries, err := v.rekorEntries(ctx, digest)
	if err != nil {
		return fmt.Errorf("%w: ask %s for the envelope: %w", ErrVerify, v.rekor, err)
	}

	for _, e := range entries {
		var body struct {
			Spec struct {
				Signatures []struct {
					Signature string `json:"signature"`
					Verifier  string `json:"verifier"`
				} `json:"signatures"`
			} `json:"spec"`
		}

		raw, err := base64.StdEncoding.DecodeString(e.Body)
		if err != nil || json.Unmarshal(raw, &body) != nil || len(body.Spec.Signatures) != 1 {
			continue
		}

		key, err := base64.StdEncoding.DecodeString(body.Spec.Signatures[0].Verifier)
		if err != nil || body.Spec.Signatures[0].Signature != signed.Sig ||
			!bytes.Equal(bytes.TrimSpace(key), bytes.TrimSpace(certPEM)) {
			continue
		}

		entry, err := logEntry(e.Body, e.IntegratedTime, e.LogIndex, e.LogID, e.Verification.SET)
		if err != nil {
			return fmt.Errorf("%w: %w", ErrVerify, err)
		}

		payload, err := base64.StdEncoding.DecodeString(envelope.Payload)
		if err != nil {
			return fmt.Errorf("%w: read the envelope: %w", ErrVerify, err)
		}

		sig, err := base64.StdEncoding.DecodeString(signed.Sig)
		if err != nil {
			return fmt.Errorf("%w: read the envelope: %w", ErrVerify, err)
		}

		b, err := certificateBundle(certPEM, entry, &protobundle.Bundle_DsseEnvelope{DsseEnvelope: &protodsse.Envelope{
			Payload: payload, PayloadType: envelope.PayloadType,
			Signatures: []*protodsse.Signature{{Sig: sig, Keyid: signed.KeyID}},
		}})
		if err != nil {
			return fmt.Errorf("%w: %w", ErrVerify, err)
		}

		return v.verify(b, digest, id)
	}

	return fmt.Errorf("%w: %s holds no entry of this envelope", ErrVerify, v.rekor)
}
