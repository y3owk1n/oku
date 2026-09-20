package cli

import (
	"fmt"
	"io"
	"os"
	"path/filepath"
	"slices"

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

// syncExposed makes the apps, fonts and services on this machine match the active
// generation of the global profile. Project profiles expose nothing, because an
// app or a font is visible to the whole user account, not to one directory.
func (e env) syncExposed(opts Options, notice io.Writer) error {
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

	manager, err := e.services(opts)
	if err != nil {
		return err
	}

	services, defs, err := e.serviceItems(manager, pkgs)
	if err != nil {
		return err
	}

	wanted = append(wanted, services...)

	if e.project != "" {
		if len(wanted) > 0 {
			fmt.Fprintln(
				notice,
				"apps, fonts and services are only set up from the global list, not from a project",
			)
		}

		return nil
	}

	ledger, err := expose.ReadLedger(e.data)
	if err != nil {
		return err
	}

	before := slices.Clone(ledger.Items)

	handlers := map[string]expose.Handler{"service": serviceHandler(manager, defs)}
	if err := ledger.Sync(wanted, handlers); err != nil {
		return err
	}

	for _, item := range ledger.Items {
		// An item that changed, such as a service that was just enabled, is new too.
		if slices.Contains(before, item) {
			continue
		}

		switch {
		case item.Kind != "service":
			fmt.Fprintf(notice, "exposed %s %s\n", item.Kind, item.Target)
		case item.Enabled:
			fmt.Fprintf(notice, "service %s is running and starts at login\n", item.Name)
		default:
			fmt.Fprintf(notice, "service %s is installed and stopped\n", item.Name)
		}
	}

	return nil
}
