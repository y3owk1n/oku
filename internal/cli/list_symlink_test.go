package cli_test

import (
	"os"
	"path/filepath"
	"runtime"
	"strings"
	"testing"
)

func TestB529AddWritesThroughASymlinkedList(t *testing.T) {
	m := newMachine(t)
	tool := m.manifest(t, "tool", map[string]string{"tool": script}, `bin = ["tool"]`)

	dotfiles := filepath.Join(t.TempDir(), "oku.toml")
	must(t, os.WriteFile(dotfiles, []byte("[packages]\n"), 0o600))
	must(t, os.MkdirAll(m.config, 0o755))

	link := filepath.Join(m.config, "oku.toml")
	if err := os.Symlink(dotfiles, link); err != nil {
		t.Skip("this user cannot create a symlink: ", err)
	}

	_, err := m.run(t, "", "add", tool)
	must(t, err)

	if info, err := os.Lstat(link); err != nil || info.Mode()&os.ModeSymlink == 0 {
		t.Fatalf("add replaced the symlinked oku.toml: %v", err)
	}

	data, err := os.ReadFile(dotfiles)
	must(t, err)

	if !strings.Contains(string(data), "tool") {
		t.Fatalf("the file the link points at lacks the package:\n%s", data)
	}

	if info, err := os.Stat(dotfiles); runtime.GOOS != "windows" && (err != nil || info.Mode().Perm() != 0o600) {
		t.Fatalf("the list lost its mode: %v %v", info.Mode(), err)
	}
}
