package cli

import (
	"bufio"
	"context"
	"errors"
	"fmt"
	"io/fs"
	"os"
	"path/filepath"
	"slices"
	"strings"

	"github.com/spf13/cobra"

	"github.com/y3owk1n/oku/internal/expose"
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
		Args:  cobra.NoArgs,
		RunE: func(cmd *cobra.Command, _ []string) error {
			return runUninstall(cmd, opts, executable, keepList, yes, system)
		},
	}
	uninstall.Flags().
		BoolVar(&keepList, "keep-list", false, "keep the global oku.toml and oku.lock")
	uninstall.Flags().BoolVarP(&yes, "yes", "y", false, "do not ask for confirmation")
	uninstall.Flags().
		BoolVar(&system, "system", false, "with --yes, also remove what needs administrator rights")

	self.AddCommand(uninstall)

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

	out := cmd.OutOrStdout()
	binDir := e.globalProfile().BinDir()

	var kept []string

	if keepList {
		for _, name := range listFiles {
			if _, err := os.Stat(filepath.Join(e.config, name)); err == nil {
				kept = append(kept, filepath.Join(e.config, name))
			}
		}
	}

	fmt.Fprintln(out, "this removes:")
	fmt.Fprintf(out, "  store and profiles  %s\n", e.data)
	fmt.Fprintf(out, "  cache               %s\n", e.cache)
	fmt.Fprintf(out, "  config              %s\n", e.config)
	fmt.Fprintf(out, "  binary              %s\n", executable)

	if e.root != e.data {
		fmt.Fprintf(out, "  shared store root   %s (needs administrator rights)\n", e.root)
	}

	if ledger, err := expose.ReadLedger(e.data); err == nil {
		for _, item := range ledger.Items {
			fmt.Fprintf(out, "  %-19s %s\n", item.Kind, item.Target)
		}
	}

	if len(kept) > 0 {
		fmt.Fprintf(out, "keeps:\n  %s\n", strings.Join(kept, "\n  "))
	}

	in := bufio.NewReader(cmd.InOrStdin())

	if !yes && !confirm(in, out, "continue? [y/N] ") {
		return errors.New("uninstall cancelled, nothing was removed")
	}

	// --yes alone never elevates.
	if e.root != e.data && !yes {
		system = confirm(
			in,
			out,
			fmt.Sprintf("remove %s with administrator rights? [y/N] ", e.root),
		)
	}

	// Files outside oku's directories go first, while the ledger still exists.
	ledger, err := expose.ReadLedger(e.data)
	if err != nil {
		return err
	}

	manager, err := e.services(opts)
	if err != nil {
		return err
	}

	handlers := map[string]expose.Handler{"service": serviceHandler(manager, nil)}
	if err := ledger.RemoveAll(handlers); err != nil {
		return err
	}

	// The cache goes first because on Windows it is inside the data directory.
	for _, dir := range []string{e.cache, e.data} {
		if err := os.RemoveAll(dir); err != nil {
			return fmt.Errorf("remove %s: %w", dir, err)
		}
	}

	left, err := removeSharedRoot(cmd.Context(), opts, e, system)
	if err != nil {
		return err
	}

	if err := removeConfig(e.config, keepList); err != nil {
		return fmt.Errorf("remove %s: %w", e.config, err)
	}

	if err := os.Remove(executable); err != nil && !errors.Is(err, fs.ErrNotExist) {
		return fmt.Errorf("remove %s: %w", executable, err)
	}

	fmt.Fprintln(out, "oku is uninstalled")

	if left != "" {
		fmt.Fprintf(out, "left in place, empty:\n  %s\nremove it with: sudo rmdir %s\n", left, left)
	}

	if lines := hookLines(); len(lines) > 0 {
		fmt.Fprintf(
			out,
			"remove the oku hook from your shell startup file:\n  %s\n",
			strings.Join(lines, "\n  "),
		)
	}

	if slices.Contains(filepath.SplitList(os.Getenv("PATH")), binDir) {
		fmt.Fprintf(out, "remove %s from PATH in your shell config\n", binDir)
	}

	return nil
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
		if err := os.RemoveAll(filepath.Join(e.root, entry.Name())); err != nil {
			return "", fmt.Errorf("remove %s: %w", e.root, err)
		}
	}

	if !elevated {
		return e.root, nil
	}

	if err := elevate(ctx, opts, []string{"rmdir", e.root}); err != nil {
		return "", fmt.Errorf("remove %s: %w", e.root, err)
	}

	return "", nil
}

// removeConfig deletes the config directory. With keepList it deletes
// everything in it except the list files.
func removeConfig(dir string, keepList bool) error {
	if !keepList {
		return os.RemoveAll(dir)
	}

	entries, err := os.ReadDir(dir)
	if errors.Is(err, fs.ErrNotExist) {
		return nil
	}

	if err != nil {
		return err
	}

	for _, entry := range entries {
		if slices.Contains(listFiles, entry.Name()) {
			continue
		}

		if err := os.RemoveAll(filepath.Join(dir, entry.Name())); err != nil {
			return err
		}
	}

	if len(entries) == 0 {
		return os.Remove(dir)
	}

	return nil
}
