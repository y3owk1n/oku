package cli

import (
	"fmt"
	"os"
	"path/filepath"
	"slices"
	"strings"

	"github.com/spf13/cobra"

	"github.com/y3owk1n/oku/internal/manifest"
	"github.com/y3owk1n/oku/internal/platform"
	"github.com/y3owk1n/oku/internal/profile"
)

func newAddCmd() *cobra.Command {
	return &cobra.Command{
		Use:   "add <manifest.toml>",
		Short: "Install a package from a manifest file",
		Args:  cobra.ExactArgs(1),
		RunE:  runAdd,
	}
}

func runAdd(cmd *cobra.Command, args []string) error {
	ref := args[0]
	if strings.Contains(ref, "://") || strings.HasPrefix(ref, "github:") ||
		strings.HasPrefix(ref, "git+") {
		return fmt.Errorf("%s: only local manifest files are supported so far", ref)
	}

	ref, err := filepath.Abs(ref)
	if err != nil {
		return err
	}

	e, err := loadEnv()
	if err != nil {
		return err
	}

	m, err := manifest.Load(ref)
	if err != nil {
		return err
	}

	host := platform.Host()

	artifact, ok, err := m.Select(host)
	if err != nil {
		return err
	}

	if !ok && m.HasBuild() {
		return fmt.Errorf(
			"%s has no artifact for %s, and building from source is not supported so far",
			m.Package.Name, host,
		)
	}

	if !ok {
		return fmt.Errorf("%s has no artifact for %s", m.Package.Name, host)
	}

	storePath, err := e.store().Realize(cmd.Context(), m, artifact, host)
	if err != nil {
		return err
	}

	prof := e.globalProfile()

	err = prof.Add(profile.Package{
		Name:      m.Package.Name,
		Version:   m.Version.Value,
		Ref:       ref,
		StorePath: storePath,
	})
	if err != nil {
		return err
	}

	fmt.Fprintf(cmd.OutOrStdout(), "added %s %s\n", m.Package.Name, m.Version.Value)

	if !slices.Contains(filepath.SplitList(os.Getenv("PATH")), prof.BinDir()) {
		fmt.Fprintf(cmd.ErrOrStderr(), "add %s to PATH to run it\n", prof.BinDir())
	}

	return nil
}
