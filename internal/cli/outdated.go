package cli

import (
	"context"
	"errors"
	"fmt"
	"slices"
	"strings"
	"sync"

	"github.com/spf13/cobra"

	"github.com/y3owk1n/oku/internal/lock"
	"github.com/y3owk1n/oku/internal/manifest"
	"github.com/y3owk1n/oku/internal/ref"
	"github.com/y3owk1n/oku/internal/ui"
)

func newOutdatedCmd(opts Options) *cobra.Command {
	return &cobra.Command{
		Use:   "outdated",
		Short: "List the packages that have a newer version than oku.lock pins",
		Long: `List the packages that have a newer version than oku.lock pins.

For each package, oku asks its version source which version is the newest.
It downloads no package and changes nothing. oku update takes the new versions.`,
		Args: cobra.NoArgs,
		RunE: func(cmd *cobra.Command, _ []string) error {
			e, err := scopedEnv(cmd, opts)
			if err != nil {
				return err
			}

			return e.outdated(cmd, opts)
		},
	}
}

// staleness is one package of the lock and the newest version of it.
type staleness struct {
	pkg    lock.Package
	newest string
	err    error
}

func (e env) outdated(cmd *cobra.Command, opts Options) error {
	locked, err := lock.Read(e.lockPath())
	if err != nil {
		return err
	}

	found := make([]staleness, len(locked.Packages))

	// Each package asks its own host, so they ask at once, a few at a time.
	var wg sync.WaitGroup

	limit := make(chan struct{}, 8)

	for i, pkg := range locked.Packages {
		wg.Go(func() {
			limit <- struct{}{}
			defer func() { <-limit }()

			newest, err := e.newest(cmd.Context(), opts, pkg)
			found[i] = staleness{pkg: pkg, newest: newest, err: err}
		})
	}

	wg.Wait()

	slices.SortFunc(found, func(a, b staleness) int { return strings.Compare(a.pkg.Name, b.pkg.Name) })

	var (
		failed []error
		stale  []staleness
	)

	for _, f := range found {
		switch {
		case f.err != nil:
			failed = append(failed, fmt.Errorf("%s: %w", f.pkg.Name, f.err))
		case f.newest != f.pkg.Version:
			stale = append(stale, f)
		}
	}

	if err := printStale(cmd, stale, len(locked.Packages)); err != nil {
		return err
	}

	return errors.Join(failed...)
}

// newest returns the newest version of pkg, as oku update would pick it.
func (e env) newest(ctx context.Context, opts Options, pkg lock.Package) (string, error) {
	r, err := ref.ParseIn(e.listDir(), pkg.Ref)
	if err != nil {
		return "", err
	}

	// oku wrote an inferred manifest, and the lock holds its text. Inferring it
	// again would open release assets, which the version does not need.
	text := []byte(pkg.Manifest)
	if !pkg.Inferred || pkg.Manifest == "" {
		fetched, err := e.fetcher(opts).Fetch(ctx, r, "", ref.Manifest)
		if err != nil {
			return "", err
		}

		text = fetched.Data
	}

	m, err := manifest.Parse(text, r.String())
	if err != nil {
		return "", err
	}

	release, err := e.resolver(opts).Pick(ctx, m.Version, "")
	if err != nil {
		return "", err
	}

	return release.Version, nil
}

func printStale(cmd *cobra.Command, stale []staleness, all int) error {
	if wantJSON(cmd) {
		type row struct {
			Name    string `json:"name"`
			Version string `json:"version"`
			Newest  string `json:"newest"`
			Ref     string `json:"ref"`
		}

		rows := []row{}
		for _, f := range stale {
			rows = append(rows, row{f.pkg.Name, f.pkg.Version, f.newest, f.pkg.Ref})
		}

		return printJSON(cmd, rows)
	}

	out := cmd.OutOrStdout()
	s := ui.For(out)

	if len(stale) == 0 {
		fmt.Fprintln(out, s.Done(fmt.Sprintf("all %d packages are at their newest version", all)))

		return nil
	}

	tab := s.Table("name", "locked", "newest", "ref")
	for _, f := range stale {
		tab.Styled([]string{f.pkg.Name, f.pkg.Version, f.newest, s.Home(f.pkg.Ref)}, s.Bold, s.Dim, nil, s.Dim)
	}

	if err := tab.Write(out); err != nil {
		return err
	}

	hint(out, "`oku update` takes the newest versions, `oku update <name>` one package")

	return nil
}
