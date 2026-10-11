package cli_test

import (
	"os"
	"path/filepath"
	"strings"
	"testing"
)

func TestB582AInteractiveServiceReachesTheManager(t *testing.T) {
	m := newMachine(t)

	ref := m.manifest(t, "pointer", map[string]string{"pointer": script},
		"bin = [\"pointer\"]\n[[service]]\nname = \"pointer\"\ncommand = \"bin/pointer\"\ninteractive = true\n")
	if out, err := m.run(t, "", "add", ref, "--service"); err != nil {
		t.Fatalf("add: %v\n%s", err, out)
	}

	if got := m.services.state["pointer"]; got == nil || !got.def.Interactive {
		t.Fatalf("the service manager should get an interactive service, got %+v", got)
	}

	path := filepath.Join(m.fixtures, "bad.toml")
	must(t, os.WriteFile(path, []byte("[package]\nname = \"bad\"\n[version]\nvalue = \"1.0.0\"\n"+
		"[[service]]\nname = \"bad\"\ncommand = \"bin/bad\"\nschedule = { every = \"1h\" }\ninteractive = true\n"), 0o644))

	if out, err := m.run(t, "", "manifest", "lint", path); err == nil || !strings.Contains(out+err.Error(), "takes no interactive") {
		t.Fatalf("lint of an interactive job should fail: %v\n%s", err, out)
	}
}
