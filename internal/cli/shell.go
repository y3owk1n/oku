package cli

import (
	"errors"
	"fmt"
	"os"
	"os/exec"
	"path/filepath"
	"strings"

	"github.com/spf13/cobra"
)

// ExitError holds the exit code of the program that "oku shell" ran, so that
// oku exits with the same code and prints nothing more.
type ExitError struct{ Code int }

func (e ExitError) Error() string { return fmt.Sprintf("exit status %d", e.Code) }

func newShellCmd(opts Options) *cobra.Command {
	var flags buildFlags

	cmd := &cobra.Command{
		Use:   "shell <ref>... [-- command [args...]]",
		Short: "Open a shell with packages on PATH, and install nothing",
		Long: `Open a shell with packages on PATH, and install nothing.

oku puts the packages in the store and starts $SHELL with their programs first
on PATH and their [env] set. It changes no oku.toml, no oku.lock and no
profile. Leave the shell with "exit". "oku gc" deletes the packages later.

After "--", oku runs that command in place of a shell and exits with its code.`,
		Args: cobra.MinimumNArgs(1),
		RunE: func(cmd *cobra.Command, args []string) error {
			refs, command := args, []string(nil)
			if at := cmd.ArgsLenAtDash(); at >= 0 {
				refs, command = args[:at], args[at:]
			}

			if len(refs) == 0 {
				return errors.New("name at least one ref before --")
			}

			return runShell(cmd, opts, &flags, refs, command)
		},
	}

	flags.register(cmd)

	return cmd
}

func runShell(
	cmd *cobra.Command,
	opts Options,
	flags *buildFlags,
	refs, command []string,
) error {
	e, err := loadEnv()
	if err != nil {
		return err
	}

	// Only the install waits for other oku processes, not the shell itself.
	release, err := lockMachine(cmd)
	if err != nil {
		return err
	}
	defer release()

	var bins []string

	environ := os.Environ()

	for _, arg := range refs {
		r, err := e.parseRef(arg)
		if err != nil {
			return err
		}

		// A package of a registry runs through, or builds with, the runtime that
		// the list names.
		if e.inferrerOf(r.Kind) != nil && e.runtimes == nil {
			all, err := e.mergedList(cmd, opts)
			if err != nil {
				return err
			}

			e.runtimes = all.runtimes
		}

		got, err := e.install(cmd.Context(), opts, request{
			ref:       r,
			acceptKey: flags.acceptKey,
			approve:   e.approver(cmd, opts, flags),
			log:       buildLog(cmd, flags),
		})
		if err != nil {
			return err
		}

		// shell writes no lock, so there is nothing to pin the digest in.
		if got.firstUse {
			fmt.Fprintf(
				cmd.ErrOrStderr(),
				"%s publishes no checksum, so oku trusted this download\n",
				got.lock.Name,
			)
		}

		reportUnsandboxed(cmd.ErrOrStderr(), got)
		reportLinks(cmd.ErrOrStderr(), got)
		reportCache(cmd.ErrOrStderr(), got)

		bins = append(bins, filepath.Join(got.profile.StorePath, "bin"))

		for name, value := range got.profile.Env {
			environ = append(environ, name+"="+value)
		}
	}

	release()

	path := strings.Join(append(bins, os.Getenv("PATH")), string(os.PathListSeparator))
	environ = append(environ, "PATH="+path, "OKU_SHELL="+strings.Join(refs, " "))

	if len(command) == 0 {
		shell := os.Getenv("SHELL")
		if shell == "" {
			shell = "/bin/sh"
		}

		command = []string{shell}

		fmt.Fprintf(
			cmd.ErrOrStderr(),
			"oku shell with %s, leave it with exit\n",
			strings.Join(refs, ", "),
		)
	}

	// The command is looked up on the new PATH, so "-- rg" finds the package's rg.
	program := command[0]
	if !strings.ContainsRune(program, filepath.Separator) {
		for _, dir := range filepath.SplitList(path) {
			if info, err := os.Stat(filepath.Join(dir, program)); err == nil && !info.IsDir() {
				program = filepath.Join(dir, program)

				break
			}
		}
	}

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
