package cli_test

import (
	"encoding/json"
	"fmt"
	"net/http"
	"net/http/httptest"
	"strings"
	"testing"
)

// osvServer fakes OSV, which lists the versions in malicious, by version, as
// malicious in npm's @scope/tool. With down it answers every query with an
// error.
func osvServer(t *testing.T, m *machine, malicious map[string]string, down bool) {
	t.Helper()

	server := httptest.NewServer(http.HandlerFunc(func(w http.ResponseWriter, r *http.Request) {
		if down {
			http.Error(w, "down", http.StatusServiceUnavailable)

			return
		}

		var query struct {
			Package struct {
				Name, Ecosystem string
			}
			Version string
		}
		must(t, json.NewDecoder(r.Body).Decode(&query))

		id, ok := malicious[query.Version]
		if !ok || query.Package.Name != "@scope/tool" || query.Package.Ecosystem != "npm" {
			_, _ = w.Write([]byte("{}"))

			return
		}

		fmt.Fprintf(w, `{"vulns": [{"id": "GHSA-0000-0000-0000"}, {"id": %q}]}`, id)
	}))
	t.Cleanup(server.Close)

	m.opts.OSVAPI = server.URL
}

func TestB513AVersionOSVListsAsMaliciousIsNotInstalled(t *testing.T) {
	m := newMachine(t)
	npmServer(t, &m, "", "1.0.0", "1.1.0")
	osvServer(t, &m, map[string]string{"1.1.0": "MAL-2025-1"}, false)

	_, err := m.run(t, "", "add", "npm:@scope/tool")
	if err == nil || !strings.Contains(err.Error(), "OSV lists it as malicious in MAL-2025-1") {
		t.Fatalf("want the malicious version refused, got %v", err)
	}

	if len(m.storeEntries(t)) != 0 {
		t.Fatalf("a malicious version reached the store: %v", m.storeEntries(t))
	}

	// A package that oku.lock holds stays at its locked version.
	_, err = m.run(t, "", "add", "npm:@scope/tool@1.0.0")
	must(t, err)

	m.writeFilesList(t, "[packages]\ntool = \"npm:@scope/tool\"\n")

	out, err := m.run(t, "", "update")
	if err != nil || !strings.Contains(out, "tool stays at 1.0.0") || !strings.Contains(out, "osv.dev/vulnerability/MAL-2025-1") {
		t.Fatalf("want update to keep 1.0.0 and say why, got %v\n%s", err, out)
	}
}

func TestB514OkuWarnsWhenItCannotAskOSV(t *testing.T) {
	m := newMachine(t)
	npmServer(t, &m, "", "1.1.0")
	osvServer(t, &m, nil, true)

	out, err := m.run(t, "", "add", "npm:@scope/tool")
	if err != nil || !strings.Contains(out, "could not check whether OSV lists it as malicious") {
		t.Fatalf("want the package installed with a warning, got %v\n%s", err, out)
	}
}
