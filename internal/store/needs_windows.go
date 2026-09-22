package store

import (
	"os"

	"github.com/y3owk1n/oku/internal/shim"
)

// linkNeed makes dest.exe a shim that runs the tool at target, the way a
// profile exposes a program. The shim is oku itself, hard-linked when the
// volume allows it and copied otherwise, beside a spec file that names the tool.
func linkNeed(dest, target string) error {
	self, err := os.Executable()
	if err != nil {
		return err
	}

	if err := os.Link(self, dest+".exe"); err != nil {
		if err := copyFile(self, dest+".exe"); err != nil {
			return err
		}
	}

	return shim.Write(dest+".exe", shim.Spec{Target: target})
}
