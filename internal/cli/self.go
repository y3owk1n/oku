package cli

import (
	"context"
	"errors"
	"fmt"
	"io/fs"
	"os"
	"path/filepath"
	"runtime"
	"slices"
	"strings"

	"github.com/spf13/cobra"

	"github.com/y3owk1n/oku/internal/expose"
	"github.com/y3owk1n/oku/internal/service"
	"github.com/y3owk1n/oku/internal/source"
	"github.com/y3owk1n/oku/internal/trash"
	"github.com/y3owk1n/oku/internal/ui"
)

// listFiles are what --keep-list leaves in the config directory.
var listFiles = []string{"oku.toml", "oku.lock"}

func newSelfCmd(opts Options) *cobra.Command {
	executable := opts.Executable

	self := &cobra.Command{
		Use:   "self",
		Short: "Manage the oku installation itself",
	}

	var keepList, yes, system bool

	uninstall := &cobra.Command{
		Use:   "uninstall",
		Short: "Remove oku and everything it installed",
		Args:  noArgs,
		RunE: func(cmd *cobra.Command, _ []string) error {
			return runUninstall(cmd, opts, executable, keepList, yes, system)
		},
	}
	uninstall.Flags().
		BoolVar(&keepList, "keep-list", false, "keep the global oku.toml and oku.lock")
	uninstall.Flags().BoolVarP(&yes, "yes", "y", false, "do not ask for confirmation")
	uninstall.Flags().
		BoolVar(&system, "system", false, "with --yes, also remove what needs administrator rights")

	self.AddCommand(uninstall, newSelfUpdateCmd(opts))

	return self
}

func runUninstall(
	cmd *cobra.Command,
	opts Options,
	executable string,
	keepList, yes, system bool,
) error {
	e, err := loadEnv()
	if err != nil {
		return err
	}

	release, err := lockMachine(cmd)
	if err != nil {
		return err
	}
	defer release()

	out := cmd.OutOrStdout()
	s := ui.For(out)
	binDir := e.globalProfile().BinDir()

	var kept []string

	if keepList {
		for _, name := range listFiles {
			if _, err := os.Stat(filepath.Join(e.config, name)); err == nil {
				kept = append(kept, filepath.Join(e.config, name))
			}
		}
	}

	fmt.Fprintln(out, s.Bold("this removes:"))
	fmt.Fprintf(out, "  store and profiles  %s\n", s.Home(e.data))
	fmt.Fprintf(out, "  cache               %s\n", s.Home(e.cache))
	fmt.Fprintf(out, "  config              oku's own files in %s\n", s.Home(e.config))
	fmt.Fprintf(out, "  binary              %s\n", s.Home(executable))

	if e.root != e.data {
		fmt.Fprintf(out, "  shared store root   %s%s\n", s.Home(e.root), s.Warn(" (needs administrator rights)"))
	}

	ledger, err := expose.ReadLedger(e.data)
	if err != nil {
		return err
	}

	needsRoot := e.root != e.data

	for _, item := range ledger.Items {
		note := ""
		if item.System {
			needsRoot = true
			note = s.Warn(" (needs administrator rights)")
		}

		fmt.Fprintf(out, "  %-19s %s%s\n", item.Kind, s.Home(item.Target), note)
	}

	if len(kept) > 0 {
		fmt.Fprintf(out, "%s\n  %s\n", s.Bold("keeps:"), s.Homes(strings.Join(kept, "\n  ")))
	}

	in := cmd.InOrStdin()

	if !yes && !confirm(in, out, "continue?") {
		return errors.New("uninstall cancelled, nothing was removed")
	}

	// --yes alone never elevates.
	if needsRoot && !yes {
		system = confirm(in, out, "remove what needs administrator rights?")
	}

	// Files outside oku's directories go first, while the ledger still exists.
	handlers, err := e.handlers(cmd.Context(), opts, nil, nil)
	if err != nil {
		return err
	}

	// Without administrator rights the items in system scope stay.
	var stay []expose.Item
	if !system {
		stay = keepSystem(ledger.Items, nil)
	}

	if err := checkExtensions(opts, ledger.Leaving(stay)); err != nil {
		return err
	}

	before := slices.Clone(ledger.Items)

	err = ledger.Sync(stay, handlers)

	tellSettings(opts, before, ledger.Items)

	if err != nil {
		return err
	}

	// Windows cannot delete the lock file while it is open.
	release()

	// The cache goes first because on Windows it is inside the data directory. A
	// program that oku installed may still run, and Windows keeps its files, so
	// they move aside and go once it ends.
	for _, dir := range []string{e.cache, e.data} {
		if err := trash.Remove(dir, uninstalled(e.data)); err != nil {
			return fmt.Errorf("remove %s: %w", dir, err)
		}
	}

	emptyRoot, err := removeSharedRoot(cmd.Context(), opts, e, system)
	if err != nil {
		return err
	}

	notOkus, err := removeConfig(e.config, keepList)
	if err != nil {
		return fmt.Errorf("remove %s: %w", e.config, err)
	}

	if err := removeBinary(executable); err != nil && !errors.Is(err, fs.ErrNotExist) {
		return fmt.Errorf("remove %s: %w", executable, err)
	}

	var aside []string

	for _, dir := range []string{uninstalled(e.data), uninstalled(e.root)} {
		if _, err := os.Stat(dir); err == nil && !slices.Contains(aside, dir) {
			if err := deleteLater(dir); err != nil {
				return fmt.Errorf("remove %s: %w", dir, err)
			}

			aside = append(aside, dir)
		}
	}

	finished(out, "oku is uninstalled")

	// Each list is a note with its items under it. A command to run is not
	// wrapped, so that it copies as one line.
	left := func(note string, items []string) {
		warn(out, "%s", note)

		for _, item := range items {
			fmt.Fprintf(out, "  %s\n", s.Home(item))
		}
	}

	commands := func(lines ...string) {
		for _, line := range lines {
			fmt.Fprintf(out, "  %s\n", s.Accent(line))
		}
	}

	if len(aside) > 0 {
		left("a program that oku installed still runs, and its files go once it ends:", aside)
	}

	if len(notOkus) > 0 {
		left("left in place, because oku did not write them:", notOkus)
	}

	if emptyRoot != "" {
		left("left in place, empty:", []string{emptyRoot})
		fmt.Fprintln(out, "remove it with:")
		commands("sudo rmdir " + emptyRoot)
	}

	if len(stay) > 0 {
		var items []string
		for _, item := range stay {
			items = append(items, fmt.Sprintf("%-8s %s", item.Kind, item.Target))
		}

		left("left in place, because removing them needs administrator rights:", items)

		if runtime.GOOS == "windows" {
			fmt.Fprintln(out, "remove them in a terminal that runs as administrator, with:")
		} else {
			fmt.Fprintln(out, "remove them with:")
		}

		for _, item := range stay {
			if item.Kind == "service" {
				commands(stopCommand(item.Name))
			}

			commands(removeCommand(item.Target))
		}
	}

	if lines := hookLines(); len(lines) > 0 {
		left("remove the oku hook from your shell startup file:", lines)
	}

	if slices.Contains(filepath.SplitList(os.Getenv("PATH")), binDir) {
		warn(out, "remove %s from PATH in your shell config", binDir)
	}

	return nil
}

// stopCommand is what stops a system service by hand once oku is gone.
func stopCommand(name string) string {
	switch runtime.GOOS {
	case "darwin":
		return "sudo launchctl bootout system/" + service.Definition{Name: name}.Label()
	case "windows":
		return "schtasks /Delete /F /TN oku-" + name
	}

	return "sudo systemctl disable --now oku-" + name + ".service"
}

// removeCommand is what deletes a file of system scope by hand once oku is gone.
func removeCommand(target string) string {
	if runtime.GOOS == "windows" {
		return fmt.Sprintf("del /f /q %q", target)
	}

	return fmt.Sprintf("sudo rm -rf %q", target)
}

// uninstalled is where uninstall moves the files that a running program keeps,
// on the same volume as dir.
func uninstalled(dir string) string {
	return filepath.Join(filepath.Dir(dir), "oku-uninstalled")
}

// removeSharedRoot empties the shared store root, which the user owns, and then
// deletes the directory itself, which needs administrator rights. Without
// elevated it returns the directory it left behind.
func removeSharedRoot(ctx context.Context, opts Options, e env, elevated bool) (string, error) {
	if e.root == e.data {
		return "", nil
	}

	entries, err := os.ReadDir(e.root)
	if err != nil && !errors.Is(err, fs.ErrNotExist) {
		return "", fmt.Errorf("read %s: %w", e.root, err)
	}

	for _, entry := range entries {
		if err := trash.Remove(filepath.Join(e.root, entry.Name()), uninstalled(e.root)); err != nil {
			return "", fmt.Errorf("remove %s: %w", e.root, err)
		}
	}

	if !elevated {
		return e.root, nil
	}

	if err := elevate(ctx, opts, removeDirArgv(e.root)); err != nil {
		return "", fmt.Errorf("remove %s: %w", e.root, err)
	}

	return "", nil
}

// removeConfig deletes the files oku writes in the config directory, without
// the list files when keepList is set, and the directory when that empties it.
// It returns what else is in there. The sources of [files], the user's own
// manifests and a .git directory are the user's, and stay.
func removeConfig(dir string, keepList bool) ([]string, error) {
	own := []string{source.FileName, signingKeyFile}
	if !keepList {
		own = append(own, listFiles...)
	}

	for _, name := range own {
		err := os.Remove(filepath.Join(dir, name))
		if err != nil && !errors.Is(err, fs.ErrNotExist) {
			return nil, err
		}
	}

	entries, err := os.ReadDir(dir)
	if errors.Is(err, fs.ErrNotExist) {
		return nil, nil
	}

	if err != nil {
		return nil, err
	}

	if len(entries) == 0 {
		return nil, os.Remove(dir)
	}

	var left []string

	for _, entry := range entries {
		if !slices.Contains(listFiles, entry.Name()) {
			left = append(left, filepath.Join(dir, entry.Name()))
		}
	}

	return left, nil
}
