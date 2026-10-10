package cli

import (
	"maps"
	"reflect"
	"slices"
	"strings"

	"github.com/y3owk1n/oku/internal/lock"
	"github.com/y3owk1n/oku/internal/profile"
)

// delta is one difference between two generations or two locks.
type delta struct {
	// Kind is "package", "dep", "file", "setting" or "list".
	Kind string `json:"kind"`
	Name string `json:"name"`
	// Change is "added", "removed" or "changed".
	Change string `json:"change"`
	// Before and After are a version, a link target or a setting's value, where
	// the entry has one.
	Before string `json:"before,omitempty"`
	After  string `json:"after,omitempty"`
	// Note says what changed when Before and After do not, such as "rebuilt".
	Note string `json:"note,omitempty"`
}

const (
	wasAdded   = "added"
	wasRemoved = "removed"
	wasChanged = "changed"
)

// generationDeltas returns what differs from one generation to the next:
// packages, then files, then settings.
func generationDeltas(from, to profile.Generation) []delta {
	var deltas []delta

	find := func(pkgs []profile.Package, name string) int {
		return slices.IndexFunc(pkgs, func(p profile.Package) bool { return p.Name == name })
	}

	for _, pkg := range from.Packages {
		if find(to.Packages, pkg.Name) < 0 {
			deltas = append(deltas, delta{Kind: "package", Name: pkg.Name, Change: wasRemoved, Before: pkg.Version})
		}
	}

	for _, pkg := range to.Packages {
		i := find(from.Packages, pkg.Name)
		d := delta{Kind: "package", Name: pkg.Name, Change: wasChanged, After: pkg.Version}

		switch {
		case i < 0:
			d.Change = wasAdded
		case from.Packages[i].Version != pkg.Version:
			d.Before = from.Packages[i].Version
		case from.Packages[i].StorePath != pkg.StorePath:
			d.Before, d.Note = pkg.Version, "rebuilt"
		default:
			continue
		}

		deltas = append(deltas, d)
	}

	findFile := func(files []profile.File, target string) int {
		return slices.IndexFunc(files, func(f profile.File) bool { return f.Target == target })
	}

	for _, f := range from.Files {
		if findFile(to.Files, f.Target) < 0 {
			deltas = append(deltas, delta{Kind: "file", Name: f.Target, Change: wasRemoved, Before: f.Link})
		}
	}

	for _, f := range to.Files {
		i := findFile(from.Files, f.Target)
		d := delta{Kind: "file", Name: f.Target, Change: wasChanged, After: f.Link}

		switch {
		case i < 0:
			d.Change = wasAdded
		case from.Files[i].Link != f.Link:
			d.Before = from.Files[i].Link
		case from.Files[i].Hash != f.Hash:
			d.Before, d.Note = f.Link, "content"
		case from.Files[i].Mode != f.Mode:
			d.Before, d.Note = f.Link, "mode"
		default:
			continue
		}

		deltas = append(deltas, d)
	}

	findSetting := func(settings []profile.Setting, want profile.Setting) int {
		return slices.IndexFunc(settings, func(have profile.Setting) bool {
			return have.Domain == want.Domain && have.Key == want.Key
		})
	}

	for _, setting := range from.Settings {
		if findSetting(to.Settings, setting) < 0 {
			deltas = append(deltas, delta{
				Kind: "setting", Name: setting.Domain + " " + setting.Key, Change: wasRemoved, Before: setting.Value,
			})
		}
	}

	for _, setting := range to.Settings {
		i := findSetting(from.Settings, setting)
		d := delta{Kind: "setting", Name: setting.Domain + " " + setting.Key, Change: wasAdded, After: setting.Value}

		switch {
		case i < 0:
		case from.Settings[i].Value != setting.Value:
			d.Change, d.Before = wasChanged, from.Settings[i].Value
		default:
			continue
		}

		deltas = append(deltas, d)
	}

	return deltas
}

// lockDeltas returns what differs from one lock to another: the included lists,
// then each package with its deps. When a package's version moved on some
// platforms and not others, each of those platforms gets a row. A package whose
// pins changed at the same version reads "repinned".
func lockDeltas(from, to *lock.Lock) []delta {
	var deltas []delta

	for _, inc := range from.Includes {
		if _, ok := to.FindInclude(inc.Ref); !ok {
			deltas = append(deltas, delta{Kind: "list", Name: inc.Ref, Change: wasRemoved, Before: short(inc.Commit)})
		}
	}

	for _, inc := range to.Includes {
		was, ok := from.FindInclude(inc.Ref)

		switch {
		case !ok:
			deltas = append(deltas, delta{Kind: "list", Name: inc.Ref, Change: wasAdded, After: short(inc.Commit)})
		case was.Commit != inc.Commit:
			deltas = append(deltas, delta{
				Kind: "list", Name: inc.Ref, Change: wasChanged, Before: short(was.Commit), After: short(inc.Commit),
			})
		case was.SHA256 != inc.SHA256:
			// A list at a URL has no commit.
			deltas = append(deltas, delta{Kind: "list", Name: inc.Ref, Change: wasChanged, Note: "content"})
		}
	}

	for _, pkg := range from.Packages {
		if _, ok := to.Find(pkg.Name); !ok {
			deltas = append(deltas, delta{Kind: "package", Name: pkg.Name, Change: wasRemoved, Before: pkg.Version})
		}
	}

	for _, pkg := range to.Packages {
		was, ok := from.Find(pkg.Name)
		if !ok {
			deltas = append(deltas, delta{Kind: "package", Name: pkg.Name, Change: wasAdded, After: pkg.Version})

			continue
		}

		deltas = append(deltas, packageDeltas(was, pkg)...)
		deltas = append(deltas, depDeltas(pkg.Name, was.Deps, pkg.Deps)...)
	}

	return deltas
}

// packageDeltas returns how the pin of one package changed.
func packageDeltas(was, now lock.Package) []delta {
	row := delta{Kind: "package", Name: now.Name, Change: wasChanged, Before: was.Version, After: now.Version}

	if was.Ref != now.Ref {
		row.Note = "ref " + was.Ref + " -> " + now.Ref
	}

	if was.Version != now.Version || row.Note != "" {
		return []delta{row}
	}

	var deltas []delta

	for _, key := range slices.Sorted(maps.Keys(now.Platforms)) {
		if _, ok := was.Platforms[key]; ok && was.VersionOn(key) != now.VersionOn(key) {
			deltas = append(deltas, delta{
				Kind: "package", Name: now.Name, Change: wasChanged,
				Before: was.VersionOn(key), After: now.VersionOn(key), Note: "on " + key,
			})
		}
	}

	if len(deltas) == 0 && (was.ManifestSHA256 != now.ManifestSHA256 || was.Commit != now.Commit ||
		was.TagCommit != now.TagCommit || !reflect.DeepEqual(was.Platforms, now.Platforms)) {
		row.Note = "repinned"
		deltas = append(deltas, row)
	}

	return deltas
}

// depDeltas returns how the deps that the package name pins changed, by ref.
// A dep that a manifest lists for each platform may pin several versions.
func depDeltas(name string, from, to []lock.Package) []delta {
	versions := func(deps []lock.Package) map[string]string {
		byRef := map[string][]string{}

		for _, dep := range deps {
			if !slices.Contains(byRef[dep.Ref], dep.Version) {
				byRef[dep.Ref] = append(byRef[dep.Ref], dep.Version)
			}
		}

		joined := map[string]string{}
		for ref, list := range byRef {
			slices.Sort(list)
			joined[ref] = strings.Join(list, ", ")
		}

		return joined
	}

	before, after := versions(from), versions(to)

	var deltas []delta

	for _, ref := range slices.Sorted(maps.Keys(before)) {
		if _, ok := after[ref]; !ok {
			deltas = append(deltas, delta{Kind: "dep", Name: name + " " + ref, Change: wasRemoved, Before: before[ref]})
		}
	}

	for _, ref := range slices.Sorted(maps.Keys(after)) {
		was, ok := before[ref]

		switch {
		case !ok:
			deltas = append(deltas, delta{Kind: "dep", Name: name + " " + ref, Change: wasAdded, After: after[ref]})
		case was != after[ref]:
			deltas = append(deltas, delta{
				Kind: "dep", Name: name + " " + ref, Change: wasChanged, Before: was, After: after[ref],
			})
		}
	}

	return deltas
}

// short cuts a commit to the 12 characters people read.
func short(commit string) string {
	return commit[:min(12, len(commit))]
}
