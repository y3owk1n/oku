// Package tempdir makes the temporary directories that oku works in, and finds the
// ones that a killed oku process left behind.
package tempdir

import (
	"errors"
	"fmt"
	"io/fs"
	"os"
	"path/filepath"
	"regexp"
	"strconv"
)

// Dir makes a directory in the system's temporary directory for kind, such as
// "build". Its name holds the pid of this process, so Stale can tell whether
// the process that made it still runs.
func Dir(kind string) (string, error) {
	return os.MkdirTemp("", prefix(kind))
}

// File makes a file in the system's temporary directory the way Dir makes a
// directory. Its name ends in suffix.
func File(kind, suffix string) (*os.File, error) {
	return os.CreateTemp("", prefix(kind)+"*"+suffix)
}

func prefix(kind string) string {
	return fmt.Sprintf("oku-%s-%d-", kind, os.Getpid())
}

// Stale returns the paths in the system's temporary directory that an oku
// process made and no longer uses, which are the paths of a process that has
// exited. It skips a name that Dir or File did not make, such as a user's
// folder called oku-notes, and what another user owns.
func Stale() ([]string, error) {
	dir := os.TempDir()

	entries, err := os.ReadDir(dir)
	if err != nil {
		return nil, err
	}

	var stale []string

	for _, entry := range entries {
		name := entry.Name()

		pid, ok := owner(name)
		if !ok || pid == os.Getpid() || running(pid) {
			continue
		}

		info, err := entry.Info()
		if err != nil || !ownedByMe(info) {
			continue
		}

		stale = append(stale, filepath.Join(dir, name))
	}

	return stale, nil
}

// madeRe matches a name that Dir or File made: oku-<kind>-<pid>-<random>, with
// the suffix of File after it.
var madeRe = regexp.MustCompile(`^oku-[a-z]+-([1-9][0-9]*)-[0-9]+(\.[a-z]+)?$`)

// owner returns the pid in a name that Dir or File made, such as
// "oku-build-4242-123".
func owner(name string) (int, bool) {
	m := madeRe.FindStringSubmatch(name)
	if m == nil {
		return 0, false
	}

	pid, err := strconv.Atoi(m[1])

	return pid, err == nil
}

// Remove deletes a path that Stale returned. It first detaches a disk image
// mounted there, and fails when that does not work, so it never deletes
// inside a mounted volume.
func Remove(path string) error {
	if err := unmount(path); err != nil {
		return err
	}

	// A Go module cache in a build directory is read-only.
	_ = filepath.WalkDir(path, func(p string, d fs.DirEntry, err error) error {
		if err == nil && d.IsDir() {
			_ = os.Chmod(p, 0o755)
		}

		return nil
	})

	if err := os.RemoveAll(path); err != nil && !errors.Is(err, fs.ErrNotExist) {
		return err
	}

	return nil
}

// Size returns the bytes of the files under path.
func Size(path string) int64 {
	var size int64

	_ = filepath.WalkDir(path, func(_ string, d fs.DirEntry, err error) error {
		if err != nil || d.IsDir() {
			return nil
		}

		if info, err := d.Info(); err == nil {
			size += info.Size()
		}

		return nil
	})

	return size
}

// Owner returns the pid of the oku process that made the directory at path
// with Dir, and whether that process still runs.
func Owner(path string) (pid int, alive bool) {
	pid, ok := owner(filepath.Base(path))

	return pid, ok && (pid == os.Getpid() || running(pid))
}

// Running reports whether the process pid runs.
func Running(pid int) bool {
	return running(pid)
}
