package cli_test

import (
	"encoding/json"
	"fmt"
	"net/http"
	"net/http/httptest"
	"os"
	"path/filepath"
	"strings"
	"testing"
)

// osvServer fakes OSV, which lists each "<name>@<version>" of malicious as
// malicious, under the id it maps to. With down it answers every query with
// an error.
func osvServer(t *testing.T, m *machine, malicious map[string]string, down bool) {
	t.Helper()

	server := httptest.NewServer(http.HandlerFunc(func(w http.ResponseWriter, r *http.Request) {
		if down {
			http.Error(w, "down", http.StatusServiceUnavailable)

			return
		}

		if strings.HasPrefix(r.URL.Path, "/v1/vulns/") {
			_, _ = w.Write([]byte(`{"id": "` + strings.TrimPrefix(r.URL.Path, "/v1/vulns/") + `"}`))

			return
		}

		var batch struct {
			Queries []struct {
				Package struct {
					Name string `json:"name"`
				} `json:"package"`
				Version string `json:"version"`
			} `json:"queries"`
		}
		must(t, json.NewDecoder(r.Body).Decode(&batch))

		results := make([]string, len(batch.Queries))
		for i, q := range batch.Queries {
			results[i] = "{}"
			if id, ok := malicious[q.Package.Name+"@"+q.Version]; ok {
				results[i] = fmt.Sprintf(`{"vulns": [{"id": "GHSA-0000-0000-0000"}, {"id": %q}]}`, id)
			}
		}

		fmt.Fprintf(w, `{"results": [%s]}`, strings.Join(results, ","))
	}))
	t.Cleanup(server.Close)

	m.opts.OSVAPI = server.URL
}

func TestB513AVersionOSVListsAsMaliciousIsNotInstalled(t *testing.T) {
	m := newMachine(t)
	npmServer(t, &m, "", "1.0.0", "1.1.0")
	osvServer(t, &m, map[string]string{"@scope/tool@1.1.0": "MAL-2025-1"}, false)

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

func TestB516ABuildStopsAtADependencyThatOSVListsAsMalicious(t *testing.T) {
	m := newMachine(t)
	npmServerWith(t, &m, "", true, "1.1.0")
	osvServer(t, &m, map[string]string{"left-pad@1.3.0": "MAL-2025-2"}, false)

	must(t, os.MkdirAll(m.config, 0o755))
	must(t, os.WriteFile(filepath.Join(m.config, "config.toml"),
		[]byte(fmt.Sprintf("[runtimes]\nnode = %q\n", m.fakeNode(t))), 0o644))

	_, err := m.run(t, "", "add", "npm:@scope/tool", "--yes")
	if err == nil || !strings.Contains(err.Error(), "left-pad 1.3.0, see https://osv.dev/vulnerability/MAL-2025-2") {
		t.Fatalf("want the build stopped at the malicious dependency, got %v", err)
	}

	// Without OSV the build goes on and says what oku could not check.
	osvServer(t, &m, nil, true)

	out, err := m.run(t, "", "add", "npm:@scope/tool", "--yes")
	if err != nil || !strings.Contains(out, "oku could not ask OSV about the packages of the npm package step") {
		t.Fatalf("want the build to go on with a note, got %v\n%s", err, out)
	}
}
