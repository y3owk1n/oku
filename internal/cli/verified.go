package cli

import (
	"fmt"
	"strings"

	"github.com/y3owk1n/oku/internal/lock"
	"github.com/y3owk1n/oku/internal/manifest"
	"github.com/y3owk1n/oku/internal/platform"
	"github.com/y3owk1n/oku/internal/ref"
	"github.com/y3owk1n/oku/internal/resolve"
)

// verifiedBy returns the strongest check that states a digest for a download:
// a signature, which is a Verified constant or "", a digest in the manifest, a
// checksum file, or a digest the version source publishes. It returns "" when
// none does.
func verifiedBy(signature, stated, sha256URL, published string) string {
	switch {
	case signature != "":
		return signature
	case stated != "":
		return lock.VerifiedManifest
	case sha256URL != "":
		return lock.VerifiedChecksumFile
	case published != "":
		return lock.VerifiedPublished
	}

	return ""
}

// pinVerified returns the check that oku.lock records for a download. A digest
// that oku.lock pinned keeps the check it was pinned with, so a sync writes
// the same lock. Otherwise it is stated, or a first use when nothing states a
// digest.
func pinVerified(stated string, pinned bool, at lock.Platform) string {
	switch {
	case pinned:
		return at.Verified
	case stated != "":
		return stated
	}

	return lock.VerifiedFirstUse
}

// artifactVerified returns what states a digest for artifact a of m, before
// the install fills in the digest that release publishes.
func artifactVerified(m *manifest.Manifest, a manifest.Artifact, release resolve.Release) string {
	published := release.Digests[a.URL]
	if published == "" {
		published = release.Integrity[a.URL]
	}

	stated := a.SHA256
	if stated == "" {
		stated = a.Integrity
	}

	signature := ""

	switch {
	case m.Package.Attestations || a.Sigstore() || a.Provenance != "":
		signature = lock.VerifiedSigstore
	case m.Package.SigningKey != "":
		signature = lock.VerifiedMinisign
	}

	return verifiedBy(signature, stated, a.SHA256URL, published)
}

// sourceVerified returns the check that oku.lock records for the source of the
// build of m on p, as pinVerified does. A build that downloads no archive has
// none, and a signing key covers artifacts only.
func sourceVerified(
	m *manifest.Manifest,
	release resolve.Release,
	p platform.Platform,
	pinned bool,
	at lock.Platform,
) string {
	src := m.Build.Source
	if src.URL == "" {
		return ""
	}

	// The same variables the build expands in its source.
	url, _ := manifest.Expand(src.URL, map[string]string{
		"version": m.Version.Value, "tag": m.Tag, "os": p.OS, "arch": p.Arch, "libc": p.Libc,
	})

	return pinVerified(verifiedBy("", src.SHA256, src.SHA256URL, release.Digests[url]), pinned, at)
}

// verifiedText says in words what oku checked a download against.
func verifiedText(verified string) string {
	switch verified {
	case lock.VerifiedSigstore:
		return "a Sigstore signature by the manifest's signer workflow"
	case lock.VerifiedMinisign:
		return "a minisign signature by the manifest's signing key"
	case lock.VerifiedManifest:
		return "the digest in the manifest"
	case lock.VerifiedChecksumFile:
		return "the checksum file at the manifest's sha256_url"
	case lock.VerifiedPublished:
		return "the digest that its source publishes"
	case lock.VerifiedFirstUse:
		return "nothing, oku trusted the first download"
	}

	return verified
}

// checkVerified returns an error when oku would check the download for p
// more weakly than it checked the one oku.lock pinned, unless req accepts that.
// oku does not compare a package whose ref changed, since the user chose the
// new source.
func (req request) checkVerified(p platform.Platform, now string) error {
	was := req.previous.Platforms[p.String()].Verified
	if req.acceptWeaker || req.previous.Ref != req.ref.String() || !lock.Weaker(now, was) {
		return nil
	}

	how := "oku can check the new one only against " + verifiedText(now)
	if now == lock.VerifiedFirstUse {
		how = "nothing states a digest for the new one, so oku would trust its first download"
	}

	return fmt.Errorf(
		"oku.lock checked the download for %s against %s, and %s\n"+
			"if the developer announced this change, run the command again with --accept-weaker-check",
		p, verifiedText(was), how,
	)
}

// trust runs req.checkTrust for the first download of version of the package
// called name for p.
func (req request) trust(name, version string, p platform.Platform) error {
	if req.checkTrust == nil {
		return nil
	}

	return req.checkTrust(name, version, p)
}

// pinSigner applies to m the signer that the list pins for the package. A
// manifest that names another signer fails, and so does one that names no
// Sigstore signature for a pinned workflow, since oku has nothing to check.
func (req request) pinSigner(m *manifest.Manifest) error {
	if key := req.signingKey; key != "" {
		if m.Package.SigningKey != "" && m.Package.SigningKey != key {
			return fmt.Errorf("oku.toml pins the signing key %s, and the manifest names %s", key, m.Package.SigningKey)
		}

		m.Package.SigningKey = key
	}

	switch w := req.signerWorkflow; {
	case w == "":
	case m.Package.SignerWorkflow == "":
		return fmt.Errorf("oku.toml pins the signer workflow %s, and the manifest names no Sigstore signature", w)
	case m.Package.SignerWorkflow != w:
		return fmt.Errorf("oku.toml pins the signer workflow %s, and the manifest names %s", w, m.Package.SignerWorkflow)
	}

	return nil
}

// osvEcosystems are OSV's names for the registries of the refs it knows.
var osvEcosystems = map[ref.Kind]string{
	ref.NPM: "npm", ref.PyPI: "PyPI", ref.Cargo: "crates.io", ref.Go: "Go",
}

// maliciousError refuses version of r, which OSV lists as malicious in ids. A
// package that oku.lock holds keeps its locked version, as for a declined build.
func maliciousError(r ref.Ref, version string, ids []string) notApprovedError {
	why := fmt.Sprintf("OSV lists it as malicious, see https://osv.dev/vulnerability/%s", ids[0])

	return notApprovedError{
		version: version,
		text: fmt.Sprintf("%s %s: OSV lists it as malicious in %s\nadd another version with @<version>",
			r, version, strings.Join(ids, ", ")),
		why:  why,
		kept: fmt.Sprintf("%s was not taken, since %s", version, why),
	}
}
