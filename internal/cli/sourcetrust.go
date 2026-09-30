package cli

import (
	"errors"
	"fmt"
	"maps"
	"slices"
	"strings"

	"github.com/spf13/cobra"

	"github.com/y3owk1n/oku/internal/list"
	"github.com/y3owk1n/oku/internal/ref"
	"github.com/y3owk1n/oku/internal/source"
	"github.com/y3owk1n/oku/internal/status"
	"github.com/y3owk1n/oku/internal/trust"
)

// untrustedError reports a project that names origins the user has not
// trusted.
type untrustedError struct {
	project string
	origins map[string][]string
}

func (u untrustedError) Error() string {
	return fmt.Sprintf(
		"%s installs from sources you have not trusted:\n%s"+
			"run `oku allow` to trust them, or pass --yes to oku sync",
		u.project, u.list(),
	)
}

// list names each origin and the entries of oku.toml that use it, one per line.
func (u untrustedError) list() string {
	var b strings.Builder

	for _, origin := range slices.Sorted(maps.Keys(u.origins)) {
		fmt.Fprintf(&b, "  %s for %s\n", origin, strings.Join(u.origins[origin], ", "))
	}

	return b.String()
}

// untrusted returns the origins that the project's own oku.toml names in its
// packages, includes and runtimes and that the user has not trusted, each with
// the entries that name it. The global list is the user's own, and what a
// trusted ref names in turn, such as a dep or an include of an include, is
// trusted with it.
func (e env) untrusted() (map[string][]string, error) {
	if e.project == "" {
		return nil, nil
	}

	own, err := list.Read(e.listPath())
	if err != nil {
		return nil, err
	}

	config, err := source.Read(e.configPath())
	if err != nil {
		return nil, err
	}

	sources, err := trust.ReadSources(e.data)
	if err != nil {
		return nil, err
	}

	trusted := func(s string) bool {
		return s != "" && (slices.Contains(config.Trust.Sources, s) || sources.Has(s))
	}

	found := map[string][]string{}
	check := func(what, s string) error {
		r, err := ref.ParseIn(e.project, s)
		if err != nil {
			return fmt.Errorf("%s: %w", what, err)
		}

		if origin := r.Origin(); origin != "" && !trusted(origin) && !trusted(r.Repo()) {
			found[origin] = append(found[origin], what)
		}

		return nil
	}

	for _, name := range slices.Sorted(maps.Keys(own.Packages)) {
		if err := check(name, own.Packages[name].Ref); err != nil {
			return nil, err
		}
	}

	for _, include := range own.Include {
		if err := check("include "+include, include); err != nil {
			return nil, err
		}
	}

	for _, name := range slices.Sorted(maps.Keys(own.Runtimes)) {
		if err := check("runtime "+name, own.Runtimes[name].Ref); err != nil {
			return nil, err
		}
	}

	if len(found) == 0 {
		return nil, nil
	}

	return found, nil
}

// trustProject asks the user to trust the origins the project names that are
// not trusted yet, and records the answer. yes trusts them without asking, and
// without a terminal oku refuses.
func (e env) trustProject(cmd *cobra.Command, opts Options, yes bool) error {
	origins, err := e.untrusted()
	if err != nil || origins == nil {
		return err
	}

	refused := untrustedError{project: e.project, origins: origins}

	if !yes {
		if !interactive(cmd, opts) {
			return refused
		}

		defer status.Pause(cmd.Context())()

		terminal := cmd.ErrOrStderr()
		fmt.Fprintf(terminal, "%s installs from sources you have not trusted:\n%s", e.project, refused.list())

		if !confirm(cmd.InOrStdin(), terminal, "trust them?") {
			return errors.New("not trusted, nothing was installed")
		}
	}

	return e.recordTrust(cmd, slices.Sorted(maps.Keys(origins)))
}

// trustTyped trusts the origin of a ref the user typed.
func (e env) trustTyped(r ref.Ref) error {
	sources, err := trust.ReadSources(e.data)
	if err != nil {
		return err
	}

	return sources.Add(r.Origin())
}

// recordTrust saves origins as trusted and says so.
func (e env) recordTrust(cmd *cobra.Command, origins []string) error {
	sources, err := trust.ReadSources(e.data)
	if err != nil {
		return err
	}

	if err := sources.Add(origins...); err != nil {
		return err
	}

	for _, origin := range origins {
		finished(cmd.ErrOrStderr(), "trusted %s", origin)
	}

	return nil
}
