package cli_test

import (
	"crypto/rand"
	"errors"
	"testing"

	"aead.dev/minisign"

	"github.com/y3owk1n/oku/internal/cli"
)

// exitCode is the code an error of m.run makes oku exit with: 0 for none, 1 for
// a failure that prints a message, and the code of an ExitError.
func exitCode(err error) int {
	var exit cli.ExitError

	switch {
	case err == nil:
		return 0
	case errors.As(err, &exit):
		return exit.Code
	default:
		return 1
	}
}

func TestB565ExitCodeIs2WhenThereIsSomethingToDo(t *testing.T) {
	m := newMachine(t)
	server := newReleaseServer(t, "v1.0.0")
	m.opts.GitHubAPI = server.URL + "/api"

	_, err := m.run(t, "", "add", m.discoveredManifest(t, "1.0.0", "1.1.0"))
	must(t, err)

	for _, args := range [][]string{{"outdated", "--exit-code"}, {"update", "--dry-run", "--exit-code"}} {
		if _, err := m.run(t, "", args...); exitCode(err) != 0 {
			t.Fatalf("%v with nothing to do: %v", args, err)
		}
	}

	server.tags = []string{"v1.1.0", "v1.0.0"}

	for _, args := range [][]string{{"outdated", "--exit-code"}, {"update", "--dry-run", "--exit-code"}} {
		if _, err := m.run(t, "", args...); exitCode(err) != 2 {
			t.Fatalf("%v with a newer version: want exit 2, got %v", args, err)
		}
	}

	if _, err := m.run(t, "", "outdated"); exitCode(err) != 0 {
		t.Fatalf("outdated without --exit-code: %v", err)
	}

	if _, err := m.run(t, "", "update", "--exit-code"); exitCode(err) != 1 {
		t.Fatalf("--exit-code without --dry-run should be refused, got %v", err)
	}

	public, secret, err := minisign.GenerateKey(rand.Reader)
	must(t, err)

	m.opts.ReleaseKey = public.String()
	m.releaseWith(t, "the new oku", "v1.4.0", secret)

	if _, err := m.run(t, "", "self", "update", "--check", "--exit-code"); exitCode(err) != 2 {
		t.Fatalf("self update --check --exit-code with a newer release: want exit 2, got %v", err)
	}

	if _, err := m.run(t, "", "self", "update", "--check"); exitCode(err) != 0 {
		t.Fatalf("self update --check: %v", err)
	}
}
