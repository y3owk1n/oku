package cli_test

import (
	"fmt"
	"testing"
)

func TestB360TheLaterPagesOfReleasesAreReadAtOnce(t *testing.T) {
	// Four pages of 100, and the version add wants is on the last.
	var tags []string
	for i := 350; i > 0; i-- {
		tags = append(tags, fmt.Sprintf("v1.0.%d", i))
	}

	for _, c := range []struct {
		name     string
		nextOnly bool
		most     int
	}{
		{"the host names the last page", false, 3},
		{"the host names only the next page", true, 1},
	} {
		t.Run(c.name, func(t *testing.T) {
			m := newMachine(t)
			server := newReleaseServer(t, tags...)
			server.nextOnly = c.nextOnly

			if !c.nextOnly {
				server.together = 3
			}

			m.opts.GitHubAPI = server.URL + "/api"

			_, err := m.run(t, "", "add", m.discoveredManifest(t, "1.0.1")+"@1.0.1")
			must(t, err)

			if got := m.toolOutput(t); got != "1.0.1" {
				t.Fatalf("add installed %s, want 1.0.1 from the fourth page", got)
			}

			if server.most != c.most {
				t.Fatalf("%d later pages were in flight at once, want %d", server.most, c.most)
			}
		})
	}
}
