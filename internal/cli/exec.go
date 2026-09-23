package cli

import (
	"bytes"
	"errors"
	"fmt"
	"maps"
	"os"
	"os/exec"
	"path/filepath"
	"runtime"
	"strings"

	"github.com/spf13/cobra"

	"github.com/y3owk1n/oku/internal/profile"
)

func newExecCmd(opts Options) *cobra.Command {
	cmd := &cobra.Command{
		Use:   "exec <command> [args...]",
		Short: "Run a command with the programs of this directory on PATH",
		Long: `Run a command with the programs of this directory on PATH.

oku puts the global profile's programs on PATH and sets their [env], as the
shell hook does. In a project it puts the project's programs first and sets
their [env] too. The project needs no "oku allow", since you asked for the
command, but its profile must match its oku.lock, so run "oku sync" first.

An editor or a script that does not run the shell hook can start a program
this way, such as a language server "oku exec gopls". oku exits with the
command's exit code.`,
		Args: cobra.MinimumNArgs(1),
		// The command's stderr is its own, so oku does not name the project.
		Annotations: map[string]string{projectShown: "yes"},
		RunE: func(cmd *cobra.Command, command []string) error {
			e, err := scopedEnv(cmd, opts)
			if err != nil {
				return err
			}

			profiles := []*profile.Profile{e.globalProfile()}

			if e.project != "" {
				locked, err := os.ReadFile(e.lockPath())
				if err != nil || !bytes.Equal(locked, e.profile().LockSnapshotOfCurrent()) {
					return fmt.Errorf("the profile of %s is behind its oku.lock, run `oku sync`", e.project)
				}

				// The project comes first, so its programs and [env] win.
				profiles = append([]*profile.Profile{e.profile()}, profiles...)
			}

			bins := make([]string, 0, len(profiles))
			env := map[string]string{}

			for i := len(profiles) - 1; i >= 0; i-- {
				pkgs, err := profiles[i].Packages()
				if err != nil {
					return err
				}

				for _, pkg := range pkgs {
					maps.Copy(env, pkg.Env)
				}
			}

			for _, prof := range profiles {
				bins = append(bins, prof.BinDir())
			}

			path := strings.Join(append(bins, os.Getenv("PATH")), string(os.PathListSeparator))
			environ := append(os.Environ(), "PATH="+path)

			for name, value := range env {
				environ = append(environ, name+"="+value)
			}

			return runCommand(cmd, command, path, environ)
		},
	}

	// Flags after the command belong to it, as in "oku exec rg --version".
	cmd.Flags().SetInterspersed(false)

	return cmd
}

// runCommand runs command with environ and exits with its code. oku looks the
// command up on path, the PATH of environ, so that "rg" is the package's rg.
func runCommand(cmd *cobra.Command, command []string, path string, environ []string) error {
	program := lookPath(command[0], path)

	child := exec.CommandContext(cmd.Context(), program, command[1:]...)
	child.Env = environ
	child.Stdin, child.Stdout, child.Stderr = cmd.InOrStdin(), cmd.OutOrStdout(), cmd.ErrOrStderr()

	var exit *exec.ExitError
	if err := child.Run(); errors.As(err, &exit) {
		return ExitError{Code: exit.ExitCode()}
	} else if err != nil {
		return fmt.Errorf("run %s: %w", command[0], err)
	}

	return nil
}

// lookPath returns the file that program names on path, or program itself when
// it holds a separator or no directory has it. On Windows a program ends in
// .exe, .cmd or .bat.
func lookPath(program, path string) string {
	if strings.ContainsRune(program, filepath.Separator) {
		return program
	}

	names := []string{program}
	if runtime.GOOS == "windows" {
		names = append(names, program+".exe", program+".cmd", program+".bat")
	}

	for _, dir := range filepath.SplitList(path) {
		for _, name := range names {
			if info, err := os.Stat(filepath.Join(dir, name)); err == nil && !info.IsDir() {
				return filepath.Join(dir, name)
			}
		}
	}

	return program
}
