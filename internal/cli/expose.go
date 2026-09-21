package cli

import (
	"bufio"
	"context"
	"encoding/json"
	"fmt"
	"os"
	"path/filepath"
	"runtime"
	"slices"
	"strings"

	"github.com/spf13/cobra"

	"github.com/y3owk1n/oku/internal/expose"
	"github.com/y3owk1n/oku/internal/profile"
	"github.com/y3owk1n/oku/internal/service"
	"github.com/y3owk1n/oku/internal/store"
)

const (
	systemFlag  = "system"
	systemUsage = "apply system-scope apps, fonts and services, which needs administrator rights"
	// systemApply is the hidden command that oku runs as root for one item.
	systemApply = "system-apply"
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

func systemDirs(opts Options) expose.Dirs {
	if opts.SystemDirs != nil {
		return *opts.SystemDirs
	}

	return expose.SystemDirs()
}

// wantedItems lists the apps, fonts and services of pkgs, with the definitions
// of the services by name.
func (e env) wantedItems(
	opts Options,
	pkgs []profile.Package,
) ([]expose.Item, map[string]service.Definition, error) {
	dirs, err := e.userDirs()
	if err != nil {
		return nil, nil, err
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

		into := dirs
		if pkg.System {
			into = systemDirs(opts)
		}

		wanted = append(
			wanted,
			expose.Wanted(pkg.Name, pkg.StorePath, launchers, into, pkg.System)...,
		)
	}

	services, defs, err := e.serviceItems(opts, pkgs)

	return append(wanted, services...), defs, err
}

// exposePlan is what a change exposes, decided before the first change.
type exposePlan struct {
	wanted []expose.Item
	defs   map[string]service.Definition
	// elevated reports that the user agreed to change system scope.
	elevated bool
}

// planExposed decides which apps, fonts and services the global profile exposes
// when it holds pkgs, and checks that no target belongs to someone else. It
// changes nothing. Project profiles expose nothing, because an app or a font is
// visible to the whole user account, not to one directory.
//
// Items in system scope change only with system set, after oku has listed them
// and the user has agreed. Otherwise oku leaves them as they are and says so.
func (e env) planExposed(
	cmd *cobra.Command,
	opts Options,
	pkgs []profile.Package,
	system bool,
) (exposePlan, error) {
	notice := cmd.ErrOrStderr()

	wanted, defs, err := e.wantedItems(opts, pkgs)
	if err != nil {
		return exposePlan{}, err
	}

	if e.project != "" {
		if len(wanted) > 0 {
			fmt.Fprintln(
				notice,
				"apps, fonts and services are only set up from the global list, not from a project",
			)
		}

		return exposePlan{}, nil
	}

	ledger, err := expose.ReadLedger(e.data)
	if err != nil {
		return exposePlan{}, err
	}

	if pending := pendingSystem(ledger.Items, wanted); len(pending) > 0 {
		if system {
			fmt.Fprintln(notice, "this changes, with administrator rights:")
		} else {
			fmt.Fprintln(notice, "left unchanged, because system scope needs administrator rights:")
		}

		fmt.Fprint(notice, strings.Join(pending, ""))

		if system {
			system = confirm(bufio.NewReader(cmd.InOrStdin()), notice, "continue? [y/N] ")
		}

		if !system {
			fmt.Fprintln(notice, `run "oku sync --system" to apply them`)

			wanted = keepSystem(ledger.Items, wanted)
		}
	} else {
		system = false
	}

	return exposePlan{wanted: wanted, defs: defs, elevated: system}, ledger.Check(wanted)
}

// placeExposed makes the apps, fonts and services on this machine match plan.
func (e env) placeExposed(cmd *cobra.Command, opts Options, plan exposePlan) error {
	if e.project != "" {
		return nil
	}

	notice := cmd.ErrOrStderr()

	ledger, err := expose.ReadLedger(e.data)
	if err != nil {
		return err
	}

	before := slices.Clone(ledger.Items)

	handlers, err := e.handlers(cmd.Context(), opts, plan.defs)
	if err != nil {
		return err
	}

	if err := ledger.Sync(plan.wanted, handlers); err != nil {
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
		case item.Enabled && item.System:
			fmt.Fprintf(notice, "service %s is running and starts at boot\n", item.Name)
		case item.Enabled:
			fmt.Fprintf(notice, "service %s is running and starts at login\n", item.Name)
		default:
			fmt.Fprintf(notice, "service %s is installed and stopped\n", item.Name)
		}
	}

	return nil
}

// pendingSystem describes the system-scope items that differ between have and
// wanted, one line each.
func pendingSystem(have, wanted []expose.Item) []string {
	var lines []string

	for _, item := range have {
		if item.System && !slices.Contains(wanted, item) {
			lines = append(lines, fmt.Sprintf("  remove %-8s %s\n", item.Kind, item.Target))
		}
	}

	for _, item := range wanted {
		if item.System && !slices.Contains(have, item) {
			lines = append(lines, fmt.Sprintf("  write  %-8s %s\n", item.Kind, item.Target))
		}
	}

	return lines
}

// keepSystem returns wanted with its system-scope items replaced by the ones in
// have, so a sync without administrator rights leaves system scope alone.
func keepSystem(have, wanted []expose.Item) []expose.Item {
	kept := slices.DeleteFunc(
		slices.Clone(wanted),
		func(item expose.Item) bool { return item.System },
	)

	for _, item := range have {
		if item.System {
			kept = append(kept, item)
		}
	}

	return kept
}

// systemChange is what oku passes to itself when it runs as root.
type systemChange struct {
	Item       expose.Item
	Definition service.Definition
}

// handlers place and remove items. This process handles an item in user scope,
// and runs "oku system-apply" as root for an item in system scope.
func (e env) handlers(
	ctx context.Context,
	opts Options,
	defs map[string]service.Definition,
) (map[string]expose.Handler, error) {
	manager, err := e.services(opts)
	if err != nil {
		return nil, err
	}

	elevated := func(action string, item expose.Item) error {
		return e.applyAsRoot(ctx, opts, action, systemChange{item, defs[item.Name]})
	}

	handlers := map[string]expose.Handler{}

	for _, kind := range []string{"app", "font", "service"} {
		local := expose.Handler{
			Place:  expose.Place,
			Remove: expose.Remove,
		}
		if kind == "service" {
			local = serviceHandler(manager, defs)
		}

		handlers[kind] = expose.Handler{
			Place: func(item expose.Item) error {
				if item.System {
					return elevated("place", item)
				}

				return local.Place(item)
			},
			Remove: func(item expose.Item) error {
				if item.System {
					return elevated("remove", item)
				}

				return local.Remove(item)
			},
		}
	}

	return handlers, nil
}

func (e env) applyAsRoot(
	ctx context.Context,
	opts Options,
	action string,
	change systemChange,
) error {
	payload, err := json.Marshal(change)
	if err != nil {
		return err
	}

	executable := opts.Executable
	if executable == "" {
		if executable, err = os.Executable(); err != nil {
			return err
		}
	}

	// JSON holds many quotes, and the Windows consent prompt does not keep the
	// quotes of an argument. So on Windows the change goes through a file.
	if runtime.GOOS == "windows" {
		file, err := os.CreateTemp("", "oku-change-*.json")
		if err != nil {
			return err
		}
		defer os.Remove(file.Name())

		if _, err := file.Write(payload); err != nil {
			return err
		}

		if err := file.Close(); err != nil {
			return err
		}

		return elevate(ctx, opts, []string{executable, systemApply, action, "@" + file.Name()})
	}

	return elevate(ctx, opts, []string{executable, systemApply, action, string(payload)})
}

// newSystemApplyCmd is the command that oku runs as root. It places or removes
// one item, and refuses a target outside the directories system scope uses.
func newSystemApplyCmd(opts Options) *cobra.Command {
	return &cobra.Command{
		Use:    systemApply + " <place|remove|start|stop|logs> <change>",
		Hidden: true,
		Args:   cobra.ExactArgs(2),
		RunE: func(cmd *cobra.Command, args []string) error {
			payload := []byte(args[1])

			// Windows passes the change in a file, see applyAsRoot.
			if path, isFile := strings.CutPrefix(args[1], "@"); isFile {
				data, err := os.ReadFile(path)
				if err != nil {
					return err
				}

				payload = data
			}

			var change systemChange
			if err := json.Unmarshal(payload, &change); err != nil {
				return fmt.Errorf("read the change: %w", err)
			}

			return applySystem(cmd, opts, args[0], change)
		},
	}
}

func applySystem(cmd *cobra.Command, opts Options, action string, change systemChange) error {
	ctx := cmd.Context()
	item, d := change.Item, change.Definition

	if item.Kind == "service" {
		manager := systemServices(opts)
		d.Name = item.Name

		if manager.File(d) != item.Target {
			return fmt.Errorf("%s is not where this OS keeps the service %s", item.Target, d.Name)
		}

		switch action {
		case "place":
			return manager.Install(ctx, d, item.Enabled)
		case "remove":
			return manager.Remove(ctx, d)
		case "start":
			return manager.Start(ctx, d)
		case "stop":
			return manager.Stop(ctx, d)
		case "logs":
			text, err := manager.Logs(ctx, d, 50)
			if err == nil {
				fmt.Fprintln(cmd.OutOrStdout(), text)
			}

			return err
		}

		return fmt.Errorf("unknown action %s", action)
	}

	dirs := systemDirs(opts)
	if dir := filepath.Dir(item.Target); dir != dirs.Apps && dir != dirs.Fonts {
		return fmt.Errorf("%s is outside %s and %s", item.Target, dirs.Apps, dirs.Fonts)
	}

	switch action {
	case "place":
		return expose.Place(item)
	case "remove":
		if err := expose.Remove(item); err != nil {
			return err
		}

		// On Linux the fonts are in a directory that oku created. Remove fails
		// while another font is in it.
		if filepath.Base(dirs.Fonts) == "oku" {
			os.Remove(dirs.Fonts)
		}

		return nil
	}

	return fmt.Errorf("unknown action %s", action)
}
