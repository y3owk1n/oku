package cli

import (
	"bufio"
	"errors"
	"fmt"
	"io/fs"
	"os"
	"path/filepath"
	"slices"
	"strings"

	"github.com/spf13/cobra"
)

// listFiles are what --keep-list leaves in the config directory.
var listFiles = []string{"oku.toml", "oku.lock"}

func newSelfCmd(executable string) *cobra.Command {
	self := &cobra.Command{
		Use:   "self",
		Short: "Manage the oku installation itself",
	}

	var keepList, yes bool

	uninstall := &cobra.Command{
		Use:   "uninstall",
		Short: "Remove oku and everything it installed",
		Args:  cobra.NoArgs,
		RunE: func(cmd *cobra.Command, _ []string) error {
			return runUninstall(cmd, executable, keepList, yes)
		},
	}
	uninstall.Flags().
		BoolVar(&keepList, "keep-list", false, "keep the global oku.toml and oku.lock")
	uninstall.Flags().BoolVarP(&yes, "yes", "y", false, "do not ask for confirmation")

	self.AddCommand(uninstall)

	return self
}

func runUninstall(cmd *cobra.Command, executable string, keepList, yes bool) error {
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

	if len(kept) > 0 {
		fmt.Fprintf(out, "keeps:\n  %s\n", strings.Join(kept, "\n  "))
	}

	if !yes {
		fmt.Fprint(out, "continue? [y/N] ")

		answer, _ := bufio.NewReader(cmd.InOrStdin()).ReadString('\n')
		if a := strings.ToLower(strings.TrimSpace(answer)); a != "y" && a != "yes" {
			return errors.New("uninstall cancelled, nothing was removed")
		}
	}

	// The cache goes first because on Windows it is inside the data directory.
	for _, dir := range []string{e.cache, e.data} {
		if err := os.RemoveAll(dir); err != nil {
			return fmt.Errorf("remove %s: %w", dir, err)
		}
	}

	if err := removeConfig(e.config, keepList); err != nil {
		return fmt.Errorf("remove %s: %w", e.config, err)
	}

	if err := os.Remove(executable); err != nil && !errors.Is(err, fs.ErrNotExist) {
		return fmt.Errorf("remove %s: %w", executable, err)
	}

	fmt.Fprintln(out, "oku is uninstalled")

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
