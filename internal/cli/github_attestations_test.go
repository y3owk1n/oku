package cli_test

import (
	"strings"
	"testing"
)

func TestB521AnAttestationOfGitHubsOwnSigstoreNeedsItsTimestamp(t *testing.T) {
	main := run{workflow + "@refs/heads/main", "owner/tool", "refs/heads/main"}

	for _, tc := range []struct {
		name    string
		run     run
		stamped bool
		wantErr bool
	}{
		{"the workflow, stamped", main, true, false},
		{"another repo", run{workflow + "@refs/heads/main", "owner/other", "refs/heads/main"}, true, true},
		{"no timestamp", main, false, true},
	} {
		t.Run(tc.name, func(t *testing.T) {
			m := newMachine(t)
			f := newFakeSigstore(t, &m)
			f.withGitHub(t, &m)
			r := newSignedRelease(t, &m)

			r.mu.Lock()
			r.attestations = [][]byte{f.githubAttest(t, tc.run, r.sum, tc.stamped)}
			r.mu.Unlock()

			tool := r.manifest(t, &m, "signer_workflow = \""+workflow+"\"\nattestations = true\n", "")

			out, err := m.run(t, "", "add", tool)

			switch {
			case !tc.wantErr && err != nil:
				t.Fatalf("add: %v\n%s", err, out)
			case tc.wantErr && (err == nil || !strings.Contains(err.Error(), "no attestation")):
				t.Fatalf("want a refusal, got %v", err)
			}
		})
	}
}
