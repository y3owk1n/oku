//go:build unix

package sandbox_test

import (
	"context"
	"fmt"
	"os"
	"os/exec"
	"path/filepath"
	"runtime"
	"strconv"
	"strings"
	"syscall"
	"testing"
	"time"

	"github.com/y3owk1n/oku/internal/sandbox"
)

// run starts sh -c script through the sandbox, as a build step does.
func run(dir, script string) (string, error) {
	cmd, _ := sandbox.Command(context.Background(), sandbox.Spec{
		Argv:     []string{"sh", "-c", script},
		Dir:      dir,
		Env:      []string{"PATH=/usr/bin:/bin"},
		Writable: []string{dir},
	})

	var out strings.Builder

	cmd.Stdout, cmd.Stderr = &out, &out
	err := sandbox.Run(cmd)

	return out.String(), err
}

// The test binary runs itself again as the child of each test, with the
// variable naming what the child does. On Linux the sandbox also runs the
// current binary as "oku sandbox-init", and in a test that binary is this one.
func TestMain(m *testing.M) {
	if len(os.Args) > 1 && os.Args[1] == sandbox.InitCommand {
		if err := sandbox.Init(); err != nil {
			fmt.Fprintln(os.Stderr, "oku:", err)
			os.Exit(1)
		}
	}

	switch os.Getenv("OKU_SANDBOX_TEST_CHILD") {
	case "tty":
		// script gave this process a terminal. A build started from it must not
		// be able to open it.
		if f, err := os.OpenFile("/dev/tty", os.O_RDWR, 0); err != nil {
			fmt.Print("child has no terminal: ", err)
			os.Exit(0)
		} else {
			f.Close()
		}

		out, _ := run(os.Getenv("OKU_SANDBOX_TEST_DIR"),
			`(: </dev/tty) 2>/dev/null && echo tty=open || echo tty=closed`)
		fmt.Print(out)
		os.Exit(0)
	case "interrupt":
		dir := os.Getenv("OKU_SANDBOX_TEST_DIR")
		out, err := run(dir, `sleep 60 & echo $! > pid.tmp && mv pid.tmp pid; wait`)
		if err != nil {
			fmt.Print(out, err)
			os.Exit(1)
		}

		os.Exit(0)
	}

	os.Exit(m.Run())
}

func TestB405BuildCommandCannotOpenTheTerminal(t *testing.T) {
	if _, err := exec.LookPath("script"); err != nil {
		t.Skip("needs script to give the child a terminal")
	}

	self, err := os.Executable()
	if err != nil {
		t.Fatal(err)
	}

	cmd := exec.Command("script", "-q", "/dev/null", self)
	if runtime.GOOS == "linux" {
		cmd = exec.Command("script", "-qec", self, "/dev/null")
	}

	cmd.Env = append(os.Environ(), "OKU_SANDBOX_TEST_CHILD=tty", "OKU_SANDBOX_TEST_DIR="+t.TempDir())

	out, err := cmd.CombinedOutput()
	if err != nil {
		t.Fatalf("script: %v\n%s", err, out)
	}

	if strings.Contains(string(out), "child has no terminal") {
		t.Skipf("script gave the child no terminal:\n%s", out)
	}

	if !strings.Contains(string(out), "tty=closed") {
		t.Fatalf("a build command opened the terminal oku runs in:\n%s", out)
	}
}

func TestB405InterruptStopsTheBuildWithItsChildren(t *testing.T) {
	self, err := os.Executable()
	if err != nil {
		t.Fatal(err)
	}

	dir := t.TempDir()
	cmd := exec.Command(self)
	cmd.Env = append(os.Environ(), "OKU_SANDBOX_TEST_CHILD=interrupt", "OKU_SANDBOX_TEST_DIR="+dir)

	var out strings.Builder

	cmd.Stdout = &out

	if err := cmd.Start(); err != nil {
		t.Fatal(err)
	}

	var pid int

	for deadline := time.Now().Add(10 * time.Second); pid == 0; {
		if data, err := os.ReadFile(filepath.Join(dir, "pid")); err == nil {
			pid, _ = strconv.Atoi(strings.TrimSpace(string(data)))
		} else if time.Now().After(deadline) {
			_ = cmd.Process.Kill()
			_ = cmd.Wait()
			t.Fatalf("the build did not start its child within 10 seconds:\n%s", out.String())
		} else {
			time.Sleep(20 * time.Millisecond)
		}
	}

	must(t, cmd.Process.Signal(syscall.SIGINT))

	interrupted := time.Now()

	if err := cmd.Wait(); err == nil || !strings.Contains(out.String(), "stopped by interrupt") {
		t.Fatalf("the build should fail as stopped by the interrupt, got %v:\n%s", err, out.String())
	}

	if waited := time.Since(interrupted); waited > 5*time.Second {
		t.Fatalf("the build took %s to stop after the interrupt", waited)
	}

	for deadline := time.Now().Add(5 * time.Second); syscall.Kill(pid, 0) == nil; {
		if time.Now().After(deadline) {
			_ = syscall.Kill(pid, syscall.SIGKILL)
			t.Fatalf("the build's child %d still runs after the interrupt", pid)
		}

		time.Sleep(20 * time.Millisecond)
	}
}

func must(t *testing.T, err error) {
	t.Helper()

	if err != nil {
		t.Fatal(err)
	}
}
