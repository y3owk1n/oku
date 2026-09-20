// Package store realizes packages into immutable directories named
// <name>-<version>-<hash>.
package store

import (
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
	"strings"

	"github.com/pelletier/go-toml/v2"

	"github.com/y3owk1n/oku/internal/infer"
	"github.com/y3owk1n/oku/internal/manifest"
	"github.com/y3owk1n/oku/internal/platform"
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
	// SHA256 is the digest of the artifact download.
	SHA256 string
	// FirstUse reports that neither the manifest nor the caller gave a digest, so
	// oku accepted the download unverified.
	FirstUse bool
	// Unsandboxed says why a build ran without the sandbox, or is empty.
	Unsandboxed string
	// Impure reports that a run step asked for the network.
	Impure bool
	// VendorSHA256 is the digest of what the vendor steps downloaded, or empty.
	VendorSHA256 string
}

// Realize downloads, verifies and unpacks artifact a of manifest m. It returns
// an existing store path untouched, and it leaves the store unchanged on any
// failure.
//
// Realize expects the first digest it finds in a.SHA256, the file at
// a.SHA256URL, and pinned. pinned is the digest oku.lock recorded earlier. With
// none of them, Realize trusts the download.
func (s *Store) Realize(
	ctx context.Context,
	m *manifest.Manifest,
	a manifest.Artifact,
	p platform.Platform,
	pinned string,
) (Realized, error) {
	want := a.SHA256
	if want == "" && a.SHA256URL != "" {
		published, err := s.publishedSHA256(ctx, a.SHA256URL, path.Base(a.URL))
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
		if final := s.pathFor(m, p, want); exists(final) {
			return Realized{Path: final, SHA256: want}, nil
		}
	}

	download, got, err := s.fetch(ctx, a.URL, want)
	if err != nil {
		return Realized{}, err
	}

	realized := Realized{Path: s.pathFor(m, p, got), SHA256: got, FirstUse: want == ""}
	if exists(realized.Path) {
		return realized, nil
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

	if err := unpack(download, tmp, a); err != nil {
		return Realized{}, fmt.Errorf("unpack %s: %w", a.URL, err)
	}

	if err := expose(tmp, a); err != nil {
		return Realized{}, err
	}

	meta, err := toml.Marshal(Meta{
		Name:     m.Package.Name,
		Version:  m.Version.Value,
		Platform: p.String(),
		URL:      a.URL,
		SHA256:   a.SHA256,
	})
	if err != nil {
		return Realized{}, fmt.Errorf("write %s: %w", metaFile, err)
	}

	if err := os.WriteFile(filepath.Join(tmp, metaFile), meta, 0o644); err != nil {
		return Realized{}, fmt.Errorf("write %s: %w", metaFile, err)
	}

	if err := os.Rename(tmp, final); err != nil {
		return Realized{}, fmt.Errorf("move package into store: %w", err)
	}

	return realized, nil
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
// is the binary itself and takes the name of the artifact's single bin entry.
func unpack(download, tmp string, a manifest.Artifact) error {
	pkg := filepath.Join(tmp, "pkg")
	if err := os.Mkdir(pkg, 0o755); err != nil {
		return err
	}

	err := extract(download, pkg, a.Strip)
	if !errors.Is(err, errNotArchive) {
		return err
	}

	if len(a.Bin) != 1 || len(a.Man)+len(a.Completions) > 0 {
		return errors.New(
			"the download is a single file, so the artifact must list exactly one bin and nothing else",
		)
	}

	if !filepath.IsLocal(filepath.FromSlash(a.Bin[0])) {
		return fmt.Errorf("bin %q: the path is outside the package", a.Bin[0])
	}

	return copyFile(download, filepath.Join(pkg, filepath.FromSlash(a.Bin[0])))
}

var manSectionRe = regexp.MustCompile(`\.([1-9])[a-z]*(\.gz)?$`)

// expose links the artifact's outputs from <tmp>/pkg into <tmp>/bin and
// <tmp>/share. Links are relative so they still resolve after the move into the
// store.
func expose(tmp string, a manifest.Artifact) error {
	for _, entry := range a.Bin {
		if err := link(tmp, entry, path.Join("bin", path.Base(entry))); err != nil {
			return fmt.Errorf("bin %q: %w", entry, err)
		}

		if err := os.Chmod(
			filepath.Join(tmp, "pkg", filepath.FromSlash(entry)),
			0o755,
		); err != nil {
			return fmt.Errorf("bin %q: %w", entry, err)
		}
	}

	for _, entry := range a.Man {
		section := manSectionRe.FindStringSubmatch(entry)
		if section == nil {
			return fmt.Errorf("man %q: the file name has no section such as .1", entry)
		}

		dest := path.Join("share", "man", "man"+section[1], path.Base(entry))
		if err := link(tmp, entry, dest); err != nil {
			return fmt.Errorf("man %q: %w", entry, err)
		}
	}

	for shell, entry := range a.Completions {
		dest := path.Join("share", "completions", shell, path.Base(entry))
		if err := link(tmp, entry, dest); err != nil {
			return fmt.Errorf("completions.%s %q: %w", shell, entry, err)
		}
	}

	return nil
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
// bytes. It skips the temporary directories of installs that are in progress.
func (s *Store) Unreferenced(keep map[string]bool) (map[string]int64, error) {
	entries, err := os.ReadDir(s.dir)
	if err != nil && !errors.Is(err, fs.ErrNotExist) {
		return nil, fmt.Errorf("read store: %w", err)
	}

	found := map[string]int64{}

	for _, entry := range entries {
		path := filepath.Join(s.dir, entry.Name())
		if !entry.IsDir() || strings.HasPrefix(entry.Name(), ".tmp-") || keep[path] {
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

	tmp, err := os.MkdirTemp("", "oku-inspect-")
	if err != nil {
		return nil, err
	}
	defer os.RemoveAll(tmp)

	err = extract(download, tmp, 0)
	if errors.Is(err, errNotArchive) {
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
