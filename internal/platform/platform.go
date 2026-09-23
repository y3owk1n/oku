// Package platform detects the host and matches manifest selectors against it.
package platform

import (
	"errors"
	"fmt"
	"path/filepath"
	"runtime"
	"slices"
	"strings"
)

const (
	LibcGlibc = "glibc"
	LibcMusl  = "musl"
)

// Platform identifies a host. OS and Arch use GOOS and GOARCH values. Libc is
// empty outside linux.
type Platform struct {
	OS   string
	Arch string
	Libc string
}

// Selector is a manifest match table. An empty field matches anything.
type Selector struct {
	OS   string `toml:"os"`
	Arch string `toml:"arch"`
	Libc string `toml:"libc"`
}

// Host returns the platform oku is running on.
func Host() Platform {
	p := Platform{OS: runtime.GOOS, Arch: runtime.GOARCH}
	if p.OS == "linux" {
		p.Libc = detectLibc()
	}

	return p
}

// String renders the platform the way oku.lock keys it, such as
// "linux-amd64-musl" or "darwin-arm64".
func (p Platform) String() string {
	s := p.OS + "-" + p.Arch
	if p.Libc != "" {
		s += "-" + p.Libc
	}

	return s
}

// All lists the platforms oku runs on.
func All() []Platform {
	return []Platform{
		{OS: "darwin", Arch: "amd64"},
		{OS: "darwin", Arch: "arm64"},
		{OS: "linux", Arch: "amd64", Libc: LibcGlibc},
		{OS: "linux", Arch: "amd64", Libc: LibcMusl},
		{OS: "linux", Arch: "arm64", Libc: LibcGlibc},
		{OS: "linux", Arch: "arm64", Libc: LibcMusl},
		{OS: "windows", Arch: "amd64"},
		{OS: "windows", Arch: "arm64"},
	}
}

// Parse reads a platform the way String writes it.
func Parse(s string) (Platform, error) {
	names := make([]string, 0, len(All()))

	for _, p := range All() {
		if p.String() == s {
			return p, nil
		}

		names = append(names, p.String())
	}

	return Platform{}, fmt.Errorf(
		"%q is not a platform, use one of %s",
		s,
		strings.Join(names, ", "),
	)
}

// TOML renders the selector as an inline table, such as `{ os = "darwin" }`.
func (s Selector) TOML() string {
	var parts []string

	for _, field := range []struct{ key, value string }{
		{"os", s.OS}, {"arch", s.Arch}, {"libc", s.Libc},
	} {
		if field.value != "" {
			parts = append(parts, fmt.Sprintf("%s = %q", field.key, field.value))
		}
	}

	return "{ " + strings.Join(parts, ", ") + " }"
}

// Matches reports whether every non-empty selector field equals the platform's.
func (s Selector) Matches(p Platform) bool {
	return (s.OS == "" || s.OS == p.OS) &&
		(s.Arch == "" || s.Arch == p.Arch) &&
		(s.Libc == "" || s.Libc == p.Libc)
}

// detectLibc reports musl when the musl dynamic loader exists on disk. oku is a
// static binary with no ELF interpreter to read, so it checks the filesystem.
func detectLibc() string {
	if m, _ := filepath.Glob("/lib/ld-musl-*.so.1"); len(m) > 0 {
		return LibcMusl
	}

	return LibcGlibc
}

// When limits a package to the platforms that any of its selectors matches.
// The empty When matches every platform.
type When []Selector

// ParseWhen reads a when value as TOML gives it: one table, or an array of
// tables. A missing one matches every platform.
func ParseWhen(value any) (When, error) {
	var tables []any

	switch v := value.(type) {
	case nil:
		return nil, nil
	case map[string]any:
		tables = []any{v}
	case []any:
		if len(v) == 0 {
			return nil, errors.New("when is an empty array, which matches no platform")
		}

		tables = v
	default:
		return nil, errors.New("when is a table or an array of tables")
	}

	w := make(When, 0, len(tables))

	for _, t := range tables {
		table, ok := t.(map[string]any)
		if !ok {
			return nil, errors.New("when is a table or an array of tables")
		}

		var sel Selector

		for key, field := range table {
			target, known := map[string]*string{"os": &sel.OS, "arch": &sel.Arch, "libc": &sel.Libc}[key]
			if !known {
				return nil, fmt.Errorf("when.%s is not a selector key, use os, arch or libc", key)
			}

			value, ok := field.(string)
			if !ok {
				return nil, fmt.Errorf("when.%s is a string", key)
			}

			*target = value
		}

		w = append(w, sel)
	}

	return w, nil
}

// Matches reports whether w is empty or one of its selectors matches p.
func (w When) Matches(p Platform) bool {
	return len(w) == 0 || slices.ContainsFunc(w, func(s Selector) bool { return s.Matches(p) })
}

// TOML renders w as one inline table, or as an array of them.
func (w When) TOML() string {
	if len(w) == 1 {
		return w[0].TOML()
	}

	tables := make([]string, len(w))
	for i, s := range w {
		tables[i] = s.TOML()
	}

	return "[" + strings.Join(tables, ", ") + "]"
}

// Of returns the platforms of All that w matches.
func (w When) Of() []Platform {
	return slices.DeleteFunc(All(), func(p Platform) bool { return !w.Matches(p) })
}

// Cover returns the shortest When that matches exactly the platforms in set,
// with a table for each OS where it can. It is empty when set holds every
// platform.
func Cover(set []Platform) When {
	// in reports whether every platform that s matches is in set.
	in := func(s Selector) bool {
		return !slices.ContainsFunc(All(), func(p Platform) bool {
			return s.Matches(p) && !slices.Contains(set, p)
		})
	}

	if in(Selector{}) {
		return nil
	}

	var (
		w       When
		covered = map[Platform]bool{}
	)

	for _, p := range All() {
		if !slices.Contains(set, p) || covered[p] {
			continue
		}

		// The widest selector that matches no platform outside set wins.
		for _, s := range []Selector{
			{OS: p.OS},
			{OS: p.OS, Arch: p.Arch},
			{OS: p.OS, Libc: p.Libc},
			{OS: p.OS, Arch: p.Arch, Libc: p.Libc},
		} {
			if !in(s) {
				continue
			}

			w = append(w, s)

			for _, q := range All() {
				if s.Matches(q) {
					covered[q] = true
				}
			}

			break
		}
	}

	return w
}
