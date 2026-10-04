// Package sigstore checks Sigstore bundles: a signature, the certificate that
// Sigstore's CA issued for it, and the transparency log's record of both.
package sigstore

import (
	"errors"
	"fmt"
	"io"
	"net/http"
	"regexp"
	"sync"
	"time"

	"github.com/sigstore/sigstore-go/pkg/bundle"
	"github.com/sigstore/sigstore-go/pkg/fulcio/certificate"
	"github.com/sigstore/sigstore-go/pkg/root"
	"github.com/sigstore/sigstore-go/pkg/tuf"
	"github.com/sigstore/sigstore-go/pkg/verify"
	"github.com/theupdateframework/go-tuf/v2/metadata"
)

// GitHubIssuer issues the identity tokens of GitHub Actions workflows.
const GitHubIssuer = "https://token.actions.githubusercontent.com"

// ErrVerify reports a bundle that does not prove what oku asked of it.
var ErrVerify = errors.New("sigstore")

// Identity is who must have signed: a GitHub Actions workflow, as
// owner/repo/.github/workflows/<file>, at any ref of that file. A workflow
// that other repos call signs for each of them, so the run must also be for
// Repo, as owner/repo, and, when Ref is set, for that ref, such as
// refs/tags/v1.2.0.
type Identity struct {
	Workflow string
	Repo     string
	Ref      string
}

// certificate returns the identity the certificate must name.
func (id Identity) certificate() (verify.CertificateIdentity, error) {
	san, err := verify.NewSANMatcher("", "^https://github\\.com/"+regexp.QuoteMeta(id.Workflow)+"@.+$")
	if err != nil {
		return verify.CertificateIdentity{}, err
	}

	issuer, err := verify.NewIssuerMatcher(GitHubIssuer, "")
	if err != nil {
		return verify.CertificateIdentity{}, err
	}

	return verify.NewCertificateIdentity(san, issuer, certificate.Extensions{
		SourceRepositoryURI: "https://github.com/" + id.Repo,
		SourceRepositoryRef: id.Ref,
	})
}

// Verifier checks bundles against a trust root, which it loads on first use.
type Verifier struct {
	load    func() (root.TrustedMaterial, error)
	options []verify.VerifierOption

	once     sync.Once
	verifier *verify.Verifier
	err      error
}

// New returns a Verifier that trusts material, with the checks of options.
func New(material root.TrustedMaterial, options ...verify.VerifierOption) *Verifier {
	return &Verifier{
		load:    func() (root.TrustedMaterial, error) { return material, nil },
		options: options,
	}
}

// Public returns a Verifier for Sigstore's public instance. It reads the
// trust root through TUF with client, and keeps it under cacheDir. A bundle
// must carry a certificate timestamp, a log entry and the log's timestamp.
func Public(cacheDir string, client *http.Client) *Verifier {
	return &Verifier{
		load: func() (root.TrustedMaterial, error) {
			opts := tuf.DefaultOptions().
				WithCachePath(cacheDir).
				WithFetcher(fetcher{client}).
				WithCacheValidity(1)

			material, err := root.FetchTrustedRootWithOptions(opts)
			if err != nil {
				return nil, fmt.Errorf("read the Sigstore trust root from %s: %w", tuf.DefaultMirror, err)
			}

			return material, nil
		},
		options: []verify.VerifierOption{
			verify.WithSignedCertificateTimestamps(1),
			verify.WithTransparencyLog(1),
			verify.WithObserverTimestamps(1),
		},
	}
}

// Verify checks that data, a bundle in JSON, signs the file whose sha256 is
// digest, and that id signed it.
func (v *Verifier) Verify(data, digest []byte, id Identity) error {
	if v == nil {
		return fmt.Errorf("%w: this oku has no Sigstore trust root here", ErrVerify)
	}

	v.once.Do(func() {
		material, err := v.load()
		if err != nil {
			v.err = err

			return
		}

		v.verifier, v.err = verify.NewVerifier(material, v.options...)
	})

	if v.err != nil {
		return v.err
	}

	var b bundle.Bundle
	if err := b.UnmarshalJSON(data); err != nil {
		return fmt.Errorf("%w: read the bundle: %w", ErrVerify, err)
	}

	identity, err := id.certificate()
	if err != nil {
		return err
	}

	if _, err := v.verifier.Verify(&b, verify.NewPolicy(
		verify.WithArtifactDigest("sha256", digest),
		verify.WithCertificateIdentity(identity),
	)); err != nil {
		return fmt.Errorf("%w: %w", ErrVerify, err)
	}

	return nil
}

// fetcher reads the files of the TUF repository with oku's client, so the
// trust root goes through the same network rules as every download.
type fetcher struct {
	client *http.Client
}

func (f fetcher) DownloadFile(url string, maxLength int64, _ time.Duration) ([]byte, error) {
	resp, err := f.client.Get(url)
	if err != nil {
		return nil, err
	}
	defer resp.Body.Close()

	// The TUF client stops looking for a newer root at the first file that is
	// not there, which it tells by this error.
	if resp.StatusCode != http.StatusOK {
		return nil, &metadata.ErrDownloadHTTP{StatusCode: resp.StatusCode, URL: url}
	}

	data, err := io.ReadAll(io.LimitReader(resp.Body, maxLength+1))
	if err != nil {
		return nil, err
	}

	if int64(len(data)) > maxLength {
		return nil, &metadata.ErrDownloadLengthMismatch{
			Msg: fmt.Sprintf("download %s: larger than %d bytes", url, maxLength),
		}
	}

	return data, nil
}
