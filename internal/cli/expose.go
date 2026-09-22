package cli

import (
	"bufio"
	"context"
	"encoding/json"
	"fmt"
	"io"
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
	"github.com/y3owk1n/oku/internal/ui"
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

// wantedItems lists the apps, fonts and services of pkgs and the files of a
// generation, with the definitions of the services by name.
func (e env) wantedItems(
	opts Options,
	pkgs []profile.Package,
	files []profile.File,
	wantedSettings []profile.Setting,
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

	for _, f := range files {
		if len(f.Secrets) > 0 {
			wanted = append(wanted, expose.Item{
				Kind: "secret", Source: e.secretPath(f), Target: f.Target, Hash: f.Hash,
				Dir: parentMode(f),
			})

			continue
		}

		source := f.Link
		if source == "" {
			source = e.globalProfile().ContentPath(f)
		}

		item := expose.Item{Kind: "file", Source: source, Target: f.Target, Dir: parentMode(f)}

		// Windows gets a copy of a file, which oku recognizes by its hash, and a
		// junction for a directory.
		if runtime.GOOS == "windows" {
			item.Hash = f.Hash
		}

		wanted = append(wanted, item)
	}

	for _, s := range wantedSettings {
		wanted = append(wanted, expose.Item{
			Kind: "setting", Domain: s.Domain, Key: s.Key, Source: s.Value,
			Target: s.Domain + " " + s.Key,
		})
	}

	services, defs, err := e.serviceItems(opts, pkgs)

	return append(wanted, services...), defs, err
}

// exposePlan is what a change exposes, decided before the first change.
type exposePlan struct {
	wanted []expose.Item
	defs   map[string]service.Definition
	// secrets is the decrypted content of the files that use secrets, by target.
	// It exists in memory only.
	secrets map[string]sealed
	// elevated reports that the user agreed to change system scope.
	elevated bool
	// rewritten are the targets whose content the change gives other bytes. Such
	// a file changes through "current" and not through the ledger.
	rewritten []string
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
	files []profile.File,
	wantedSettings []profile.Setting,
	system bool,
) (exposePlan, error) {
	notice := cmd.ErrOrStderr()

	wanted, defs, err := e.wantedItems(opts, pkgs, files, wantedSettings)
	if err != nil {
		return exposePlan{}, err
	}

	if reason := e.skippedServices(opts, pkgs); reason != "" && e.project == "" {
		fmt.Fprintf(notice, "services are skipped, because %s\n", reason)
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

	if edited := ledger.Edited(); len(edited) > 0 {
		return exposePlan{}, fmt.Errorf(
			"%s changed since oku wrote it, and a sync would overwrite it\n"+
				"move the change into its source or into oku.toml, then delete the file",
			strings.Join(edited, ", "),
		)
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

	handlers, err := e.handlers(cmd.Context(), opts, plan.defs, plan.secrets)
	if err != nil {
		return err
	}

	if err := ledger.Sync(plan.wanted, handlers); err != nil {
		return err
	}

	tellSettings(opts, before, ledger.Items)
	tellExposed(notice, before, ledger.Items, plan.rewritten)

	return nil
}

// tellExposed prints one line for each item that the change placed, changed
// or removed. An item of the same kind and target on both sides with another
// source, such as a setting with a new value, is a change.
func tellExposed(notice io.Writer, before, after []expose.Item, rewritten []string) {
	s := ui.For(notice)
	done := func(format string, args ...any) {
		fmt.Fprintln(notice, s.Done(fmt.Sprintf(format, args...)))
	}
	gone := func(format string, args ...any) {
		fmt.Fprintln(notice, s.Gone(fmt.Sprintf(format, args...)))
	}

	same := func(items []expose.Item, item expose.Item) bool {
		return slices.ContainsFunc(items, func(other expose.Item) bool {
			return other.Kind == item.Kind && other.Target == item.Target
		})
	}

	for _, item := range before {
		if slices.Contains(after, item) || same(after, item) {
			continue
		}

		switch {
		case item.Kind == "secret":
			gone("removed the secret %s", item.Target)
		case item.Kind == "setting" && item.HadPrior:
			done("restored %s", item.Target)
		case item.Kind == "setting":
			gone("unset %s", item.Target)
		case item.Kind == "file":
			gone("removed %s", item.Target)
		case item.Kind != "service":
			gone("removed the %s %s", item.Kind, item.Target)
		default:
			gone("service %s is removed", item.Name)
		}
	}

	for _, target := range rewritten {
		done("changed %s", target)
	}

	for _, item := range after {
		// An item that changed, such as a service that was just enabled, is new too.
		if slices.Contains(before, item) {
			continue
		}

		changed := same(before, item)

		switch {
		case item.Kind == "secret" && changed:
			done("changed the secret %s", item.Target)
		case item.Kind == "secret":
			done("wrote the secret %s", item.Target)
		case item.Kind == "setting" && changed:
			done("changed %s", item.Target)
		case item.Kind == "setting":
			done("set %s", item.Target)
		case item.Kind == "file" && changed:
			done("changed %s", item.Target)
		case item.Kind == "file":
			done("wrote %s", item.Target)
		case item.Kind != "service":
			done("exposed %s %s", item.Kind, item.Target)
		case item.Enabled && item.System:
			done("service %s is running and starts at boot", item.Name)
		case item.Enabled:
			done("service %s is running and starts at login", item.Name)
		default:
			done("service %s is installed and stopped", item.Name)
		}
	}
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
	secrets map[string]sealed,
) (map[string]expose.Handler, error) {
	manager, err := e.services(opts)
	if err != nil {
		return nil, err
	}

	elevated := func(action string, item expose.Item) error {
		return e.applyAsRoot(ctx, opts, action, systemChange{item, defs[item.Name]})
	}

	store, _ := settingsStore(opts)
	handlers := map[string]expose.Handler{
		"setting": settingHandler(store),
		"secret":  secretHandler(secrets),
	}

	for _, kind := range []string{"app", "font", "service", "file"} {
		local := expose.Handler{
			Place:  expose.Place,
			Remove: expose.Remove,
		}

		switch kind {
		case "service":
			local = serviceHandler(manager, defs)
		case "file":
			local = expose.Handler{Place: expose.PlaceFile, Remove: expose.RemoveFile}
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
		Args:   exactArgs(2),
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
