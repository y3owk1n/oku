package profile

import (
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
// Windows cannot rename over a junction, so there is a moment without one.
func (p *Profile) point(gen string) error {
	link := filepath.Join(p.dir, current)

	if err := os.Remove(link); err != nil && !os.IsNotExist(err) {
		return fmt.Errorf("activate generation: %w", err)
	}

	target := filepath.Join(p.dir, gen)

	out, err := exec.Command("cmd", "/c", "mklink", "/J", link, target).CombinedOutput()
	if err != nil {
		return fmt.Errorf(
			"activate generation: mklink /J: %w: %s",
			err,
			strings.TrimSpace(string(out)),
		)
	}

	return nil
}

// linkEntry makes dest in a generation stand for the store file target. A
// program becomes a shim, which also puts the bin directories of the package's
// deps on PATH so that Windows finds their DLLs. Any other file is a hard link.
func linkEntry(target, dest string, pkg Package) error {
	if !strings.EqualFold(filepath.Ext(target), ".exe") {
		return hardLinkOrCopy(target, dest)
	}

	self, err := os.Executable()
	if err != nil {
		return err
	}

	if err := hardLinkOrCopy(self, dest); err != nil {
		return err
	}

	spec := shim.Spec{Target: target}
	for _, dep := range pkg.Closure {
		spec.Dirs = append(spec.Dirs, filepath.Join(dep, "bin"))
	}

	return shim.Write(dest, spec)
}

// hardLinkOrCopy copies when source and dest are on different volumes.
func hardLinkOrCopy(source, dest string) error {
	if err := os.Link(source, dest); err == nil {
		return nil
	}

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
