package cli

import (
	"errors"
	"fmt"
	"io/fs"
	"os"
	"strings"

	"github.com/spf13/cobra"

	"github.com/y3owk1n/oku/internal/list"
	"github.com/y3owk1n/oku/internal/platform"
	"github.com/y3owk1n/oku/internal/ref"
)

func newManifestCmd(opts Options) *cobra.Command {
	cmd := &cobra.Command{
		Use:   "manifest",
		Short: "Tools for people who publish a package manifest",
	}

	var (
		from, output string
		force        bool
	)

	init := &cobra.Command{
		Use:   "init --from <owner/repo>",
		Short: "Write a manifest inferred from a GitHub repo's newest release",
		Long: `Write a manifest inferred from a GitHub repo's newest release.

This is the manifest "oku add github:owner/repo" uses for a repo that has none.
Commit it as oku.pkg.toml to control it yourself. Inference opens the asset for
this machine to find the executable, so run it where a release asset exists.`,
		Args: cobra.NoArgs,
		RunE: func(cmd *cobra.Command, _ []string) error {
			r, err := ref.Parse("github:" + strings.TrimPrefix(from, "github:"))
			if err != nil || r.Fragment != "" || r.Version != "" {
				return fmt.Errorf("--from %q: want owner/repo", from)
			}

			e, err := loadEnv()
			if err != nil {
				return err
			}

			text, err := e.inferrer(opts).Manifest(cmd.Context(), r.Location, platform.Host())
			if err != nil {
				return err
			}

			if output == "-" {
				fmt.Fprint(cmd.OutOrStdout(), text)

				return nil
			}

			if _, err := os.Stat(output); !force && !errors.Is(err, fs.ErrNotExist) {
				return fmt.Errorf("%s already exists, pass --force to replace it", output)
			}

			if err := list.WriteFile(output, []byte(text)); err != nil {
				return err
			}

			fmt.Fprintf(cmd.OutOrStdout(), "wrote %s\n", output)

			return nil
		},
	}

	init.Flags().StringVar(&from, "from", "", "the GitHub repo to read, as owner/repo")
	init.Flags().
		StringVarP(&output, "output", "o", ref.Manifest.Default, `the file to write, or "-" for stdout`)
	init.Flags().BoolVar(&force, "force", false, "replace the output file when it exists")
	_ = init.MarkFlagRequired("from")

	cmd.AddCommand(init)

	return cmd
}
