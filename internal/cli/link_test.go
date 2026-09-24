package cli_test

import (
	"fmt"
	"os"
	"os/exec"
	"path/filepath"
	"runtime"
	"strings"
	"testing"
)

// linkedApp writes the manifest of a program named name that links libgreet
// from the greet package, with greet as a build dep and runtime as its
// [runtime] table.
func (m machine) linkedApp(t *testing.T, name, runtimeDeps string) string {
	t.Helper()

	return m.linkedAppWith(t, name, "-lgreet", runtimeDeps)
}

// linkedAppWith is linkedApp with link as the flag that links libgreet.
func (m machine) linkedAppWith(t *testing.T, name, link, runtimeDeps string) string {
	t.Helper()

	shared := "cc -shared -fPIC -o libgreet.so greet.c"
	libFile := "libgreet.so"

	if runtime.GOOS == "darwin" {
		shared = "cc -dynamiclib -o libgreet.dylib -install_name {{prefix}}/lib/libgreet.dylib greet.c"
		libFile = "libgreet.dylib"
	}

	must(t, os.WriteFile(filepath.Join(m.fixtures, "greet.toml"), []byte(fmt.Sprintf(`[package]
name = "greet"
[version]
value = "1.0.0"
[build]
needs = ["cc"]
[[build.step]]
run = """
printf 'const char *greet(void);\\n' > greet.h
printf 'const char *greet(void) { return "hello"; }\\n' > greet.c
%s
"""
shell = "sh"
[[build.step]]
install = { lib = [%q], include = ["greet.h"] }
`, shared, libFile)), 0o644))

	path := filepath.Join(m.fixtures, name+".toml")
	must(t, os.WriteFile(path, []byte(fmt.Sprintf(`[package]
name = %q
[version]
value = "1.0.0"
[build]
needs = ["cc"]
deps = ["./greet.toml"]
[[build.step]]
run = """
printf '#include <stdio.h>\\n#include <greet.h>\\nint main(void) { puts(greet()); return 0; }\\n' > main.c
cc -o %s main.c %s
"""
shell = "sh"
[[build.step]]
install = { bin = [%q] }
%s`, name, name, link, name, runtimeDeps)), 0o644))

	return path
}

func TestB202BuildWarnsWhenABinaryLoadsAStorePackageOutsideRuntimeDeps(t *testing.T) {
	if runtime.GOOS != "darwin" && runtime.GOOS != "linux" {
		t.Skip("the link check reads Mach-O and ELF files")
	}

	if _, err := exec.LookPath("cc"); err != nil {
		t.Skip("needs a C compiler")
	}

	m := newMachine(t)

	out, err := m.run(t, "", "add", m.linkedApp(t, "app", ""), "--yes")
	if err != nil {
		t.Fatalf("add: %v\n%s", err, out)
	}

	want := "app: bin/app loads greet, which is not in runtime.deps"
	if !strings.Contains(out, want) {
		t.Fatalf("add printed no warning for the unlisted dep:\n%s", out)
	}

	if got := strings.Count(out, "loads greet"); got != 1 {
		t.Fatalf("add warned %d times for one dep:\n%s", got, out)
	}

	listed := m.linkedApp(t, "listed", "[runtime]\ndeps = [\"./greet.toml\"]\n")

	out, err = m.run(t, "", "add", listed, "--yes")
	if err != nil {
		t.Fatalf("add: %v\n%s", err, out)
	}

	if strings.Contains(out, "runtime.deps") {
		t.Fatalf("add warned although runtime.deps names the dep:\n%s", out)
	}
}

func TestB328BuildWarnsForAWeaklyLinkedStorePackageOutsideRuntimeDeps(t *testing.T) {
	if runtime.GOOS != "darwin" {
		t.Skip("weak linking is a Mach-O load command")
	}

	if _, err := exec.LookPath("cc"); err != nil {
		t.Skip("needs a C compiler")
	}

	m := newMachine(t)

	out, err := m.run(t, "", "add", m.linkedAppWith(t, "app", "-weak-lgreet", ""), "--yes")
	if err != nil {
		t.Fatalf("add: %v\n%s", err, out)
	}

	if !strings.Contains(out, "app: bin/app loads greet, which is not in runtime.deps") {
		t.Fatalf("add printed no warning for the weakly linked dep:\n%s", out)
	}
}
