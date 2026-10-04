package cli

import (
	"errors"
	"fmt"
	"os"
	"os/exec"
	"path/filepath"
	"runtime"
	"strings"

	"github.com/spf13/cobra"
)

func newExecCmd(opts Options) *cobra.Command {
	cmd := &cobra.Command{
		Use:   "exec <command> [args...]",
		Short: "Run a command with the programs of this directory on PATH",
		Long: `Run a command with the programs of this directory on PATH.

oku puts the global profile's programs on PATH and sets the variables of the
global oku.toml and its packages, as the shell hook does. In a project it puts
the project's programs first and sets the variables of its oku.toml and its
packages too. oku stops when a variable that a list requires is not set.

In a project, as for the shell hook, you must have run "oku allow" for its
oku.toml, and its profile must match its oku.lock, so run "oku sync" first.
Without that oku stops and says what to run, since a project's [env] can put
its own programs first on PATH.

An editor or a script that does not run the shell hook can start a program
this way, such as a language server "oku exec gopls". oku exits with the
command's exit code.`,
		Args: minArgs(1),
		// The command's stderr is its own, so oku does not name the project.
		Annotations: map[string]string{projectShown: "yes"},
		RunE: func(cmd *cobra.Command, command []string) error {
			e, err := scopedEnv(cmd, opts)
			if err != nil {
				return err
			}

			// A project's [env] can put its own programs first on PATH, so exec
			// applies it only as the hook does, once the user allowed it.
			if e.project != "" {
				if active, why := e.projectActive(); !active {
					return errors.New(why)
				}
			}

			environ, path, err := e.execEnviron()
			if err != nil {
				return err
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
