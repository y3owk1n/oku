package cli_test

import (
	"context"
	"os"
	"path/filepath"
	"strconv"
	"strings"
	"testing"
	"time"

	"github.com/y3owk1n/oku/internal/busy"
)

func TestB243ACommandThatChangesTheMachineWaitsForAnotherOne(t *testing.T) {
	m := newMachine(t)
	tool := m.manifest(t, "tool", map[string]string{"tool": script}, `bin = ["tool"]`)

	// This test stands in for the other oku process.
	release, err := busy.Lock(context.Background(), m.data, func(int) {})
	must(t, err)

	type result struct {
		out string
		err error
	}

	done := make(chan result)

	go func() {
		out, err := m.run(t, "", "add", tool)
		done <- result{out, err}
	}()

	select {
	case got := <-done:
		t.Fatalf("add ran while another oku held the lock:\n%s", got.out)
	case <-time.After(500 * time.Millisecond):
	}

	// A command that only reads does not wait.
	_, err = m.run(t, "", "list")
	must(t, err)

	release()

	got := <-done
	must(t, got.err)

	if want := "waiting for oku process " + strconv.Itoa(os.Getpid()) + " to finish"; !strings.Contains(got.out, want) {
		t.Fatalf("add did not say what it waited for, want %q:\n%s", want, got.out)
	}

	if !exists(m.profile("bin", "tool")) {
		t.Fatalf("add did not install tool after the wait:\n%s", got.out)
	}
}

func TestB244GCDeletesWhatAKilledInstallLeftInTheStore(t *testing.T) {
	m := newMachine(t)

	_, err := m.run(t, "", "add", m.manifest(t, "tool", map[string]string{"tool": script}, `bin = ["tool"]`))
	must(t, err)

	left := filepath.Join(m.data, "store", ".tmp-1234")
	must(t, os.MkdirAll(left, 0o755))
	must(t, os.WriteFile(filepath.Join(left, "half"), []byte("unpacked"), 0o644))

	out, err := m.run(t, "", "gc")
	must(t, err)

	if exists(left) {
		t.Fatalf("gc left the temporary directory of a killed install:\n%s", out)
	}

	if !exists(m.profile("bin", "tool")) {
		t.Fatalf("gc deleted a package in use:\n%s", out)
	}
}
