package cli

import (
	"crypto/sha256"
	"encoding/hex"
	"errors"
	"fmt"
	"io"
	"os"
	"path/filepath"
	"regexp"
	"runtime"
	"strings"
	"time"
	"unicode"

	"aead.dev/minisign"
	"github.com/spf13/cobra"

	"github.com/y3owk1n/oku/internal/forge"
	"github.com/y3owk1n/oku/internal/list"
	"github.com/y3owk1n/oku/internal/manifest"
	"github.com/y3owk1n/oku/internal/resolve"
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
	var (
		check, nightly, release bool
		to                      string
	)

	cmd := &cobra.Command{
		Use:     "update",
		Aliases: []string{"upgrade"},
		Short:   "Replace oku with the newest release, after checking its signature",
		Long: `Replace oku with the newest release, after checking its signature.

oku downloads the binary for this OS and CPU from its GitHub releases, with the
minisign signature beside it. It replaces itself only when the release key that
is built into this binary made that signature.

--nightly takes the build of the newest commit on main instead. It is a
prerelease with the same signature. A nightly build stays on nightly until you
pass --release, which goes back to the newest release.

--to <tag> takes that release, older or newer, after the same check.

A release that came out less than the minimum release age ago waits, as it
does for packages: [lock] min_release_age in the global oku.toml, 1d unless it
says otherwise, or --min-release-age for one run. --to and --nightly skip it.`,
		Args: noArgs,
		RunE: func(cmd *cobra.Command, _ []string) error {
			if nightly && release {
				return errors.New("--nightly and --release exclude each other")
			}

			if to != "" && (nightly || release) {
				return errors.New("--to names the release, so it excludes --nightly and --release")
			}

			return runSelfUpdate(cmd, opts, check, nightly, release, to)
		},
	}

	cmd.Flags().
		BoolVar(&check, "check", false, "say whether a newer release exists, and change nothing")
	cmd.Flags().
		BoolVar(&nightly, "nightly", false, "take the build of the newest commit on main")
	cmd.Flags().
		BoolVar(&release, "release", false, "go from a nightly build back to the newest release")
	cmd.Flags().StringVar(&to, "to", "", "take the release with this tag, such as v0.5.0")
	cmd.Flags().String(minReleaseAgeFlag, "",
		"take only a release made at least this long ago, such as 3d, or 0 for the newest")

	return cmd
}

// oldEnoughRelease returns latest, the newest release of repo, when it is older
// than the minimum release age. Otherwise it says that latest waits, and
// returns the newest release that is old enough and newer than this oku, or
// this oku's own release when there is none, and true.
func (e env) oldEnoughRelease(
	cmd *cobra.Command,
	opts Options,
	repo string,
	latest forge.Release,
) (forge.Release, bool, error) {
	own, err := list.Read(e.listPath())
	if err != nil {
		return forge.Release{}, false, err
	}

	age, err := releaseAge(cmd, own, list.Entry{})
	if err != nil {
		return forge.Release{}, false, err
	}

	now := time.Now
	if opts.Now != nil {
		now = opts.Now
	}

	if age == 0 || !latest.Published.After(now().Add(-age)) {
		return latest, false, nil
	}

	warn(
		cmd.OutOrStdout(),
		"oku %s came out less than %s ago, so it waits until %s. "+
			"`--min-release-age 0` takes it now",
		strings.TrimPrefix(latest.Tag, "v"), resolve.FormatAge(age),
		latest.Published.Add(age).Local().Format("2006-01-02 15:04"),
	)

	picked, _, err := e.resolverAged(opts, age).PickWaiting(cmd.Context(), manifest.Version{
		From: manifest.FromGitHubReleases, Repo: repo, StripPrefix: "v",
	}, "")

	running := strings.TrimPrefix(opts.Version, "v")
	if errors.Is(err, resolve.ErrTooNew) ||
		err == nil && isVersion(running) && resolve.Compare(picked.Version, running) <= 0 {
		return forge.Release{Tag: "v" + running}, true, nil
	}

	if err != nil {
		return forge.Release{}, true, err
	}

	found, err := e.inferrer(opts).Tagged(cmd.Context(), repo, picked.Tag)

	return found, true, err
}

// isVersion reports whether version is a release number and not a build such
// as "dev".
func isVersion(version string) bool {
	return version != "" && unicode.IsDigit(rune(version[0]))
}

// releaseAsset names the release file for this OS and CPU.
func releaseAsset() string {
	name := "oku-" + runtime.GOOS + "-" + runtime.GOARCH
	if runtime.GOOS == "windows" {
		name += ".exe"
	}

	return name
}

func runSelfUpdate(cmd *cobra.Command, opts Options, check, nightly, release bool, to string) error {
	// A bare run on a nightly build would go back to the release, which
	// is older than the build. Only an explicit flag does that.
	if strings.HasPrefix(opts.Version, nightlyTag) && !nightly && !release && to == "" {
		return fmt.Errorf(
			"oku %s is a nightly build. --nightly takes the newest nightly, "+
				"and --release goes back to the newest release",
			opts.Version,
		)
	}

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
		found   forge.Release
		newest  string
		current bool
		// held reports that the minimum release age held a newer release back.
		held bool
	)

	switch {
	case nightly:
		if found, err = e.inferrer(opts).Tagged(cmd.Context(), repo, nightlyTag); err != nil {
			return err
		}

		// The nightly workflow makes the release from a commit and ends the
		// version of its binaries with the same seven characters. A release made
		// from a branch has the branch name here, and oku cannot tell from it which
		// commit the files are from.
		if !commitRe.MatchString(found.Commit) {
			return fmt.Errorf(
				"release %s of %s was made from %q, which is no commit",
				nightlyTag, repo, found.Commit,
			)
		}

		newest = nightlyTag + " " + found.Commit[:7]
		current = strings.HasSuffix(opts.Version, "-"+found.Commit[:7])
	case to != "":
		if found, err = e.inferrer(opts).Tagged(cmd.Context(), repo, to); err != nil {
			return err
		}
	default:
		if found, err = e.inferrer(opts).Latest(cmd.Context(), repo); err != nil {
			return err
		}

		if found, held, err = e.oldEnoughRelease(cmd, opts, repo, found); err != nil {
			return err
		}
	}

	if !nightly {
		newest = strings.TrimPrefix(found.Tag, "v")
		current = newest == strings.TrimPrefix(opts.Version, "v")
	}

	if current {
		switch {
		case nightly:
			finished(out, "oku %s is the newest nightly build", newest)
		case to != "":
			finished(out, "oku %s is release %s already", newest, to)
		case held:
			finished(out, "oku %s is the newest release that is old enough", newest)
		default:
			finished(out, "oku %s is the newest release", newest)
		}

		return nil
	}

	if check {
		fmt.Fprintf(out, "oku %s is available, this is %s\n", newest, opts.Version)
		// The hint repeats the flags that chose this version.
		run := "oku self update"

		switch {
		case nightly:
			run += " --nightly"
		case release:
			run += " --release"
		case to != "":
			run += " --to " + to
		}

		hint(out, "run `"+run+"` to take it")

		return nil
	}

	urls := map[string]string{}
	names := make([]string, 0, len(found.Assets))

	for _, asset := range found.Assets {
		urls[asset.Name] = asset.URL
		names = append(names, asset.Name)
	}

	binary, signature := releaseAsset(), releaseAsset()+".minisig"
	if urls[binary] == "" || urls[signature] == "" {
		return fmt.Errorf(
			"release %s of %s has no %s with a %s beside it. It has: %s",
			found.Tag, repo, binary, signature, strings.Join(names, ", "),
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
	// The release workflow signs "oku <tag>", which stops an older release that
	// the same key signed from passing for this one.
	if err := store.VerifyDetached(key, downloaded, signed, "oku "+found.Tag); err != nil {
		if errors.Is(err, store.ErrSignature) {
			return fmt.Errorf(
				"release %s: %w\nif oku's release key was rotated, run the install script again, "+
					"see https://github.com/%s/blob/main/CONTRIBUTING.md#rotate-the-signing-key",
				found.Tag, err, repo,
			)
		}

		return fmt.Errorf("release %s: %w", found.Tag, err)
	}

	if nightly {
		if err := e.checkNightly(cmd, key, urls, found, opts.Version, binary, downloaded); err != nil {
			return fmt.Errorf("release %s: %w", found.Tag, err)
		}
	}

	if err := swapBinary(downloaded, opts.Executable); err != nil {
		return fmt.Errorf("replace %s: %w", opts.Executable, err)
	}

	finished(out, "updated oku from %s to %s", opts.Version, newest)
	fmt.Fprintf(out, "what changed: https://github.com/%s/releases/tag/%s\n", repo, found.Tag)

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

// checkNightly checks the signed checksums of a nightly. Every nightly binary
// is signed "oku nightly", so an older one would pass that check. The release
// workflow signs checksums.txt with the full version, such as
// "oku nightly-20260927084032-b796323". It must name the commit of the release,
// be newer than the nightly that runs, and list the sha256 of the binary.
func (e env) checkNightly(
	cmd *cobra.Command,
	key string,
	urls map[string]string,
	found forge.Release,
	running, binary, downloaded string,
) error {
	if urls[checksumsFile] == "" || urls[checksumsFile+".minisig"] == "" {
		return fmt.Errorf("the release has no signed %s", checksumsFile)
	}

	checksums, err := e.store().Download(cmd.Context(), urls[checksumsFile])
	if err != nil {
		return err
	}

	signaturePath, err := e.store().Download(cmd.Context(), urls[checksumsFile+".minisig"])
	if err != nil {
		return err
	}

	signature, err := os.ReadFile(signaturePath)
	if err != nil {
		return err
	}

	var parsed minisign.Signature
	if err := parsed.UnmarshalText(signature); err != nil {
		return err
	}

	// VerifyDetached checks the comment too, which the release key signs.
	version, ok := strings.CutPrefix(parsed.TrustedComment, "oku ")
	if err := store.VerifyDetached(key, checksums, signaturePath, parsed.TrustedComment); err != nil {
		return err
	}

	stamp, commit, _ := strings.Cut(strings.TrimPrefix(version, nightlyTag+"-"), "-")
	if !ok || !strings.HasPrefix(version, nightlyTag+"-") || commit != found.Commit[:7] {
		return fmt.Errorf("the checksums are signed for %q, not for a nightly of commit %s", version, found.Commit[:7])
	}

	if now, _, isNightly := strings.Cut(strings.TrimPrefix(running, nightlyTag+"-"), "-"); isNightly &&
		strings.HasPrefix(running, nightlyTag+"-") && stamp <= now {
		return fmt.Errorf("%s is not newer than %s, which runs", version, running)
	}

	data, err := os.ReadFile(checksums)
	if err != nil {
		return err
	}

	sum, err := fileSHA256(downloaded)
	if err != nil {
		return err
	}

	for line := range strings.Lines(string(data)) {
		if fields := strings.Fields(line); len(fields) == 2 && fields[1] == binary && fields[0] == sum {
			return nil
		}
	}

	return fmt.Errorf("the signed %s does not list %s with the sha256 of the download", checksumsFile, binary)
}

// checksumsFile is the file of a release that lists the sha256 of each binary.
const checksumsFile = "checksums.txt"

// fileSHA256 returns the hex sha256 of the file at path.
func fileSHA256(path string) (string, error) {
	f, err := os.Open(path)
	if err != nil {
		return "", err
	}
	defer f.Close()

	h := sha256.New()
	if _, err := io.Copy(h, f); err != nil {
		return "", err
	}

	return hex.EncodeToString(h.Sum(nil)), nil
}
