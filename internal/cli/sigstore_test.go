package cli_test

import (
	"fmt"
	"net/http"
	"net/http/httptest"
	"os"
	"path/filepath"
	"strings"
	"sync"
	"testing"
)

const workflow = "owner/tool/.github/workflows/release.yml"

// release is the run of workflow for the tag v1.0.0 of owner/tool.
var release = run{workflow + "@refs/tags/v1.0.0", "owner/tool", "refs/tags/v1.0.0"}

// signedRelease fakes GitHub for owner/tool with release v1.0.0, whose file
// tool.tar.gz sits beside checksums.txt. files holds the files of the release,
// such as bundles, and attestations the bundles that GitHub lists for
// tool.tar.gz, whose sha256 is sum.
type signedRelease struct {
	mu           sync.Mutex
	files        map[string][]byte
	attestations [][]byte
	sum          string
	archive      []byte
	url          string
}

func newSignedRelease(t *testing.T, m *machine) *signedRelease {
	t.Helper()

	path, sum := m.archive(t, "tool", map[string]string{"tool": "#!/bin/sh\necho tool\n"})

	archive, err := os.ReadFile(path)
	must(t, err)

	r := &signedRelease{sum: sum, archive: archive, files: map[string][]byte{
		"tool.tar.gz":   archive,
		"checksums.txt": []byte(sum + "  tool.tar.gz\n"),
	}}

	server := httptest.NewServer(http.HandlerFunc(func(w http.ResponseWriter, req *http.Request) {
		r.mu.Lock()
		defer r.mu.Unlock()

		switch {
		case req.URL.Path == "/api/repos/owner/tool/releases":
			fmt.Fprintf(w, `[{"tag_name": "v1.0.0", "assets": []}]`)
		case req.URL.Path == "/api/repos/owner/tool/attestations/sha256:"+r.sum:
			var listed []string
			for _, b := range r.attestations {
				listed = append(listed, `{"bundle": `+string(b)+`}`)
			}

			fmt.Fprintf(w, `{"attestations": [%s]}`, strings.Join(listed, ","))
		case strings.HasPrefix(req.URL.Path, "/owner/tool/releases/download/v1.0.0/"):
			data, ok := r.files[strings.TrimPrefix(req.URL.Path, "/owner/tool/releases/download/v1.0.0/")]
			if !ok {
				http.NotFound(w, req)

				return
			}

			_, _ = w.Write(data)
		default:
			http.NotFound(w, req)
		}
	}))
	t.Cleanup(server.Close)

	m.opts.GitHubAPI = server.URL + "/api"
	r.url = server.URL

	return r
}

func (r *signedRelease) set(name string, data []byte) {
	r.mu.Lock()
	defer r.mu.Unlock()

	r.files[name] = data
}

// manifest writes a manifest for the release with the keys in pkg added to
// [package] and those in artifact to its artifact.
func (r *signedRelease) manifest(t *testing.T, m *machine, pkg, artifact string) string {
	t.Helper()

	path := filepath.Join(m.fixtures, "tool.toml")
	must(t, os.WriteFile(path, []byte(fmt.Sprintf(
		"[package]\nname = \"tool\"\n%s"+
			"[version]\nfrom = \"github-releases\"\nrepo = \"owner/tool\"\n"+
			"[[artifact]]\nurl = \"%s/owner/tool/releases/download/{{tag}}/tool.tar.gz\"\nbin = [\"tool\"]\n%s",
		pkg, r.url, artifact,
	)), 0o644))

	return path
}

// release is the address of a file of the release.
func (r *signedRelease) release(name string) string {
	return r.url + "/owner/tool/releases/download/{{tag}}/" + name
}

func TestB501ASigstoreBundleMustShowTheWorkflowSignedTheDownloadForItsRepoAndTag(t *testing.T) {
	for _, tc := range []struct {
		name    string
		run     run
		signed  string
		wantErr bool
	}{
		{"the workflow for the tag", release, "", false},
		// A reusable workflow runs at its own ref, for the repo and tag that call it.
		{"the workflow at another ref", run{workflow + "@refs/heads/main", "owner/tool", "refs/tags/v1.0.0"}, "", false},
		{"another workflow", run{"owner/tool/.github/workflows/other.yml@refs/tags/v1.0.0", "owner/tool", "refs/tags/v1.0.0"}, "", true},
		{"another repo", run{workflow + "@refs/tags/v1.0.0", "owner/other", "refs/tags/v1.0.0"}, "", true},
		{"another tag", run{workflow + "@refs/tags/v0.9.0", "owner/tool", "refs/tags/v0.9.0"}, "", true},
		{"other bytes", release, "other bytes", true},
	} {
		t.Run(tc.name, func(t *testing.T) {
			m := newMachine(t)
			f := newFakeSigstore(t, &m)
			r := newSignedRelease(t, &m)

			signed := r.archive
			if tc.signed != "" {
				signed = []byte(tc.signed)
			}

			r.set("tool.tar.gz.sigstore.json", f.signBlob(t, tc.run, signed))

			tool := r.manifest(t, &m, "signer_workflow = \""+workflow+"\"\n",
				"sigstore_bundle = \""+r.release("tool.tar.gz.sigstore.json")+"\"\n")

			out, err := m.run(t, "", "add", tool)

			switch {
			case tc.wantErr && (err == nil || !strings.Contains(err.Error(), "does not show that")):
				t.Fatalf("want a refusal, got %v\n%s", err, out)
			case !tc.wantErr && err != nil:
				t.Fatalf("add: %v\n%s", err, out)
			case tc.wantErr && len(m.storeEntries(t)) != 0:
				t.Fatal("a download that the bundle does not sign reached the store")
			}
		})
	}

	// A bundle that signs the checksum file vouches for the digest in it.
	m := newMachine(t)
	f := newFakeSigstore(t, &m)
	r := newSignedRelease(t, &m)
	r.set("checksums.txt.sigstore.json", f.signBlob(t, release, r.files["checksums.txt"]))

	tool := r.manifest(t, &m, "signer_workflow = \""+workflow+"\"\n",
		"sha256_url = \""+r.release("checksums.txt")+"\"\n"+
			"sha256_url_bundle = \""+r.release("checksums.txt.sigstore.json")+"\"\n")

	_, err := m.run(t, "", "add", tool)
	must(t, err)

	text, err := os.ReadFile(filepath.Join(m.config, "oku.lock"))
	must(t, err)

	if !strings.Contains(string(text), "verified = 'sigstore'") {
		t.Fatalf("want the lock to record the Sigstore check:\n%s", text)
	}

	r.set("checksums.txt", []byte(strings.Repeat("0", 64)+"  tool.tar.gz\n"))

	tampered := newMachine(t)
	tampered.opts.Sigstore = m.opts.Sigstore
	tampered.opts.GitHubAPI = m.opts.GitHubAPI

	if _, err := tampered.run(t, "", "add", tool); err == nil || !strings.Contains(err.Error(), "does not show that") {
		t.Fatalf("want a changed checksum file refused, got %v", err)
	}
}

func TestB502AttestationsMustIncludeOneTheWorkflowSignedForTheRepo(t *testing.T) {
	m := newMachine(t)
	f := newFakeSigstore(t, &m)
	r := newSignedRelease(t, &m)
	tool := r.manifest(t, &m, "signer_workflow = \""+workflow+"\"\nattestations = true\n", "")

	if _, err := m.run(t, "", "add", tool); err == nil || !strings.Contains(err.Error(), "has none") {
		t.Fatalf("want a refusal without attestations, got %v", err)
	}

	r.mu.Lock()
	r.attestations = [][]byte{
		f.attest(t, run{"other/repo/.github/workflows/x.yml@refs/heads/main", "owner/tool", "refs/heads/main"}, r.sum),
		f.attest(t, run{workflow + "@refs/heads/main", "owner/other", "refs/heads/main"}, r.sum),
	}
	r.mu.Unlock()

	if _, err := m.run(t, "", "add", tool); err == nil || !strings.Contains(err.Error(), "no attestation") {
		t.Fatalf("want a refusal of attestations by another workflow or for another repo, got %v", err)
	}

	// An attestation names no tag, since it comes from whatever run built the file.
	r.mu.Lock()
	r.attestations = append(r.attestations, f.attest(t, run{workflow + "@refs/heads/main", "owner/tool", "refs/heads/main"}, r.sum))
	r.mu.Unlock()

	out, err := m.run(t, "", "add", tool)
	if err != nil {
		t.Fatalf("add: %v\n%s", err, out)
	}

	if strings.Contains(out, "trusted this download") {
		t.Fatalf("an attested download is not a first use:\n%s", out)
	}
}

func TestB503TheLockPinsTheSignerWorkflow(t *testing.T) {
	m := newMachine(t)
	f := newFakeSigstore(t, &m)
	r := newSignedRelease(t, &m)

	sign := func(workflow string) {
		r.set("tool.tar.gz.sigstore.json", f.signBlob(t, run{workflow + "@refs/tags/v1.0.0", "owner/tool", "refs/tags/v1.0.0"}, r.archive))
	}

	sign(workflow)

	bundled := "sigstore_bundle = \"" + r.release("tool.tar.gz.sigstore.json") + "\"\n"
	tool := r.manifest(t, &m, "signer_workflow = \""+workflow+"\"\n", bundled)

	_, err := m.run(t, "", "add", tool)
	must(t, err)

	text, err := os.ReadFile(filepath.Join(m.config, "oku.lock"))
	must(t, err)

	if !strings.Contains(string(text), "signer_workflow = '"+workflow+"'") {
		t.Fatalf("want the lock to pin the signer workflow:\n%s", text)
	}

	other := "owner/tool/.github/workflows/other.yml"
	sign(other)
	r.manifest(t, &m, "signer_workflow = \""+other+"\"\n", bundled)

	// A local manifest that changed stops sync before the workflow does.
	if _, err := m.run(t, "", "sync"); err == nil {
		t.Fatal("sync took a changed signer workflow")
	}

	if _, err := m.run(t, "", "update"); err == nil || !strings.Contains(err.Error(), "--accept-key") {
		t.Fatalf("update took a changed signer workflow: %v", err)
	}

	_, err = m.run(t, "", "update", "--accept-key")
	must(t, err)

	text, err = os.ReadFile(filepath.Join(m.config, "oku.lock"))
	must(t, err)

	if !strings.Contains(string(text), "signer_workflow = '"+other+"'") {
		t.Fatalf("want the lock to pin the accepted workflow:\n%s", text)
	}
}

func TestB504AnAquaEntryKeepsItsSigstoreChecks(t *testing.T) {
	m := newMachine(t)
	recipeServer{aqua: `packages:
  - type: github_release
    repo_owner: owner
    repo_name: tool
    asset: tool_{{.OS}}_{{.Arch}}.tar.gz
    supported_envs: [linux/amd64]
    github_artifact_attestations:
      signer_workflow: owner/tool/.github/workflows/release.yml
    checksum:
      type: github_release
      asset: checksums.txt
      algorithm: sha256
      cosign:
        bundle:
          type: github_release
          asset: checksums.txt.sigstore.json
        opts:
          - --certificate-identity
          - https://github.com/owner/tool/.github/workflows/release.yml@refs/tags/{{.Version}}
          - --certificate-oidc-issuer
          - https://token.actions.githubusercontent.com
    cosign:
      bundle:
        type: github_release
        asset: "{{.Asset}}.bundle"
      opts:
        - --certificate-identity
        - https://github.com/owner/tool/.github/workflows/release.yml@refs/tags/{{.Version}}
        - --certificate-oidc-issuer
        - https://token.actions.githubusercontent.com
`}.start(t, &m)

	out, err := m.run(t, "", "manifest", "init", "--from", "aqua:owner/tool", "-o", "-")
	if err != nil {
		t.Fatalf("init: %v\n%s", err, out)
	}

	release := "https://github.com/owner/tool/releases/download/{{tag}}/"
	for _, want := range []string{
		"signer_workflow = \"" + workflow + "\"\nattestations = true\n",
		"sha256_url_bundle = \"" + release + "checksums.txt.sigstore.json\"",
	} {
		if !strings.Contains(out, want) {
			t.Fatalf("the manifest lacks %q:\n%s", want, out)
		}
	}

	// A bundle in cosign's older format is not one oku reads.
	if strings.Contains(out, "sigstore_bundle") {
		t.Fatalf("the manifest names a bundle of cosign's older format:\n%s", out)
	}
}

func TestB504AnAquaPatternOfAWorkflowNeedsTheTagRef(t *testing.T) {
	entry := func(ref string) string {
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
        bundle:
          type: github_release
          asset: checksums.txt.sigstore.json
        opts:
          - --certificate-identity-regexp
          - "^https://github\\.com/flows/release/\\.github/workflows/release\\.yaml@.+$"
          - --certificate-oidc-issuer
          - https://token.actions.githubusercontent.com
          - --certificate-github-workflow-repository
          - owner/tool
          - --certificate-github-workflow-ref
          - ` + ref + `
`
	}

	for _, tc := range []struct {
		ref  string
		want bool
	}{
		{"refs/tags/{{.Version}}", true},
		{"refs/heads/main", false},
	} {
		m := newMachine(t)
		recipeServer{aqua: entry(tc.ref)}.start(t, &m)

		out, err := m.run(t, "", "manifest", "init", "--from", "aqua:owner/tool", "-o", "-")
		if err != nil {
			t.Fatalf("init: %v\n%s", err, out)
		}

		got := strings.Contains(out, "signer_workflow = \"flows/release/.github/workflows/release.yaml\"") &&
			strings.Contains(out, "sha256_url_bundle")
		if got != tc.want {
			t.Fatalf("with the ref %s, want the bundle kept %v:\n%s", tc.ref, tc.want, out)
		}
	}
}
