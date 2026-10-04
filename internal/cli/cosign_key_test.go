package cli_test

import (
	"crypto/ecdsa"
	"os"
	"path/filepath"
	"strings"
	"testing"
)

func TestB519ACosignSigningKeyMustHaveSignedTheSignatureInTheLog(t *testing.T) {
	developer, developerText, _ := cosignKey(t)
	other, _, _ := cosignKey(t)

	for _, tc := range []struct {
		name    string
		signer  *ecdsa.PrivateKey
		logged  bool
		wantErr string
	}{
		{"the developer's key", developer, true, ""},
		{"another key", other, true, "holds no entry"},
		{"a signature Rekor does not hold", developer, false, "holds no entry"},
	} {
		t.Run(tc.name, func(t *testing.T) {
			m := newMachine(t)
			f := newFakeSigstore(t, &m)
			r := newSignedRelease(t, &m)
			r.set("checksums.txt.sig", f.keySign(t, tc.signer, r.files["checksums.txt"], tc.logged))

			tool := r.manifest(t, &m, "signing_key = \""+developerText+"\"\n",
				"sha256_url = \""+r.release("checksums.txt")+"\"\n"+
					"sha256_url_signature = \""+r.release("checksums.txt.sig")+"\"\n")

			out, err := m.run(t, "", "add", tool)

			switch {
			case tc.wantErr == "" && err != nil:
				t.Fatalf("add: %v\n%s", err, out)
			case tc.wantErr != "" && (err == nil || !strings.Contains(err.Error(), tc.wantErr)):
				t.Fatalf("want a refusal that says %q, got %v", tc.wantErr, err)
			}

			if tc.wantErr == "" {
				lock, err := os.ReadFile(filepath.Join(m.config, "oku.lock"))
				must(t, err)

				if !strings.Contains(string(lock), "verified = 'cosign'") {
					t.Fatalf("oku.lock does not record the cosign check:\n%s", lock)
				}
			}
		})
	}

	// A bundle of the download itself works the same way.
	for _, tc := range []struct {
		name    string
		signer  *ecdsa.PrivateKey
		wantErr bool
	}{
		{"the developer's key", developer, false},
		{"another key", other, true},
	} {
		t.Run("bundle by "+tc.name, func(t *testing.T) {
			m := newMachine(t)
			f := newFakeSigstore(t, &m)
			r := newSignedRelease(t, &m)
			r.set("tool.tar.gz.sigstore.json", f.keyBundle(t, tc.signer, r.archive))

			tool := r.manifest(t, &m, "signing_key = \""+developerText+"\"\n",
				"sigstore_bundle = \""+r.release("tool.tar.gz.sigstore.json")+"\"\n")

			out, err := m.run(t, "", "add", tool)
			if (err != nil) != tc.wantErr {
				t.Fatalf("want a refusal %v, got %v\n%s", tc.wantErr, err, out)
			}
		})
	}
}

func TestB520AnAquaEntryWithAKeyBecomesTheSigningKey(t *testing.T) {
	_, text, public := cosignKey(t)

	entry := func(extra string) string {
		return `packages:
  - type: github_release
    repo_owner: owner
    repo_name: tool
    asset: tool_{{.OS}}_{{.Arch}}.tar.gz
    supported_envs: [linux/amd64]
    checksum:
      type: github_release
      asset: checksums.txt
      algorithm: sha256
      cosign:
        opts:
          - --key
          - SERVER/keys/{{.Version}}/cosign.pub
          - --signature
          - https://github.com/owner/tool/releases/download/{{.Version}}/checksums.txt.sig
` + extra
	}

	release := "https://github.com/owner/tool/releases/download/{{tag}}/"

	for _, tc := range []struct {
		name, entry string
		served      map[string]string
		want        bool
	}{
		{"a key oku can read", entry(""), map[string]string{"/keys/v1.0.0/cosign.pub": string(public)}, true},
		{"no key at its URL", entry(""), nil, false},
		{"a check that skips the log", entry("          - --insecure-ignore-tlog\n"),
			map[string]string{"/keys/v1.0.0/cosign.pub": string(public)}, false},
	} {
		m := newMachine(t)
		recipeServer{aqua: tc.entry, versions: []string{"1.0.0"}, served: tc.served}.start(t, &m)

		out, err := m.run(t, "", "manifest", "init", "--from", "aqua:owner/tool", "-o", "-")
		if err != nil {
			t.Fatalf("%s: init: %v\n%s", tc.name, err, out)
		}

		got := strings.Contains(out, "signing_key = \""+text+"\"") &&
			strings.Contains(out, "sha256_url_signature = \""+release+"checksums.txt.sig\"")
		if got != tc.want || strings.Contains(out, "certificate") {
			t.Fatalf("%s: want the key and its signature kept %v:\n%s", tc.name, tc.want, out)
		}
	}
}

func TestB522InferenceKeepsASignatureByTheReleasesCosignKey(t *testing.T) {
	developer, text, public := cosignKey(t)
	other, _, otherPublic := cosignKey(t)

	for _, tc := range []struct {
		name      string
		files     func(f *fakeSigstore, archive, sums []byte) map[string][]byte
		want, not []string
	}{
		{
			name: "a bundle of the checksum file",
			files: func(f *fakeSigstore, _, sums []byte) map[string][]byte {
				return map[string][]byte{"cosign.pub": public, "checksums.txt.sigstore.json": f.keyBundle(t, developer, sums)}
			},
			want: []string{"signing_key = \"" + text + "\"", "sha256_url_bundle = "},
		},
		{
			name: "a signature of the download",
			files: func(f *fakeSigstore, archive, _ []byte) map[string][]byte {
				return map[string][]byte{"tool_cosign.pub": public, hostAssetName() + ".sig": f.keySign(t, developer, archive, true)}
			},
			want: []string{"signing_key = \"" + text + "\"", "sigstore_signature = "},
			not:  []string{"certificate"},
		},
		{
			name: "a signature by another key",
			files: func(f *fakeSigstore, _, sums []byte) map[string][]byte {
				return map[string][]byte{"cosign.pub": public, "checksums.txt.sig": f.keySign(t, other, sums, true)}
			},
			not: []string{"signing_key", "sha256_url_signature"},
		},
		{
			name: "two keys",
			files: func(f *fakeSigstore, _, sums []byte) map[string][]byte {
				return map[string][]byte{
					"cosign.pub": public, "cosign-old.pub": otherPublic,
					"checksums.txt.sig": f.keySign(t, developer, sums, true),
				}
			},
			not: []string{"signing_key", "sha256_url_signature"},
		},
	} {
		t.Run(tc.name, func(t *testing.T) {
			m := newMachine(t)
			f := newFakeSigstore(t, &m)
			inferRelease(t, &m, func(archive, sums []byte) map[string][]byte { return tc.files(f, archive, sums) }, nil)

			out, err := m.run(t, "", "add", "github:owner/tool", "--verbose")
			if err != nil {
				t.Fatalf("add: %v\n%s", err, out)
			}

			for _, want := range tc.want {
				if !strings.Contains(out, want) {
					t.Fatalf("the inferred manifest lacks %q:\n%s", want, out)
				}
			}

			for _, not := range tc.not {
				if strings.Contains(out, not) {
					t.Fatalf("the inferred manifest has %q:\n%s", not, out)
				}
			}

			if len(tc.want) > 0 {
				lock, err := os.ReadFile(filepath.Join(m.config, "oku.lock"))
				must(t, err)

				if !strings.Contains(string(lock), "verified = 'cosign'") {
					t.Fatalf("oku.lock does not record the cosign check:\n%s", lock)
				}
			}
		})
	}
}
