package cli

import (
	"errors"
	"fmt"
	"os"
	"path/filepath"
	"strings"

	"github.com/spf13/cobra"

	"github.com/y3owk1n/oku/internal/list"
)

// ExitError holds the exit code of the program that "oku shell", "oku run" or
// "oku exec" ran, so that oku exits with the same code and prints nothing more.
type ExitError struct{ Code int }

func (e ExitError) Error() string { return fmt.Sprintf("exit status %d", e.Code) }

func newShellCmd(opts Options) *cobra.Command {
	var (
		flags  buildFlags
		chosen pickFlags
	)

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

			return runShell(cmd, opts, &flags, &chosen, refs, command)
		},
	}

	flags.register(cmd)
	chosen.register(cmd)

	return cmd
}

func runShell(
	cmd *cobra.Command,
	opts Options,
	flags *buildFlags,
	chosen *pickFlags,
	refs, command []string,
) error {
	held, err := openRefs(cmd, opts, flags, chosen, refs)
	if err != nil {
		return err
	}

	environ := append(held.environ, "OKU_SHELL="+strings.Join(refs, " "))

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

	return runCommand(cmd, command, held.path, environ)
}

// pickFlags choose what a "shell" or a "run" installs: the asset and the
// programs of a manifest oku infers, and a build in place of a prebuilt
// download. They are the flags of the same name on "oku add".
type pickFlags struct {
	asset      string
	bins       []string
	fromSource bool
}

func (f *pickFlags) register(cmd *cobra.Command) {
	cmd.Flags().StringVar(&f.asset, "asset", "",
		"with no manifest, the release asset for this machine, as a glob")
	cmd.Flags().StringArrayVar(&f.bins, "bin", nil,
		"with no manifest, the file name of a program in the asset, once per program")
	cmd.Flags().BoolVar(&f.fromSource, "from-source", false,
		"build from source even when a prebuilt download fits")
}

// opened holds the packages an install for "shell" or "run" put in the store,
// and the environment their programs run with.
type opened struct {
	pkgs []installed
	// path is PATH with the packages' bin directories first, and environ holds
	// it together with the packages' [env].
	path    string
	environ []string
}

// openRefs puts the packages of refs in the store, without touching oku.toml,
// the lock or a profile. Only the install waits for other oku processes, not
// the program that runs afterwards.
func openRefs(
	cmd *cobra.Command,
	opts Options,
	flags *buildFlags,
	chosen *pickFlags,
	refs []string,
) (opened, error) {
	if len(refs) > 1 && (chosen.asset != "" || len(chosen.bins) > 0) {
		return opened{}, errors.New(
			"--asset and --bin describe one download, so name that ref on its own",
		)
	}

	e, err := loadEnv()
	if err != nil {
		return opened{}, err
	}

	release, err := lockMachine(cmd)
	if err != nil {
		return opened{}, err
	}
	defer release()

	held := opened{environ: os.Environ()}

	var bins []string

	own, err := list.Read(e.listPath())
	if err != nil {
		return opened{}, err
	}

	age, err := releaseAge(cmd, own, list.Entry{})
	if err != nil {
		return opened{}, err
	}

	for _, arg := range refs {
		r, err := e.parseRef(arg)
		if err != nil {
			return opened{}, err
		}

		// A package of a registry runs through, or builds with, the runtime that
		// the list names.
		if e.inferrerOf(r.Kind) != nil && e.runtimes == nil {
			all, err := e.mergedList(cmd, opts)
			if err != nil {
				return opened{}, err
			}

			e.runtimes = all.runtimes
		}

		got, err := e.install(cmd.Context(), opts, request{
			ref:          r,
			releaseAge:   age,
			asset:        chosen.asset,
			bins:         chosen.bins,
			fromSource:   chosen.fromSource,
			acceptKey:    flags.acceptKey,
			acceptWeaker: flags.acceptWeaker,
			approve:      e.approver(cmd, opts, flags),
			checkAge:     e.ageChecker(cmd, opts, flags),
			checkTrust:   e.trustChecker(cmd, opts, flags),
			log:          buildLog(cmd, flags),
		})
		if err != nil {
			return opened{}, err
		}

		// shell and run write no lock, so there is nothing to pin the digest in.
		if got.firstUse {
			fmt.Fprintf(
				cmd.ErrOrStderr(),
				"%s publishes no checksum, so oku trusted this download\n",
				got.lock.Name,
			)
		}

		reportUnsandboxed(cmd.ErrOrStderr(), got)
		reportNotes(cmd.ErrOrStderr(), got)
		reportCache(cmd.ErrOrStderr(), got)

		held.pkgs = append(held.pkgs, got)
		bins = append(bins, filepath.Join(got.profile.StorePath, "bin"))

		for name, value := range got.profile.Env {
			held.environ = append(held.environ, name+"="+value)
		}
	}

	release()

	held.path = strings.Join(append(bins, os.Getenv("PATH")), string(os.PathListSeparator))
	held.environ = append(held.environ, "PATH="+held.path)

	return held, nil
}
