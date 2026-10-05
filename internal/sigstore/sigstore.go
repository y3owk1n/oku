// Package sigstore checks Sigstore bundles: a signature, the certificate that
// Sigstore's CA issued for it or the developer's own key, and the transparency
// log's record of both.
package sigstore

import (
	_ "embed"
	"errors"
	"fmt"
	"io"
	"net/http"
	"regexp"
	"slices"
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

// GitHub runs a Sigstore of its own, which signs the attestations of private
// repos. Its CA's certificates name githubOrg as their issuer. oku reads its
// trust root through TUF from githubTUF, starting from githubRoot, the
// root.json that the gh CLI embeds.
const (
	githubOrg = "GitHub, Inc."
	githubTUF = "https://tuf-repo.github.com"
)

//go:embed github-root.json
var githubRoot []byte

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
	// subject replaces the pattern that Workflow makes for the certificate's
	// subject.
	subject string
}

// slsaBuilders are the workflows of slsa-github-generator whose provenance
// slsa-verifier trusts for a release file, each at a release of the builder.
const slsaBuilders = `^https://github\.com/slsa-framework/slsa-github-generator/\.github/workflows/` +
	`(generator_generic_slsa3|builder_go_slsa3|builder_container-based_slsa3)\.yml@refs/tags/v[0-9]+\.[0-9]+\.[0-9]+$`

// SLSABuilder is the identity of the SLSA provenance of a release: a trusted
// builder of slsa-github-generator, run for repo and ref.
func SLSABuilder(repo, ref string) Identity {
	return Identity{Repo: repo, Ref: ref, subject: slsaBuilders}
}

// certificate returns the identity the certificate must name.
func (id Identity) certificate() (verify.CertificateIdentity, error) {
	subject := id.subject
	if subject == "" {
		subject = "^https://github\\.com/" + regexp.QuoteMeta(id.Workflow) + "@.+$"
	}

	san, err := verify.NewSANMatcher("", subject)
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
	// keyOptions are the checks of a signature by a key, which has no
	// certificate and so no certificate timestamp.
	keyOptions []verify.VerifierOption
	// rekor is the transparency log that client asks for the entry of a
	// signature that comes without a bundle.
	rekor  string
	client *http.Client
	// github checks a bundle whose certificate GitHub's own Sigstore issued.
	github *Verifier

	once     sync.Once
	material root.TrustedMaterial
	verifier *verify.Verifier
	err      error
}

// New returns a Verifier that trusts public for Sigstore's public instance and
// github for GitHub's own, with the checks of Public. It asks the transparency
// log at rekor with client.
func New(public, github root.TrustedMaterial, rekor string, client *http.Client) *Verifier {
	return newVerifier(
		func() (root.TrustedMaterial, error) { return public, nil },
		func() (root.TrustedMaterial, error) { return github, nil },
		rekor, client,
	)
}

// Public returns a Verifier for Sigstore's public instance, and for GitHub's
// own. It reads their trust roots through TUF with client, and keeps them
// under cacheDir.
func Public(cacheDir string, client *http.Client) *Verifier {
	return newVerifier(
		func() (root.TrustedMaterial, error) {
			return trustRoot(tuf.DefaultOptions(), cacheDir, client)
		},
		func() (root.TrustedMaterial, error) {
			return trustRoot(tuf.DefaultOptions().WithRoot(githubRoot).WithRepositoryBaseURL(githubTUF), cacheDir, client)
		},
		PublicRekor, client,
	)
}

// newVerifier returns a Verifier that loads the trust roots of the public
// instance with public and of GitHub's with github. A bundle of the public
// instance must carry a log entry and the log's timestamp, and a certificate
// timestamp when it has a certificate. A bundle of GitHub's must carry a
// timestamp of GitHub's timestamp authority.
func newVerifier(public, github func() (root.TrustedMaterial, error), rekor string, client *http.Client) *Verifier {
	return &Verifier{
		load:   public,
		github: &Verifier{load: github, options: []verify.VerifierOption{verify.WithSignedTimestamps(1)}},
		options: []verify.VerifierOption{
			verify.WithSignedCertificateTimestamps(1),
			verify.WithTransparencyLog(1),
			verify.WithObserverTimestamps(1),
		},
		keyOptions: []verify.VerifierOption{
			verify.WithTransparencyLog(1),
			verify.WithObserverTimestamps(1),
		},
		rekor:  rekor,
		client: client,
	}
}

// Verify checks that data, a bundle in JSON, signs the file whose sha256 is
// digest, and that id signed it. The bundle may also be in cosign's older
// format.
func (v *Verifier) Verify(data, digest []byte, id Identity) error {
	b, err := readBundle(data, digest)
	if err != nil {
		return fmt.Errorf("%w: read the bundle: %w", ErrVerify, err)
	}

	return v.verify(b, digest, id)
}

// trustRoot reads the trust root of the TUF repository that opts name with
// client, and keeps it under cacheDir.
func trustRoot(opts *tuf.Options, cacheDir string, client *http.Client) (root.TrustedMaterial, error) {
	material, err := root.FetchTrustedRootWithOptions(
		opts.WithCachePath(cacheDir).WithFetcher(fetcher{client}).WithCacheValidity(1),
	)
	if err != nil {
		return nil, fmt.Errorf("read the Sigstore trust root from %s: %w", opts.RepositoryBaseURL, err)
	}

	return material, nil
}

// verify checks that b signs the file whose sha256 is digest, and that id
// signed it.
func (v *Verifier) verify(b *bundle.Bundle, digest []byte, id Identity) error {
	if v != nil && v.github != nil && issuedByGitHub(b) {
		return v.github.verify(b, digest, id)
	}

	if err := v.trust(); err != nil {
		return err
	}

	identity, err := id.certificate()
	if err != nil {
		return err
	}

	if _, err := v.verifier.Verify(b, verify.NewPolicy(
		verify.WithArtifactDigest("sha256", digest),
		verify.WithCertificateIdentity(identity),
	)); err != nil {
		return fmt.Errorf("%w: %w", ErrVerify, err)
	}

	return nil
}

// issuedByGitHub reports whether GitHub's own Sigstore issued the certificate
// of b.
func issuedByGitHub(b *bundle.Bundle) bool {
	content, err := b.VerificationContent()
	if err != nil {
		return false
	}

	cert := content.Certificate()

	return cert != nil && slices.Equal(cert.Issuer.Organization, []string{githubOrg})
}

// trust loads the trust root on first use.
func (v *Verifier) trust() error {
	if v == nil {
		return fmt.Errorf("%w: this oku has no Sigstore trust root here", ErrVerify)
	}

	v.once.Do(func() {
		if v.material, v.err = v.load(); v.err != nil {
			return
		}

		v.verifier, v.err = verify.NewVerifier(v.material, v.options...)
	})

	return v.err
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
