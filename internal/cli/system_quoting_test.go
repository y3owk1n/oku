package cli_test

import (
	"os"
	"path/filepath"
	"runtime"
	"strings"
	"testing"
)

func TestB534ADesktopEntryQuotesAProgramPathWithASpace(t *testing.T) {
	if runtime.GOOS != "linux" {
		t.Skip("desktop entries are Linux's")
	}

	m := newMachine(t)

	// A data directory with a space puts one in every store path.
	data := filepath.Join(t.TempDir(), "my data")
	t.Cleanup(func() { _ = writable(data) })
	t.Setenv("XDG_DATA_HOME", data)
	m.data = filepath.Join(data, "oku")

	_, err := m.run(t, "", "add", m.desktopManifest(t))
	must(t, err)

	entry, err := os.ReadFile(filepath.Join(data, "applications", "oku-foo.desktop"))
	must(t, err)

	for line := range strings.Lines(string(entry)) {
		if exec, ok := strings.CutPrefix(line, "Exec="); ok &&
			(!strings.HasPrefix(exec, `"`+data) || !strings.HasSuffix(strings.TrimSpace(exec), `/bin/foo"`)) {
			t.Fatalf("the program path is not one quoted argument:\n%s", entry)
		}
	}
}

func TestB534TheRootStepRefusesAServiceNameThatIsAPath(t *testing.T) {
	m := newMachine(t)

	_, err := m.run(t, "", "system-apply", "remove",
		`{"Item":{"Kind":"service","Name":"x/../../../Library/Evil","Target":"/Library/Evil.plist"},"Definition":{}}`)
	if err == nil || !strings.Contains(err.Error(), "is not the name of a service") {
		t.Fatalf("want a refusal of the name, got %v", err)
	}
}
