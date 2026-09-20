package store

import (
	"bufio"
	"bytes"
	"compress/bzip2"
	"compress/gzip"
	"errors"
	"fmt"
	"io"
	"io/fs"
	"os"
	"os/exec"
	"path/filepath"
	"runtime"
	"strconv"
	"strings"

	"github.com/cavaliergopher/cpio"
	"github.com/cavaliergopher/rpm"
	"github.com/klauspost/compress/zstd"
	"github.com/ulikunitz/xz"
)

var (
	magicGzip  = []byte{0x1f, 0x8b}
	magicBzip2 = []byte("BZh")
	magicXZ    = []byte{0xfd, '7', 'z', 'X', 'Z', 0x00}
	magicZstd  = []byte{0x28, 0xb5, 0x2f, 0xfd}
	magicZip   = []byte("PK\x03\x04")
	magicAr    = []byte("!<arch>\n")
	magicRPM   = []byte{0xed, 0xab, 0xee, 0xdb}
	magicXar   = []byte("xar!")
)

// decompress wraps r according to the compression its first bytes show. Plain
// data comes back unchanged. The buffer matters, because the xz decoder reads a
// few bytes at a time, and without it each read is a system call.
func decompress(r io.Reader) (io.Reader, error) {
	buffered := bufio.NewReaderSize(r, 1<<20)

	head, err := buffered.Peek(6)
	if err != nil && !errors.Is(err, io.EOF) {
		return nil, err
	}

	switch {
	case bytes.HasPrefix(head, magicGzip):
		return gzip.NewReader(buffered)
	case bytes.HasPrefix(head, magicBzip2):
		return bzip2.NewReader(buffered), nil
	case bytes.HasPrefix(head, magicXZ):
		return xz.NewReader(buffered)
	case bytes.HasPrefix(head, magicZstd):
		decoder, err := zstd.NewReader(buffered)
		if err != nil {
			return nil, err
		}

		return decoder.IOReadCloser(), nil
	default:
		return buffered, nil
	}
}

// undeb unpacks the data archive of a Debian package. It never reads the
// control archive, which holds the maintainer scripts.
func undeb(f *os.File, root *os.Root, strip int) error {
	if _, err := f.Seek(int64(len(magicAr)), io.SeekStart); err != nil {
		return err
	}

	for {
		header := make([]byte, 60)
		if _, err := io.ReadFull(f, header); err != nil {
			return errors.New("the package has no data archive")
		}

		name := strings.TrimRight(strings.TrimSpace(string(header[:16])), "/")

		size, err := strconv.ParseInt(strings.TrimSpace(string(header[48:58])), 10, 64)
		if err != nil {
			return fmt.Errorf("read the package: %w", err)
		}

		if strings.HasPrefix(name, "data.tar") {
			data, err := decompress(io.LimitReader(f, size))
			if err != nil {
				return err
			}

			return untar(data, root, strip)
		}

		// Members are padded to an even length.
		if _, err := f.Seek(size+size%2, io.SeekCurrent); err != nil {
			return err
		}
	}
}

// unrpm unpacks the file payload of an RPM package. It never runs the scriptlets
// in the package header.
func unrpm(f *os.File, root *os.Root, strip int) error {
	pkg, err := rpm.Read(f)
	if err != nil {
		return err
	}

	if format := pkg.PayloadFormat(); format != "cpio" {
		return fmt.Errorf("the payload is %s, and oku unpacks cpio payloads", format)
	}

	payload, err := decompress(f)
	if err != nil {
		return err
	}

	archive := cpio.NewReader(payload)

	for {
		hdr, err := archive.Next()
		if errors.Is(err, io.EOF) {
			return nil
		}

		if err != nil {
			return err
		}

		// An rpm names its files from the package root, as "/usr/bin/x" or
		// "./usr/bin/x". Neither is a path on this machine.
		name, ok, err := stripPath(strings.TrimLeft(hdr.Name, "/"), strip)
		if err != nil {
			return err
		}

		if !ok {
			continue
		}

		switch {
		case hdr.Mode.IsDir():
			err = root.MkdirAll(name, 0o755)
		case hdr.Mode&cpio.TypeSymlink == cpio.TypeSymlink:
			err = writeSymlink(root, name, hdr.Linkname)
		case hdr.Mode.IsRegular():
			err = writeFile(root, name, fs.FileMode(hdr.Mode.Perm()), archive)
		}

		if err != nil {
			return fmt.Errorf("extract %s: %w", hdr.Name, err)
		}
	}
}

// undmg copies the files of a macOS disk image. It mounts the image read-only
// without opening it in Finder, and runs nothing from it.
func undmg(src, dest string) error {
	if runtime.GOOS != "darwin" {
		return errors.New("a .dmg can only be unpacked on macOS")
	}

	mount, err := os.MkdirTemp("", "oku-dmg-")
	if err != nil {
		return err
	}
	defer os.Remove(mount)

	attach := exec.Command(
		"/usr/bin/hdiutil", "attach", "-nobrowse", "-readonly", "-noverify", "-noautoopen",
		"-mountpoint", mount, src,
	)
	// An image with a license asks for a "Y" before it mounts.
	attach.Stdin = strings.NewReader("Y\n")

	if out, err := attach.CombinedOutput(); err != nil {
		return fmt.Errorf("mount the disk image: %w: %s", err, strings.TrimSpace(string(out)))
	}
	defer exec.Command("/usr/bin/hdiutil", "detach", "-force", mount).Run() //nolint:errcheck

	return copyImage(mount, dest)
}

// copyImage copies a mounted image. It leaves out Finder's hidden files and
// links that point out of the image, such as the usual shortcut to /Applications.
func copyImage(mount, dest string) error {
	return filepath.WalkDir(mount, func(path string, entry fs.DirEntry, err error) error {
		if err != nil {
			return err
		}

		rel, err := filepath.Rel(mount, path)
		if err != nil || rel == "." {
			return err
		}

		if strings.HasPrefix(filepath.Base(rel), ".") &&
			!strings.Contains(rel, string(filepath.Separator)) {
			if entry.IsDir() {
				return filepath.SkipDir
			}

			return nil
		}

		target := filepath.Join(dest, rel)

		info, err := entry.Info()
		if err != nil {
			return err
		}

		switch {
		case entry.IsDir():
			return os.MkdirAll(target, 0o755)
		case info.Mode()&fs.ModeSymlink != 0:
			link, err := os.Readlink(path)
			if err != nil {
				return err
			}

			resolved := filepath.Join(filepath.Dir(rel), link)
			if filepath.IsAbs(link) || resolved == ".." ||
				strings.HasPrefix(resolved, ".."+string(filepath.Separator)) {
				return nil
			}

			return os.Symlink(link, target)
		default:
			return copyFileMode(path, target, info.Mode().Perm()|0o600)
		}
	})
}

// unpkg expands a macOS installer package into its payload files. pkgutil
// unpacks the scripts as files and does not run them.
func unpkg(src, dest string) error {
	if runtime.GOOS != "darwin" {
		return errors.New("a .pkg can only be unpacked on macOS")
	}

	// pkgutil requires a directory that does not exist yet.
	expanded := filepath.Join(dest, "expanded")

	out, err := exec.Command("/usr/sbin/pkgutil", "--expand-full", src, expanded).CombinedOutput()
	if err != nil {
		return fmt.Errorf(
			"expand the installer package: %w: %s",
			err,
			strings.TrimSpace(string(out)),
		)
	}

	entries, err := os.ReadDir(expanded)
	if err != nil {
		return err
	}

	for _, entry := range entries {
		if err := os.Rename(
			filepath.Join(expanded, entry.Name()),
			filepath.Join(dest, entry.Name()),
		); err != nil {
			return err
		}
	}

	return os.Remove(expanded)
}

func copyFileMode(source, dest string, mode fs.FileMode) error {
	if err := copyFile(source, dest); err != nil {
		return err
	}

	return os.Chmod(dest, mode)
}

// isDiskImage reports whether f ends with the "koly" trailer of a macOS disk
// image, which has no signature at its start.
func isDiskImage(f *os.File) bool {
	info, err := f.Stat()
	if err != nil || info.Size() < 512 {
		return false
	}

	trailer := make([]byte, 4)
	if _, err := f.ReadAt(trailer, info.Size()-512); err != nil {
		return false
	}

	return string(trailer) == "koly"
}
