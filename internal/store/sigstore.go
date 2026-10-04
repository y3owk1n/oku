package store

import (
	"context"
	"crypto/sha256"
	"encoding/hex"
	"errors"
	"fmt"
	"io"
	"path"
	"strings"

	"github.com/y3owk1n/oku/internal/manifest"
	"github.com/y3owk1n/oku/internal/sigstore"
)

// signer returns who must sign the files of m: its workflow, run for the
// repo of its releases. A bundle beside a release file must come from a run
// for the release's tag, so an older signed file cannot pass for a newer one.
func signer(m *manifest.Manifest) sigstore.Identity {
	id := sigstore.Identity{Workflow: m.Package.SignerWorkflow, Repo: sourceRepo(m)}
	if m.Tag != "" {
		id.Ref = "refs/tags/" + m.Tag
	}

	return id
}

// verifySigstore checks the download whose sha256 is digest against the
// Sigstore signatures that m names for a: the bundle at a.SigstoreBundle, the
// cosign signature at a.SigstoreSignature, and the GitHub attestations of the
// digest.
func (s *Store) verifySigstore(ctx context.Context, m *manifest.Manifest, a manifest.Artifact, digest string) error {
	sum, err := hex.DecodeString(digest)
	if err != nil {
		return err
	}

	if a.SigstoreBundle != "" {
		if err := s.verifySigned(ctx, m, a.SigstoreBundle, "", sum); err != nil {
			return fmt.Errorf("%w: the bundle at %s does not show that %s signed %s: %w",
				ErrSignature, a.SigstoreBundle, signerName(m), path.Base(a.URL), err)
		}
	}

	if a.SigstoreSignature != "" {
		if err := s.verifySigned(ctx, m, a.SigstoreSignature, a.SigstoreCertificate, sum); err != nil {
			return fmt.Errorf("%w: the signature at %s does not show that %s signed %s: %w",
				ErrSignature, a.SigstoreSignature, signerName(m), path.Base(a.URL), err)
		}
	}

	if a.Provenance != "" {
		if err := s.verifyProvenance(ctx, m, a, sum); err != nil {
			return err
		}
	}

	if !m.Package.Attestations {
		return nil
	}

	repo := sourceRepo(m)

	bundles, err := s.Attestations(ctx, repo, digest)
	if err != nil {
		return fmt.Errorf("%w: read the attestations of %s in %s: %w", ErrSignature, path.Base(a.URL), repo, err)
	}

	// A release file may have several attestations. One that the workflow
	// signed is enough. An attestation is made after the build, for whatever
	// ref the run had, so it names no tag.
	var errs []error

	for _, data := range bundles {
		err := s.Sigstore.Verify(data, sum, sigstore.Identity{Workflow: m.Package.SignerWorkflow, Repo: repo})
		if err == nil {
			return nil
		}

		errs = append(errs, err)
	}

	return fmt.Errorf("%w: no attestation in %s shows that %s built %s: %w",
		ErrSignature, repo, m.Package.SignerWorkflow, path.Base(a.URL), errors.Join(errs...))
}

// verifyProvenance checks the download of a, whose sha256 is sum, against
// its SLSA provenance: a trusted builder of slsa-github-generator built it in a
// run for the repo of the releases and the release's tag. The file holds a
// bundle or an envelope on each line, and one that names the download is
// enough.
func (s *Store) verifyProvenance(ctx context.Context, m *manifest.Manifest, a manifest.Artifact, sum []byte) error {
	data, err := s.ReadSmall(ctx, a.Provenance)
	if err != nil {
		return err
	}

	id := sigstore.SLSABuilder(sourceRepo(m), "refs/tags/"+m.Tag)

	var errs []error

	for line := range strings.Lines(string(data)) {
		if strings.TrimSpace(line) == "" {
			continue
		}

		err := s.Sigstore.VerifyStatement(ctx, []byte(line), sum, id)
		if err == nil {
			return nil
		}

		errs = append(errs, err)
	}

	return fmt.Errorf("%w: the provenance at %s does not show that slsa-github-generator built %s for %s at %s: %w",
		ErrSignature, a.Provenance, path.Base(a.URL), sourceRepo(m), m.Tag, errors.Join(errs...))
}

// sourceRepo is the GitHub repo that m's files come from, which holds their
// attestations: the repo of its releases, else the repo of the workflow.
func sourceRepo(m *manifest.Manifest) string {
	if m.Version.From == manifest.FromGitHubReleases && strings.Count(m.Version.Repo, "/") == 1 {
		return m.Version.Repo
	}

	parts := strings.SplitN(m.Package.SignerWorkflow, "/", 3)

	return parts[0] + "/" + parts[1]
}

// signedSHA256 returns the sha256 of the download of a that the checksum file
// at a.SHA256URL lists. With a.SHA256URLBundle the workflow of m must have
// signed that file.
func (s *Store) signedSHA256(ctx context.Context, m *manifest.Manifest, a manifest.Artifact) (string, error) {
	data, err := s.checksums(ctx, a.SHA256URL)
	if err != nil {
		return "", err
	}

	sum := sha256.Sum256(data)

	if a.SHA256URLBundle != "" {
		if err := s.verifySigned(ctx, m, a.SHA256URLBundle, "", sum[:]); err != nil {
			return "", fmt.Errorf("%w: the bundle at %s does not show that %s signed %s: %w",
				ErrSignature, a.SHA256URLBundle, signerName(m), a.SHA256URL, err)
		}
	}

	if a.SHA256URLSignature != "" {
		if err := s.verifySigned(ctx, m, a.SHA256URLSignature, a.SHA256URLCertificate, sum[:]); err != nil {
			return "", fmt.Errorf("%w: the signature at %s does not show that %s signed %s: %w",
				ErrSignature, a.SHA256URLSignature, signerName(m), a.SHA256URL, err)
		}
	}

	return digestIn(data, a.SHA256URL, path.Base(a.URL))
}

// verifySigned checks the bundle or cosign signature at url, of the file whose
// sha256 is sum. A cosign signing key of m must have signed it. Otherwise its
// signer workflow must have, and a signature then comes with the certificate
// at certURL.
func (s *Store) verifySigned(ctx context.Context, m *manifest.Manifest, url, certURL string, sum []byte) error {
	data, err := s.bundle(ctx, url)
	if err != nil {
		return err
	}

	if manifest.CosignKey(m.Package.SigningKey) {
		return s.Sigstore.VerifyWithKey(ctx, data, sum, m.Package.SigningKey)
	}

	if certURL == "" {
		return s.Sigstore.Verify(data, sum, signer(m))
	}

	cert, err := s.bundle(ctx, certURL)
	if err != nil {
		return err
	}

	return s.Sigstore.VerifySignature(ctx, data, cert, sum, signer(m))
}

// signerName names who signs the Sigstore signatures of m.
func signerName(m *manifest.Manifest) string {
	if manifest.CosignKey(m.Package.SigningKey) {
		return "the manifest's cosign signing key"
	}

	return m.Package.SignerWorkflow
}

// bundle downloads the Sigstore bundle, signature or certificate at url.
func (s *Store) bundle(ctx context.Context, url string) ([]byte, error) {
	return s.fetchSmall(ctx, url, 1<<20)
}

// ReadSmall downloads a signature file at url, such as a bundle or a
// provenance file, of at most 16 MiB.
func (s *Store) ReadSmall(ctx context.Context, url string) ([]byte, error) {
	return s.fetchSmall(ctx, url, 16<<20)
}

// fetchSmall downloads the signature file at url, of at most limit bytes.
func (s *Store) fetchSmall(ctx context.Context, url string, limit int64) ([]byte, error) {
	resp, err := s.get(ctx, url)
	if err != nil {
		return nil, fmt.Errorf("%w: the manifest names a Sigstore signature, and %w", ErrSignature, err)
	}
	defer resp.Body.Close()

	data, err := io.ReadAll(io.LimitReader(resp.Body, limit))
	if err != nil {
		return nil, fmt.Errorf("download %s: %w", url, err)
	}

	return data, nil
}
