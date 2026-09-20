package store

import (
	"archive/tar"
	"archive/zip"
	"bytes"
	"errors"
	"fmt"
	"io"
	"io/fs"
	"os"
	"path"
	"strings"
)

var errNotArchive = errors.New("not an archive")

// extract unpacks the archive at src into the directory dest, dropping the
// first strip path components of every entry. It returns errNotArchive when src
// is not an archive oku knows: tar (plain, gz, bz2, xz, zst), zip, deb, rpm,
// and on macOS dmg and pkg. Every write goes through an
// os.Root, so extract cannot write outside dest.
func extract(src, dest string, strip int) error {
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
		return undmg(src, dest)
	case len(head) > 262 && string(head[257:262]) == "ustar":
		return untar(f, root, strip)
	}

	for _, magic := range [][]byte{magicGzip, magicBzip2, magicXZ, magicZstd} {
		if bytes.HasPrefix(head, magic) {
			data, err := decompress(f)
			if err != nil {
				return err
			}

			return untar(data, root, strip)
		}
	}

	return errNotArchive
}

func untar(r io.Reader, root *os.Root, strip int) error {
	tr := tar.NewReader(r)

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
			err = writeFile(root, name, hdr.FileInfo().Mode(), tr)
		case tar.TypeSymlink:
			err = writeSymlink(root, name, hdr.Linkname)
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

	for _, entry := range zr.File {
		name, ok, err := stripPath(entry.Name, strip)
		if err != nil {
			return err
		}

		if !ok {
			continue
		}

		if err := unzipEntry(root, name, entry); err != nil {
			return fmt.Errorf("extract %s: %w", entry.Name, err)
		}
	}

	return nil
}

func unzipEntry(root *os.Root, name string, entry *zip.File) error {
	mode := entry.Mode()
	if mode.IsDir() {
		return root.MkdirAll(name, 0o755)
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

		return writeSymlink(root, name, string(target))
	}

	return writeFile(root, name, mode, rc)
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

func writeFile(root *os.Root, name string, mode fs.FileMode, r io.Reader) error {
	if err := root.MkdirAll(path.Dir(name), 0o755); err != nil {
		return err
	}

	// Owner read and write stay set so oku can delete the file later.
	f, err := root.OpenFile(name, os.O_CREATE|os.O_WRONLY|os.O_TRUNC, mode.Perm()|0o600)
	if err != nil {
		return err
	}

	_, err = io.Copy(f, r)
	if closeErr := f.Close(); err == nil {
		err = closeErr
	}

	return err
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
