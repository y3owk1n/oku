package cli

import (
	"fmt"
	"io"
	"os"
	"path/filepath"

	"github.com/y3owk1n/oku/internal/expose"
	"github.com/y3owk1n/oku/internal/store"
)

// userDirs returns where this OS reads per-user apps and fonts from.
func (e env) userDirs() (expose.Dirs, error) {
	home, err := os.UserHomeDir()
	if err != nil {
		return expose.Dirs{}, err
	}

	// e.data is "<XDG data home>/oku".
	return expose.UserDirs(home, filepath.Dir(e.data)), nil
}

// syncExposed makes the apps and fonts on this machine match the active
// generation of the global profile. Project profiles expose nothing, because an
// app or a font is visible to the whole user account, not to one directory.
func (e env) syncExposed(notice io.Writer) error {
	pkgs, err := e.profile().Packages()
	if err != nil {
		return err
	}

	dirs, err := e.userDirs()
	if err != nil {
		return err
	}

	var wanted []expose.Item

	for _, pkg := range pkgs {
		meta, err := store.ReadMeta(pkg.StorePath)
		if err != nil {
			continue
		}

		launchers := make([]expose.Launcher, len(meta.Launchers))
		for i, app := range meta.Launchers {
			launchers[i] = expose.Launcher(app)
		}

		wanted = append(wanted, expose.Wanted(pkg.Name, pkg.StorePath, launchers, dirs)...)
	}

	if e.project != "" {
		if len(wanted) > 0 {
			fmt.Fprintln(
				notice,
				"apps and fonts are only exposed from the global list, not from a project",
			)
		}

		return nil
	}

	ledger, err := expose.ReadLedger(e.data)
	if err != nil {
		return err
	}

	before := len(ledger.Items)

	if err := ledger.Sync(wanted); err != nil {
		return err
	}

	for _, item := range ledger.Items[min(before, len(ledger.Items)):] {
		fmt.Fprintf(notice, "exposed %s %s\n", item.Kind, item.Target)
	}

	return nil
}
