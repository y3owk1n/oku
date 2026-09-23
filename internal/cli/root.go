// Package cli holds the cobra commands.
package cli

import (
	"context"
	"crypto/sha256"
	"encoding/hex"
	"fmt"
	"os"
	"path/filepath"
	"slices"
	"strings"
	"time"

	"github.com/spf13/cobra"
	"github.com/spf13/pflag"

	"github.com/y3owk1n/oku/internal/dirs"
	"github.com/y3owk1n/oku/internal/expose"
	"github.com/y3owk1n/oku/internal/forge"
	"github.com/y3owk1n/oku/internal/infer"
	"github.com/y3owk1n/oku/internal/list"
	"github.com/y3owk1n/oku/internal/lock"
	"github.com/y3owk1n/oku/internal/profile"
	"github.com/y3owk1n/oku/internal/ref"
	"github.com/y3owk1n/oku/internal/resolve"
	"github.com/y3owk1n/oku/internal/sandbox"
	"github.com/y3owk1n/oku/internal/service"
	"github.com/y3owk1n/oku/internal/settings"
	"github.com/y3owk1n/oku/internal/source"
	"github.com/y3owk1n/oku/internal/status"
	"github.com/y3owk1n/oku/internal/store"
	"github.com/y3owk1n/oku/internal/ui"
)

// Options are the values main and the tests pass to the command tree.
type Options struct {
	Version string
	// Executable is the path of the running binary, which "self uninstall"
	// deletes.
	Executable string
	// WorkDir is where oku looks for a project list. Empty means the working
	// directory. Tests set it.
	WorkDir string
	// Services replaces the OS's service manager. Tests set it, because a real
	// one changes the user's login session.
	Services service.Manager
	// Sleep replaces time.Sleep where a command waits, such as the check after
	// "service start". Tests set it to a no-op.
	Sleep func(time.Duration)
	// ReleaseRepo and ReleaseKey replace the GitHub repo that "oku self update"
	// reads and the minisign key it trusts. Tests set them.
	ReleaseRepo string
	ReleaseKey  string
	// Settings replaces the OS's store of per-user settings. Tests set it, because
	// the real one changes the preferences of whoever runs the tests.
	Settings settings.Store
	// SystemRoot replaces the shared store root that "oku setup --system" creates.
	SystemRoot string
	// SystemDirs replaces the directories for apps and fonts in system scope.
	SystemDirs *expose.Dirs
	// Elevate replaces how oku runs a command with administrator rights. Tests
	// set it.
	Elevate func(ctx context.Context, argv []string) error
	// Interactive overrides the check for a terminal on stdin. Tests set it.
	Interactive *bool
	// GitHubAPI and GitHubRaw replace the github.com URLs when set.
	GitHubAPI string
	GitHubRaw string
	// NPMRegistry replaces the URL of the npm registry when set.
	NPMRegistry string
}

// NewRootCmd builds the oku command tree.
func NewRootCmd(opts Options) *cobra.Command {
	root := &cobra.Command{
		Use:           "oku",
		Short:         "Set up your machine from one file: tools, dotfiles, secrets and settings",
		Version:       opts.Version,
		SilenceUsage:  true,
		SilenceErrors: true,
		PersistentPreRun: func(cmd *cobra.Command, _ []string) {
			cmd.SetContext(status.With(cmd.Context(), status.New(cmd.ErrOrStderr())))
		},
	}

	root.PersistentFlags().BoolP(
		globalFlag, "g", false, "use the global list even inside a project",
	)
	root.PersistentFlags().Bool(
		jsonFlag, false, "print data as JSON, on the commands that print data",
	)

	root.AddCommand(
		newAddCmd(opts),
		newRemoveCmd(opts),
		newSyncCmd(opts),
		newUpdateCmd(opts),
		newGenerationsCmd(opts),
		newRollbackCmd(opts),
		newGCCmd(),
		newServiceCmd(opts),
		newHookCmd(opts),
		newEnvCmd(opts),
		newAllowCmd(opts),
		newDenyCmd(opts),
		newManifestCmd(opts),
		newSourceCmd(),
		newSearchCmd(opts),
		newListCmd(opts),
		newWhyCmd(opts),
		newWhichCmd(opts),
		newInfoCmd(opts),
		newCacheCmd(),
		newKeyCmd(),
		newShellCmd(opts),
		newDoctorCmd(opts),
		newSetupCmd(opts),
		newSystemApplyCmd(opts),
		newSelfCmd(opts),
	)

	root.AddCommand(platformCommands()...)
	groupCommands(root)

	for _, path := range exclusive {
		if c, _, err := root.Find(path); err == nil && c != root {
			oneAtATime(c)
		}
	}

	// The Linux sandbox re-runs oku inside new namespaces to finish the setup.
	root.AddCommand(&cobra.Command{
		Use:    sandbox.InitCommand,
		Hidden: true,
		Args:   cobra.NoArgs,
		RunE:   func(*cobra.Command, []string) error { return sandbox.Init() },
	})

	return root
}

const globalFlag = "global"

// projectShown is the annotation a command gets once it printed its project.
const projectShown = "oku-project-shown"

// groupCommands sorts the help into sections, so that a reader finds the
// command for a job without reading all of them. Colour and headings come from
// the Style of the writer the help goes to.
func groupCommands(root *cobra.Command) {
	groups := []struct {
		title    string
		commands []string
	}{
		{
			"Packages",
			[]string{"add", "remove", "sync", "update", "list", "info", "why", "which", "shell"},
		},
		{"Finding packages", []string{"search", "source"}},
		{"Generations", []string{"generations", "rollback", "gc"}},
		{"Projects and shells", []string{"hook", "env", "allow", "deny"}},
		{"Services and caches", []string{"service", "cache", "key"}},
		{"Publishing", []string{"manifest"}},
		{"oku itself", []string{"doctor", "setup", "self", "completion", "help"}},
	}

	groupOf := map[string]string{}

	for _, g := range groups {
		root.AddGroup(&cobra.Group{ID: g.title, Title: g.title})

		for _, name := range g.commands {
			groupOf[name] = g.title
		}
	}

	for _, c := range root.Commands() {
		c.GroupID = groupOf[c.Name()]
	}

	root.SetHelpCommandGroupID(groupOf["help"])
	root.SetCompletionCommandGroupID(groupOf["completion"])

	style := func() ui.Style { return ui.For(root.OutOrStdout()) }

	cobra.AddTemplateFunc("heading", func(text string) string {
		s := style()
		if s.On() {
			return s.Heading(text)
		}

		return text + ":"
	})
	cobra.AddTemplateFunc("dim", func(text string) string { return style().Dim(text) })
	cobra.AddTemplateFunc("name", func(text string) string { return style().Accent(text) })
	cobra.AddTemplateFunc("flags", func(flags *pflag.FlagSet) string {
		return flagUsages(style(), flags)
	})
	cobra.AddTemplateFunc("fit", func(text string) string { return fit(style(), text) })

	// The commands stay in the order AddCommand gave them, which puts the daily
	// ones first in each section.
	cobra.EnableCommandSorting = false

	root.SetUsageTemplate(usageTemplate)
	root.SetHelpTemplate(helpTemplate)
}

// flagUsages lists the flags of a help page. A pipe, and a terminal wide
// enough, get pflag's list. A narrower terminal gets each description wrapped
// beside its flag, or under it when two columns do not fit.
func flagUsages(s ui.Style, flags *pflag.FlagSet) string {
	usages := flags.FlagUsages()
	if s.Width() <= 0 || !slices.ContainsFunc(strings.Split(usages, "\n"), func(line string) bool {
		return len([]rune(line)) > s.Width()
	}) {
		return usages
	}

	type entry struct{ name, usage string }

	var (
		entries []entry
		width   int
	)

	flags.VisitAll(func(f *pflag.Flag) {
		if f.Hidden {
			return
		}

		name := "      --" + f.Name
		if f.Shorthand != "" && f.ShorthandDeprecated == "" {
			name = "  -" + f.Shorthand + ", --" + f.Name
		}

		kind, usage := pflag.UnquoteUsage(f)
		if kind != "" {
			name += " " + kind
		}

		if !slices.Contains([]string{"", "false", "0", "[]"}, f.DefValue) {
			usage += " (default " + f.DefValue + ")"
		}

		entries = append(entries, entry{name, usage})
		width = max(width, len(name))
	})

	// A description narrower than this reads better under its flag.
	const minUsage = 24

	var b strings.Builder

	for _, e := range entries {
		if width+gapColumns+minUsage <= s.Width() {
			pad := strings.Repeat(" ", width+gapColumns)
			fmt.Fprintf(&b, "%-*s%s\n", width+gapColumns, e.name,
				strings.TrimPrefix(s.Wrap(pad+e.usage, width+gapColumns), pad))

			continue
		}

		fmt.Fprintf(&b, "%s\n%s\n", e.name, s.Wrap(strings.Repeat(" ", usageIndent)+e.usage, usageIndent))
	}

	return b.String()
}

const (
	// gapColumns is the space between a flag and its description.
	gapColumns = 3
	// usageIndent is where a description under its flag starts.
	usageIndent = 10
)

// helpWidth is the width the long help is written at.
const helpWidth = 80

// fit reflows the long help of a command for a terminal narrower than the
// width it is written at. A paragraph joins into one line and wraps again. An
// indented line, such as an example, and a list item keep their own line.
func fit(s ui.Style, text string) string {
	if s.Width() <= 0 || s.Width() >= helpWidth {
		return text
	}

	paragraphs := strings.Split(text, "\n\n")

	for i, p := range paragraphs {
		var lines []string

		for _, line := range strings.Split(p, "\n") {
			if n := len(lines); n > 0 && !ownLine(line) && !strings.HasPrefix(lines[n-1], " ") {
				lines[n-1] += " " + line

				continue
			}

			lines = append(lines, line)
		}

		paragraphs[i] = s.Wrap(strings.Join(lines, "\n"), 0)
	}

	return strings.Join(paragraphs, "\n\n")
}

// ownLine reports whether a line of help stays on a line of its own: an
// indented one, or an item of a list.
func ownLine(line string) bool {
	return strings.HasPrefix(line, " ") || strings.HasPrefix(line, "- ")
}

// helpTemplate is cobra's default, with the long help fitted to the terminal.
const helpTemplate = `{{with (or .Long .Short)}}{{fit . | trimTrailingWhitespaces}}

{{end}}{{if or .Runnable .HasSubCommands}}{{.UsageString}}{{end}}`

// usageTemplate is cobra's default with styled headings, coloured command
// names and grouped commands.
const usageTemplate = `{{heading "Usage"}}{{if .Runnable}}
  {{.UseLine}}{{end}}{{if .HasAvailableSubCommands}}
  {{.CommandPath}} [command]{{end}}{{if gt (len .Aliases) 0}}

{{heading "Aliases"}}
  {{.NameAndAliases}}{{end}}{{if .HasExample}}

{{heading "Examples"}}
{{.Example}}{{end}}{{if .HasAvailableSubCommands}}{{$cmds := .Commands}}{{if eq (len .Groups) 0}}

{{heading "Commands"}}{{range $cmds}}{{if (or .IsAvailableCommand (eq .Name "help"))}}
  {{name (rpad .Name .NamePadding)}} {{.Short}}{{end}}{{end}}{{else}}{{range $group := .Groups}}

{{heading $group.Title}}{{range $cmds}}{{if (and (eq .GroupID $group.ID) (or .IsAvailableCommand (eq .Name "help")))}}
  {{name (rpad .Name .NamePadding)}} {{.Short}}{{end}}{{end}}{{end}}{{if not .AllChildCommandsHaveGroup}}

{{heading "Other"}}{{range $cmds}}{{if (and (eq .GroupID "") (or .IsAvailableCommand (eq .Name "help")))}}
  {{name (rpad .Name .NamePadding)}} {{.Short}}{{end}}{{end}}{{end}}{{end}}{{end}}{{if .HasAvailableLocalFlags}}

{{heading "Flags"}}
{{flags .LocalFlags | trimTrailingWhitespaces}}{{end}}{{if .HasAvailableInheritedFlags}}

{{heading "Global flags"}}
{{flags .InheritedFlags | trimTrailingWhitespaces}}{{end}}{{if .HasHelpSubCommands}}

{{heading "Additional help topics"}}{{range .Commands}}{{if .IsAdditionalHelpTopicCommand}}
  {{rpad .CommandPath .CommandPathPadding}} {{.Short}}{{end}}{{end}}{{end}}{{if .HasAvailableSubCommands}}

{{dim (printf "Use \"%s [command] --help\" for more information about a command." .CommandPath)}}{{end}}
`

// env is where oku keeps its files on this machine, and which list it acts on.
type env struct {
	config string
	data   string
	cache  string
	// root is the directory that contains the store. It is data, or the shared
	// root from "oku setup --system".
	root string
	// project is the directory of the project list in use, or empty for the
	// global list.
	project string
	// runtimes holds the [runtimes] of the list in use and its includes, after a
	// command loaded it. config.toml's [runtimes] is the fallback.
	runtimes map[string]string
}

func loadEnv() (env, error) {
	var (
		e   env
		err error
	)

	if e.config, err = dirs.Config(); err != nil {
		return e, err
	}

	if e.data, err = dirs.Data(); err != nil {
		return e, err
	}

	if e.cache, err = dirs.Cache(); err != nil {
		return e, err
	}

	config, err := source.Read(filepath.Join(e.config, source.FileName))
	if err != nil {
		return e, err
	}

	e.root = e.data
	if config.StoreRoot != "" {
		e.root = config.StoreRoot
	}

	return e, nil
}

// scopedEnv is loadEnv for commands that act on a list. Inside a directory tree
// with an oku.toml they act on that project, unless --global is set.
func scopedEnv(cmd *cobra.Command, opts Options) (env, error) {
	e, err := loadEnv()
	if err != nil {
		return e, err
	}

	if global, _ := cmd.Flags().GetBool(globalFlag); global {
		return e, nil
	}

	dir := opts.WorkDir
	if dir == "" {
		if dir, err = os.Getwd(); err != nil {
			return e, err
		}
	}

	e.project = findProject(dir, e.config)

	// A command can load its env more than once, and says which project once.
	if e.project != "" && cmd.Annotations[projectShown] == "" {
		if cmd.Annotations == nil {
			cmd.Annotations = map[string]string{}
		}

		cmd.Annotations[projectShown] = "yes"
		s := ui.For(cmd.ErrOrStderr())
		fmt.Fprintln(cmd.ErrOrStderr(), s.Dim("project "+s.Home(e.project)))
	}

	return e, nil
}

// findProject returns the nearest directory at or above dir that holds an
// oku.toml. The config directory holds the global list and is not a project.
func findProject(dir, config string) string {
	for {
		if _, err := os.Stat(filepath.Join(dir, list.FileName)); err == nil && dir != config {
			return dir
		}

		parent := filepath.Dir(dir)
		if parent == dir {
			return ""
		}

		dir = parent
	}
}

func (e env) listDir() string {
	if e.project != "" {
		return e.project
	}

	return e.config
}

func (e env) listPath() string {
	return filepath.Join(e.listDir(), list.FileName)
}

func (e env) lockPath() string {
	return filepath.Join(e.listDir(), lock.FileName)
}

// profile returns the profile of the list in use. A project profile is named
// after a hash of its directory, so moving a project gives it a new profile.
func (e env) profile() *profile.Profile {
	if e.project == "" {
		return e.globalProfile()
	}

	sum := sha256.Sum256([]byte(e.project))

	return profile.Open(e.data, "project-"+hex.EncodeToString(sum[:])[:12])
}

func (e env) store() *store.Store {
	return store.New(e.root, e.cache)
}

// stores returns every store that can hold packages. After "oku setup
// --system", older generations still point into the store in the data directory.
func (e env) stores() []*store.Store {
	if e.root == e.data {
		return []*store.Store{e.store()}
	}

	return []*store.Store{e.store(), store.New(e.data, e.cache)}
}

func (e env) fetcher(opts Options) *ref.Fetcher {
	f := ref.NewFetcher(e.cache)
	f.Hosts.GitHubAPI, f.Hosts.GitHubRaw = opts.GitHubAPI, opts.GitHubRaw

	return f
}

func (e env) resolver(opts Options) *resolve.Resolver {
	return &resolve.Resolver{Hosts: e.fetcher(opts).Hosts, NPM: opts.NPMRegistry}
}

func (e env) inferrer(opts Options) *infer.Inferrer {
	return &infer.Inferrer{
		Hosts: e.fetcher(opts).Hosts,
		Inspect: func(ctx context.Context, url string, auth forge.Auth) ([]infer.File, error) {
			return e.store().As(auth).Inspect(ctx, url)
		},
	}
}

func (e env) globalProfile() *profile.Profile {
	return profile.Open(e.data, "global")
}
