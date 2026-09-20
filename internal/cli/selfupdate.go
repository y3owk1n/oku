package cli

import (
	"errors"
	"fmt"
	"io"
	"os"
	"path/filepath"
	"regexp"
	"runtime"
	"strings"

	"github.com/spf13/cobra"

	"github.com/y3owk1n/oku/internal/infer"
	"github.com/y3owk1n/oku/internal/store"
)

const (
	// releaseRepo is where oku's own releases are published.
	releaseRepo = "y3owk1n/oku"
	// releaseKey is the minisign public key that signs those releases. Its secret
	// half is the MINISIGN_SECRET_KEY secret of the repo.
	releaseKey = "RWSjFGqIxI8IPGwKE/uRgugZ51qCEMe1CDbFRVTMUAuin42JiOxg2HNW"
	// nightlyTag is the prerelease that holds the build of the newest commit on
	// main.
	nightlyTag = "nightly"
)

var commitRe = regexp.MustCompile(`^[0-9a-f]{40}$`)

func newSelfUpdateCmd(opts Options) *cobra.Command {
	var check, nightly bool

	cmd := &cobra.Command{
		Use:   "update",
		Short: "Replace oku with the newest release, after checking its signature",
		Long: `Replace oku with the newest release, after checking its signature.

oku downloads the binary for this OS and CPU from its GitHub releases, with the
minisign signature beside it. It replaces itself only when the release key that
is built into this binary made that signature.

--nightly takes the build of the newest commit on main instead. It is a
prerelease with the same signature. Run "oku self update" without the flag to go
back to the newest release.`,
		Args: cobra.NoArgs,
		RunE: func(cmd *cobra.Command, _ []string) error {
			return runSelfUpdate(cmd, opts, check, nightly)
		},
	}

	cmd.Flags().
		BoolVar(&check, "check", false, "say whether a newer release exists, and change nothing")
	cmd.Flags().
		BoolVar(&nightly, "nightly", false, "take the build of the newest commit on main")

	return cmd
}

// releaseAsset names the release file for this OS and CPU.
func releaseAsset() string {
	name := "oku-" + runtime.GOOS + "-" + runtime.GOARCH
	if runtime.GOOS == "windows" {
		name += ".exe"
	}

	return name
}

func runSelfUpdate(cmd *cobra.Command, opts Options, check, nightly bool) error {
	key, repo := releaseKey, releaseRepo
	if opts.ReleaseKey != "" {
		key = opts.ReleaseKey
	}

	if opts.ReleaseRepo != "" {
		repo = opts.ReleaseRepo
	}

	e, err := loadEnv()
	if err != nil {
		return err
	}

	out := cmd.OutOrStdout()

	var (
		release infer.Release
		newest  string
		current bool
	)

	if nightly {
		if release, err = e.inferrer(opts).Tagged(cmd.Context(), repo, nightlyTag); err != nil {
			return err
		}

		// The nightly workflow makes the release from a commit and ends the
		// version of its binaries with the same seven characters. A release made
		// from a branch has the branch name here, and oku cannot tell from it which
		// commit the files are from.
		if !commitRe.MatchString(release.Commit) {
			return fmt.Errorf(
				"release %s of %s was made from %q, which is no commit",
				nightlyTag, repo, release.Commit,
			)
		}

		newest = nightlyTag + " " + release.Commit[:7]
		current = strings.HasSuffix(opts.Version, "-"+release.Commit[:7])
	} else {
		if release, err = e.inferrer(opts).Latest(cmd.Context(), repo); err != nil {
			return err
		}

		newest = strings.TrimPrefix(release.Tag, "v")
		current = newest == strings.TrimPrefix(opts.Version, "v")
	}

	if current {
		kind := "release"
		if nightly {
			kind = "nightly build"
		}

		fmt.Fprintf(out, "oku %s is the newest %s\n", newest, kind)

		return nil
	}

	if check {
		fmt.Fprintf(out, "oku %s is available, this is %s\n", newest, opts.Version)

		return nil
	}

	urls := map[string]string{}
	names := make([]string, 0, len(release.Assets))

	for _, asset := range release.Assets {
		urls[asset.Name] = asset.URL
		names = append(names, asset.Name)
	}

	binary, signature := releaseAsset(), releaseAsset()+".minisig"
	if urls[binary] == "" || urls[signature] == "" {
		return fmt.Errorf(
			"release %s of %s has no %s with a %s beside it. It has: %s",
			release.Tag, repo, binary, signature, strings.Join(names, ", "),
		)
	}

	downloaded, err := e.store().Download(cmd.Context(), urls[binary])
	if err != nil {
		return err
	}

	signed, err := e.store().Download(cmd.Context(), urls[signature])
	if err != nil {
		return err
	}

	// Nothing is written near the running binary before this check has passed.
	if err := store.VerifyDetached(key, downloaded, signed); err != nil {
		if errors.Is(err, store.ErrSignature) {
			return fmt.Errorf(
				"release %s: %w\nif oku's release key was rotated, run the install script again, "+
					"see https://github.com/%s/blob/main/docs/releasing.md",
				release.Tag, err, repo,
			)
		}

		return fmt.Errorf("release %s: %w", release.Tag, err)
	}

	if err := swapBinary(downloaded, opts.Executable); err != nil {
		return fmt.Errorf("replace %s: %w", opts.Executable, err)
	}

	fmt.Fprintf(out, "updated oku from %s to %s\n", opts.Version, newest)

	return nil
}

// swapBinary puts a copy of source where executable is. The copy is made beside
// executable first, so the swap is a rename on the same volume.
func swapBinary(source, executable string) error {
	staged := executable + ".new"

	in, err := os.Open(source)
	if err != nil {
		return err
	}
	defer in.Close()

	out, err := os.OpenFile(staged, os.O_CREATE|os.O_TRUNC|os.O_WRONLY, 0o755)
	if err != nil {
		return err
	}

	if _, err := io.Copy(out, in); err != nil {
		out.Close()
		os.Remove(staged)

		return err
	}

	if err := out.Close(); err != nil {
		return err
	}

	// A running program cannot be replaced on Windows, so it is moved aside first.
	if runtime.GOOS == "windows" {
		if err := removeBinary(executable); err != nil {
			return err
		}
	}

	return os.Rename(staged, filepath.Clean(executable))
}
