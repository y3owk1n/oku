package store

import (
	"archive/tar"
	"context"
	"errors"
	"fmt"
	"io"
	"io/fs"
	"os"
	"path/filepath"
	"strings"
	"time"

	"aead.dev/minisign"
	"github.com/klauspost/compress/zstd"

	"github.com/y3owk1n/oku/internal/status"
)

const (
	entrySuffix     = ".tar.zst"
	signatureSuffix = ".minisig"
)

// Substitute fills prefix from the first cache that has it with a signature of
// one of keys. It reports whether prefix is now in the store. notes says which
// entries it ignored and why, and a cache that lacks the entry adds no note.
//
// A location is a directory or an http(s) URL. An entry is
// "<store name>.tar.zst" beside "<store name>.tar.zst.minisig".
func (s *Store) Substitute(
	ctx context.Context,
	prefix string,
	locations []string,
	keys []minisign.PublicKey,
) (found bool, notes []string, err error) {
	if exists(filepath.Join(prefix, metaFile)) {
		return true, nil, nil
	}

	if err := os.MkdirAll(s.dir, 0o755); err != nil {
		return false, nil, err
	}

	for _, location := range locations {
		url := entryURL(location, filepath.Base(prefix))

		done := status.Start(ctx, "looking in the cache %s", location)
		note, err := s.substituteFrom(ctx, prefix, url, keys)

		done()

		if err != nil {
			return false, notes, err
		}

		if note == "" && exists(filepath.Join(prefix, metaFile)) {
			return true, notes, nil
		}

		if note != "" {
			notes = append(notes, note)
		}
	}

	return false, notes, nil
}

func entryURL(location, name string) string {
	if !strings.Contains(location, "://") {
		location = "file://" + filepath.ToSlash(location)
	}

	return strings.TrimRight(location, "/") + "/" + name + entrySuffix
}

// substituteFrom returns a note when it ignored the entry at url, and an empty
// note when the cache has no such entry or the entry is now in the store.
func (s *Store) substituteFrom(
	ctx context.Context,
	prefix, url string,
	keys []minisign.PublicKey,
) (string, error) {
	work, err := os.MkdirTemp(s.dir, ".tmp-")
	if err != nil {
		return "", err
	}
	defer removeTree(work)

	archive := filepath.Join(work, "entry")

	verifier, err := s.downloadEntry(ctx, url, archive)
	if err != nil {
		// Most caches lack most entries, so a failed download is not worth a note.
		return "", nil //nolint:nilerr
	}

	resp, err := s.get(ctx, url+signatureSuffix)
	if err != nil {
		return fmt.Sprintf("ignored %s, it has no signature", url), nil //nolint:nilerr
	}

	signature, err := io.ReadAll(io.LimitReader(resp.Body, 1<<16))
	resp.Body.Close()

	if err != nil {
		return "", fmt.Errorf("download %s: %w", url+signatureSuffix, err)
	}

	trusted := false
	for _, key := range keys {
		trusted = trusted || verifier.Verify(key, signature)
	}

	if !trusted {
		return fmt.Sprintf("ignored %s, no trusted key signed it", url), nil
	}

	unpacked := filepath.Join(work, "out")
	if err := os.Mkdir(unpacked, 0o755); err != nil {
		return "", err
	}

	if err := extract(archive, unpacked, 0); err != nil {
		return "", fmt.Errorf("unpack %s: %w", url, err)
	}

	// The signature covers the archive, and the archive names its store path, so
	// a signed entry cannot be served under another package's name.
	inner := filepath.Join(unpacked, filepath.Base(prefix))
	if !exists(filepath.Join(inner, metaFile)) {
		return fmt.Sprintf("ignored %s, it does not hold %s", url, filepath.Base(prefix)), nil
	}

	return "", os.Rename(inner, prefix)
}

// downloadEntry saves url to dest and returns a reader that has digested it.
func (s *Store) downloadEntry(ctx context.Context, url, dest string) (*minisign.Reader, error) {
	resp, err := s.get(ctx, url)
	if err != nil {
		return nil, err
	}
	defer resp.Body.Close()

	out, err := os.Create(dest)
	if err != nil {
		return nil, err
	}
	defer out.Close()

	verifier := minisign.NewReader(resp.Body)
	if _, err := io.Copy(out, verifier); err != nil {
		return nil, err
	}

	return verifier, out.Close()
}

// ErrImpure reports a package whose build could use the network, so its result
// is not a function of its inputs and must not be shared.
var ErrImpure = errors.New("it was built with network access, so it cannot go into a cache")

// Pack writes the signed cache entry of the store path prefix into dir.
func (s *Store) Pack(prefix, dir string, key minisign.PrivateKey) error {
	meta, err := ReadMeta(prefix)
	if err != nil {
		return err
	}

	if meta.Impure {
		return ErrImpure
	}

	if err := os.MkdirAll(dir, 0o755); err != nil {
		return err
	}

	name := filepath.Base(prefix)
	dest := filepath.Join(dir, name+entrySuffix)

	tmp, err := os.CreateTemp(dir, ".partial-*")
	if err != nil {
		return err
	}
	defer os.Remove(tmp.Name())

	if err := writeEntry(tmp, prefix, name); err != nil {
		tmp.Close()

		return err
	}

	if _, err := tmp.Seek(0, io.SeekStart); err != nil {
		return err
	}

	signer := minisign.NewReader(tmp)
	if _, err := io.Copy(io.Discard, signer); err != nil {
		return err
	}

	if err := tmp.Close(); err != nil {
		return err
	}

	if err := os.WriteFile(dest+signatureSuffix, signer.Sign(key), 0o644); err != nil {
		return err
	}

	if err := os.Chmod(tmp.Name(), 0o644); err != nil {
		return err
	}

	return os.Rename(tmp.Name(), dest)
}

func writeEntry(w io.Writer, prefix, name string) error {
	compressed, err := zstd.NewWriter(w)
	if err != nil {
		return err
	}

	archive := tar.NewWriter(compressed)

	err = filepath.WalkDir(prefix, func(path string, entry fs.DirEntry, err error) error {
		if err != nil {
			return err
		}

		info, err := entry.Info()
		if err != nil {
			return err
		}

		rel, err := filepath.Rel(prefix, path)
		if err != nil {
			return err
		}

		link := ""
		if info.Mode()&fs.ModeSymlink != 0 {
			if link, err = os.Readlink(path); err != nil {
				return err
			}
		}

		header, err := tar.FileInfoHeader(info, link)
		if err != nil {
			return err
		}

		// Owner and time differ between machines and are not part of the package.
		header.Name = filepath.ToSlash(filepath.Join(name, rel))
		header.Uid, header.Gid, header.Uname, header.Gname = 0, 0, "", ""
		header.ModTime, header.AccessTime, header.ChangeTime = time.Time{}, time.Time{}, time.Time{}

		if err := archive.WriteHeader(header); err != nil {
			return err
		}

		if !info.Mode().IsRegular() {
			return nil
		}

		f, err := os.Open(path)
		if err != nil {
			return err
		}
		defer f.Close()

		_, err = io.Copy(archive, f)

		return err
	})
	if err != nil {
		return err
	}

	if err := archive.Close(); err != nil {
		return err
	}

	return compressed.Close()
}
