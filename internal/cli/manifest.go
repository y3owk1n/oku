package cli

import (
	"errors"
	"fmt"
	"io/fs"
	"os"
	"path/filepath"
	"regexp"
	"strconv"
	"strings"

	"github.com/spf13/cobra"

	"github.com/y3owk1n/oku/internal/infer"
	"github.com/y3owk1n/oku/internal/list"
	"github.com/y3owk1n/oku/internal/manifest"
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
		Long: `Write a manifest inferred from a repo's newest release.

This is the manifest "oku add github:owner/repo" uses for a repo that has none.
Commit it as oku.pkg.toml to control it yourself. Inference opens the asset for
this machine to find the executable, so run it where a release asset exists.`,
		Args: cobra.NoArgs,
		RunE: func(cmd *cobra.Command, _ []string) error {
			// A bare "owner/repo" is a GitHub repo.
			if !strings.Contains(from, ":") {
				from = "github:" + from
			}

			r, err := ref.Parse(from)
			if err != nil || r.Kind != ref.Forge || r.Fragment != "" || r.Version != "" {
				return fmt.Errorf("--from %q: want owner/repo or a ref such as codeberg:owner/repo", from)
			}

			e, err := loadEnv()
			if err != nil {
				return err
			}

			text, err := e.inferrer(opts).Manifest(
				cmd.Context(), r.Scheme, r.Location, platform.Host(), infer.Options{},
			)
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

	init.Flags().StringVar(&from, "from", "", "the repo to read, as owner/repo on GitHub or as a ref such as codeberg:owner/repo")
	init.Flags().
		StringVarP(&output, "output", "o", ref.Manifest.Default, `the file to write, or "-" for stdout`)
	init.Flags().BoolVar(&force, "force", false, "replace the output file when it exists")
	_ = init.MarkFlagRequired("from")

	cmd.AddCommand(init, newLintCmd(), newBumpCmd(opts), newTestCmd(opts))

	return cmd
}

func newLintCmd() *cobra.Command {
	return &cobra.Command{
		Use:   "lint [file...]",
		Short: "Check manifests for mistakes before you publish them",
		Long: `Check manifests for mistakes before you publish them.

Without a file it checks oku.pkg.toml. Lint knows every key of the manifest
schema, so it catches a misspelt key that "oku add" would ignore. It exits with
status 1 when any file has an error. Warnings do not fail it.`,
		RunE: func(cmd *cobra.Command, args []string) error {
			if len(args) == 0 {
				args = []string{ref.Manifest.Default}
			}

			out := cmd.OutOrStdout()
			failed := 0

			for _, file := range args {
				data, err := os.ReadFile(file)
				if err != nil {
					return err
				}

				report := manifest.Lint(data)

				for _, problem := range report.Errors {
					fmt.Fprintf(out, "%s: error: %s\n", file, problem)
				}

				for _, warning := range report.Warnings {
					fmt.Fprintf(out, "%s: warning: %s\n", file, warning)
				}

				if len(report.Errors) > 0 {
					failed++
				} else {
					fmt.Fprintf(out, "%s: ok\n", file)
				}
			}

			if failed > 0 {
				return fmt.Errorf("%d of %d manifests have errors", failed, len(args))
			}

			return nil
		},
	}
}

var releaseURLRe = regexp.MustCompile(
	`github\.com/([A-Za-z0-9][A-Za-z0-9-]*/[A-Za-z0-9._-]+)/releases/download/`,
)

func newBumpCmd(opts Options) *cobra.Command {
	var repo, prefix, to string

	cmd := &cobra.Command{
		Use:   "bump [file]",
		Short: "Move a fixed-version manifest to the newest upstream release",
		Long: `Move a fixed-version manifest to the newest upstream release.

Bump rewrites version.value and every inline sha256, and downloads each artifact
to compute its new digest. It reads the GitHub repo from the artifact URLs, or
from --repo. A manifest that discovers its versions needs no bump.`,
		Args: cobra.MaximumNArgs(1),
		RunE: func(cmd *cobra.Command, args []string) error {
			file := ref.Manifest.Default
			if len(args) == 1 {
				file = args[0]
			}

			return runBump(cmd, opts, file, repo, prefix, to)
		},
	}

	cmd.Flags().StringVar(&repo, "repo", "", "the GitHub repo to read releases from, as owner/repo")
	cmd.Flags().
		StringVar(&prefix, "strip-prefix", "", `text before the version in a tag, such as "v"`)
	cmd.Flags().StringVar(&to, "to", "", "the version to move to, instead of the newest")

	return cmd
}

func runBump(cmd *cobra.Command, opts Options, file, repo, prefix, to string) error {
	data, err := os.ReadFile(file)
	if err != nil {
		return err
	}

	m, err := manifest.Parse(data, file)
	if err != nil {
		return err
	}

	if m.Version.From != "" {
		return fmt.Errorf(
			"%s discovers its versions from %s, so there is nothing to bump",
			file,
			m.Version.From,
		)
	}

	text := string(data)
	old := m.Version.Value

	if repo == "" {
		found := releaseURLRe.FindStringSubmatch(text)
		if found == nil {
			return fmt.Errorf(
				"%s has no GitHub release URL to read the repo from, pass --repo owner/repo",
				file,
			)
		}

		repo = found[1]
	}

	// A URL such as ".../download/v1.2.0/..." shows that tags start with "v".
	if !cmd.Flags().Changed("strip-prefix") && strings.Contains(text, "/releases/download/v") {
		prefix = "v"
	}

	e, err := loadEnv()
	if err != nil {
		return err
	}

	release, err := e.resolver(opts).Pick(cmd.Context(), manifest.Version{
		From: manifest.FromGitHubReleases, Repo: repo, StripPrefix: prefix,
	}, to)
	if err != nil {
		return err
	}

	out := cmd.OutOrStdout()

	if release.Version == old {
		fmt.Fprintf(out, "%s is already at %s\n", m.Package.Name, old)

		return nil
	}

	// A URL that spells the version out, without {{version}}, gets the new one
	// too.
	lines := strings.Split(text, "\n")
	for i, line := range lines {
		key, _, _ := strings.Cut(strings.TrimSpace(line), "=")

		switch strings.TrimSpace(key) {
		case "value":
			lines[i] = strings.Replace(line, strconv.Quote(old), strconv.Quote(release.Version), 1)
		case "url", "sha256_url":
			lines[i] = strings.ReplaceAll(line, old, release.Version)
		}
	}

	text = strings.Join(lines, "\n")

	bumped, err := manifest.Parse([]byte(text), file)
	if err != nil {
		return fmt.Errorf("the bumped manifest is not valid: %w", err)
	}

	bumped.Tag = release.Tag
	updated := 0

	for i, artifact := range bumped.Artifacts {
		if artifact.SHA256 == "" {
			continue
		}

		// Select expands templates for a platform, so ask for this artifact's own.
		target := platform.Platform{
			OS:   artifact.Match.OS,
			Arch: artifact.Match.Arch,
			Libc: artifact.Match.Libc,
		}
		single := *bumped
		single.Artifacts = bumped.Artifacts[i : i+1]

		expanded, _, err := single.Select(target)
		if err != nil {
			return err
		}

		digest, err := e.store().Digest(cmd.Context(), expanded.URL)
		if err != nil {
			return fmt.Errorf("artifact[%d]: %w", i, err)
		}

		text = strings.ReplaceAll(text, artifact.SHA256, digest)
		updated++
	}

	if err := list.WriteFile(file, []byte(text)); err != nil {
		return err
	}

	fmt.Fprintf(
		out,
		"%s %s -> %s, %d checksums updated in %s\n",
		m.Package.Name,
		old,
		release.Version,
		updated,
		file,
	)

	return nil
}

func newTestCmd(opts Options) *cobra.Command {
	var (
		flags buildFlags
		keep  bool
	)

	cmd := &cobra.Command{
		Use:   "test [file]",
		Short: "Install a manifest into a throwaway store to see that it works",
		Long: `Install a manifest into a throwaway store to see that it works.

Without a file it tests oku.pkg.toml. A manifest with a [build] is built from
source, deps included, in the same sandbox a user gets. A manifest with only
artifacts installs the artifact for this machine. Your own store, profile,
oku.toml and oku.lock are not touched. Downloads still go to your cache.`,
		Args: cobra.MaximumNArgs(1),
		RunE: func(cmd *cobra.Command, args []string) error {
			file := ref.Manifest.Default
			if len(args) == 1 {
				file = args[0]
			}

			return runManifestTest(cmd, opts, file, &flags, keep)
		},
	}

	flags.register(cmd)
	cmd.Flags().BoolVar(&keep, "keep", false, "keep the throwaway store and print where it is")

	return cmd
}

func runManifestTest(
	cmd *cobra.Command,
	opts Options,
	file string,
	flags *buildFlags,
	keep bool,
) error {
	r, err := ref.Parse(file)
	if err != nil {
		return err
	}

	data, err := os.ReadFile(r.Location)
	if err != nil {
		return err
	}

	m, err := manifest.Parse(data, file)
	if err != nil {
		return err
	}

	own, err := loadEnv()
	if err != nil {
		return err
	}

	scratch, err := os.MkdirTemp("", "oku-test-")
	if err != nil {
		return err
	}

	if !keep {
		defer os.RemoveAll(scratch)
	}

	// Only the store is throwaway. The cache is content-addressed, so sharing it
	// is safe and saves downloads.
	e := env{
		config: scratch + "/config",
		data:   scratch + "/data",
		root:   scratch + "/data",
		cache:  own.cache,
	}
	out := cmd.OutOrStdout()

	got, err := e.install(cmd.Context(), opts, request{
		ref:        r,
		fromSource: m.HasBuild(),
		approve:    e.approver(cmd, opts, flags),
		log:        buildLog(cmd, flags),
		progress: func(step, total int, kind string, err error) {
			result := "ok"
			if err != nil {
				result = "FAILED"
			}

			fmt.Fprintf(out, "[%d/%d] %-8s %s\n", step+1, total, kind, result)
		},
	})
	if err != nil {
		return err
	}

	reportUnsandboxed(cmd.ErrOrStderr(), got)
	reportCache(cmd.ErrOrStderr(), got)

	strategy := got.lock.Platforms[platform.Host().String()].Strategy
	fmt.Fprintf(
		out,
		"%s %s works on %s (%s)\n",
		got.lock.Name,
		got.lock.Version,
		platform.Host(),
		strategy,
	)

	for _, sub := range []string{"bin", "share/man", "share/completions"} {
		_ = filepath.WalkDir(
			filepath.Join(got.profile.StorePath, sub),
			func(path string, entry fs.DirEntry, err error) error {
				if err == nil && !entry.IsDir() {
					rel, _ := filepath.Rel(got.profile.StorePath, path)
					fmt.Fprintf(out, "  %s\n", filepath.ToSlash(rel))
				}

				return nil
			},
		)
	}

	if keep {
		fmt.Fprintf(out, "kept %s\n", got.profile.StorePath)
	}

	return nil
}
