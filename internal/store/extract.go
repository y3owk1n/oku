package store

import (
	"archive/tar"
	"archive/zip"
	"bufio"
	"bytes"
	"errors"
	"fmt"
	"io"
	"io/fs"
	"os"
	"path"
	"path/filepath"
	"strings"
	"time"
)

var errNotArchive = errors.New("not an archive")

// extract unpacks the archive at src into the directory dest, dropping the
// first strip path components of every entry. It returns errNotArchive when src
// is not an archive oku knows: tar (plain, gz, bz2, xz, zst), zip, deb, rpm,
// and on macOS dmg and pkg. A compressed file that holds no tar archive is not
// an archive either. Every write goes through an
// os.Root, so extract cannot write outside dest, and no symlink it leaves in
// dest leads outside dest.
func extract(src, dest string, strip int) error {
	if err := unpackArchive(src, dest, strip); err != nil {
		return err
	}

	return linksInside(dest)
}

func unpackArchive(src, dest string, strip int) error {
	f, err := os.Open(src)
	if err != nil {
		return err
	}
	defer f.Close()

	head := make([]byte, 512)

	n, err := io.ReadFull(f, head)
	if err != nil && !errors.Is(err, io.ErrUnexpectedEOF) && !errors.Is(err, io.EOF) {
		return err
	}

	head = head[:n]

	if _, err := f.Seek(0, io.SeekStart); err != nil {
		return err
	}

	root, err := os.OpenRoot(dest)
	if err != nil {
		return err
	}
	defer root.Close()

	switch {
	case bytes.HasPrefix(head, magicZip):
		return unzip(f, root, strip)
	case bytes.HasPrefix(head, magic7z):
		return un7z(f, root, strip)
	case bytes.HasPrefix(head, magicAr):
		return undeb(f, root, strip)
	case bytes.HasPrefix(head, magicRPM):
		return unrpm(f, root, strip)
	case bytes.HasPrefix(head, magicXar):
		return unpkg(src, dest)
	case bytes.HasPrefix(head, magicOLE):
		return unmsi(src, dest)
	case isDiskImage(f):
		if err := undmg(src, dest); err != nil {
			return err
		}

		return expandPackages(dest)
	case len(head) > 262 && string(head[257:262]) == "ustar":
		return untar(f, root, strip)
	}

	for _, magic := range [][]byte{magicGzip, magicBzip2, magicXZ, magicZstd} {
		if bytes.HasPrefix(head, magic) {
			data, err := decompress(f)
			if err != nil {
				return err
			}

			// A compressed file that is no tar archive is a single binary.
			buffered := bufio.NewReader(data)

			block, err := buffered.Peek(512)
			if err != nil && !errors.Is(err, io.EOF) {
				return fmt.Errorf("decompress: %w", err)
			}

			_, err = tar.NewReader(bytes.NewReader(block)).Next()
			if len(block) < 512 || errors.Is(err, tar.ErrHeader) {
				return errNotArchive
			}

			return untar(buffered, root, strip)
		}
	}

	return errNotArchive
}

func untar(r io.Reader, root *os.Root, strip int) error {
	return untarLinks(r, root, strip, false)
}

// untarLinks is untar. With rooted, an absolute symlink target names a file of
// the package, as it does in a .deb or an .rpm, and becomes a relative link.
func untarLinks(r io.Reader, root *os.Root, strip int, rooted bool) error {
	tr := tar.NewReader(r)

	files := &fileWriter{root: root}
	defer files.close()

	for {
		hdr, err := tr.Next()
		if errors.Is(err, io.EOF) {
			return nil
		}

		if err != nil {
			return err
		}

		name, ok, err := stripPath(hdr.Name, strip)
		if err != nil {
			return err
		}

		if !ok {
			continue
		}

		switch hdr.Typeflag {
		case tar.TypeDir:
			err = root.MkdirAll(name, 0o755)
		case tar.TypeReg:
			err = files.write(name, hdr.FileInfo().Mode(), hdr.ModTime, tr)
		case tar.TypeSymlink:
			target := hdr.Linkname
			if rooted {
				target, err = rootedLink(name, target, strip)
			}

			if err == nil {
				err = writeSymlink(root, name, target)
			}
		case tar.TypeLink:
			target, ok, linkErr := stripPath(hdr.Linkname, strip)
			if linkErr != nil {
				return linkErr
			}

			if !ok {
				return fmt.Errorf("hard link %s points at a stripped path", hdr.Name)
			}

			err = root.Link(target, name)
		}

		if err != nil {
			return fmt.Errorf("extract %s: %w", hdr.Name, err)
		}
	}
}

func unzip(f *os.File, root *os.Root, strip int) error {
	info, err := f.Stat()
	if err != nil {
		return err
	}

	zr, err := zip.NewReader(f, info.Size())
	if err != nil {
		return err
	}

	files := &fileWriter{root: root}
	defer files.close()

	for _, entry := range zr.File {
		name, ok, err := stripPath(entry.Name, strip)
		if err != nil {
			return err
		}

		if !ok {
			continue
		}

		if err := unpackEntry(files, name, entry); err != nil {
			return fmt.Errorf("extract %s: %w", entry.Name, err)
		}
	}

	return nil
}

// archiveEntry is what a zip entry and a 7z entry have in common.
type archiveEntry interface {
	Mode() fs.FileMode
	FileInfo() fs.FileInfo
	Open() (io.ReadCloser, error)
}

// unpackEntry writes one entry of a zip or a 7z archive under the root of files.
func unpackEntry(files *fileWriter, name string, entry archiveEntry) error {
	mode := entry.Mode()
	if mode.IsDir() {
		return files.root.MkdirAll(name, 0o755)
	}

	rc, err := entry.Open()
	if err != nil {
		return err
	}
	defer rc.Close()

	if mode&fs.ModeSymlink != 0 {
		target, err := io.ReadAll(rc)
		if err != nil {
			return err
		}

		return writeSymlink(files.root, name, string(target))
	}

	return files.write(name, mode, entry.FileInfo().ModTime(), rc)
}

// rootedLink returns target as a link relative to the directory of name, when
// target is an absolute path in the package such as "/usr/bin/fd". Any other
// target comes back as it is.
func rootedLink(name, target string, strip int) (string, error) {
	if !path.IsAbs(target) {
		return target, nil
	}

	stripped, ok, err := stripPath(strings.TrimLeft(target, "/"), strip)
	if err != nil {
		return "", err
	}

	if !ok {
		return "", fmt.Errorf("symlink target %q points at a stripped path", target)
	}

	rel, err := filepath.Rel(path.Dir(name), stripped)
	if err != nil {
		return "", err
	}

	return filepath.ToSlash(rel), nil
}

// stripPath drops the first n components of an archive path. It reports false
// when nothing is left, and fails on a path that is absolute or contains "..".
func stripPath(name string, n int) (string, bool, error) {
	name = path.Clean(strings.ReplaceAll(name, `\`, "/"))
	if path.IsAbs(name) || name == ".." || strings.HasPrefix(name, "../") {
		return "", false, fmt.Errorf("entry %q is outside the package", name)
	}

	parts := strings.Split(name, "/")
	if len(parts) <= n {
		return "", false, nil
	}

	name = path.Join(parts[n:]...)

	return name, name != ".", nil
}

// fileWriter writes the files of one archive under root. os.Root opens each
// directory on the way to a file, one at a time. An archive lists the files of
// a directory together, so the writer keeps the directory of the last file open
// and most files cost one open.
type fileWriter struct {
	root *os.Root
	name string
	dir  *os.Root
}

// write keeps the time the archive gives the file. make compares the times of a
// source tree, and a release tarball relies on its generated files, such as
// aclocal.m4, being newer than their inputs. With the time of the unpacking,
// make sees them as stale and runs autotools, which the machine may not have.
func (w *fileWriter) write(name string, mode fs.FileMode, modified time.Time, r io.Reader) error {
	if dir := path.Dir(name); w.dir == nil || dir != w.name {
		w.close()

		if err := w.root.MkdirAll(dir, 0o755); err != nil {
			return err
		}

		opened, err := w.root.OpenRoot(dir)
		if err != nil {
			return err
		}

		w.name, w.dir = dir, opened
	}

	base := path.Base(name)

	// Owner read and write stay set so oku can delete the file later.
	f, err := w.dir.OpenFile(base, os.O_CREATE|os.O_WRONLY|os.O_TRUNC, mode.Perm()|0o600)
	if err != nil {
		return err
	}

	_, err = io.Copy(f, r)
	if closeErr := f.Close(); err == nil {
		err = closeErr
	}

	if err != nil || modified.IsZero() {
		return err
	}

	return w.dir.Chtimes(base, modified, modified)
}

func (w *fileWriter) close() {
	if w.dir != nil {
		w.dir.Close()
		w.dir = nil
	}
}

// writeSymlink refuses a target that is absolute or resolves outside the
// archive root. os.Root stops oku following such a link, but the link itself would
// stay in the store.
func writeSymlink(root *os.Root, name, target string) error {
	resolved := path.Join(path.Dir(name), target)
	if path.IsAbs(target) || resolved == ".." || strings.HasPrefix(resolved, "../") {
		return fmt.Errorf("symlink target %q is outside the package", target)
	}

	if err := root.MkdirAll(path.Dir(name), 0o755); err != nil {
		return err
	}

	return root.Symlink(target, name)
}

// linksInside fails when a symlink under dest leads outside it. writeSymlink
// checks each link as it is written, but a later entry can turn a directory on
// the way into a link, which moves where the earlier link leads. So the check
// runs again on the finished tree.
func linksInside(dest string) error {
	root, err := os.OpenRoot(dest)
	if err != nil {
		return err
	}
	defer root.Close()

	return filepath.WalkDir(dest, func(file string, entry fs.DirEntry, err error) error {
		if err != nil || entry.Type()&fs.ModeSymlink == 0 {
			return err
		}

		rel, err := filepath.Rel(dest, file)
		if err != nil {
			return err
		}

		name := filepath.ToSlash(rel)

		target, err := root.Readlink(name)
		if err != nil {
			return err
		}

		if !resolvesInside(root, path.Dir(name)+"/"+filepath.ToSlash(target)) {
			return fmt.Errorf("symlink %s -> %s leads outside the package", name, target)
		}

		return nil
	})
}

// maxLinks is how many symlinks resolvesInside follows before it gives up. An OS
// has the same kind of limit.
const maxLinks = 255

// resolvesInside follows every symlink on name the way the OS does and reports
// whether it stays inside root. A component that does not exist counts as a
// directory.
func resolvesInside(root *os.Root, name string) bool {
	var at []string

	rest := strings.Split(name, "/")

	for hops := 0; len(rest) > 0; {
		part := rest[0]
		rest = rest[1:]

		switch part {
		case "", ".":
			continue
		case "..":
			if len(at) == 0 {
				return false
			}

			at = at[:len(at)-1]

			continue
		}

		next := append(at, part)

		info, err := root.Lstat(path.Join(next...))
		if err != nil || info.Mode()&fs.ModeSymlink == 0 {
			at = next

			continue
		}

		if hops++; hops > maxLinks {
			return false
		}

		target, err := root.Readlink(path.Join(next...))
		if err != nil || path.IsAbs(target) || filepath.IsAbs(target) {
			return false
		}

		rest = append(strings.Split(filepath.ToSlash(target), "/"), rest...)
	}

	return true
}
