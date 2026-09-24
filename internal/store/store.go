// Package store realizes packages into immutable directories named
// <name>-<version>-<hash>.
package store

import (
	"bufio"
	"context"
	"crypto/sha256"
	"encoding/hex"
	"errors"
	"fmt"
	"io"
	"io/fs"
	"net/http"
	"os"
	"path"
	"path/filepath"
	"regexp"
	"runtime"
	"slices"
	"strings"

	"github.com/pelletier/go-toml/v2"

	"github.com/y3owk1n/oku/internal/forge"
	"github.com/y3owk1n/oku/internal/infer"
	"github.com/y3owk1n/oku/internal/manifest"
	"github.com/y3owk1n/oku/internal/platform"
	"github.com/y3owk1n/oku/internal/sandbox"
	"github.com/y3owk1n/oku/internal/status"
	"github.com/y3owk1n/oku/internal/tempdir"
)

// metaFile is the description oku writes into every store path.
const metaFile = "oku-meta.toml"

// ErrPinConflict reports a manifest digest that differs from the one oku.lock
// pinned for the same version and URL.
var ErrPinConflict = errors.New("checksum changed")

// Store is the directory of realized packages plus the download cache.
type Store struct {
	dir   string
	cache string
	http  *http.Client
	// auth is the login for downloads from one host.
	auth forge.Auth
	// Private finds where the API serves a download that answered 404, such as
	// a release asset of a private GitHub repo, and the header it needs. It
	// returns "" when it knows no other address.
	Private func(ctx context.Context, url string) (string, string, error)
}

// As returns a store that sends auth with the downloads it is for.
func (s *Store) As(auth forge.Auth) *Store {
	with := *s
	with.auth = auth

	return &with
}

// Meta describes a realized package.
type Meta struct {
	Name     string `toml:"name"`
	Version  string `toml:"version"`
	Platform string `toml:"platform"`
	URL      string `toml:"url"`
	SHA256   string `toml:"sha256"`
	// Impure marks a build whose run steps could use the network.
	Impure bool `toml:"impure,omitempty"`
	// VendorSHA256 is the digest of what the vendor steps of a build downloaded.
	// For a build, URL and SHA256 are its source archive.
	VendorSHA256 string `toml:"vendor_sha256,omitempty"`
	// Launchers are the package's Linux desktop entries.
	Launchers []manifest.App `toml:"launcher,omitempty"`
	// Services are the package's long-running programs.
	Services []manifest.Service `toml:"service,omitempty"`
}

// New returns the store under dataDir that caches downloads under cacheDir.
func New(dataDir, cacheDir string) *Store {
	transport := http.DefaultTransport.(*http.Transport).Clone()
	transport.RegisterProtocol("file", http.NewFileTransport(http.Dir("/")))

	return &Store{
		dir:   filepath.Join(dataDir, "store"),
		cache: cacheDir,
		http:  &http.Client{Transport: transport},
	}
}

// Realized is a package in the store.
type Realized struct {
	Path string
	// SHA256 is the digest of the artifact download, or of the source archive of
	// a build, whose expanded address is SourceURL.
	SHA256    string
	SourceURL string
	// FirstUse reports that neither the manifest nor the caller gave a digest, so
	// oku accepted the download unverified.
	FirstUse bool
	// Unsandboxed says why a build ran without the sandbox, or is empty.
	Unsandboxed string
	// Impure reports that a run step asked for the network.
	Impure bool
	// VendorSHA256 is the digest of what the vendor steps downloaded, or empty.
	VendorSHA256 string
	// UnnamedScripts are the packages of an npm step whose install scripts did
	// not run, because its scripts does not name them.
	UnnamedScripts []string
	// MissingDeps are the store packages a build loads that are not runtime deps.
	MissingDeps []MissingDep
}

// Has reports whether artifact a of m with that digest is in the store.
func (s *Store) Has(
	m *manifest.Manifest,
	a manifest.Artifact,
	p platform.Platform,
	sha256 string,
	deps []Dep,
) bool {
	return sha256 != "" && exists(s.artifactPath(m, a, p, sha256, deps))
}

// artifactPath returns the store path of artifact a with that digest. A wrapper
// holds the paths of the store and of the deps, so they count for an artifact
// that has one.
func (s *Store) artifactPath(
	m *manifest.Manifest,
	a manifest.Artifact,
	p platform.Platform,
	sha256 string,
	deps []Dep,
) string {
	extra := []string{sha256}

	if len(a.Wrap) > 0 {
		extra = append(extra, "wrap", s.dir)
		for _, dep := range deps {
			extra = append(extra, filepath.Base(dep.Prefix))
		}

		// See BuildPath.
		if len(deps) > 0 {
			extra = append(extra, "path")
		}
	}

	return s.pathFor(m, p, extra...)
}

// Realize downloads, verifies and unpacks artifact a of manifest m. It returns
// an existing store path untouched, and it leaves the store unchanged on any
// failure.
//
// Realize expects the first digest it finds in a.SHA256, the file at
// a.SHA256URL, and pinned. pinned is the digest oku.lock recorded earlier. With
// none of them, Realize trusts the download. deps are the runtime deps, which a
// wrapper may name.
func (s *Store) Realize(
	ctx context.Context,
	m *manifest.Manifest,
	a manifest.Artifact,
	p platform.Platform,
	pinned string,
	deps []Dep,
) (Realized, error) {
	// The build oku.lock pinned was verified when it entered the store, so it
	// needs no second look at the published checksum.
	if pinned != "" && (a.SHA256 == "" || a.SHA256 == pinned) {
		if final := s.artifactPath(m, a, p, pinned, deps); exists(final) {
			return Realized{Path: final, SHA256: pinned}, nil
		}
	}

	want := a.SHA256
	if want == "" && a.SHA256URL != "" {
		published, err := s.PublishedSHA256(ctx, a.SHA256URL, path.Base(a.URL))
		if err != nil {
			return Realized{}, err
		}

		want = published
	}

	if want != "" && pinned != "" && want != pinned {
		return Realized{}, fmt.Errorf(
			"%s: %w: upstream publishes sha256 %s, oku.lock pinned %s",
			m.Package.Name, ErrPinConflict, want, pinned,
		)
	}

	if want == "" {
		want = pinned
	}

	if want != "" {
		if final := s.artifactPath(m, a, p, want, deps); exists(final) {
			return Realized{Path: final, SHA256: want}, nil
		}
	}

	download, got, err := s.fetch(ctx, a.URL, want)
	if err != nil {
		return Realized{}, err
	}

	realized := Realized{
		Path: s.artifactPath(m, a, p, got, deps), SHA256: got, FirstUse: want == "",
	}
	if exists(realized.Path) {
		return realized, nil
	}

	vouched, err := s.vouched(ctx, m, a, download)
	if err != nil {
		return Realized{}, err
	}

	if vouched {
		realized.FirstUse = false
	}

	a.SHA256 = got
	final := realized.Path

	if err := os.MkdirAll(s.dir, 0o755); err != nil {
		return Realized{}, fmt.Errorf("create store: %w", err)
	}

	tmp, err := os.MkdirTemp(s.dir, ".tmp-")
	if err != nil {
		return Realized{}, fmt.Errorf("create store: %w", err)
	}
	defer os.RemoveAll(tmp)

	done := status.Start(ctx, "unpacking %s", path.Base(a.URL))
	err = unpack(download, tmp, a)

	done()

	if err != nil {
		return Realized{}, fmt.Errorf("unpack %s: %w", a.URL, err)
	}

	if err := linkOutputs(tmp, a); err != nil {
		return Realized{}, err
	}

	if err := writeWrappers(tmp, final, m, a, p, deps); err != nil {
		return Realized{}, err
	}

	if a.Completions.Generate != "" {
		if realized.Unsandboxed, err = s.generateArtifactCompletions(ctx, a, tmp); err != nil {
			return Realized{}, fmt.Errorf("%s: %w", m.Package.Name, err)
		}
	}

	meta, err := toml.Marshal(Meta{
		Name:      m.Package.Name,
		Version:   m.Version.Value,
		Platform:  p.String(),
		URL:       a.URL,
		SHA256:    a.SHA256,
		Launchers: m.Apps,
		Services:  m.ServicesFor(p),
	})
	if err != nil {
		return Realized{}, fmt.Errorf("write %s: %w", metaFile, err)
	}

	if err := os.WriteFile(filepath.Join(tmp, metaFile), meta, 0o644); err != nil {
		return Realized{}, fmt.Errorf("write %s: %w", metaFile, err)
	}

	// Another install of the same package may have put it there first.
	if err := os.Rename(tmp, final); err != nil && !exists(final) {
		return Realized{}, fmt.Errorf("move package into store: %w", err)
	}

	return realized, nil
}

// generateArtifactCompletions runs the artifact's completions command in the
// unpacked package under tmp, with the package's own bin first on PATH, and
// writes the files under tmp/share/completions.
func (s *Store) generateArtifactCompletions(
	ctx context.Context,
	a manifest.Artifact,
	tmp string,
) (string, error) {
	work, err := tempdir.Dir("completions")
	if err != nil {
		return "", err
	}
	defer os.RemoveAll(work)

	for _, dir := range []string{filepath.Join(work, "home"), filepath.Join(work, "tmp")} {
		if err := os.Mkdir(dir, 0o755); err != nil {
			return "", err
		}
	}

	systemDirs, env := hostEnv(filepath.Join(work, "home"), filepath.Join(work, "tmp"))
	env = append(env, "PATH="+joinPaths(append([]string{filepath.Join(tmp, "bin")}, systemDirs...)))

	home, _ := os.UserHomeDir()
	box := sandbox.Spec{Home: home, Readable: []string{s.dir}, Writable: []string{tmp, work}}

	done := status.Start(ctx, "generating completions")
	defer done()

	return generateCompletions(ctx, a.Completions, filepath.Join(tmp, "pkg"), tmp, env, box, nil)
}

// vouched checks download against the integrity value of a and the signing key
// of m. It reports whether one of them vouched for the download, which then is
// not a first use.
func (s *Store) vouched(
	ctx context.Context,
	m *manifest.Manifest,
	a manifest.Artifact,
	download string,
) (bool, error) {
	if a.Integrity != "" {
		if err := verifyIntegrity(download, a.Integrity); err != nil {
			os.Remove(download)

			return false, fmt.Errorf("%s: %s: %w", m.Package.Name, a.URL, err)
		}
	}

	if m.Package.SigningKey != "" {
		if err := s.verifySignature(ctx, m.Package.SigningKey, a.URL, download); err != nil {
			return false, fmt.Errorf("%s: %w", m.Package.Name, err)
		}
	}

	return a.Integrity != "" || m.Package.SigningKey != "", nil
}

// Pin returns the sha256 that oku.lock holds for artifact a of m, and whether
// oku trusted a download for it. It takes the first digest it finds in
// a.SHA256 and the file at a.SHA256URL. With neither it downloads a and checks
// it the way Realize does, and it unpacks nothing, so a may be for another
// platform.
func (s *Store) Pin(
	ctx context.Context,
	m *manifest.Manifest,
	a manifest.Artifact,
) (string, bool, error) {
	if a.SHA256 != "" {
		return a.SHA256, false, nil
	}

	if a.SHA256URL != "" {
		published, err := s.PublishedSHA256(ctx, a.SHA256URL, path.Base(a.URL))

		return published, false, err
	}

	download, got, err := s.fetch(ctx, a.URL, "")
	if err != nil {
		return "", false, err
	}

	vouched, err := s.vouched(ctx, m, a, download)

	return got, !vouched, err
}

// pathFor names the store path of m. extra is the artifact digest, or "build"
// followed by the store paths of the deps the build links against.
func (s *Store) pathFor(m *manifest.Manifest, p platform.Platform, extra ...string) string {
	sum := sha256.Sum256([]byte(strings.Join(
		append([]string{m.SHA256, m.Version.Value, p.String(), "artifact"}, extra...), "\n",
	)))

	return filepath.Join(s.dir, fmt.Sprintf(
		"%s-%s-%s", m.Package.Name, m.Version.Value, hex.EncodeToString(sum[:])[:16],
	))
}

func exists(path string) bool {
	_, err := os.Stat(path)

	return err == nil
}

// unpack fills <tmp>/pkg from the download. A download that is not an archive
// is the binary itself, compressed or not, and takes the name of the artifact's
// single bin entry. With a single wrapper instead, it keeps the name it has in
// the URL, so the wrapper can run it as {{pkg}}/<name>.
func unpack(download, tmp string, a manifest.Artifact) error {
	pkg := filepath.Join(tmp, "pkg")
	if err := os.Mkdir(pkg, 0o755); err != nil {
		return err
	}

	err := extract(download, pkg, a.Strip)
	if !errors.Is(err, errNotArchive) {
		return err
	}

	if len(a.Bin)+len(a.Wrap) != 1 || len(a.Man) > 0 || len(a.Completions.Paths) > 0 {
		return errors.New(
			"the download is a single file, so the artifact must list exactly one bin and nothing else",
		)
	}

	name := singleFileName(a.URL)
	if len(a.Bin) == 1 {
		name = a.Bin[0]
	}

	if !filepath.IsLocal(filepath.FromSlash(name)) {
		return fmt.Errorf("bin %q: the path is outside the package", name)
	}

	in, err := os.Open(download)
	if err != nil {
		return err
	}
	defer in.Close()

	binary, err := decompress(in)
	if err != nil {
		return err
	}

	buffered := bufio.NewReader(binary)

	head, err := buffered.Peek(512)
	if err != nil && !errors.Is(err, io.EOF) {
		return fmt.Errorf("decompress: %w", err)
	}

	if err := checkSingleFile(head, name, len(a.Wrap) == 1); err != nil {
		return err
	}

	return writeNew(buffered, filepath.Join(pkg, filepath.FromSlash(name)))
}

// singleFileName names a download that is the program itself after the last
// part of its URL, without the suffix of a compression.
func singleFileName(url string) string {
	name := path.Base(url)

	for _, suffix := range []string{".gz", ".xz", ".bz2", ".zst"} {
		name = strings.TrimSuffix(name, suffix)
	}

	return name
}

var manSectionRe = regexp.MustCompile(`\.([1-9])[a-z]*(\.gz)?$`)

// linkOutputs links the artifact's outputs from <tmp>/pkg into <tmp>/bin and
// <tmp>/share. Links are relative so they still resolve after the move into the
// store.
func linkOutputs(tmp string, a manifest.Artifact) error {
	bins := make([][2]string, 0, len(a.Bin)+len(a.Wrap))
	for _, entry := range a.Bin {
		bins = append(bins, [2]string{entry, path.Base(entry)})
	}

	// A table with a path exposes the file under the table's name.
	for _, w := range a.Wrap {
		if w.Path != "" {
			bins = append(bins, [2]string{w.Path, w.Name})
		}
	}

	for _, bin := range bins {
		entry, name := bin[0], bin[1]
		if err := link(tmp, entry, path.Join("bin", name)); err != nil {
			return fmt.Errorf("bin %q: %w", entry, err)
		}

		if err := os.Chmod(
			filepath.Join(tmp, "pkg", filepath.FromSlash(entry)),
			0o755,
		); err != nil {
			return fmt.Errorf("bin %q: %w", entry, err)
		}
	}

	mans, err := expandGlobs(filepath.Join(tmp, "pkg"), "man", a.Man)
	if err != nil {
		return err
	}

	for _, entry := range mans {
		section := manSectionRe.FindStringSubmatch(entry)
		if section == nil {
			return fmt.Errorf("man %q: the file name has no section such as .1", entry)
		}

		dest := path.Join("share", "man", "man"+section[1], path.Base(entry))
		if err := link(tmp, entry, dest); err != nil {
			return fmt.Errorf("man %q: %w", entry, err)
		}
	}

	completions, err := completionPaths(a.Completions, filepath.Join(tmp, "pkg"))
	if err != nil {
		return err
	}

	for shell, entry := range completions {
		if err := link(tmp, entry, completionDest(shell, entry)); err != nil {
			return fmt.Errorf("completions.%s %q: %w", shell, entry, err)
		}
	}

	// A build that depends on this package looks in these three directories.
	for dir, entries := range map[string][]string{
		"lib": a.Lib, "include": a.Include, "share": a.Share,
	} {
		for _, entry := range entries {
			if err := linkAny(tmp, entry, path.Join(dir, path.Base(entry))); err != nil {
				return fmt.Errorf("%s %q: %w", dir, entry, err)
			}
		}
	}

	fonts, err := expandGlobs(filepath.Join(tmp, "pkg"), "font", a.Font)
	if err != nil {
		return err
	}

	for _, entry := range fonts {
		if err := link(tmp, entry, path.Join("fonts", path.Base(entry))); err != nil {
			return fmt.Errorf("font %q: %w", entry, err)
		}
	}

	for _, entry := range a.App {
		if err := linkDir(tmp, entry, path.Join("apps", path.Base(entry))); err != nil {
			return fmt.Errorf("app %q: %w", entry, err)
		}
	}

	return nil
}

// expandGlobs replaces each entry that holds "*", "?" or "**" with the files
// under root it matches, sorted, and keeps a literal entry as it is. "*" and "?"
// match within one path segment, and "**" matches any number of segments, so a
// font family that ships dozens of files needs one line. Field names the
// manifest key for the error.
func expandGlobs(root, field string, entries []string) ([]string, error) {
	var out []string

	for _, entry := range entries {
		if !strings.ContainsAny(entry, "*?") {
			out = append(out, entry)

			continue
		}

		pattern := strings.Split(entry, "/")
		for _, segment := range pattern {
			if _, err := path.Match(segment, ""); err != nil {
				return nil, fmt.Errorf("%s %q is not a valid pattern", field, entry)
			}
		}

		var matched []string

		err := filepath.WalkDir(root, func(file string, info fs.DirEntry, err error) error {
			if err != nil || !info.Type().IsRegular() {
				return err
			}

			rel, err := filepath.Rel(root, file)
			if err != nil {
				return err
			}

			rel = filepath.ToSlash(rel)
			if matchSegments(pattern, strings.Split(rel, "/")) {
				matched = append(matched, rel)
			}

			return nil
		})
		if err != nil {
			return nil, err
		}

		if len(matched) == 0 {
			return nil, fmt.Errorf("%s %q matches no file in the package", field, entry)
		}

		slices.Sort(matched)

		out = append(out, matched...)
	}

	return out, nil
}

// matchSegments matches a validated pattern against a path, segment by segment.
// "**" may stand for zero or more segments.
func matchSegments(pattern, segments []string) bool {
	for len(pattern) > 0 {
		if pattern[0] == "**" {
			for skip := 0; skip <= len(segments); skip++ {
				if matchSegments(pattern[1:], segments[skip:]) {
					return true
				}
			}

			return false
		}

		if len(segments) == 0 {
			return false
		}

		if ok, _ := path.Match(pattern[0], segments[0]); !ok {
			return false
		}

		pattern, segments = pattern[1:], segments[1:]
	}

	return len(segments) == 0
}

// linkAny links a file or a whole directory, such as include/webp.
func linkAny(tmp, entry, dest string) error {
	if !filepath.IsLocal(filepath.FromSlash(entry)) {
		return errors.New("the path is outside the package")
	}

	info, err := os.Stat(filepath.Join(tmp, "pkg", filepath.FromSlash(entry)))
	if err != nil {
		return errors.New("no such file or directory in the package")
	}

	if info.IsDir() {
		return linkDir(tmp, entry, dest)
	}

	return link(tmp, entry, dest)
}

// linkDir is link for a directory, which is what a macOS app bundle is.
func linkDir(tmp, entry, dest string) error {
	if !filepath.IsLocal(filepath.FromSlash(entry)) {
		return errors.New("the path is outside the package")
	}

	source := filepath.Join(tmp, "pkg", filepath.FromSlash(entry))

	info, err := os.Stat(source)
	if err != nil || !info.IsDir() {
		return errors.New("no such directory in the package")
	}

	destPath := filepath.Join(tmp, filepath.FromSlash(dest))
	if err := os.MkdirAll(filepath.Dir(destPath), 0o755); err != nil {
		return err
	}

	rel, err := filepath.Rel(filepath.Dir(destPath), source)
	if err != nil {
		return err
	}

	err = os.Symlink(rel, destPath)
	if err == nil || errors.Is(err, os.ErrExist) || runtime.GOOS != "windows" {
		return err
	}

	// Windows refuses a symlink without developer mode, so the directory is copied.
	return filepath.WalkDir(source, func(file string, entry fs.DirEntry, err error) error {
		if err != nil || entry.IsDir() {
			return err
		}

		inside, err := filepath.Rel(source, file)
		if err != nil {
			return err
		}

		return copyFile(file, filepath.Join(destPath, inside))
	})
}

// link makes <tmp>/<dest> point at <tmp>/pkg/<entry>. It copies the file where
// the OS refuses symlinks, which Windows does without developer mode.
func link(tmp, entry, dest string) error {
	if !filepath.IsLocal(filepath.FromSlash(entry)) {
		return errors.New("the path is outside the package")
	}

	source := filepath.Join(tmp, "pkg", filepath.FromSlash(entry))

	info, err := os.Stat(source)
	if errors.Is(err, fs.ErrNotExist) {
		return errors.New("no such file in the package")
	}

	if err != nil {
		return err
	}

	if !info.Mode().IsRegular() {
		return errors.New("not a regular file")
	}

	destPath := filepath.Join(tmp, filepath.FromSlash(dest))
	if err := os.MkdirAll(filepath.Dir(destPath), 0o755); err != nil {
		return err
	}

	rel, err := filepath.Rel(filepath.Dir(destPath), source)
	if err != nil {
		return err
	}

	if err := os.Symlink(rel, destPath); err != nil {
		if errors.Is(err, os.ErrExist) {
			return fmt.Errorf("two outputs are both named %s", path.Base(dest))
		}

		return copyFile(source, destPath)
	}

	return nil
}

func copyFile(source, dest string) error {
	if err := os.MkdirAll(filepath.Dir(dest), 0o755); err != nil {
		return err
	}

	in, err := os.Open(source)
	if err != nil {
		return err
	}
	defer in.Close()

	return writeNew(in, dest)
}

// writeNew writes in to the new file dest.
func writeNew(in io.Reader, dest string) error {
	out, err := os.OpenFile(dest, os.O_CREATE|os.O_WRONLY|os.O_EXCL, 0o755)
	if err != nil {
		return err
	}

	_, err = io.Copy(out, in)
	if closeErr := out.Close(); err == nil {
		err = closeErr
	}

	return err
}

// Unreferenced returns the store paths that are not in keep, with their sizes in
// bytes. The caller holds the lock of the busy package, so a temporary directory
// is left over from an install that was killed.
func (s *Store) Unreferenced(keep map[string]bool) (map[string]int64, error) {
	entries, err := os.ReadDir(s.dir)
	if err != nil && !errors.Is(err, fs.ErrNotExist) {
		return nil, fmt.Errorf("read store: %w", err)
	}

	found := map[string]int64{}

	for _, entry := range entries {
		path := filepath.Join(s.dir, entry.Name())
		if !entry.IsDir() || keep[path] {
			continue
		}

		// A rebuild that a crash interrupted moved the old build to <path>.old. The
		// next install of the package moves it back.
		if keep[strings.TrimSuffix(path, ".old")] {
			continue
		}

		var size int64

		err := filepath.WalkDir(path, func(_ string, item fs.DirEntry, err error) error {
			if err != nil || item.IsDir() {
				return err
			}

			info, err := item.Info()
			if err != nil {
				return err
			}

			size += info.Size()

			return nil
		})
		if err != nil {
			return nil, fmt.Errorf("measure %s: %w", path, err)
		}

		found[path] = size
	}

	return found, nil
}

// Tree unpacks the tar.gz of a repo's files that download returns into the
// store path name, and returns that path. It drops strip directories from each
// entry. A path that the store holds already is used as it is, with no
// download, because name is unique to the repo and the commit.
func (s *Store) Tree(name string, download func() ([]byte, int, error)) (string, error) {
	final := filepath.Join(s.dir, name)
	if exists(final) {
		return final, nil
	}

	archive, strip, err := download()
	if err != nil {
		return "", err
	}

	if err := os.MkdirAll(s.dir, 0o755); err != nil {
		return "", fmt.Errorf("create store: %w", err)
	}

	tmp, err := os.MkdirTemp(s.dir, ".tmp-")
	if err != nil {
		return "", err
	}
	defer os.RemoveAll(tmp)

	src := filepath.Join(tmp, "archive.tar.gz")
	if err := os.WriteFile(src, archive, 0o600); err != nil {
		return "", err
	}

	files := filepath.Join(tmp, "files")
	if err := os.Mkdir(files, 0o755); err != nil {
		return "", err
	}

	if err := extract(src, files, strip); err != nil {
		return "", fmt.Errorf("unpack the files of %s: %w", name, err)
	}

	if err := os.Rename(files, final); err != nil {
		return "", err
	}

	return final, nil
}

// Holding returns the store path that file is in, or false for a file outside
// the store.
func (s *Store) Holding(file string) (string, bool) {
	rel, err := filepath.Rel(s.dir, file)
	if err != nil || !filepath.IsLocal(rel) {
		return "", false
	}

	top, _, _ := strings.Cut(filepath.ToSlash(rel), "/")

	return filepath.Join(s.dir, top), true
}

// Remove deletes one store path. It refuses a path outside the store.
func (s *Store) Remove(path string) error {
	if filepath.Dir(path) != s.dir {
		return fmt.Errorf("%s is not a store path", path)
	}

	return os.RemoveAll(path)
}

// Inspect downloads url into the cache, unpacks it into a temporary directory
// and returns its regular files. A download that is not an archive is one
// executable file.
func (s *Store) Inspect(ctx context.Context, url string) ([]infer.File, error) {
	download, _, err := s.fetch(ctx, url, "")
	if err != nil {
		return nil, err
	}

	tmp, err := tempdir.Dir("inspect")
	if err != nil {
		return nil, err
	}
	defer os.RemoveAll(tmp)

	err = extract(download, tmp, 0)
	if errors.Is(err, errNotArchive) {
		name := singleFileName(url)
		if err := checkHead(download, name); err != nil {
			return nil, err
		}

		return []infer.File{{Path: path.Base(url), Executable: true}}, nil
	}

	if err != nil {
		return nil, err
	}

	var files []infer.File

	err = filepath.WalkDir(tmp, func(p string, entry fs.DirEntry, err error) error {
		if err != nil || !entry.Type().IsRegular() {
			return err
		}

		info, err := entry.Info()
		if err != nil {
			return err
		}

		rel, err := filepath.Rel(tmp, p)
		if err != nil {
			return err
		}

		files = append(files, infer.File{
			Path:       filepath.ToSlash(rel),
			Executable: info.Mode()&0o111 != 0 || strings.HasSuffix(p, ".exe"),
		})

		return nil
	})

	return files, err
}

// Hashes downloads url into the cache and returns the two digests a manifest
// can state for it: its sha256, and its sha512 as an integrity value.
func (s *Store) Hashes(ctx context.Context, url string) (string, string, error) {
	download, sum, err := s.fetch(ctx, url, "")
	if err != nil {
		return "", "", err
	}

	integrity, err := fileIntegrity(download)

	return sum, integrity, err
}

// Digest downloads url into the cache and returns its sha256.
func (s *Store) Digest(ctx context.Context, url string) (string, error) {
	_, sum, err := s.fetch(ctx, url, "")

	return sum, err
}

// ReadMeta reads the description of the package at storePath.
func ReadMeta(storePath string) (Meta, error) {
	var meta Meta

	data, err := os.ReadFile(filepath.Join(storePath, metaFile))
	if err != nil {
		return meta, err
	}

	return meta, toml.Unmarshal(data, &meta)
}
