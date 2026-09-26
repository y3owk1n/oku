package cli

import (
	"context"
	"errors"
	"fmt"
	"maps"
	"slices"
	"strings"
	"sync"
	"time"

	"github.com/spf13/cobra"

	"github.com/y3owk1n/oku/internal/lock"
	"github.com/y3owk1n/oku/internal/manifest"
	"github.com/y3owk1n/oku/internal/platform"
	"github.com/y3owk1n/oku/internal/ref"
	"github.com/y3owk1n/oku/internal/resolve"
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
// allows and that is old enough, and the latest release.
type staleness struct {
	pkg            lock.Package
	newest, latest string
	// waiting is the newest version that the list allows and that is newer than
	// the minimum release age, or the zero Release.
	waiting resolve.Release
	age     time.Duration
	err     error
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

	n, err := parallel()
	if err != nil {
		return err
	}

	found := make([]staleness, len(locked.Packages))

	// Each package asks its own host, so they ask at once, a few at a time.
	var wg sync.WaitGroup

	limit := make(chan struct{}, n)

	host := platform.Host().String()

	for i, pkg := range locked.Packages {
		// A package whose artifacts find their own versions is at the version of
		// this machine's platform.
		pkg.Version = pkg.VersionOn(host)

		age, err := releaseAge(cmd, all.own, all.packages[pkg.Name].entry)
		if err != nil {
			return fmt.Errorf("%s: %w", pkg.Name, err)
		}

		wg.Go(func() {
			limit <- struct{}{}
			defer func() { <-limit }()

			found[i] = e.newest(cmd.Context(), opts, pkg, all.packages[pkg.Name].ref.Version, age)
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
		case f.newest != f.pkg.Version || f.latest != f.pkg.Version || f.waiting.Version != "":
			stale = append(stale, f)
		}
	}

	if err := printStale(cmd, stale, len(locked.Packages)); err != nil {
		return err
	}

	return errors.Join(failed...)
}

// newest finds the newest version of pkg that want allows and age lets in, as
// oku update would pick it, the one that waits for age, and the latest release.
func (e env) newest(
	ctx context.Context,
	opts Options,
	pkg lock.Package,
	want string,
	age time.Duration,
) staleness {
	newest, latest, waiting, err := e.newestOf(ctx, opts, pkg, want, age)

	return staleness{pkg: pkg, newest: newest, latest: latest, waiting: waiting, age: age, err: err}
}

func (e env) newestOf(
	ctx context.Context,
	opts Options,
	pkg lock.Package,
	want string,
	age time.Duration,
) (string, string, resolve.Release, error) {
	r, err := ref.ParseIn(e.listDir(), pkg.Ref)
	if err != nil {
		return "", "", resolve.Release{}, err
	}

	// oku wrote an inferred manifest, and the lock holds its text. Inferring it
	// again would open release assets, which the version does not need.
	text := []byte(pkg.Manifest)
	if !pkg.Inferred || pkg.Manifest == "" {
		fetched, err := e.fetcher(opts).Fetch(ctx, r, "", ref.Manifest)
		if err != nil {
			return "", "", resolve.Release{}, err
		}

		text = fetched.Data
	}

	m, err := manifest.Parse(text, r.String())
	if err != nil {
		return "", "", resolve.Release{}, err
	}

	source := m.Version

	if m.PerArtifact() {
		// The version of a package pinned only for other platforms is that of the
		// first one, as VersionOn gives it.
		at := platform.Host()
		if pkg.Platforms[at.String()].Version == "" {
			for _, key := range slices.Sorted(maps.Keys(pkg.Platforms)) {
				if p, err := platform.Parse(key); err == nil && pkg.Platforms[key].Version != "" {
					at = p

					break
				}
			}
		}

		i := slices.IndexFunc(m.Artifacts, func(a manifest.Artifact) bool { return a.Match.Matches(at) })
		if i < 0 {
			return "", "", resolve.Release{}, fmt.Errorf("%s has no artifact for %s", m.Package.Name, at)
		}

		source = *m.Artifacts[i].Version
	}

	latest, err := e.resolver(opts).Pick(ctx, source, "")
	if err != nil {
		return "", "", resolve.Release{}, err
	}

	// When every version it allows waits for age, or the lock holds a newer one,
	// update keeps the locked one.
	newest, waiting, err := e.resolverAged(opts, age).PickWaiting(ctx, source, want)
	if waiting.Version != "" && (errors.Is(err, resolve.ErrTooNew) ||
		err == nil && resolve.Compare(pkg.Version, newest.Version) > 0) {
		newest, err = resolve.Release{Version: pkg.Version}, nil
	}

	if err != nil {
		return "", "", resolve.Release{}, err
	}

	if waiting.Version != "" && resolve.Compare(waiting.Version, pkg.Version) <= 0 {
		waiting = resolve.Release{}
	}

	return newest.Version, latest.Version, waiting, nil
}

func printStale(cmd *cobra.Command, stale []staleness, all int) error {
	if wantJSON(cmd) {
		type row struct {
			Name    string `json:"name"`
			Version string `json:"version"`
			Newest  string `json:"newest"`
			Latest  string `json:"latest"`
			Ref     string `json:"ref"`
			// Waiting is the newest version that waits for the minimum release age,
			// and WaitsUntil the time it passes it.
			Waiting    string     `json:"waiting,omitempty"`
			WaitsUntil *time.Time `json:"waits_until,omitempty"`
		}

		rows := []row{}
		for _, f := range stale {
			r := row{
				Name: f.pkg.Name, Version: f.pkg.Version, Newest: f.newest, Latest: f.latest,
				Ref: f.pkg.Ref, Waiting: f.waiting.Version,
			}

			if f.waiting.Version != "" {
				until := f.waiting.Published.Add(f.age)
				r.WaitsUntil = &until
			}

			rows = append(rows, r)
		}

		return printJSON(cmd, rows)
	}

	out := cmd.OutOrStdout()
	s := ui.For(out)

	if len(stale) == 0 {
		fmt.Fprintln(out, s.Done(fmt.Sprintf("all %d packages are at their newest version", all)))

		return nil
	}

	// The waiting column shows only when a version waits.
	waits := slices.ContainsFunc(stale, func(f staleness) bool { return f.waiting.Version != "" })

	tab := s.Table("name", "locked", "newest", "latest", "ref")
	if waits {
		tab = s.Table("name", "locked", "newest", "latest", "waiting", "ref")
	}

	for _, f := range stale {
		cells := []string{f.pkg.Name, f.pkg.Version, f.newest, f.latest}
		styles := []func(string) string{s.Bold, s.Dim, nil, nil}

		if waits {
			waiting := ""
			if f.waiting.Version != "" {
				waiting = f.waiting.Version + " from " +
					f.waiting.Published.Add(f.age).Local().Format("2006-01-02 15:04")
			}

			cells, styles = append(cells, waiting), append(styles, s.Dim)
		}

		tab.Styled(append(cells, s.Home(f.pkg.Ref)), append(styles, s.Dim)...)
	}

	if err := tab.Write(out); err != nil {
		return err
	}

	hint(out, "`oku update` takes the newest versions. To take a latest beyond them, change its version in oku.toml")

	if waits {
		hint(out, "a waiting version is newer than the minimum release age. "+
			"`oku update <name> --min-release-age 0` takes it now")
	}

	return nil
}
