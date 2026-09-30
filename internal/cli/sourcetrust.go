package cli

import (
	"errors"
	"fmt"
	"maps"
	"net/url"
	"slices"
	"strings"

	"github.com/spf13/cobra"

	"github.com/y3owk1n/oku/internal/list"
	"github.com/y3owk1n/oku/internal/lock"
	"github.com/y3owk1n/oku/internal/platform"
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
	// downloads names, for a package of the lock, where its downloads come from
	// when that is not its source.
	downloads map[string][]string
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

		for _, what := range u.origins[origin] {
			if from := u.downloads[what]; len(from) > 0 {
				fmt.Fprintf(&b, "    %s downloads from %s\n", what, strings.Join(from, ", "))
			}
		}
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

	refused, err := e.refuseUntrusted(origins)
	if err != nil {
		return err
	}

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

// refuseUntrusted is the error for the origins, with where oku.lock says each
// of their packages downloads from.
func (e env) refuseUntrusted(origins map[string][]string) (untrustedError, error) {
	locked, err := lock.Read(e.lockPath())
	if err != nil {
		return untrustedError{}, err
	}

	downloads := map[string][]string{}

	for origin, names := range origins {
		for _, name := range names {
			if p, ok := locked.Find(name); ok {
				if from := publishers(p, origin); len(from) > 0 {
					downloads[name] = from
				}
			}
		}
	}

	return untrustedError{project: e.project, origins: origins, downloads: downloads}, nil
}

// publishers names who serves the downloads that p pins for this machine, or
// for every platform when it pins none for this one, leaving out the ones that
// origin already names. A registry's package downloads from the registry, which
// says nothing more than origin does.
func publishers(p lock.Package, origin string) []string {
	r, err := ref.Parse(p.Ref)
	if err != nil || r.Kind == ref.NPM || r.Kind == ref.PyPI || r.Kind == ref.Cargo || r.Kind == ref.Go {
		return nil
	}

	entries := slices.Collect(maps.Values(p.Platforms))
	if entry, ok := p.Platforms[platform.Host().String()]; ok {
		entries = []lock.Platform{entry}
	}

	var found []string

	for _, entry := range entries {
		if from := publisher(entry.URL); from != "" && !sameOwner(origin, from) && !slices.Contains(found, from) {
			found = append(found, from)
		}
	}

	slices.Sort(found)

	return found
}

// sharedHosts serve files of many owners, so the first part of the path, the
// owner, says who published a file there.
var sharedHosts = []string{
	"github.com", "gitlab.com", "codeberg.org", "bitbucket.org",
	"storage.googleapis.com", "sourceforge.net", "downloads.sourceforge.net",
}

// publisher is who serves the file at rawURL: the host, or on a shared host the
// host and the owner, such as github.com/acme. It is "" for a file on this
// machine.
func publisher(rawURL string) string {
	u, err := url.Parse(rawURL)
	if err != nil || u.Hostname() == "" || u.Scheme == "file" {
		return ""
	}

	host := strings.ToLower(u.Hostname())
	if !slices.Contains(sharedHosts, host) {
		return host
	}

	parts := strings.Split(strings.Trim(u.Path, "/"), "/")

	// SourceForge puts its projects under /projects/ and /project/.
	if len(parts) > 1 && (parts[0] == "projects" || parts[0] == "project") {
		parts = parts[1:]
	}

	return host + "/" + parts[0]
}

// forgeHosts are the hosts that a forge ref without a host of its own reads.
var forgeHosts = map[string]string{"github": "github.com", "gitlab": "gitlab.com", "codeberg": "codeberg.org"}

// sameOwner reports whether publisher is the owner or host that origin names,
// as github.com/acme is github:acme.
func sameOwner(origin, publisher string) bool {
	scheme, owner, ok := strings.Cut(origin, ":")
	if !ok {
		return strings.EqualFold(origin, publisher)
	}

	if host, known := forgeHosts[scheme]; known && !strings.Contains(owner, "/") {
		owner = host + "/" + owner
	}

	// A forge of the user's own serves its files from its host.
	if host, _, _ := strings.Cut(owner, "/"); strings.EqualFold(host, publisher) {
		return true
	}

	return strings.EqualFold(owner, publisher)
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
