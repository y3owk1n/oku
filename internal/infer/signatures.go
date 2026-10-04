package infer

import (
	"context"
	"fmt"
	"slices"
	"strings"

	"github.com/y3owk1n/oku/internal/forge"
	"github.com/y3owk1n/oku/internal/sigstore"
)

// signing is what a GitHub release holds to check its files with Sigstore:
// the workflow that signs them, its attestations and cosign signatures, and
// SLSA provenance from a builder oku trusts.
type signing struct {
	workflow     string
	attestations bool
	cosign       bool
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

// packageTOML writes the keys of s for [package].
func (s signing) packageTOML() string {
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
