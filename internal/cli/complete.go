package cli

import (
	"maps"
	"slices"
	"strconv"
	"strings"

	"github.com/spf13/cobra"

	"github.com/y3owk1n/oku/internal/lock"
	"github.com/y3owk1n/oku/internal/shellhook"
	"github.com/y3owk1n/oku/internal/source"
)

// Shell completions read local files only, so a tab press never waits on the
// network, and print nothing but the candidates.

// fixed completes from names, minus the ones the command line has already.
func fixed(names []string, args []string) ([]cobra.Completion, cobra.ShellCompDirective) {
	var out []cobra.Completion

	for _, name := range names {
		if !slices.Contains(args, name) {
			out = append(out, name)
		}
	}

	return out, cobra.ShellCompDirectiveNoFileComp
}

// completePackages completes the names of the packages oku.lock holds.
func completePackages(opts Options) cobra.CompletionFunc {
	return func(cmd *cobra.Command, args []string, _ string) ([]cobra.Completion, cobra.ShellCompDirective) {
		e, err := quietScopedEnv(cmd, opts)
		if err != nil {
			return nil, cobra.ShellCompDirectiveNoFileComp
		}

		locked, err := lock.Read(e.lockPath())
		if err != nil {
			return nil, cobra.ShellCompDirectiveNoFileComp
		}

		names := make([]string, 0, len(locked.Packages))
		for _, pkg := range locked.Packages {
			names = append(names, pkg.Name)
		}

		return fixed(names, args)
	}
}

// completeGenerations completes the numbers of the profile's generations, with
// the date each was made.
func completeGenerations(opts Options) cobra.CompletionFunc {
	return func(cmd *cobra.Command, args []string, _ string) ([]cobra.Completion, cobra.ShellCompDirective) {
		e, err := quietScopedEnv(cmd, opts)
		if len(args) > 0 || err != nil {
			return nil, cobra.ShellCompDirectiveNoFileComp
		}

		gens, err := e.profile().Generations()
		if err != nil {
			return nil, cobra.ShellCompDirectiveNoFileComp
		}

		var out []cobra.Completion

		for _, g := range slices.Backward(gens) {
			if !g.Current {
				out = append(out, cobra.CompletionWithDesc(strconv.Itoa(g.Number), g.Created.Local().Format("2006-01-02 15:04")))
			}
		}

		return out, cobra.ShellCompDirectiveNoFileComp | cobra.ShellCompDirectiveKeepOrder
	}
}

// completeServices completes the names of the services of installed packages.
func completeServices(opts Options) cobra.CompletionFunc {
	return func(_ *cobra.Command, args []string, _ string) ([]cobra.Completion, cobra.ShellCompDirective) {
		_, defs, _, err := globalServices(opts)
		if len(args) > 0 || err != nil {
			return nil, cobra.ShellCompDirectiveNoFileComp
		}

		return fixed(slices.Sorted(maps.Keys(defs)), args)
	}
}

// completeConfig completes from a list in config.toml, such as the aliases of
// the sources.
func completeConfig(list func(*source.Config) []string) cobra.CompletionFunc {
	return func(_ *cobra.Command, args []string, _ string) ([]cobra.Completion, cobra.ShellCompDirective) {
		e, err := loadEnv()
		if len(args) > 0 || err != nil {
			return nil, cobra.ShellCompDirectiveNoFileComp
		}

		config, err := source.Read(e.configPath())
		if err != nil {
			return nil, cobra.ShellCompDirectiveNoFileComp
		}

		return fixed(list(config), args)
	}
}

// completeShells completes the shells oku writes code for.
func completeShells(_ *cobra.Command, args []string, _ string) ([]cobra.Completion, cobra.ShellCompDirective) {
	if len(args) > 0 {
		return nil, cobra.ShellCompDirectiveNoFileComp
	}

	return fixed(shellhook.Shells, args)
}

// completeRefs completes the start of a ref: a scheme, or an alias of a source
// followed by "/". A path is left to the shell.
func completeRefs(_ *cobra.Command, _ []string, toComplete string) ([]cobra.Completion, cobra.ShellCompDirective) {
	if strings.ContainsAny(toComplete, `/:.~\`) {
		return nil, cobra.ShellCompDirectiveDefault
	}

	out := []cobra.Completion{
		"github:", "gitlab:", "codeberg:", "gitea:", "npm:", "pypi:", "go:", "cargo:",
		"cask:", "scoop:", "aqua:", "winget:",
	}

	if e, err := loadEnv(); err == nil {
		if config, err := source.Read(e.configPath()); err == nil {
			for _, alias := range slices.Sorted(maps.Keys(config.Sources)) {
				out = append(out, alias+"/")
			}
		}
	}

	return out, cobra.ShellCompDirectiveNoSpace
}
