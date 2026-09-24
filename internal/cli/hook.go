package cli

import (
	"bytes"
	"encoding/json"
	"errors"
	"fmt"
	"io/fs"
	"maps"
	"os"
	"path/filepath"
	"runtime"
	"slices"
	"strings"

	"github.com/spf13/cobra"

	"github.com/y3owk1n/oku/internal/list"
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
	var (
		shell  string
		dotenv bool
	)

	cmd := &cobra.Command{
		Use:   "env",
		Short: "Print the environment changes for the current directory",
		Long: `Print the environment changes for the current directory.

This is what the shell hook evaluates before each prompt. It reads local files
only. It never uses the network and never runs anything from a manifest.

With --json or --dotenv, oku prints every variable the directory sets, for an
editor or a tool that reads an environment file. JSON gives an unset variable
as null, and a .env file leaves it out.`,
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

			project := findProject(dir, e.config)
			if wantJSON(cmd) && dotenv {
				return errors.New("--json and --dotenv print different formats, pick one")
			}

			if !wantJSON(cmd) && !dotenv {
				out, err := shellhook.Render(shell, e.hookChange(project))
				if err != nil {
					return err
				}

				fmt.Fprint(cmd.OutOrStdout(), out)

				return nil
			}

			final, _, problems := e.dirEnv(project, readHookState())
			for _, problem := range problems {
				fmt.Fprintln(cmd.ErrOrStderr(), "oku: "+problem)
			}

			if wantJSON(cmd) {
				return printJSON(cmd, final)
			}

			for _, name := range slices.Sorted(maps.Keys(final)) {
				if value := final[name]; value != nil {
					fmt.Fprintln(cmd.OutOrStdout(), name+"="+dotenvQuote(*value))
				}
			}

			return nil
		},
	}

	cmd.Flags().BoolVar(&dotenv, "dotenv", false, "print the variables as a .env file")

	cmd.Flags().
		StringVar(&shell, "shell", "bash", "the shell to write for: "+strings.Join(shellhook.Shells, ", "))

	return cmd
}

// hookChange works out what the shell must change for project, which is empty
// outside a project. It undoes what the last run applied, using the state the
// hook keeps in the environment, and then applies what holds now. A variable
// the hook stops setting gets back the value it had before.
func (e env) hookChange(project string) shellhook.Change {
	state := readHookState()
	final, prepended, problems := e.dirEnv(project, state)
	change := shellhook.Change{Set: map[string]string{}}
	saved := map[string]*string{}

	apply := func(name string, value *string) {
		current, isSet := os.LookupEnv(name)

		switch {
		case value == nil && isSet:
			change.Unset = append(change.Unset, name)
		case value != nil && (!isSet || current != *value) && name == "PATH":
			change.Path = *value
		case value != nil && (!isSet || current != *value):
			change.Set[name] = *value
		}
	}

	for name, value := range final {
		if _, only := prepended[name]; !only {
			saved[name] = orNil(state.base(name))
		}

		apply(name, value)
	}

	// A variable the hook set before and does not now gets its old value back.
	for name := range state.saved {
		if _, still := final[name]; !still {
			apply(name, orNil(state.base(name)))
		}
	}

	for name := range state.added {
		if _, still := final[name]; !still {
			old, _ := state.base(name)
			apply(name, &old)
		}
	}

	for name, value := range map[string]any{shellhook.StateSaved: saved, shellhook.StateAdded: prepended} {
		encoded := ""
		if data, _ := json.Marshal(value); string(data) != "{}" && string(data) != "null" {
			encoded = string(data)
		}

		keepState(&change, name, encoded)
	}

	// This run read the state of an older oku, so the old variables go.
	keepState(&change, shellhook.StatePath, "")
	keepState(&change, shellhook.StateKeys, "")

	// A hint is shown once, until the directory or the reason changes.
	hint := ""
	if len(problems) > 0 {
		hint = "oku: " + strings.Join(problems, "\noku: ")
	}

	if hint != os.Getenv(shellhook.StateHint) {
		keepState(&change, shellhook.StateHint, hint)
		change.Hint = hint
	}

	slices.Sort(change.Unset)

	return change
}

// dirEnv returns the value of each variable the hook sets in project, nil for
// one it unsets, with PATH in full, and the entries it only puts in front of a
// list variable. It also returns what keeps a part from applying, such as a
// project that is not allowed.
func (e env) dirEnv(project string, state hookState) (map[string]*string, map[string][]string, []string) {
	active, why := false, ""
	if project != "" {
		e.project = project
		active, why = e.projectActive()
	}

	want, problems := e.wantedEnv(project, active, state.base)
	if why != "" {
		problems = append([]string{why}, problems...)
	}

	final, prepended := want.finalValues(state.base)

	return final, prepended, problems
}

// orNil returns a pointer to value, or nil when ok is false.
func orNil(value string, ok bool) *string {
	if !ok {
		return nil
	}

	return &value
}

// dotenvQuote quotes value for a .env file. Single quotes keep it as it is in
// every reader, and a value that holds one or a newline takes double quotes.
func dotenvQuote(value string) string {
	if !strings.ContainsAny(value, "'\n") {
		return "'" + value + "'"
	}

	return `"` + strings.NewReplacer(`\`, `\\`, `"`, `\"`, "\n", `\n`, "$", `\$`).Replace(value) + `"`
}

// keepState sets the state variable name to value, or removes it when value is
// empty.
func keepState(change *shellhook.Change, name, value string) {
	current, isSet := os.LookupEnv(name)

	switch {
	case value == "" && isSet:
		change.Unset = append(change.Unset, name)
	case value != "" && current != value:
		change.Set[name] = value
	}
}

// projectActive reports whether the hook may apply the project in e. It may when
// the user allowed this exact oku.toml, with the .env files it loads that git
// tracks, and the profile matches oku.lock. The
// string says what to run otherwise.
func (e env) projectActive() (bool, string) {
	allowed, err := trust.ReadAllowed(e.data)
	if err != nil {
		return false, err.Error()
	}

	holds, err := e.allowHolds(allowed)
	if err != nil {
		return false, err.Error()
	}

	if !holds {
		return false, fmt.Sprintf(
			"%s is not allowed, run `oku allow` to use its programs here",
			e.listPath(),
		)
	}

	if !e.projectSynced() {
		return false, "this project's profile is behind its oku.lock, run `oku sync`"
	}

	return true, ""
}

// projectSynced reports whether the profile of the project in e matches its
// oku.lock. A project that lists no packages has no lock and needs none.
func (e env) projectSynced() bool {
	snapshot := e.profile().LockSnapshotOfCurrent()

	locked, err := os.ReadFile(e.lockPath())
	if errors.Is(err, fs.ErrNotExist) && snapshot == nil {
		l, err := list.Read(e.listPath())

		return err == nil && len(l.Packages) == 0 && len(l.Include) == 0
	}

	return err == nil && bytes.Equal(locked, snapshot)
}

func newAllowCmd(opts Options) *cobra.Command {
	return &cobra.Command{
		Use:   "allow [dir]",
		Short: "Let the shell hook apply this project's environment",
		Long: `Let the shell hook apply this project's environment.

An allow belongs to the oku.toml as it is now, and to each .env file it loads
that git tracks, since a pull can change those. After one of them changes, the
hook stops and asks you to allow it again. A .env file that git does not track
is yours, and you change it without a new allow.`,
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

	if !allow {
		if err := allowed.Set(e.project, nil); err != nil {
			return err
		}

		fmt.Fprintf(cmd.OutOrStdout(), "denied %s\n", e.project)

		return nil
	}

	record, tracked, err := e.newAllow()
	if err != nil {
		return err
	}

	if err := allowed.Set(e.project, record); err != nil {
		return err
	}

	fmt.Fprintf(cmd.OutOrStdout(), "allowed %s\n", e.project)

	for _, path := range tracked {
		fmt.Fprintf(cmd.OutOrStdout(), "  git tracks %s, so a change to it needs a new allow\n", path)
	}

	for _, u := range record.Untracked {
		fmt.Fprintf(cmd.OutOrStdout(), "  git does not track %s, so it is yours to change\n", u.Path)
	}

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
