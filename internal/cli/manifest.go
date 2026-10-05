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
	"github.com/y3owk1n/oku/internal/store"
	"github.com/y3owk1n/oku/internal/tempdir"
	"github.com/y3owk1n/oku/internal/ui"
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
		Short: "Write a manifest inferred from a repo's newest release or a registry package",
		Long: `Write a manifest inferred from a repo's newest release, or from a package
of npm, PyPI, the Go module proxy or crates.io.

This is the manifest "oku add github:owner/repo" uses for a repo that has none.
Commit it as oku.pkg.toml to control it yourself. Inference opens the asset for
this machine to find the executable, so run it where a release asset exists.`,
		Args: noArgs,
		RunE: func(cmd *cobra.Command, _ []string) error {
			if from == "" {
				return fmt.Errorf("missing --from\nusage: %s", cmd.UseLine())
			}

			// A bare "owner/repo" is a GitHub repo.
			if !strings.Contains(from, ":") {
				from = "github:" + from
			}

			r, err := ref.Parse(from)
			if err != nil || r.Kind != ref.Forge && !fromRegistry(r.Kind) ||
				r.Fragment != "" || r.Version != "" {
				return fmt.Errorf(
					"--from %q: want owner/repo or a ref such as codeberg:owner/repo, "+
						"npm:@scope/name, pypi:name, go:host/path or cargo:name",
					from,
				)
			}

			e, err := loadEnv()
			if err != nil {
				return err
			}

			var (
				text     string
				inferred infer.Inferred
			)

			if write := e.inferrerOf(r.Kind); write != nil {
				all, listErr := e.mergedList(cmd, opts)
				if listErr != nil {
					return listErr
				}

				e.runtimes = all.runtimes
				text, err = write(cmd.Context(), opts, request{ref: r})
			} else {
				// A published manifest serves every platform, so init opens an
				// asset for each, where add opens them for the lock's.
				inferred, err = e.inferrer(opts).Manifest(
					cmd.Context(), r.Scheme, r.Location, platform.Host(),
					infer.Options{Platforms: platform.All()},
				)
				text = inferred.Text
			}

			if err != nil {
				return err
			}

			if len(inferred.Others) > 0 {
				warn(
					cmd.ErrOrStderr(), "these assets fit %s too: %s\nedit the artifact's url to use one",
					infer.Machine(platform.Host()), strings.Join(inferred.Others, ", "),
				)
			}

			if len(inferred.Found) > 0 {
				warn(
					cmd.ErrOrStderr(), "the asset also holds %s, which the manifest leaves out\nadd them to bin to expose them",
					strings.Join(inferred.Found, ", "),
				)
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

			finished(cmd.OutOrStdout(), "wrote %s", output)

			return nil
		},
	}

	init.Flags().
		StringVar(&from, "from", "", "the repo to read, as owner/repo on GitHub or as a ref such as codeberg:owner/repo")
	init.Flags().
		StringVarP(&output, "output", "o", ref.Manifest.Default, `the file to write, or "-" for stdout`)
	init.Flags().BoolVar(&force, "force", false, "replace the output file when it exists")

	cmd.AddCommand(init, newLintCmd(), newBumpCmd(opts), newTestCmd(opts), newHashCmd())

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
			s := ui.For(out)
			failed := 0

			type linted struct {
				File     string   `json:"file"`
				Errors   []string `json:"errors"`
				Warnings []string `json:"warnings"`
			}

			var reports []linted

			for _, file := range args {
				data, err := readManifestFile(file)
				if err != nil {
					return err
				}

				report := manifest.Lint(data)
				if len(report.Errors) > 0 {
					failed++
				}

				if wantJSON(cmd) {
					// A script iterates over each list, so an empty one is [] and not null.
					reports = append(reports, linted{
						file, append([]string{}, report.Errors...), append([]string{}, report.Warnings...),
					})

					continue
				}

				// A terminal gets a mark in front of each line. A pipe keeps the
				// "file: kind: text" form that editors read.
				for _, problem := range report.Errors {
					if s.On() {
						fmt.Fprintf(out, "%s %s %s\n", s.Cross(), s.Bold(file+":"), s.Code(problem))
					} else {
						fmt.Fprintf(out, "%s: error: %s\n", file, problem)
					}
				}

				for _, warning := range report.Warnings {
					if s.On() {
						fmt.Fprintf(out, "%s %s %s\n", s.Note(), s.Bold(file+":"), s.Code(warning))
					} else {
						fmt.Fprintf(out, "%s: warning: %s\n", file, warning)
					}
				}

				if len(report.Errors) == 0 {
					if s.On() {
						fmt.Fprintln(out, s.Done(file))
					} else {
						fmt.Fprintf(out, "%s: ok\n", file)
					}
				}
			}

			if wantJSON(cmd) {
				if err := printJSON(cmd, reports); err != nil {
					return err
				}
			}

			if failed > 0 {
				return fmt.Errorf("%d of %d manifests have errors", failed, len(args))
			}

			return nil
		},
	}
}

// The release URLs of the hosts oku can tell apart by name. Any other host may
// be GitHub Enterprise, Gitea or GitLab, so bump needs --repo for it.
var (
	githubURLRe = regexp.MustCompile(
		`https://github\.com/([A-Za-z0-9][A-Za-z0-9-]*/[A-Za-z0-9._-]+)/releases/download/`,
	)
	codebergURLRe = regexp.MustCompile(
		`https://(codeberg\.org/[A-Za-z0-9][A-Za-z0-9._-]*/[A-Za-z0-9._-]+)/releases/download/`,
	)
	gitlabURLRe = regexp.MustCompile(`https://gitlab\.com/([A-Za-z0-9._/-]+?)/-/releases/`)
	npmURLRe    = regexp.MustCompile(
		`https://registry\.npmjs\.org/((?:@[a-z0-9][a-z0-9._~-]*/)?[a-z0-9][a-z0-9._~-]*)/-/`,
	)
)

// bumpSource returns the version.from and version.repo that bump lists releases
// with. flag is --repo, a forge ref or a bare "owner/repo" on GitHub. Without it
// the artifact URLs in text name the repo.
func bumpSource(file, text, flag string) (string, string, error) {
	if name, ok := strings.CutPrefix(flag, "npm:"); ok {
		return manifest.FromNPM, name, nil
	}

	if flag != "" {
		if !strings.Contains(flag, ":") {
			flag = "github:" + flag
		}

		r, err := ref.Parse(flag)
		if err != nil || r.Kind != ref.Forge || r.Fragment != "" || r.Version != "" {
			return "", "", fmt.Errorf(
				"--repo %q: want owner/repo or a ref such as gitlab:group/project", flag,
			)
		}

		switch r.Scheme {
		case "codeberg":
			return manifest.FromGiteaReleases, "codeberg.org/" + r.Location, nil
		default:
			return r.Scheme + "-releases", r.Location, nil
		}
	}

	for _, host := range []struct {
		from string
		re   *regexp.Regexp
	}{
		{manifest.FromGitHubReleases, githubURLRe},
		{manifest.FromGiteaReleases, codebergURLRe},
		{manifest.FromGitLabReleases, gitlabURLRe},
		{manifest.FromNPM, npmURLRe},
	} {
		if found := host.re.FindStringSubmatch(text); found != nil {
			return host.from, found[1], nil
		}
	}

	return "", "", fmt.Errorf(
		"%s has no release URL on github.com, codeberg.org, gitlab.com or registry.npmjs.org "+
			"to read the repo from, pass --repo with a ref such as gitea:host/owner/repo",
		file,
	)
}

func newHashCmd() *cobra.Command {
	return &cobra.Command{
		Use:   "hash <url | file>",
		Short: "Print the checksums of a download, ready to paste into a manifest",
		Long: `Print the checksums of a download, ready to paste into a manifest.

An artifact needs one of the two lines. Most projects publish a sha256, and npm
publishes an integrity.`,
		Args: exactArgs(1),
		RunE: func(cmd *cobra.Command, args []string) error {
			at := args[0]

			// A local file is a download that is already here.
			if !strings.Contains(at, "://") {
				abs, err := filepath.Abs(at)
				if err != nil {
					return err
				}

				if info, err := os.Stat(abs); err != nil || info.IsDir() {
					return fmt.Errorf("there is no file at %s", abs)
				}

				at = "file://" + filepath.ToSlash(abs)
			}

			e, err := loadEnv()
			if err != nil {
				return err
			}

			sum, integrity, err := e.store().Hashes(cmd.Context(), at)
			if err != nil {
				return err
			}

			fmt.Fprintf(cmd.OutOrStdout(), "sha256 = %q\nintegrity = %q\n", sum, integrity)

			return nil
		},
	}
}

// readManifestFile reads the manifest at path, and names a missing one plainly.
func readManifestFile(path string) ([]byte, error) {
	data, err := os.ReadFile(path)
	if errors.Is(err, fs.ErrNotExist) {
		return nil, fmt.Errorf("there is no manifest at %s", path)
	}

	return data, err
}

func newBumpCmd(opts Options) *cobra.Command {
	var repo, prefix, to string

	cmd := &cobra.Command{
		Use:   "bump [file]",
		Short: "Move a fixed-version manifest to the newest upstream release",
		Long: `Move a fixed-version manifest to the newest upstream release.

Bump rewrites version.value and every inline sha256, and downloads each artifact
to compute its new digest. It reads the repo from artifact URLs on github.com,
codeberg.org or gitlab.com, or from --repo, which any other host needs. A manifest that discovers its versions needs no bump.`,
		Args: maxArgs(1),
		RunE: func(cmd *cobra.Command, args []string) error {
			file := ref.Manifest.Default
			if len(args) == 1 {
				file = args[0]
			}

			return runBump(cmd, opts, file, repo, prefix, to)
		},
	}

	cmd.Flags().
		StringVar(&repo, "repo", "", "the repo to read releases from, as owner/repo on GitHub or a ref such as gitlab:group/project")
	cmd.Flags().
		StringVar(&prefix, "strip-prefix", "", `text before the version in a tag, such as "v"`)
	cmd.Flags().StringVar(&to, "to", "", "the version to move to, instead of the newest")

	return cmd
}

func runBump(cmd *cobra.Command, opts Options, file, repo, prefix, to string) error {
	data, err := readManifestFile(file)
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

	if m.PerArtifact() {
		return fmt.Errorf("each artifact of %s finds its own version, so there is nothing to bump", file)
	}

	text := string(data)
	old := m.Version.Value

	from, repo, err := bumpSource(file, text, repo)
	if err != nil {
		return err
	}

	// A URL such as ".../download/v1.2.0/..." shows that tags start with "v".
	if !cmd.Flags().Changed("strip-prefix") &&
		(strings.Contains(text, "/releases/download/v") || strings.Contains(text, "/-/releases/v")) {
		prefix = "v"
	}

	e, err := loadEnv()
	if err != nil {
		return err
	}

	release, err := e.resolver(opts).Pick(cmd.Context(), manifest.Version{
		From: from, Repo: repo, StripPrefix: prefix,
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
		if artifact.SHA256 == "" && artifact.Integrity == "" {
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

		// An inline integrity moves to the one the npm registry publishes.
		if artifact.Integrity != "" {
			published := release.Integrity[expanded.URL]
			if published == "" {
				return fmt.Errorf(
					"artifact[%d]: the npm registry publishes no integrity for %s", i, expanded.URL,
				)
			}

			text = strings.ReplaceAll(text, artifact.Integrity, published)
			updated++
		}

		if artifact.SHA256 == "" {
			continue
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

	finished(
		out,
		"%s %s %s %s, %d checksums updated in %s",
		m.Package.Name,
		old,
		ui.For(out).Arrow(),
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
		Args: maxArgs(1),
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

	scratch, err := tempdir.Dir("test")
	if err != nil {
		return err
	}

	// The store paths a test built are frozen, which os.RemoveAll cannot undo.
	if !keep {
		defer func() { _ = store.RemoveFrozen(scratch) }()
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
		fromSource: m.BuildsOn(platform.Host()),
		approve:    e.approver(cmd, opts, flags),
		log:        buildLog(cmd, flags),
		progress: func(step, total int, kind string, err error) {
			s := ui.For(out)
			result := s.Pick(s.Check(), "ok")

			if err != nil {
				result = s.Pick(s.Cross()+" "+s.Bad("failed"), "FAILED")
			}

			fmt.Fprintf(out, "%s %-8s %s\n", s.Dim(fmt.Sprintf("[%d/%d]", step+1, total)), kind, result)
		},
	})
	if err != nil {
		return err
	}

	reportUnsandboxed(cmd.ErrOrStderr(), got)
	reportNotes(cmd.ErrOrStderr(), got)
	reportCache(cmd.ErrOrStderr(), got)

	strategy := got.lock.Platforms[platform.Host().String()].Strategy
	finished(out, "%s %s works on %s (%s)", got.lock.Name, got.lock.Version, platform.Host(), strategy)

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
		finished(out, "kept %s", got.profile.StorePath)
	}

	return nil
}
