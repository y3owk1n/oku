package cli

import (
	"bufio"
	"context"
	"errors"
	"fmt"
	"io"
	"os"
	"os/exec"
	"os/user"
	"path/filepath"
	"runtime"
	"strings"

	"github.com/spf13/cobra"

	"github.com/y3owk1n/oku/internal/source"
)

func newSetupCmd(opts Options) *cobra.Command {
	var system, yes bool

	cmd := &cobra.Command{
		Use:   "setup --system",
		Short: "Create a store root that is the same path on every machine",
		Long: `Create a store root that is the same path on every machine.

Packages built from source can embed their store path, so only machines with
the same store root can share them. "oku setup --system" creates that root once,
with administrator rights, and makes your user its owner. No later command
needs those rights.`,
		Args: cobra.NoArgs,
		RunE: func(cmd *cobra.Command, _ []string) error {
			if !system {
				return errors.New(`"oku setup" needs --system, which is the only thing it sets up`)
			}

			return runSetup(cmd, opts, yes)
		},
	}

	cmd.Flags().BoolVar(&system, "system", false, "create the shared store root")
	cmd.Flags().BoolVarP(&yes, "yes", "y", false, "do not ask for confirmation")

	return cmd
}

// systemRoot is the shared store root for this OS.
func systemRoot(opts Options) string {
	if opts.SystemRoot != "" {
		return opts.SystemRoot
	}

	if runtime.GOOS == "windows" {
		return filepath.Join(os.Getenv("ProgramData"), "oku")
	}

	return "/opt/oku"
}

func runSetup(cmd *cobra.Command, opts Options, yes bool) error {
	e, err := loadEnv()
	if err != nil {
		return err
	}

	out := cmd.OutOrStdout()
	root := systemRoot(opts)

	if e.root == root {
		fmt.Fprintf(out, "the store root is already %s\n", root)

		return nil
	}

	owner, err := user.Current()
	if err != nil {
		return fmt.Errorf("find the current user: %w", err)
	}

	fmt.Fprintln(out, "this creates, with administrator rights:")
	fmt.Fprintf(out, "  %s  owned by %s\n", root, owner.Username)

	if !yes && !confirm(bufio.NewReader(cmd.InOrStdin()), out, "continue? [y/N] ") {
		return errors.New("setup cancelled, nothing was created")
	}

	argv := []string{"install", "-d", "-m", "0755", "-o", owner.Uid, "-g", owner.Gid, root}
	if err := elevate(cmd.Context(), opts, argv); err != nil {
		return fmt.Errorf("create %s: %w", root, err)
	}

	path := filepath.Join(e.config, source.FileName)

	config, err := source.Read(path)
	if err != nil {
		return err
	}

	config.StoreRoot = root
	if err := config.Write(path); err != nil {
		return err
	}

	fmt.Fprintf(out, "the store root is now %s\n", root)
	fmt.Fprintln(
		out,
		`run "oku sync" to install your packages there, then "oku gc" to delete the old copies`,
	)

	return nil
}

// confirm asks a yes or no question. Callers that ask twice share one reader,
// because a reader buffers past the first answer.
func confirm(in *bufio.Reader, out io.Writer, question string) bool {
	fmt.Fprint(out, question)

	answer, _ := in.ReadString('\n')
	a := strings.ToLower(strings.TrimSpace(answer))

	return a == "y" || a == "yes"
}

// elevate runs argv with administrator rights. As root it runs argv directly.
// Otherwise sudo asks for the password on the terminal.
func elevate(ctx context.Context, opts Options, argv []string) error {
	if opts.Elevate != nil {
		return opts.Elevate(ctx, argv)
	}

	if os.Geteuid() != 0 {
		argv = append([]string{"sudo"}, argv...)
	}

	command := exec.CommandContext(ctx, argv[0], argv[1:]...)
	command.Stdin, command.Stdout, command.Stderr = os.Stdin, os.Stdout, os.Stderr

	if err := command.Run(); err != nil {
		return fmt.Errorf("%s: %w", strings.Join(argv, " "), err)
	}

	return nil
}
