package cli_test

import (
	"fmt"
	"os"
	"os/exec"
	"strings"
	"testing"
)

func TestB524AListWithAnUnknownOrMistypedKeyChangesNothing(t *testing.T) {
	for _, tc := range []struct{ name, list, wantErr string }{
		{"a misspelt table", "[package]\ntool = %q\n", "line 1: unknown table or key package"},
		{"a misspelt entry key", "[packages]\ntool = { ref = %q, verison = \"1\" }\n", "unknown key verison"},
		{"a value of the wrong type", "[packages]\ntool = { ref = %q, version = 1 }\n", "version wants a string"},
	} {
		t.Run(tc.name, func(t *testing.T) {
			m := newMachine(t)
			tool := m.namedManifest(t, "tool", "tool", "tool")
			m.writeFilesList(t, fmt.Sprintf("[packages]\ntool = %q\n", tool))

			_, err := m.run(t, "", "sync")
			must(t, err)

			m.writeFilesList(t, fmt.Sprintf(tc.list, tool))

			if _, err := m.run(t, "", "sync"); err == nil || !strings.Contains(err.Error(), tc.wantErr) {
				t.Fatalf("want a refusal that says %q, got %v", tc.wantErr, err)
			}

			if _, err := exec.Command(m.profile("bin", "tool")).Output(); err != nil {
				t.Fatalf("the refused list removed tool: %v", err)
			}
		})
	}
}

func TestB525AnInstallWarnsOfAManifestKeyItDoesNotKnow(t *testing.T) {
	m := newMachine(t)
	tool := m.namedManifest(t, "tool", "tool", "tool")

	data, err := os.ReadFile(tool)
	must(t, err)
	must(t, os.WriteFile(tool, []byte(strings.Replace(string(data), "[[artifact]]\n", "[[artifact]]\nshiny = true\n", 1)), 0o644))

	out, err := m.run(t, "", "add", tool)
	if err != nil {
		t.Fatalf("a manifest for a newer oku did not install: %v\n%s", err, out)
	}

	if !strings.Contains(out, "line 6: unknown key artifact.shiny") || !strings.Contains(out, "newer oku") {
		t.Fatalf("add did not name the key it left out:\n%s", out)
	}
}

func TestB526ARefInTheListTakesNoVersion(t *testing.T) {
	for _, tc := range []struct{ name, list, wantErr string }{
		{"a package ref", "[packages]\ntool = \"%s@1.2.3\"\n", "write the version on its own"},
		{"an include", "include = [\"%s@1.2.3\"]\n", "takes no @version"},
	} {
		t.Run(tc.name, func(t *testing.T) {
			m := newMachine(t)
			tool := m.namedManifest(t, "tool", "tool", "tool")
			m.writeFilesList(t, fmt.Sprintf(tc.list, tool))

			if _, err := m.run(t, "", "sync"); err == nil || !strings.Contains(err.Error(), tc.wantErr) {
				t.Fatalf("want a refusal that says %q, got %v", tc.wantErr, err)
			}
		})
	}
}
