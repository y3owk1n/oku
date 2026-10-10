package cli

import (
	"errors"
	"fmt"
	"io/fs"
	"os"
	"path/filepath"
	"strings"

	"github.com/spf13/cobra"

	"github.com/y3owk1n/oku/internal/profile"
	"github.com/y3owk1n/oku/internal/shim"
	"github.com/y3owk1n/oku/internal/store"
)

func newRunCmd(opts Options) *cobra.Command {
	var (
		flags  buildFlags
		chosen pickFlags
		app    string
	)

	cmd := &cobra.Command{
		Use:   "run <ref> [-- args...]",
		Short: "Run a package's app once, and install nothing",
		Long: `Run a package's app once, and install nothing.

oku puts the package in the store and starts its app with the package's
programs first on PATH and its [env] set. It changes no oku.toml, no oku.lock
and no profile, and it places no launcher and no copy of an app, so the app
reaches no menu and no Spotlight result. "oku gc" deletes the package later.

A package that ships no app and one program runs that program. Arguments after
"--" go to what starts, and oku exits with its code.`,
		Args:              minArgs(1),
		ValidArgsFunction: completeRefs,
		RunE: func(cmd *cobra.Command, args []string) error {
			refs, command := args, []string(nil)
			if at := cmd.ArgsLenAtDash(); at >= 0 {
				refs, command = args[:at], args[at:]
			}

			switch {
			case len(refs) == 0:
				return errors.New("name a ref before --")
			case len(refs) > 1:
				return fmt.Errorf(
					"`oku run` takes one ref, so use `oku shell %s` for several",
					strings.Join(refs, " "),
				)
			}

			return runApp(cmd, opts, &flags, &chosen, refs[0], app, command)
		},
	}

	flags.register(cmd)
	chosen.register(cmd)
	cmd.Flags().StringVar(&app, "app", "", "the app to start, when the package ships several")

	return cmd
}

func runApp(
	cmd *cobra.Command,
	opts Options,
	flags *buildFlags,
	chosen *pickFlags,
	ref, app string,
	args []string,
) error {
	held, err := openRefs(cmd, opts, flags, chosen, []string{ref})
	if err != nil {
		return err
	}
	defer held.done()

	program, err := starts(held.pkgs[0].profile, ref, app)
	if err != nil {
		return err
	}

	// The store reaches an app and a program through a link, or on Windows
	// through a copy with a spec beside it that names the file. macOS cannot
	// issue a sandbox extension for a bundle behind a link, and a program that
	// loads a file next to its own finds nothing there, so run starts the file
	// itself.
	program, err = runs(program)
	if err != nil {
		return fmt.Errorf("read %s: %w", program, err)
	}

	return runCommand(cmd, append([]string{program}, args...), held.path, held.environ)
}

// starts returns the file in the store that "oku run" starts: the app that want
// names, the package's one app, or its one program.
func starts(pkg profile.Package, ref, want string) (string, error) {
	apps, err := store.Apps(pkg.StorePath)
	if err != nil {
		return "", err
	}

	inside := func(exec string) string {
		return filepath.Join(pkg.StorePath, filepath.FromSlash(exec))
	}

	names := make([]string, 0, len(apps))
	for _, app := range apps {
		names = append(names, app.Name)
	}

	switch {
	case want != "" && len(apps) == 0:
		return "", fmt.Errorf("%s ships no app, so --app has nothing to name", pkg.Name)
	case want != "":
		for _, app := range apps {
			if appName(app.Name) == appName(want) {
				return inside(app.Exec), nil
			}
		}

		return "", fmt.Errorf(
			"%s ships no app %s, it ships %s", pkg.Name, want, strings.Join(names, ", "),
		)
	case len(apps) == 1:
		return inside(apps[0].Exec), nil
	case len(apps) > 1:
		return "", fmt.Errorf(
			"%s ships %d apps, so name one with --app: %s",
			pkg.Name, len(apps), strings.Join(names, ", "),
		)
	}

	programs, err := storePrograms(pkg.StorePath)
	if err != nil {
		return "", err
	}

	switch len(programs) {
	case 0:
		return "", fmt.Errorf("%s ships no app and no program to run", pkg.Name)
	case 1:
		return filepath.Join(pkg.StorePath, "bin", programs[0]), nil
	}

	for i, name := range programs {
		programs[i] = strings.TrimSuffix(name, ".exe")
	}

	return "", fmt.Errorf(
		"%s ships no app and %d programs, so run one with `oku shell %s -- <program>`: %s",
		pkg.Name, len(programs), ref, strings.Join(programs, ", "),
	)
}

// appName is an app's name to compare, which ignores case and the .app of a
// macOS bundle.
func appName(name string) string {
	return strings.ToLower(strings.TrimSuffix(name, ".app"))
}

// storePrograms lists the programs in the package's bin directory. The spec
// file beside a program on Windows is not a program.
func storePrograms(storePath string) ([]string, error) {
	entries, err := os.ReadDir(filepath.Join(storePath, "bin"))
	if err != nil && !errors.Is(err, fs.ErrNotExist) {
		return nil, err
	}

	programs := make([]string, 0, len(entries))

	for _, entry := range entries {
		if !entry.IsDir() && filepath.Ext(entry.Name()) != shim.Ext {
			programs = append(programs, entry.Name())
		}
	}

	return programs, nil
}
