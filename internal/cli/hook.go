package cli

import (
	"bytes"
	"crypto/sha256"
	"encoding/hex"
	"fmt"
	"maps"
	"os"
	"path/filepath"
	"runtime"
	"slices"
	"strings"

	"github.com/spf13/cobra"

	"github.com/y3owk1n/oku/internal/profile"
	"github.com/y3owk1n/oku/internal/shellhook"
	"github.com/y3owk1n/oku/internal/trust"
)

func newHookCmd(opts Options) *cobra.Command {
	oku := displayPath(opts.Executable)

	long := "Print the shell code that sets oku up in a shell.\n\n" +
		"Add one line to your shell's startup file. oku never edits that file.\n\n"
	for _, shell := range shellhook.Shells {
		long += "  " + shellhook.StartupFile(shell) + "\n    " + shellhook.Line(shell, oku) + "\n\n"
	}

	long += "That one line puts oku and the programs it installs on PATH, and applies a\n" +
		"project's environment while you are inside the project. It does nothing when\n" +
		"oku is not installed, so it is safe to leave behind."

	return &cobra.Command{
		Use:   "hook <bash|zsh|fish|pwsh>",
		Short: "Print the shell code that sets oku up in a shell",
		Long:  long,
		Args:  exactArgs(1),
		RunE: func(cmd *cobra.Command, args []string) error {
			e, err := loadEnv()
			if err != nil {
				return err
			}

			prof := e.globalProfile()

			dirs := []string{prof.BinDir()}
			oku := "oku"
			if opts.Executable != "" {
				dirs = append(dirs, filepath.Dir(opts.Executable))
				oku = opts.Executable
			}

			code, err := shellhook.Hook(
				args[0], dirs, filepath.Join(prof.ShareDir(), "completions"), oku,
			)
			if err != nil {
				return err
			}

			fmt.Fprint(cmd.OutOrStdout(), code)

			return nil
		},
	}
}

func newEnvCmd(opts Options) *cobra.Command {
	var shell string

	cmd := &cobra.Command{
		Use:   "env",
		Short: "Print the environment changes for the current directory",
		Long: `Print the environment changes for the current directory.

This is what the shell hook evaluates before each prompt. It reads local files
only. It never uses the network and never runs anything from a manifest.`,
		Args: cobra.NoArgs,
		RunE: func(cmd *cobra.Command, _ []string) error {
			e, err := loadEnv()
			if err != nil {
				return err
			}

			dir := opts.WorkDir
			if dir == "" {
				if dir, err = os.Getwd(); err != nil {
					return err
				}
			}

			out, err := shellhook.Render(shell, e.hookChange(findProject(dir, e.config)))
			if err != nil {
				return err
			}

			fmt.Fprint(cmd.OutOrStdout(), out)

			return nil
		},
	}

	cmd.Flags().
		StringVar(&shell, "shell", "bash", "the shell to write for: "+strings.Join(shellhook.Shells, ", "))

	return cmd
}

// hookChange works out what the shell must change for project, which is empty
// outside a project. It undoes what the last run applied, using the state the
// hook keeps in the environment, and then applies what holds now.
func (e env) hookChange(project string) shellhook.Change {
	wantPath, hint := "", ""
	wantEnv := map[string]string{}

	addEnv := func(prof *profile.Profile) {
		pkgs, _ := prof.Packages()
		for _, pkg := range pkgs {
			maps.Copy(wantEnv, pkg.Env)
		}
	}

	addEnv(e.globalProfile())

	if project != "" {
		e.project = project

		switch ok, why := e.projectActive(); {
		case ok:
			wantPath = e.profile().BinDir()
			addEnv(e.profile())
		default:
			hint = "oku: " + why
		}
	}

	change := shellhook.Change{Set: map[string]string{}}

	// PATH without the entry the hook added last time, plus the entry for now.
	havePath := os.Getenv(shellhook.StatePath)
	entries := slices.DeleteFunc(
		filepath.SplitList(os.Getenv("PATH")),
		func(entry string) bool { return havePath != "" && entry == havePath },
	)

	if wantPath != "" {
		entries = append([]string{wantPath}, entries...)
	}

	if wantPath != havePath {
		change.Path = strings.Join(entries, string(os.PathListSeparator))
		change.Set[shellhook.StatePath] = wantPath
	}

	haveKeys := strings.FieldsFunc(
		os.Getenv(shellhook.StateKeys),
		func(r rune) bool { return r == ':' },
	)

	for _, name := range haveKeys {
		if _, still := wantEnv[name]; !still {
			change.Unset = append(change.Unset, name)
		}
	}

	for name, value := range wantEnv {
		if os.Getenv(name) != value || !slices.Contains(haveKeys, name) {
			change.Set[name] = value
		}
	}

	wantKeys := strings.Join(slices.Sorted(maps.Keys(wantEnv)), ":")
	if wantKeys != strings.Join(slices.Sorted(slices.Values(haveKeys)), ":") {
		change.Set[shellhook.StateKeys] = wantKeys
	}

	// A hint is shown once, until the directory or the reason changes.
	if hint != os.Getenv(shellhook.StateHint) {
		change.Set[shellhook.StateHint] = hint
		change.Hint = hint
	}

	for _, state := range []string{shellhook.StatePath, shellhook.StateKeys, shellhook.StateHint} {
		if value, set := change.Set[state]; set && value == "" {
			delete(change.Set, state)

			if os.Getenv(state) != "" {
				change.Unset = append(change.Unset, state)
			}
		}
	}

	slices.Sort(change.Unset)

	return change
}

// projectActive reports whether the hook may apply the project in e. It may when
// the user allowed this exact oku.toml and the profile matches oku.lock. The
// string says what to run otherwise.
func (e env) projectActive() (bool, string) {
	listed, err := os.ReadFile(e.listPath())
	if err != nil {
		return false, "cannot read " + e.listPath()
	}

	allowed, err := trust.ReadAllowed(e.data)
	if err != nil {
		return false, err.Error()
	}

	if !allowed.Has(e.project, digest(listed)) {
		return false, fmt.Sprintf(
			"%s is not allowed, run `oku allow` to use its programs here",
			e.listPath(),
		)
	}

	locked, err := os.ReadFile(e.lockPath())
	if err != nil || !bytes.Equal(locked, e.profile().LockSnapshotOfCurrent()) {
		return false, "this project's profile is behind its oku.lock, run `oku sync`"
	}

	return true, ""
}

func digest(data []byte) string {
	sum := sha256.Sum256(data)

	return hex.EncodeToString(sum[:])
}

func newAllowCmd(opts Options) *cobra.Command {
	return &cobra.Command{
		Use:   "allow [dir]",
		Short: "Let the shell hook apply this project's environment",
		Long: `Let the shell hook apply this project's environment.

An allow belongs to the oku.toml as it is now. After the file changes, the hook
stops and asks you to allow it again.`,
		Args: cobra.MaximumNArgs(1),
		RunE: func(cmd *cobra.Command, args []string) error {
			return setAllowed(cmd, opts, args, true)
		},
	}
}

func newDenyCmd(opts Options) *cobra.Command {
	return &cobra.Command{
		Use:   "deny [dir]",
		Short: "Stop the shell hook from applying this project's environment",
		Args:  cobra.MaximumNArgs(1),
		RunE: func(cmd *cobra.Command, args []string) error {
			return setAllowed(cmd, opts, args, false)
		},
	}
}

func setAllowed(cmd *cobra.Command, opts Options, args []string, allow bool) error {
	e, err := loadEnv()
	if err != nil {
		return err
	}

	dir := opts.WorkDir
	if len(args) == 1 {
		dir = args[0]
	}

	if dir == "" {
		if dir, err = os.Getwd(); err != nil {
			return err
		}
	}

	if dir, err = filepath.Abs(dir); err != nil {
		return err
	}

	e.project = findProject(dir, e.config)
	if e.project == "" {
		return fmt.Errorf("no oku.toml in %s or above it", dir)
	}

	allowed, err := trust.ReadAllowed(e.data)
	if err != nil {
		return err
	}

	sum := ""

	if allow {
		listed, err := os.ReadFile(e.listPath())
		if err != nil {
			return err
		}

		sum = digest(listed)
	}

	if err := allowed.Set(e.project, sum); err != nil {
		return err
	}

	verb := "allowed"
	if !allow {
		verb = "denied"
	}

	fmt.Fprintf(cmd.OutOrStdout(), "%s %s\n", verb, e.project)

	return nil
}

// displayPath writes a path under the home directory with $HOME, which every
// shell oku supports expands, so the line also works in a shared dotfiles repo.
func displayPath(path string) string {
	home, err := os.UserHomeDir()
	if err != nil || home == "" {
		return path
	}

	if rel, ok := strings.CutPrefix(path, home+string(filepath.Separator)); ok {
		return "$HOME" + string(filepath.Separator) + rel
	}

	return path
}

// setupHint says which line to add to which file, for the user's shell. It is
// empty when oku does not know the shell.
func setupHint(opts Options) string {
	shell := filepath.Base(os.Getenv("SHELL"))
	if runtime.GOOS == "windows" {
		shell = "pwsh"
	}

	if !slices.Contains(shellhook.Shells, shell) {
		return ""
	}

	return fmt.Sprintf(
		"add this line to %s, then open a new terminal:\n  %s",
		shellhook.StartupFile(shell), shellhook.Line(shell, displayPath(opts.Executable)),
	)
}

// hookLines returns the lines in the user's shell startup files that load the
// oku hook, as "file: line".
func hookLines() []string {
	home, err := os.UserHomeDir()
	if err != nil {
		return nil
	}

	var found []string

	for _, name := range []string{
		".bashrc", ".bash_profile", ".profile", ".zshrc", ".zprofile", ".config/fish/config.fish",
		".config/powershell/Microsoft.PowerShell_profile.ps1",
		"Documents/PowerShell/Microsoft.PowerShell_profile.ps1",
		"Documents/WindowsPowerShell/Microsoft.PowerShell_profile.ps1",
	} {
		file := filepath.Join(home, filepath.FromSlash(name))

		data, err := os.ReadFile(file)
		if err != nil {
			continue
		}

		for _, line := range strings.Split(string(data), "\n") {
			// The current line quotes the path, and older ones call oku by name.
			if (strings.Contains(line, `oku" hook`) || strings.Contains(line, "oku hook")) &&
				!strings.HasPrefix(strings.TrimSpace(line), "#") {
				found = append(found, file+": "+strings.TrimSpace(line))
			}
		}
	}

	return found
}
