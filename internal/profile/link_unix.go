//go:build !windows

package profile

import (
	"fmt"
	"os"
	"path/filepath"
)

// point makes "current" name the generation directory gen. Rename replaces the
// link in one step, so the link exists at every moment.
func (p *Profile) point(gen string) error {
	tmp := filepath.Join(p.dir, current+".tmp")
	os.Remove(tmp)

	if err := os.Symlink(gen, tmp); err != nil {
		return fmt.Errorf("activate generation: %w", err)
	}

	if err := os.Rename(tmp, filepath.Join(p.dir, current)); err != nil {
		return fmt.Errorf("activate generation: %w", err)
	}

	return nil
}

// linkEntry makes dest in a generation stand for the store file target.
func linkEntry(target, dest string, _ Package) error {
	return os.Symlink(target, dest)
}
