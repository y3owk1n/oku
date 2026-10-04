package cli_test

import (
	"crypto/rand"
	"fmt"
	"os"
	"path/filepath"
	"strings"
	"testing"

	"aead.dev/minisign"
)

func TestB509ASigningKeyInOkuTomlIsCheckedFromTheFirstInstall(t *testing.T) {
	public, secret, err := minisign.GenerateKey(rand.Reader)
	must(t, err)
	other, otherSecret, err := minisign.GenerateKey(rand.Reader)
	must(t, err)

	// The manifest names no key, and the list pins the developer's.
	m := newMachine(t)
	tool := m.signedManifest(t, "", secret, false)
	m.writeFilesList(t, fmt.Sprintf("[packages]\ntool = { ref = %q, signing_key = %q }\n", tool, public.String()))

	out, err := m.run(t, "", "sync")
	if err != nil || strings.Contains(out, "trusted this download") {
		t.Fatalf("want the download checked against the pinned key, got %v\n%s", err, out)
	}

	text, err := os.ReadFile(filepath.Join(m.config, "oku.lock"))
	must(t, err)

	if !strings.Contains(string(text), "verified = 'minisign'") {
		t.Fatalf("want the lock to record the signature check:\n%s", text)
	}

	// A file that another key signed fails on the first install.
	signedByOther := newMachine(t)
	tool = signedByOther.signedManifest(t, "", otherSecret, false)
	signedByOther.writeFilesList(t, fmt.Sprintf("[packages]\ntool = { ref = %q, signing_key = %q }\n", tool, public.String()))

	if _, err := signedByOther.run(t, "", "sync"); err == nil || !strings.Contains(err.Error(), "is not signed by") {
		t.Fatalf("want a refusal for a file another key signed, got %v", err)
	}

	// A manifest that names another key fails.
	named := newMachine(t)
	tool = named.signedManifest(t, other.String(), otherSecret, false)
	named.writeFilesList(t, fmt.Sprintf("[packages]\ntool = { ref = %q, signing_key = %q }\n", tool, public.String()))

	if _, err := named.run(t, "", "sync"); err == nil || !strings.Contains(err.Error(), "oku.toml pins the signing key") {
		t.Fatalf("want a refusal for a manifest with another key, got %v", err)
	}

	// A key that is not a minisign key fails when oku reads the list.
	named.writeFilesList(t, fmt.Sprintf("[packages]\ntool = { ref = %q, signing_key = \"nope\" }\n", tool))

	if _, err := named.run(t, "", "sync"); err == nil || !strings.Contains(err.Error(), "not a minisign public key") {
		t.Fatalf("want a refusal for a bad key, got %v", err)
	}
}

func TestB510ASignerWorkflowInOkuTomlMustBeTheManifests(t *testing.T) {
	for _, tc := range []struct {
		name, pkg, wantErr string
	}{
		{"the same workflow", "signer_workflow = \"" + workflow + "\"\n", ""},
		{"another workflow", "signer_workflow = \"owner/tool/.github/workflows/other.yml\"\n", "oku.toml pins the signer workflow"},
		{"no Sigstore signature", "", "names no Sigstore signature"},
	} {
		t.Run(tc.name, func(t *testing.T) {
			m := newMachine(t)
			f := newFakeSigstore(t, &m)
			r := newSignedRelease(t, &m)

			bundle := ""
			if tc.pkg != "" {
				r.set("tool.tar.gz.sigstore.json", f.signBlob(t, release, r.archive))
				bundle = "sigstore_bundle = \"" + r.release("tool.tar.gz.sigstore.json") + "\"\n"
			}

			tool := r.manifest(t, &m, tc.pkg, bundle)
			m.writeFilesList(t, fmt.Sprintf("[packages]\ntool = { ref = %q, signer_workflow = %q }\n", tool, workflow))

			_, err := m.run(t, "", "sync")

			switch {
			case tc.wantErr == "" && err != nil:
				t.Fatalf("sync: %v", err)
			case tc.wantErr != "" && (err == nil || !strings.Contains(err.Error(), tc.wantErr)):
				t.Fatalf("want a refusal that says %q, got %v", tc.wantErr, err)
			}
		})
	}
}
