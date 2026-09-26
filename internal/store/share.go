package store

import (
	"bufio"
	"bytes"
	"crypto/sha256"
	"encoding/hex"
	"errors"
	"fmt"
	"io"
	"io/fs"
	"os"
	"path/filepath"
	"strconv"
	"strings"
	"time"

	"github.com/y3owk1n/oku/internal/clone"
)

const (
	// LinksDir in the store holds one file per content that store paths share,
	// named by its key.
	LinksDir = ".links"
	// recordsDir in LinksDir holds a record per store path of the files it shares.
	recordsDir = "paths"
	// shareMin is the smallest file that is shared. Guix picks the same size, which
	// keeps the index small, since most small files are unique.
	shareMin = 8192
	// shareSuffix names the temporary file that replaces a file of a store path.
	shareSuffix = ".oku-share"
)

// Shared is one file of a store path whose content lives under LinksDir.
type Shared struct {
	Key string
	// Mode is the file's mode when it entered the store. A hard link shares the
	// mode of every file it links, which has no write bits.
	Mode fs.FileMode
}

// Share keeps one copy on disk of each file of the store path path that another
// store path also holds, and returns the bytes that saved. Where the filesystem
// clones, the file becomes a clone and keeps its mode and time. Elsewhere it
// becomes a hard link, and the shared file loses its write bits so that no
// store path can change another's. A store path without oku-meta.toml is a
// repo's files, which the home links to, and is left alone.
func (s *Store) Share(path string) (int64, error) {
	if filepath.Dir(path) != s.dir || !exists(filepath.Join(path, metaFile)) {
		return 0, nil
	}

	sh := sharer{links: filepath.Join(s.dir, LinksDir)}

	var (
		record bytes.Buffer
		saved  int64
		// big counts the files large enough to share, so a store path of small
		// files needs no record.
		big int
	)

	err := filepath.WalkDir(path, func(file string, entry fs.DirEntry, err error) error {
		if err != nil || !entry.Type().IsRegular() {
			return err
		}

		// A share that was killed left its temporary file.
		if strings.HasSuffix(file, shareSuffix) {
			return os.Remove(file)
		}

		info, err := entry.Info()
		if err != nil || info.Size() < shareMin {
			return nil //nolint:nilerr
		}

		big++

		if !shareable(file) {
			return nil
		}

		key, err := contentKey(file, info.Mode())
		if err != nil {
			return nil //nolint:nilerr
		}

		if err := os.MkdirAll(sh.links, 0o755); err != nil {
			return err
		}

		shared, n := sh.share(file, filepath.Join(sh.links, key), info)
		if !shared {
			return nil
		}

		saved += n

		rel, err := filepath.Rel(path, file)
		if err != nil {
			return err
		}

		fmt.Fprintf(&record, "%s %o %s\n", key, info.Mode().Perm(), filepath.ToSlash(rel))

		return nil
	})
	if err != nil {
		return saved, fmt.Errorf("share %s: %w", filepath.Base(path), err)
	}

	if big == 0 {
		return saved, nil
	}

	records := filepath.Join(sh.links, recordsDir)
	if err := os.MkdirAll(records, 0o755); err != nil {
		return saved, fmt.Errorf("share %s: %w", filepath.Base(path), err)
	}

	if err := writeRecord(filepath.Join(records, filepath.Base(path)), record.Bytes()); err != nil {
		return saved, fmt.Errorf("share %s: %w", filepath.Base(path), err)
	}

	return saved, nil
}

// Unshared returns the store paths with a file large enough to share that have
// not been through Share, which are those from before oku shared files.
func (s *Store) Unshared() ([]string, error) {
	entries, err := os.ReadDir(s.dir)
	if err != nil && !errors.Is(err, fs.ErrNotExist) {
		return nil, fmt.Errorf("read store: %w", err)
	}

	var paths []string

	for _, entry := range entries {
		path := filepath.Join(s.dir, entry.Name())
		if !entry.IsDir() || strings.HasPrefix(entry.Name(), ".") ||
			strings.HasSuffix(entry.Name(), ".old") || !exists(filepath.Join(path, metaFile)) {
			continue
		}

		if exists(filepath.Join(s.dir, LinksDir, recordsDir, entry.Name())) || !hasBig(path) {
			continue
		}

		paths = append(paths, path)
	}

	return paths, nil
}

// hasBig reports whether dir holds a file large enough to share.
func hasBig(dir string) bool {
	found := false

	_ = filepath.WalkDir(dir, func(_ string, entry fs.DirEntry, err error) error {
		if err != nil || !entry.Type().IsRegular() {
			return nil //nolint:nilerr
		}

		if info, err := entry.Info(); err == nil && info.Size() >= shareMin {
			found = true

			return fs.SkipAll
		}

		return nil
	})

	return found
}

// SharedFiles returns the shared files of the store path path by their paths
// relative to it, with slashes. A path that shares nothing has none.
func (s *Store) SharedFiles(path string) (map[string]Shared, error) {
	f, err := os.Open(filepath.Join(s.dir, LinksDir, recordsDir, filepath.Base(path)))
	if errors.Is(err, fs.ErrNotExist) {
		return nil, nil
	}

	if err != nil {
		return nil, fmt.Errorf("read the shared files of %s: %w", filepath.Base(path), err)
	}
	defer f.Close()

	files := map[string]Shared{}

	lines := bufio.NewScanner(f)
	for lines.Scan() {
		fields := strings.SplitN(lines.Text(), " ", 3)
		if len(fields) != 3 {
			continue
		}

		mode, err := strconv.ParseUint(fields[1], 8, 32)
		if err != nil {
			continue
		}

		files[fields[2]] = Shared{Key: fields[0], Mode: fs.FileMode(mode)}
	}

	if err := lines.Err(); err != nil {
		return nil, fmt.Errorf("read the shared files of %s: %w", filepath.Base(path), err)
	}

	return files, nil
}

// DropLinks deletes the record of each store path that is gone, and each file
// under LinksDir that no record names any more.
func (s *Store) DropLinks() error {
	links := filepath.Join(s.dir, LinksDir)
	records := filepath.Join(links, recordsDir)

	entries, err := os.ReadDir(records)
	if err != nil && !errors.Is(err, fs.ErrNotExist) {
		return fmt.Errorf("read %s: %w", records, err)
	}

	used := map[string]bool{}

	for _, entry := range entries {
		if !exists(filepath.Join(s.dir, entry.Name())) {
			if err := os.Remove(filepath.Join(records, entry.Name())); err != nil {
				return fmt.Errorf("delete the record of %s: %w", entry.Name(), err)
			}

			continue
		}

		files, err := s.SharedFiles(entry.Name())
		if err != nil {
			return err
		}

		for _, f := range files {
			used[f.Key] = true
		}
	}

	contents, err := os.ReadDir(links)
	if errors.Is(err, fs.ErrNotExist) {
		return nil
	}

	if err != nil {
		return fmt.Errorf("read %s: %w", links, err)
	}

	for _, entry := range contents {
		if entry.Name() == recordsDir || used[entry.Name()] {
			continue
		}

		if err := os.Remove(filepath.Join(links, entry.Name())); err != nil &&
			!errors.Is(err, fs.ErrNotExist) {
			return fmt.Errorf("delete %s: %w", filepath.Join(links, entry.Name()), err)
		}
	}

	return nil
}

// sharer shares the files of one store path.
type sharer struct {
	links string
	// hardLinks is set once the filesystem refused a clone.
	hardLinks bool
}

// share makes file and entry, the file under links with the same key, one copy
// on disk. When there is no entry, file becomes it. It reports whether file
// shares the entry's content now, and the bytes that saved.
func (sh *sharer) share(file, entry string, info fs.FileInfo) (bool, int64) {
	err := os.Link(file, entry)
	if err == nil {
		return true, 0
	}

	// A filesystem that has no hard links, or a file with too many, keeps a copy.
	if !errors.Is(err, fs.ErrExist) {
		return false, 0
	}

	held, err := os.Stat(entry)
	if err != nil {
		return false, 0
	}

	if os.SameFile(held, info) {
		return true, 0
	}

	// A package edited the entry after it entered the index, so it no longer holds
	// the bytes of its key. file replaces it.
	if same, err := sameBytes(entry, file); err != nil || !same {
		return replaceWith(entry, file, nil) == nil, 0
	}

	if !sh.hardLinks {
		err := replaceWith(file, entry, func(tmp string) error {
			if err := os.Chmod(tmp, info.Mode()&(fs.ModePerm|fs.ModeSetuid|fs.ModeSetgid)); err != nil {
				return err
			}

			return os.Chtimes(tmp, time.Time{}, info.ModTime())
		})
		if err == nil {
			return true, info.Size()
		}

		if !errors.Is(err, clone.ErrUnsupported) {
			return false, 0
		}

		sh.hardLinks = true
	}

	// A write through one hard link would change every store path that shares it.
	if held.Mode().Perm()&0o222 != 0 {
		if err := os.Chmod(entry, held.Mode().Perm()&^0o222); err != nil {
			return false, 0
		}
	}

	if err := replaceWith(file, entry, nil); err != nil {
		return false, 0
	}

	return true, info.Size()
}

// replaceWith replaces dest with a clone of source, or a hard link to it when
// finish is nil, through a temporary file beside dest. finish sets up the clone
// before it takes dest's place.
func replaceWith(dest, source string, finish func(string) error) error {
	tmp := dest + shareSuffix
	os.Remove(tmp)

	var err error
	if finish == nil {
		err = os.Link(source, tmp)
	} else if err = clone.File(source, tmp); err == nil {
		err = finish(tmp)
	}

	if err == nil {
		err = os.Rename(tmp, dest)
	}

	if err != nil {
		os.Remove(tmp)
	}

	return err
}

// contentKey names the content of file with its exec bit, since every hard link
// of a file has one mode.
func contentKey(file string, mode fs.FileMode) (string, error) {
	f, err := os.Open(file)
	if err != nil {
		return "", err
	}
	defer f.Close()

	sum := sha256.New()
	if _, err := io.Copy(sum, f); err != nil {
		return "", err
	}

	key := hex.EncodeToString(sum.Sum(nil))
	if mode&0o111 != 0 {
		key += "x"
	}

	return key, nil
}

func sameBytes(a, b string) (bool, error) {
	fa, err := os.Open(a)
	if err != nil {
		return false, err
	}
	defer fa.Close()

	fb, err := os.Open(b)
	if err != nil {
		return false, err
	}
	defer fb.Close()

	bufA, bufB := make([]byte, 1<<16), make([]byte, 1<<16)

	for {
		na, errA := io.ReadFull(fa, bufA)
		nb, errB := io.ReadFull(fb, bufB)

		if na != nb || !bytes.Equal(bufA[:na], bufB[:nb]) {
			return false, nil
		}

		endA := errors.Is(errA, io.EOF) || errors.Is(errA, io.ErrUnexpectedEOF)
		endB := errors.Is(errB, io.EOF) || errors.Is(errB, io.ErrUnexpectedEOF)

		if endA || endB {
			return endA == endB, nil
		}

		if errA != nil {
			return false, errA
		}

		if errB != nil {
			return false, errB
		}
	}
}

func writeRecord(path string, data []byte) error {
	tmp := path + ".tmp"
	if err := os.WriteFile(tmp, data, 0o644); err != nil {
		return err
	}

	return os.Rename(tmp, path)
}
