package profile

import (
	"errors"
	"fmt"
	"io"
	"os"
	"os/exec"
	"path/filepath"
	"strings"

	"github.com/y3owk1n/oku/internal/shim"
)

// point makes "current" name the generation directory gen. Windows users cannot
// create symlinks without extra rights, so "current" is a directory junction.
// Windows cannot rename over a junction either, so the new one is made beside it
// and the old one moves aside first. Between those two renames there is no
// "current", for far less time than mklink takes.
func (p *Profile) point(gen string) error {
	link := filepath.Join(p.dir, current)
	tmp, old := link+".tmp", link+".old"

	// Either may be left by a switch that was killed. Remove deletes a junction,
	// not what it points at.
	for _, path := range []string{tmp, old} {
		if err := os.Remove(path); err != nil && !os.IsNotExist(err) {
			return fmt.Errorf("activate generation: %w", err)
		}
	}

	target := filepath.Join(p.dir, gen)

	out, err := exec.Command("cmd", "/c", "mklink", "/J", tmp, target).CombinedOutput()
	if err != nil {
		return fmt.Errorf(
			"activate generation: mklink /J: %w: %s",
			err,
			strings.TrimSpace(string(out)),
		)
	}

	if err := os.Rename(link, old); err != nil && !os.IsNotExist(err) {
		return fmt.Errorf("activate generation: %w", err)
	}

	if err := os.Rename(tmp, link); err != nil {
		return fmt.Errorf("activate generation: %w", errors.Join(err, os.Rename(old, link)))
	}

	if err := os.Remove(old); err != nil && !os.IsNotExist(err) {
		return fmt.Errorf("activate generation: %w", err)
	}

	return nil
}

// linkEntry creates dest in a generation for the store file target. A
// program becomes a shim, which also puts the bin directories of the package's
// deps on PATH so that Windows finds their DLLs. Any other file is a hard link.
func linkEntry(target, dest string, pkg Package) error {
	// A wrapper is a shim spec that the store wrote. Its shim goes beside a copy
	// of the spec that also names the dep directories.
	wrapper := strings.EqualFold(filepath.Ext(target), shim.Ext)
	if wrapper {
		dest = strings.TrimSuffix(dest, filepath.Ext(dest)) + ".exe"
	}

	if !wrapper && !strings.EqualFold(filepath.Ext(target), ".exe") {
		return hardLinkOrCopy(target, dest)
	}

	// dest is <data>/oku/profiles/<profile>/gen-<n>/bin/<name>.exe, and the shims
	// of every profile share <data>/oku/shims.
	source, err := shimSource(filepath.Join(filepath.Dir(dest), "..", "..", "..", "..", "shims"))
	if err != nil {
		return err
	}

	if err := hardLinkOrCopy(source, dest); err != nil {
		return err
	}

	spec := shim.Spec{Target: target}

	if wrapper {
		if spec, err = shim.Read(target); err != nil {
			return err
		}
	}

	// Windows looks for a program's DLLs beside the file it started, which is
	// a link in bin, and then on PATH. The DLLs sit beside the real files in the
	// download of the package and of each dep.
	downloads := []string{filepath.Join(pkg.StorePath, "pkg")}

	for _, dep := range pkg.Closure {
		spec.Dirs = append(spec.Dirs, filepath.Join(dep, "bin"))
		downloads = append(downloads, filepath.Join(dep, "pkg"))
	}

	for _, dir := range downloads {
		if info, err := os.Stat(dir); err == nil && info.IsDir() {
			spec.Dirs = append(spec.Dirs, dir)
		}
	}

	return shim.Write(dest, spec)
}

// shimSource returns a copy of the running oku.exe in dir, and makes it when it
// is missing. Shims are hard links to that copy and not to oku.exe itself,
// because Windows refuses to delete any link to a program that is running, and
// "oku gc" deletes shims while oku runs. The name holds the size and time of
// oku.exe, so an updated oku gets a new copy.
func shimSource(dir string) (string, error) {
	self, err := os.Executable()
	if err != nil {
		return "", err
	}

	info, err := os.Stat(self)
	if err != nil {
		return "", err
	}

	source := filepath.Join(dir, fmt.Sprintf("oku-%d-%d.exe", info.Size(), info.ModTime().Unix()))
	if _, err := os.Stat(source); err == nil {
		return source, nil
	}

	if err := os.MkdirAll(dir, 0o755); err != nil {
		return "", err
	}

	// This must be a copy. A hard link would be the running program again.
	os.Remove(source + ".tmp")

	if err := copyFile(self, source+".tmp"); err != nil {
		return "", err
	}

	return source, os.Rename(source+".tmp", source)
}

// hardLinkOrCopy copies when source and dest are on different volumes.
func hardLinkOrCopy(source, dest string) error {
	if err := os.Link(source, dest); err == nil {
		return nil
	}

	return copyFile(source, dest)
}

func copyFile(source, dest string) error {
	in, err := os.Open(source)
	if err != nil {
		return err
	}
	defer in.Close()

	out, err := os.OpenFile(dest, os.O_CREATE|os.O_EXCL|os.O_WRONLY, 0o755)
	if err != nil {
		return err
	}

	if _, err := io.Copy(out, in); err != nil {
		out.Close()

		return err
	}

	return out.Close()
}
