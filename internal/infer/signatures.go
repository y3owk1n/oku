package infer

import (
	"context"
	"crypto/sha256"
	"encoding/hex"
	"fmt"
	"slices"
	"strings"

	"github.com/y3owk1n/oku/internal/forge"
	"github.com/y3owk1n/oku/internal/sigstore"
)

// signing is what a GitHub release holds to check its files with Sigstore:
// the workflow that signs them, its attestations and cosign signatures, or the
// developer's cosign key and which files it signed, and SLSA provenance from a
// builder oku trusts.
type signing struct {
	workflow     string
	attestations bool
	cosign       bool
	key          string
	keySums      bool
	keyAsset     bool
	provenance   bool
}

// signaturesOf reads the signatures of asset, the host's file of the release of
// repo, and of sums, its checksum file. oku keeps a signature only when its
// certificate names a workflow run for repo, and provenance only from a
// builder oku trusts. digest is the sha256 GitHub reports for asset.
func (inf *Inferrer) signaturesOf(
	ctx context.Context,
	auth forge.Auth,
	repo string,
	names []string,
	urls map[string]string,
	asset, sums, digest string,
) signing {
	var s signing

	if digest != "" {
		bundles, _ := inf.Hosts.GitHubAttestations(ctx, repo, digest)
		for _, b := range bundles {
			if signer, err := sigstore.SignerOf(b); err == nil && signer.Repo == repo && signer.Workflow != "" {
				s.workflow, s.attestations = signer.Workflow, true

				break
			}
		}
	}

	for _, file := range []string{sums, asset} {
		if file == "" {
			continue
		}

		files := cosignFiles(names, file)
		if len(files) == 0 {
			continue
		}

		// The certificate is in the bundle, or is the file after the signature.
		data, err := inf.Download(ctx, urls[files[len(files)-1]], auth)
		if err != nil {
			continue
		}

		signer, err := sigstore.SignerOf(data)
		if err == nil && signer.Repo == repo && signer.Workflow != "" &&
			(s.workflow == "" || s.workflow == signer.Workflow) {
			s.workflow, s.cosign = signer.Workflow, true

			break
		}
	}

	if s.workflow == "" {
		s.key, s.keySums, s.keyAsset = inf.keySignatures(ctx, auth, names, urls, asset, sums, digest)
	}

	if file := provenanceFile(names, asset); file != "" {
		if data, err := inf.Download(ctx, urls[file], auth); err == nil {
			line, _, _ := strings.Cut(strings.TrimSpace(string(data)), "\n")
			if signer, err := sigstore.SignerOf([]byte(line)); err == nil && signer.Repo == repo && signer.SLSABuilder() {
				s.provenance = true
			}
		}
	}

	return s
}

// keySignatures returns the cosign public key that the release holds, and
// whether it signed sums and asset. The release must hold one key, as a file
// whose name has "cosign" and ends in ".pub". oku checks the signature of sums
// against the file, and that of asset against digest, the sha256 GitHub
// reports for it. The install checks the log.
func (inf *Inferrer) keySignatures(
	ctx context.Context,
	auth forge.Auth,
	names []string,
	urls map[string]string,
	asset, sums, digest string,
) (key string, bySums, byAsset bool) {
	var pubs []string

	for _, n := range names {
		if strings.HasSuffix(n, ".pub") && strings.Contains(strings.ToLower(n), "cosign") {
			pubs = append(pubs, n)
		}
	}

	if len(pubs) != 1 {
		return "", false, false
	}

	data, err := inf.Download(ctx, urls[pubs[0]], auth)
	if err != nil {
		return "", false, false
	}

	if key = cosignKeyText(data); key == "" {
		return "", false, false
	}

	signedBy := func(sig string, sum []byte) bool {
		data, err := inf.Download(ctx, urls[sig], auth)

		return err == nil && sigstore.SignedBy(data, sum, key)
	}

	if sig := keyFile(names, sums); sig != "" {
		if content, err := inf.Download(ctx, urls[sums], auth); err == nil {
			sum := sha256.Sum256(content)
			bySums = signedBy(sig, sum[:])
		}
	}

	if sig := keyFile(names, asset); sig != "" {
		if sum, err := hex.DecodeString(digest); err == nil && len(sum) > 0 {
			byAsset = signedBy(sig, sum)
		}
	}

	if !bySums && !byAsset {
		return "", false, false
	}

	return key, bySums, byAsset
}

// keyFile returns the bundle or the signature beside file, or "".
func keyFile(names []string, file string) string {
	if file == "" {
		return ""
	}

	if files := cosignFiles(names, file); len(files) == 1 {
		return files[0]
	}

	if slices.Contains(names, file+".sig") {
		return file + ".sig"
	}

	return ""
}

// packageTOML writes the keys of s for [package].
func (s signing) packageTOML() string {
	if s.key != "" {
		return fmt.Sprintf("signing_key = %q\n", s.key)
	}

	if s.workflow == "" {
		return ""
	}

	out := fmt.Sprintf("signer_workflow = %q\n", s.workflow)
	if s.attestations {
		out += "attestations = true\n"
	}

	return out
}

// artifactTOML writes the signature keys of the artifact for asset, whose
// checksum file is sums. url turns an asset's name into its URL.
func (s signing) artifactTOML(names []string, asset, sums string, url func(string) string) string {
	var b strings.Builder

	key := func(k, name string) {
		fmt.Fprintf(&b, "%s = %q\n", k, url(name))
	}

	if s.cosign {
		for _, f := range []struct{ file, prefix string }{{sums, "sha256_url"}, {asset, "sigstore"}} {
			if f.file == "" {
				continue
			}

			switch files := cosignFiles(names, f.file); len(files) {
			case 1:
				key(f.prefix+"_bundle", files[0])
			case 2:
				key(f.prefix+"_signature", files[0])
				key(f.prefix+"_certificate", files[1])
			}
		}
	}

	// The host's own files show whether the key signs the checksum file, the
	// download or both.
	for _, f := range []struct {
		file, prefix string
		signed       bool
	}{{sums, "sha256_url", s.keySums}, {asset, "sigstore", s.keyAsset}} {
		switch sig := keyFile(names, f.file); {
		case s.key == "" || !f.signed || sig == "":
		case strings.HasSuffix(sig, ".sig"):
			key(f.prefix+"_signature", sig)
		default:
			key(f.prefix+"_bundle", sig)
		}
	}

	if s.provenance {
		if file := provenanceFile(names, asset); file != "" {
			key("provenance", file)
		}
	}

	return b.String()
}

// cosignFiles returns the bundle beside file, or its signature and
// certificate, or none.
func cosignFiles(names []string, file string) []string {
	for _, ext := range []string{".sigstore.json", ".bundle", ".cosign.bundle"} {
		if slices.Contains(names, file+ext) {
			return []string{file + ext}
		}
	}

	for _, cert := range []string{".pem", ".cert", ".crt"} {
		if slices.Contains(names, file+".sig") && slices.Contains(names, file+cert) {
			return []string{file + ".sig", file + cert}
		}
	}

	return nil
}

// provenanceFile returns the SLSA provenance of asset: one named after it, or
// the release's one provenance file, or "".
func provenanceFile(names []string, asset string) string {
	if slices.Contains(names, asset+".intoto.jsonl") {
		return asset + ".intoto.jsonl"
	}

	var all []string

	for _, n := range names {
		if strings.HasSuffix(n, ".intoto.jsonl") {
			all = append(all, n)
		}
	}

	if len(all) == 1 {
		return all[0]
	}

	return ""
}
