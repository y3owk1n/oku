// Package platform detects the host and matches manifest selectors against it.
package platform

import (
	"fmt"
	"path/filepath"
	"runtime"
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
