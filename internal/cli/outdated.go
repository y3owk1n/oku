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

For each package, oku asks its version source for two versions. The newest is
the newest that the version in oku.toml allows, and oku update takes it. The
latest is the newest release. To take a latest beyond the newest, change the
version in oku.toml. oku downloads no package and changes nothing.`,
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

// staleness is one package of the lock, the newest version that the list
// allows, and the latest release.
type staleness struct {
	pkg            lock.Package
	newest, latest string
	err            error
}

func (e env) outdated(cmd *cobra.Command, opts Options) error {
	locked, err := lock.Read(e.lockPath())
	if err != nil {
		return err
	}

	// A version in the list limits what oku update takes.
	all, err := e.loadList(cmd.Context(), opts, locked, false)
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

			newest, latest, err := e.newest(cmd.Context(), opts, pkg, all.packages[pkg.Name].ref.Version)
			found[i] = staleness{pkg: pkg, newest: newest, latest: latest, err: err}
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
		case f.newest != f.pkg.Version || f.latest != f.pkg.Version:
			stale = append(stale, f)
		}
	}

	if err := printStale(cmd, stale, len(locked.Packages)); err != nil {
		return err
	}

	return errors.Join(failed...)
}

// newest returns the newest version of pkg that want allows, as oku update
// would pick it, and the latest release.
func (e env) newest(ctx context.Context, opts Options, pkg lock.Package, want string) (string, string, error) {
	r, err := ref.ParseIn(e.listDir(), pkg.Ref)
	if err != nil {
		return "", "", err
	}

	// oku wrote an inferred manifest, and the lock holds its text. Inferring it
	// again would open release assets, which the version does not need.
	text := []byte(pkg.Manifest)
	if !pkg.Inferred || pkg.Manifest == "" {
		fetched, err := e.fetcher(opts).Fetch(ctx, r, "", ref.Manifest)
		if err != nil {
			return "", "", err
		}

		text = fetched.Data
	}

	m, err := manifest.Parse(text, r.String())
	if err != nil {
		return "", "", err
	}

	latest, err := e.resolver(opts).Pick(ctx, m.Version, "")
	if err != nil || want == "" {
		return latest.Version, latest.Version, err
	}

	newest, err := e.resolver(opts).Pick(ctx, m.Version, want)
	if err != nil {
		return "", "", err
	}

	return newest.Version, latest.Version, nil
}

func printStale(cmd *cobra.Command, stale []staleness, all int) error {
	if wantJSON(cmd) {
		type row struct {
			Name    string `json:"name"`
			Version string `json:"version"`
			Newest  string `json:"newest"`
			Latest  string `json:"latest"`
			Ref     string `json:"ref"`
		}

		rows := []row{}
		for _, f := range stale {
			rows = append(rows, row{f.pkg.Name, f.pkg.Version, f.newest, f.latest, f.pkg.Ref})
		}

		return printJSON(cmd, rows)
	}

	out := cmd.OutOrStdout()
	s := ui.For(out)

	if len(stale) == 0 {
		fmt.Fprintln(out, s.Done(fmt.Sprintf("all %d packages are at their newest version", all)))

		return nil
	}

	tab := s.Table("name", "locked", "newest", "latest", "ref")
	for _, f := range stale {
		tab.Styled([]string{f.pkg.Name, f.pkg.Version, f.newest, f.latest, s.Home(f.pkg.Ref)}, s.Bold, s.Dim, nil, nil, s.Dim)
	}

	if err := tab.Write(out); err != nil {
		return err
	}

	hint(out, "`oku update` takes the newest versions. To take a latest beyond them, change its version in oku.toml")

	return nil
}
