package cli

import (
	"cmp"
	"fmt"
	"io/fs"
	"maps"
	"os"
	"path/filepath"
	"slices"
	"strings"

	"github.com/spf13/cobra"

	"github.com/y3owk1n/oku/internal/clone"
	"github.com/y3owk1n/oku/internal/expose"
	"github.com/y3owk1n/oku/internal/profile"
	"github.com/y3owk1n/oku/internal/status"
	"github.com/y3owk1n/oku/internal/store"
	"github.com/y3owk1n/oku/internal/tempdir"
	"github.com/y3owk1n/oku/internal/trust"
	"github.com/y3owk1n/oku/internal/ui"
)

func newDuCmd() *cobra.Command {
	var packages bool

	cmd := &cobra.Command{
		Use:   "du",
		Short: "Show how much disk oku uses and where",
		Long: `Show how much disk oku uses and where: the store, the profiles, the cache,
the apps and fonts it copied out of the store, and the rest of its data.

--packages lists each store path instead, with what keeps it.`,
		Args: cobra.NoArgs,
		RunE: func(cmd *cobra.Command, _ []string) error {
			e, err := loadEnv()
			if err != nil {
				return err
			}

			profiles, err := profile.All(e.data)
			if err != nil {
				return err
			}

			holders, err := e.storeHolders(profiles, nil)
			if err != nil {
				return err
			}

			done := status.Start(cmd.Context(), "measuring what oku keeps on disk")
			m := meter{seen: map[fileID]bool{}, shared: map[string]bool{}}
			paths, err := e.measureStore(&m, holders)

			var areas []area
			if err == nil && !packages {
				areas, err = e.measureAreas(&m, profiles, paths)
			}

			done()

			switch {
			case err != nil:
				return err
			case packages:
				return e.duPackages(cmd, paths)
			}

			// What gc --cache deletes, with every generation kept.
			used := map[string]bool{}
			for path := range holders {
				used[path] = true
			}

			stale, err := e.staleDownloads(used)
			if err != nil {
				return err
			}

			return e.duAreas(cmd, areas, paths, stale)
		},
	}

	cmd.Flags().
		BoolVar(&packages, "packages", false, "list each store path with its size and what keeps it")

	return cmd
}

// holders says what keeps a store path: the profiles whose generations hold it
// as a package or a file, and the packages that depend on it.
type holders struct {
	// profiles holds the names of the profiles whose generations hold the path.
	profiles map[string]bool
	// deps holds the names of the packages whose closure has the path.
	deps map[string]bool
	// active reports that an active generation holds the path.
	active bool
}

// storeHolders returns what keeps each store path that a generation uses. It
// skips the generations in skip.
func (e env) storeHolders(
	profiles []*profile.Profile,
	skip map[*profile.Profile][]int,
) (map[string]*holders, error) {
	found := map[string]*holders{}

	hold := func(path string) *holders {
		h := found[path]
		if h == nil {
			h = &holders{profiles: map[string]bool{}, deps: map[string]bool{}}
			found[path] = h
		}

		return h
	}

	for _, prof := range profiles {
		gens, err := prof.Generations()
		if err != nil {
			return nil, err
		}

		for _, gen := range gens {
			if slices.Contains(skip[prof], gen.Number) {
				continue
			}

			byProfile := func(path string) {
				h := hold(path)
				h.profiles[prof.Name()] = true
				h.active = h.active || gen.Current
			}

			for _, pkg := range gen.Packages {
				byProfile(pkg.StorePath)

				for _, dep := range pkg.Closure {
					h := hold(dep)
					h.deps[pkg.Name] = true
					h.active = h.active || gen.Current
				}
			}

			// A link of [files] from a remote list leads into the repo's files in
			// the store.
			for _, file := range gen.Files {
				for _, st := range e.stores() {
					if at, in := st.Holding(file.Link); in {
						byProfile(at)
					}
				}
			}
		}
	}

	return found, nil
}

// storePath is one directory in a store, with its size and what keeps it.
type storePath struct {
	path string
	// store is the directory of the store that holds the path.
	store string
	size  int64
	// frees is what gc frees when it deletes the path.
	frees   int64
	holders *holders
	// unused reports that no generation holds the path, so gc deletes it.
	unused bool
}

// measureStore sizes every store path. It runs before measureAreas, so the
// store counts a file that a Windows generation hard links.
func (e env) measureStore(m *meter, holders map[string]*holders) ([]storePath, error) {
	used := map[string]bool{}
	for path := range holders {
		used[path] = true
	}

	var paths []storePath

	for _, st := range e.stores() {
		entries, err := os.ReadDir(st.Dir())
		if err != nil && !os.IsNotExist(err) {
			return nil, fmt.Errorf("read store: %w", err)
		}

		// gc deletes the paths Unreferenced returns, so du reports the same ones.
		unused, err := st.Unreferenced(used)
		if err != nil {
			return nil, err
		}

		for _, entry := range entries {
			// The index of shared files holds the same files as the store paths.
			if !entry.IsDir() || entry.Name() == store.LinksDir {
				continue
			}

			path := filepath.Join(st.Dir(), entry.Name())
			frees, gone := unused[path]

			files, err := st.SharedFiles(path)
			if err != nil {
				return nil, err
			}

			paths = append(paths, storePath{
				path:    path,
				store:   st.Dir(),
				size:    m.storeSize(path, files),
				frees:   frees,
				holders: holders[path],
				unused:  gone,
			})
		}
	}

	return paths, nil
}

// area is one place where oku keeps files.
type area struct {
	Area  string   `json:"area"`
	Paths []string `json:"paths"`
	Bytes int64    `json:"bytes"`
	// detail says what the area holds, in the text output.
	detail string
}

// measureAreas sizes the areas beside the store: the profiles, the cache, the
// apps and fonts, the rest of the data directory and what killed oku
// processes left in the temporary directory.
func (e env) measureAreas(
	m *meter,
	profiles []*profile.Profile,
	paths []storePath,
) ([]area, error) {
	var areas []area

	for _, st := range e.stores() {
		var size, unused, old int64

		n := 0

		for _, p := range paths {
			if p.store != st.Dir() {
				continue
			}

			n++
			size += p.size

			switch {
			case p.unused:
				unused += p.frees
			case p.holders != nil && !p.holders.active:
				old += p.size
			}
		}

		detail := []string{count(n, "path")}
		if unused > 0 {
			detail = append(detail, status.Size(unused)+" unused")
		}

		if old > 0 {
			detail = append(detail, status.Size(old)+" only in old generations")
		}

		areas = append(areas, area{"store", []string{st.Dir()}, size, strings.Join(detail, ", ")})
	}

	gens := 0

	for _, prof := range profiles {
		all, err := prof.Generations()
		if err != nil {
			return nil, err
		}

		gens += len(all)
	}

	profilesDir := filepath.Join(e.data, "profiles")
	areas = append(areas, area{
		"profiles",
		[]string{profilesDir},
		m.size(profilesDir),
		count(len(profiles), "profile") + ", " + count(gens, "generation"),
	})

	cache := area{Area: "cache", Paths: []string{e.cache}}

	var parts []string

	for _, name := range []string{"downloads", "git", "api"} {
		size := m.size(filepath.Join(e.cache, name))
		if size > 0 {
			parts = append(parts, name+" "+status.Size(size))
		}

		cache.Bytes += size
	}

	cache.detail = strings.Join(parts, ", ")
	cache.Bytes += m.sizeExcept(e.cache, "downloads", "git", "api")
	areas = append(areas, cache)

	ledger, err := expose.ReadLedger(e.data)
	if err != nil {
		return nil, err
	}

	for _, kind := range []string{"app", "font"} {
		a := area{Area: kind + "s"}
		n := 0

		for _, item := range ledger.Items {
			if item.Kind != kind {
				continue
			}

			n++
			a.Bytes += m.size(item.Target)

			if dir := filepath.Dir(item.Target); !slices.Contains(a.Paths, dir) {
				a.Paths = append(a.Paths, dir)
			}
		}

		if n > 0 {
			a.detail = count(n, kind)
			areas = append(areas, a)
		}
	}

	// The rest of the data directory: secrets, services, logs and the like.
	other := area{Area: "other", Paths: []string{e.data}}
	other.Bytes = m.sizeExcept(e.data, "store", "profiles")

	if entries, err := os.ReadDir(e.data); err == nil {
		var names []string

		for _, entry := range entries {
			if entry.IsDir() && entry.Name() != "store" && entry.Name() != "profiles" {
				names = append(names, entry.Name())
			}
		}

		other.detail = strings.Join(names, ", ")
	}

	areas = append(areas, other)

	leftovers, err := tempdir.Stale()
	if err != nil {
		return nil, err
	}

	if len(leftovers) > 0 {
		temp := area{Area: "temporary", Paths: []string{os.TempDir()}}
		for _, path := range leftovers {
			temp.Bytes += m.size(path)
		}

		temp.detail = count(len(leftovers), "entry") + " left by oku processes that ended"
		areas = append(areas, temp)
	}

	return areas, nil
}

// duAreas prints one row per area, the total, and what gc and gc --cache give
// back. stale holds the cached files that gc --cache deletes beside what gc
// does.
func (e env) duAreas(cmd *cobra.Command, areas []area, paths []storePath, stale map[string]int64) error {
	var total, frees, staleBytes int64

	for _, n := range stale {
		staleBytes += n
	}

	for _, a := range areas {
		total += a.Bytes

		if a.Area == "temporary" {
			frees += a.Bytes
		}
	}

	for _, p := range paths {
		if p.unused {
			frees += p.frees
		}
	}

	if wantJSON(cmd) {
		return printJSON(cmd, struct {
			Areas        []area `json:"areas"`
			Total        int64  `json:"total"`
			GCFrees      int64  `json:"gc_frees"`
			GCCacheFrees int64  `json:"gc_cache_frees"`
		}{areas, total, frees, frees + staleBytes})
	}

	out := cmd.OutOrStdout()
	s := ui.For(out)
	tab := s.Table("area", "path", "size", "holds")

	for _, a := range areas {
		homes := make([]string, len(a.Paths))
		for i, path := range a.Paths {
			homes[i] = s.Home(path)
		}

		tab.Styled(
			[]string{a.Area, strings.Join(homes, ", "), status.Size(a.Bytes), a.detail},
			s.Bold, s.Dim, nil, s.Dim,
		)
	}

	tab.Styled([]string{"total", "", status.Size(total), ""}, s.Bold, nil, s.Bold)

	if err := tab.Write(out); err != nil {
		return err
	}

	// The size of an app or a font is what the file says, and no filesystem tells
	// which of a clone's blocks it shares, so those rows are an upper bound where
	// oku clones.
	for _, a := range areas {
		if (a.Area == "apps" || a.Area == "fonts") && len(a.Paths) > 0 &&
			clone.Possible(e.store().Dir(), a.Paths[0]) {
			hint(out, "an app or a font that oku cloned shares its blocks with the store, "+
				"so it takes less disk than its size above")

			break
		}
	}

	var next []string
	if frees > 0 {
		next = append(next, "`oku gc` frees "+status.Size(frees))
	}

	if staleBytes > 0 {
		next = append(next, "`oku gc --cache` frees "+status.Size(frees+staleBytes))
	}

	if len(next) > 0 {
		hint(out, strings.Join(next, ", and "))
	}

	return nil
}

// duPackages prints one row per store path, largest first, with what keeps it.
func (e env) duPackages(cmd *cobra.Command, paths []storePath) error {
	allowed, err := trust.ReadAllowed(e.data)
	if err != nil {
		return err
	}

	// A project profile is named after a hash of the project's directory, so
	// the projects oku knows give the names back.
	names := map[string]string{}
	for _, item := range allowed.Items {
		names[projectProfile(item.Dir)] = item.Dir
	}

	type row struct {
		Name     string   `json:"name"`
		Version  string   `json:"version,omitempty"`
		Path     string   `json:"path"`
		Bytes    int64    `json:"bytes"`
		Profiles []string `json:"profiles"`
		DepOf    []string `json:"dep_of"`
		// Old reports that only generations that are not active hold the path.
		Old    bool `json:"old"`
		Unused bool `json:"unused"`
	}

	rows := []row{}

	for _, p := range slices.SortedFunc(slices.Values(paths), func(a, b storePath) int {
		return cmp.Or(cmp.Compare(b.size, a.size), cmp.Compare(a.path, b.path))
	}) {
		r := row{
			Name: filepath.Base(p.path), Path: p.path, Bytes: p.size, Unused: p.unused,
			Profiles: []string{}, DepOf: []string{},
		}

		// A repo's files have no meta, and their directory name says what they are.
		if meta, err := store.ReadMeta(p.path); err == nil {
			r.Name, r.Version = meta.Name, meta.Version
		}

		if h := p.holders; h != nil {
			for _, name := range slices.Sorted(maps.Keys(h.profiles)) {
				r.Profiles = append(r.Profiles, cmp.Or(names[name], name))
			}

			r.DepOf = slices.Sorted(maps.Keys(h.deps))
			r.Old = !h.active
		}

		rows = append(rows, r)
	}

	if wantJSON(cmd) {
		return printJSON(cmd, rows)
	}

	out := cmd.OutOrStdout()

	if len(rows) == 0 {
		hint(out, "the store is empty, `oku add <ref>` installs a package")

		return nil
	}

	s := ui.For(out)
	tab := s.Table("store path", "size", "kept by")

	for _, r := range rows {
		var kept []string

		for _, name := range r.Profiles {
			kept = append(kept, s.Home(name))
		}

		if len(r.DepOf) > 0 {
			kept = append(kept, "dep of "+strings.Join(r.DepOf, ", "))
		}

		keptBy, style := strings.Join(kept, ", "), s.Dim

		switch {
		case r.Unused:
			keptBy, style = "unused", s.Warn
		case r.Old:
			keptBy = "old: " + keptBy
		}

		// Builds of one version against other deps differ only in the hash.
		tab.Styled(
			[]string{filepath.Base(r.Path), status.Size(r.Bytes), keptBy},
			s.Bold,
			nil,
			style,
		)
	}

	return tab.Write(out)
}

// meter adds up the bytes of files. A file with more hard links than one, and
// a content that store paths share, counts once, at the first path the meter
// reads.
type meter struct {
	seen   map[fileID]bool
	shared map[string]bool
}

// size is the bytes of the files under path, or of path itself when it is a
// file. What it cannot read counts as nothing.
func (m *meter) size(path string) int64 {
	return m.storeSize(path, nil)
}

// storeSize is size for a store path, and counts each file of files once by
// its content.
func (m *meter) storeSize(path string, files map[string]store.Shared) int64 {
	var size int64

	_ = filepath.WalkDir(path, func(file string, entry fs.DirEntry, err error) error {
		if err != nil || entry.IsDir() {
			return nil
		}

		info, err := entry.Info()
		if err != nil {
			return nil
		}

		id, linked := idOf(file, info)

		if rel, err := filepath.Rel(path, file); err == nil {
			if f, ok := files[filepath.ToSlash(rel)]; ok {
				if m.shared[f.Key] {
					return nil
				}

				m.shared[f.Key] = true
			}
		}

		if linked {
			if m.seen[id] {
				return nil
			}

			m.seen[id] = true
		}

		size += info.Size()

		return nil
	})

	return size
}

// sizeExcept is size of the entries of dir other than those named in skip.
func (m *meter) sizeExcept(dir string, skip ...string) int64 {
	entries, err := os.ReadDir(dir)
	if err != nil {
		return 0
	}

	var size int64

	for _, entry := range entries {
		if !slices.Contains(skip, entry.Name()) {
			size += m.size(filepath.Join(dir, entry.Name()))
		}
	}

	return size
}
