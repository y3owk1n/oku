package cli

import (
	"context"
	"fmt"
	"maps"
	"path/filepath"
	"slices"
	"strings"

	"github.com/spf13/cobra"

	"github.com/y3owk1n/oku/internal/infer"
	"github.com/y3owk1n/oku/internal/lock"
	"github.com/y3owk1n/oku/internal/manifest"
	"github.com/y3owk1n/oku/internal/platform"
	"github.com/y3owk1n/oku/internal/ref"
	"github.com/y3owk1n/oku/internal/store"
	"github.com/y3owk1n/oku/internal/ui"
)

// planFlags are the flags of add that change what a plan finds.
type planFlags struct {
	manifest   bool
	fromSource bool
	asset      string
	bins       []string
	verbose    bool
	acceptKey  bool
	when       platform.When
}

// planned is what add would do for one ref. oku finds it without installing
// the package or changing the machine.
type planned struct {
	Ref         string `json:"ref"`
	Name        string `json:"name"`
	Version     string `json:"version"`
	Description string `json:"description,omitempty"`
	Homepage    string `json:"homepage,omitempty"`
	License     string `json:"license,omitempty"`
	// Manifest is the file oku read, and empty for a manifest oku inferred.
	Manifest    string   `json:"manifest,omitempty"`
	Commit      string   `json:"commit,omitempty"`
	Inferred    bool     `json:"inferred"`
	Asset       string   `json:"asset,omitempty"`
	OtherAssets []string `json:"other_assets"`
	// Install is "download", "build", or "pin" for a package that has nothing
	// for this machine and goes in oku.lock only.
	Install    string   `json:"install"`
	Platform   string   `json:"platform"`
	URL        string   `json:"url,omitempty"`
	Verify     string   `json:"verify,omitempty"`
	SigningKey string   `json:"signing_key,omitempty"`
	Commands   bool     `json:"commands"`
	Needs      []string `json:"needs"`
	Deps       []string `json:"deps"`
	BuildDeps  []string `json:"build_deps"`
	Programs   []string `json:"programs"`
	Apps       []string `json:"apps"`
	Fonts      []string `json:"fonts"`
	Services   []string `json:"services"`
	Env        []string `json:"env"`
	Platforms  []string `json:"platforms"`
	Installed  string   `json:"installed,omitempty"`
	List       string   `json:"list"`
}

// runPlan prints, for each ref in args, what add would do, or with
// flags.manifest the manifest it would use. It changes nothing.
func runPlan(cmd *cobra.Command, opts Options, args []string, flags planFlags) error {
	var plans []planned

	for _, arg := range args {
		e, req, locked, err := addRequest(cmd, opts, arg, flags.when)
		if err != nil {
			return err
		}

		req.fromSource, req.asset, req.bins = flags.fromSource, flags.asset, flags.bins
		req.acceptKey, req.verbose = flags.acceptKey, flags.verbose

		if flags.manifest {
			// A manifest to keep serves every platform, not only the lock's.
			req.platforms = slices.DeleteFunc(platform.All(), func(p platform.Platform) bool {
				return p == platform.Host()
			})

			fetched, _, err := e.manifestData(cmd.Context(), opts, req)
			if err != nil {
				return notFound(arg, err)
			}

			fmt.Fprint(cmd.OutOrStdout(), string(fetched.Data))

			// Only an install of the tree shows which of its packages have install
			// scripts, and --manifest installs nothing.
			if m, err := manifest.Parse(fetched.Data, req.ref.String()); err == nil &&
				req.ref.Kind == ref.NPM && store.CanCrossVendor(m.Build, platform.Host()) {
				warn(
					cmd.ErrOrStderr(),
					"this manifest names no install scripts, since oku finds them when npm installs the tree. `oku add %s` names them in scripts and asks first.",
					arg,
				)
			}

			continue
		}

		p, err := e.planAdd(cmd.Context(), opts, req, locked)
		if err != nil {
			return notFound(arg, err)
		}

		// A script can always iterate over a list, so none is null.
		for _, list := range []*[]string{
			&p.OtherAssets, &p.Needs, &p.Deps, &p.BuildDeps, &p.Programs, &p.Apps,
			&p.Fonts, &p.Services, &p.Env, &p.Platforms,
		} {
			if *list == nil {
				*list = []string{}
			}
		}

		plans = append(plans, p)
	}

	if flags.manifest {
		return nil
	}

	if wantJSON(cmd) {
		return printJSON(cmd, plans)
	}

	out := cmd.OutOrStdout()
	s := ui.For(out)

	for i, p := range plans {
		if i > 0 {
			fmt.Fprintln(out)
		}

		if err := s.KV(out, p.pairs(s)...); err != nil {
			return err
		}

		if len(p.OtherAssets) > 0 {
			hint(out, fmt.Sprintf("pick another asset with `oku add %s --asset %s`", p.Ref, p.OtherAssets[0]))
		}

		if p.Inferred {
			hint(out, fmt.Sprintf("`oku add %s --manifest` prints the inferred manifest", p.Ref))
		}
	}

	fmt.Fprintln(out, s.Dim("plan: nothing was changed"))

	return nil
}

// planAdd finds what add would install for req. It reads the manifest, or infers
// one, picks the version and the download or build for this machine, and checks
// that the download is there. It downloads no package and runs no manifest
// command.
func (e env) planAdd(
	ctx context.Context,
	opts Options,
	req request,
	locked *lock.Lock,
) (planned, error) {
	fetched, inferred, err := e.manifestData(ctx, opts, req)
	if err != nil {
		return planned{}, err
	}

	p, err := e.planFrom(ctx, opts, req, locked, fetched, inferred)

	// The error has the hints of add, because the user never saw the inferred
	// manifest.
	if err != nil && inferred.Text != "" {
		return planned{}, fmt.Errorf("%w\n%s", err, inferredHints(req, inferred))
	}

	return p, err
}

// planFrom is planAdd for the manifest in fetched.
func (e env) planFrom(
	ctx context.Context,
	opts Options,
	req request,
	locked *lock.Lock,
	fetched ref.Fetched,
	inferred infer.Inferred,
) (planned, error) {
	m, err := manifest.Parse(fetched.Data, req.ref.String())
	if err != nil {
		return planned{}, err
	}

	// add pins a package in oku.lock only, when nothing fits this machine but a
	// platform of the lock.
	host := platform.Host()

	fits, unsupported := supportedTargets(m, req)
	if len(fits) == 0 {
		return planned{}, noSupportError(m, unsupported)
	}

	if len(unsupported) > 0 {
		req.platforms = slices.DeleteFunc(slices.Clone(req.platforms), func(p platform.Platform) bool {
			return !m.Supports(p)
		})
		req.lockOnly = req.lockOnly || !m.Supports(host)
	}

	m, release, _, err := e.pickRelease(ctx, opts, req, fetched)
	if err != nil {
		return planned{}, err
	}

	p := planned{
		Ref:         req.ref.String(),
		Name:        m.Package.Name,
		Version:     m.Version.Value,
		Description: m.Package.Description,
		Homepage:    m.Package.Homepage,
		License:     m.Package.License,
		Manifest:    fetched.Path,
		Commit:      fetched.Commit,
		Inferred:    inferred.Text != "",
		Asset:       inferred.Asset,
		OtherAssets: inferred.Others,
		Platform:    host.String(),
		SigningKey:  m.Package.SigningKey,
		Deps:        depNames(m.Runtime.Deps),
		List:        e.listPath(),
	}

	for _, at := range platform.All() {
		if m.Supports(at) {
			p.Platforms = append(p.Platforms, at.String())
		}
	}

	if entry, ok := locked.Find(m.Package.Name); ok {
		p.Installed = entry.VersionOn(host.String())
	}

	for _, svc := range m.ServicesFor(host) {
		p.Services = append(p.Services, svc.Name)
	}

	p.Env = slices.Sorted(maps.Keys(m.Env))

	if req.lockOnly {
		p.Install = "pin"

		return p, nil
	}

	artifact, build, err := hostStrategy(m, req, host)
	if err != nil {
		return planned{}, err
	}

	previous := req.previous.Platforms[host.String()]
	sameVersion := req.previous.VersionOn(host.String()) == p.Version

	if build {
		p.Install = "build"
		p.BuildDeps = depNames(m.Build.Deps)
		p.Needs = m.Build.Needs
		p.Commands = len(m.Build.CommandSteps(host)) > 0

		vars := map[string]string{"version": m.Version.Value, "tag": m.Tag}
		src := m.Build.Source

		switch {
		case src.Git != "":
			tag, err := manifest.Expand(src.Tag, vars)
			if err != nil {
				return planned{}, err
			}

			p.URL = src.Git + " at " + tag
		case src.URL != "":
			if p.URL, err = manifest.Expand(src.URL, vars); err != nil {
				return planned{}, err
			}

			pinned := ""
			if sameVersion && previous.Strategy == strategyBuild {
				pinned = previous.SHA256
			}

			p.Verify = verifyText(src.SHA256, src.SHA256URL, release.Digests[p.URL], "", pinned)

			if err := e.store().Reachable(ctx, p.URL); err != nil {
				return planned{}, fmt.Errorf("%s: %w", m.Package.Name, err)
			}
		}

		return p, nil
	}

	// add would fail on a file it fetches that is not there, so the plan fails
	// too.
	urls := []string{artifact.URL}
	if artifact.SHA256URL != "" {
		urls = append(urls, artifact.SHA256URL)
	}

	if m.Package.SigningKey != "" {
		urls = append(urls, artifact.URL+".minisig")
	}

	auth := e.fetcher(opts).Hosts.AuthFor(m.Version.From, m.Version.Repo)
	for _, url := range urls {
		if err := e.store().As(auth).Reachable(ctx, url); err != nil {
			return planned{}, err
		}
	}

	p.Install = "download"
	p.URL = artifact.URL
	p.Commands = artifact.Completions.Generate != ""
	p.Apps, p.Fonts = artifact.App, artifact.Font

	for _, bin := range artifact.Bin {
		p.Programs = append(p.Programs, filepath.Base(bin))
	}

	for _, w := range artifact.Wrap {
		p.Programs = append(p.Programs, w.Name)
	}

	pinned := ""
	if sameVersion && previous.URL == artifact.URL {
		pinned = previous.SHA256
	}

	integrity := artifact.Integrity
	if integrity == "" {
		integrity = release.Integrity[artifact.URL]
	}

	p.Verify = verifyText(
		artifact.SHA256, artifact.SHA256URL, release.Digests[artifact.URL], integrity, pinned,
	)

	return p, nil
}

// verifyText says how oku checks a download, from the most specific digest it
// has.
func verifyText(sha256, sha256URL, published, integrity, pinned string) string {
	switch {
	case sha256 != "":
		return "sha256 from the manifest"
	case sha256URL != "":
		return "sha256 from " + sha256URL
	case pinned != "":
		return "sha256 pinned in oku.lock"
	case published != "":
		return "sha256 the release publishes"
	case integrity != "":
		return "sha512 the registry publishes"
	default:
		return "none, so oku pins the sha256 of the first download in oku.lock"
	}
}

// depNames writes each dep as its ref and version constraint.
func depNames(deps []manifest.Dep) []string {
	names := make([]string, 0, len(deps))

	for _, d := range deps {
		names = append(names, strings.TrimSpace(d.Ref+" "+d.Version))
	}

	return names
}

// pairs are the rows of the plan on a terminal.
func (p planned) pairs(s ui.Style) [][2]string {
	install := map[string]string{
		"download": "download for " + p.Platform,
		"build":    "build from source on " + p.Platform,
		"pin":      "pin in oku.lock only, nothing fits " + p.Platform,
	}[p.Install]

	source := "url"
	if p.Install == "build" {
		source = "source"
	}

	commands := ""
	if p.Commands {
		commands = s.Warn("runs commands from the manifest after you approve them")
	}

	signed := ""
	if p.SigningKey != "" {
		signed = "minisign key " + p.SigningKey
	}

	manifestRow := s.Home(p.Manifest)

	switch {
	case p.Inferred:
		manifestRow = "inferred by oku, because the ref has none"
	case p.Manifest == p.Ref:
		// A local file is its own manifest, which the ref row already names.
		manifestRow = ""
	}

	// A terminal gets the start of the commit, as in "oku info".
	commit := p.Commit
	if s.On() && len(commit) > 12 {
		commit = commit[:12]
	}

	installed := "no"
	if p.Installed != "" {
		installed = p.Name + " " + p.Installed
	}

	return [][2]string{
		{"name", s.Bold(p.Name)},
		{"version", p.Version},
		{"about", p.Description},
		{"homepage", p.Homepage},
		{"license", p.License},
		{"ref", s.Home(p.Ref)},
		{"manifest", manifestRow},
		{"commit", commit},
		{"asset", p.Asset},
		{"also fits", strings.Join(p.OtherAssets, ", ")},
		{"install", install},
		{source, p.URL},
		{"verify", p.Verify},
		{"signed", signed},
		{"commands", commands},
		{"needs", strings.Join(p.Needs, ", ")},
		{"build deps", strings.Join(p.BuildDeps, ", ")},
		{"deps", strings.Join(p.Deps, ", ")},
		{"programs", strings.Join(p.Programs, ", ")},
		{"apps", strings.Join(p.Apps, ", ")},
		{"fonts", strings.Join(p.Fonts, ", ")},
		{"services", strings.Join(p.Services, ", ")},
		{"env", strings.Join(p.Env, ", ")},
		{"platforms", strings.Join(p.Platforms, ", ")},
		{"installed", installed},
		{"list", s.Home(p.List)},
	}
}
