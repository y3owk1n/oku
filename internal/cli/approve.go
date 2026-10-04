package cli

import (
	"cmp"
	"errors"
	"fmt"
	"io"
	"maps"
	"os"
	"slices"
	"strings"
	"sync"
	"time"

	"github.com/spf13/cobra"
	"golang.org/x/term"

	"github.com/y3owk1n/oku/internal/list"
	"github.com/y3owk1n/oku/internal/manifest"
	"github.com/y3owk1n/oku/internal/platform"
	"github.com/y3owk1n/oku/internal/sandbox"
	"github.com/y3owk1n/oku/internal/source"
	"github.com/y3owk1n/oku/internal/status"
	"github.com/y3owk1n/oku/internal/trust"
	"github.com/y3owk1n/oku/internal/ui"
)

// buildFlags are the flags of every command that may build from source.
type buildFlags struct {
	yes     bool
	verbose bool
	// acceptKey accepts a signing key that differs from the one in oku.lock.
	acceptKey bool
	// acceptWeaker accepts a download that oku checks more weakly than the one
	// in oku.lock.
	acceptWeaker bool
	// minReleaseAge replaces the list's minimum release age for this command.
	minReleaseAge string
	// acceptUnknownAge takes a version whose source gives no release time,
	// whatever [lock] unknown_release_age says.
	acceptUnknownAge bool
	// acceptUnverified trusts a first download, whatever [lock] unverified says.
	acceptUnverified bool
}

func (f *buildFlags) register(cmd *cobra.Command) {
	cmd.Flags().
		BoolVarP(&f.yes, "yes", "y", false, "approve a manifest's commands and [env] without asking")
	cmd.Flags().
		BoolVarP(&f.verbose, "verbose", "v", false, "show the output of build commands, and a manifest that oku inferred")
	cmd.Flags().
		BoolVar(&f.acceptKey, "accept-key", false, "accept a signing key that differs from the one in oku.lock")
	cmd.Flags().BoolVar(&f.acceptWeaker, "accept-weaker-check", false,
		"accept a download that oku checks more weakly than the one in oku.lock")
	cmd.Flags().StringVar(&f.minReleaseAge, minReleaseAgeFlag, "",
		"take only versions released at least this long ago, such as 3d, or 0 for the newest")
	cmd.Flags().BoolVar(&f.acceptUnknownAge, "accept-unknown-age", false,
		"take a version whose source gives no release time without asking")
	cmd.Flags().BoolVar(&f.acceptUnverified, "accept-unverified", false,
		"trust a download that nothing states a digest for without asking")
}

// askAge makes one question about a version's age wait for another, since
// sync installs several packages at once.
var askAge sync.Mutex

// ageChecker returns the check that install runs before it takes a version
// whose source gives no release time, by [lock] unknown_release_age: allow
// takes it and says so, refuse fails, and warn asks on a terminal and fails
// without one. --accept-unknown-age takes it.
func (e env) ageChecker(
	cmd *cobra.Command,
	opts Options,
	flags *buildFlags,
) func(name, version, locked string) (bool, error) {
	return func(name, version, locked string) (bool, error) {
		if flags.acceptUnknownAge {
			return true, nil
		}

		own, err := list.Read(e.listPath())
		if err != nil {
			return false, err
		}

		why := fmt.Sprintf("%s %s: its source gives no release time", name, version)

		switch cmp.Or(own.UnknownReleaseAge, opts.UnknownReleaseAge, list.UnknownWarn) {
		case list.UnknownAllow:
			return true, nil
		case list.UnknownRefuse:
			return false, fmt.Errorf(
				"%s, and [lock] unknown_release_age refuses such a version: %w\n"+
					"run the command with --accept-unknown-age, or set min_release_age = \"0\" on %s",
				why, errNotTaken, name,
			)
		}

		if !interactive(cmd, opts) {
			return false, fmt.Errorf(
				"%s, so oku asks before it takes it, and this is not a terminal: %w\n"+
					"run the command with --accept-unknown-age, "+
					"or set [lock] unknown_release_age = \"allow\"",
				why, errNotTaken,
			)
		}

		askAge.Lock()
		defer askAge.Unlock()

		// Other packages keep installing, and their waits would redraw over the
		// question.
		defer status.Pause(cmd.Context())()

		out := cmd.OutOrStdout()

		question := "take it?"
		if locked != "" {
			question = fmt.Sprintf("take it? (no keeps %s)", locked)
		}

		fmt.Fprintf(out, "%s, so oku cannot check how old it is.\n", why)

		if !confirm(cmd.InOrStdin(), out, question) {
			return false, fmt.Errorf("%s %s: %w", name, version, errNotTaken)
		}

		return false, nil
	}
}

// trustChecker returns the check that install runs before it trusts the first
// download of a package for a platform, by [lock] unverified: allow trusts it,
// refuse fails, and warn asks on a terminal and fails without one. One answer
// covers every platform of the same version. --accept-unverified trusts it.
func (e env) trustChecker(
	cmd *cobra.Command,
	opts Options,
	flags *buildFlags,
) func(name, version string, p platform.Platform) error {
	trusted := map[string]bool{}

	return func(name, version string, p platform.Platform) error {
		if flags.acceptUnverified {
			return nil
		}

		own, err := list.Read(e.listPath())
		if err != nil {
			return err
		}

		why := fmt.Sprintf("nothing states a digest for the download of %s %s for %s", name, version, p)

		switch cmp.Or(own.Unverified, list.UnknownAllow) {
		case list.UnknownAllow:
			return nil
		case list.UnknownRefuse:
			return firstUseRefused(version, "[lock] unverified refuses it", fmt.Sprintf(
				"%s, and [lock] unverified refuses to trust a first download\n"+
					"run the command with --accept-unverified, or ask the developer to publish a checksum",
				why,
			))
		}

		askTrust.Lock()
		defer askTrust.Unlock()

		if trusted[name+" "+version] {
			return nil
		}

		if !interactive(cmd, opts) {
			return firstUseRefused(version, "oku cannot ask without a terminal", fmt.Sprintf(
				"%s, so oku asks before it trusts it, and this is not a terminal\n"+
					"run the command with --accept-unverified, or set [lock] unverified = \"allow\"",
				why,
			))
		}

		// Other packages keep installing, and their waits would redraw over the
		// question.
		defer status.Pause(cmd.Context())()

		out := cmd.OutOrStdout()
		fmt.Fprintf(out, "%s, so oku would trust its first download.\n", why)

		if !confirm(cmd.InOrStdin(), out, "trust it?") {
			return firstUseRefused(version, "you did not trust it", fmt.Sprintf("%s %s: not trusted", name, version))
		}

		trusted[name+" "+version] = true

		return nil
	}
}

// askTrust makes one question about a first download wait for another.
var askTrust sync.Mutex

// firstUseRefused is the notApprovedError of a first download that oku did not
// trust, which keeps a locked version as a declined build does.
func firstUseRefused(version, why, text string) notApprovedError {
	return notApprovedError{
		version: version,
		text:    text,
		why:     why,
		kept:    fmt.Sprintf("oku did not trust the first download of %s, since %s", version, why),
	}
}

const minReleaseAgeFlag = "min-release-age"

// releaseAge is the minimum release age for the package that entry of own
// names: --min-release-age, else the entry's, else the list's, else
// list.DefaultReleaseAge.
func releaseAge(cmd *cobra.Command, own *list.List, entry list.Entry) (time.Duration, error) {
	text := entry.MinReleaseAge
	if own != nil && text == "" {
		text = own.MinReleaseAge
	}

	if flag := cmd.Flags().Lookup(minReleaseAgeFlag); flag != nil && flag.Changed {
		text = flag.Value.String()
	}

	if text == "" {
		return list.DefaultReleaseAge, nil
	}

	age, err := list.ParseAge(text)
	if err != nil {
		return 0, fmt.Errorf("min_release_age: %w", err)
	}

	return age, nil
}

// approver returns the check that install runs before a build, before an
// artifact whose completions a command generates, and before a package that
// sets [env]. A manifest that runs commands, or sets variables in the user's
// shell, needs the user's approval once per manifest hash.
func (e env) approver(
	cmd *cobra.Command,
	opts Options,
	flags *buildFlags,
) func(*manifest.Manifest, platform.Platform, *manifest.Artifact) error {
	return func(m *manifest.Manifest, host platform.Platform, a *manifest.Artifact) error {
		var steps map[int]manifest.Step
		if a == nil {
			steps = m.Build.CommandSteps(host)
		}

		generates := a != nil && a.Completions.Generate != ""

		runs := len(steps) > 0 || generates
		if !runs && len(m.Env) == 0 {
			return nil
		}

		unsandboxed := ""
		if ok, why := sandbox.Available(); runs && !ok {
			config, err := source.Read(e.configPath())
			if err != nil {
				return err
			}

			if config.RequireSandbox {
				return fmt.Errorf(
					"%s %s runs commands, and this host cannot sandbox them, because %s\n"+
						"require_sandbox in config.toml refuses that, delete the line to run them anyway",
					m.Package.Name, m.Version.Value, why,
				)
			}

			unsandboxed = why
		}

		approvals, err := trust.Read(e.data)
		if err != nil {
			return err
		}

		if approvals.Has(m.SHA256) {
			return nil
		}

		if !flags.yes {
			// Other packages keep installing, and their waits would redraw over the
			// question.
			defer status.Pause(cmd.Context())()

			// The block goes to a buffer first, so that oku knows how many lines to
			// erase once the user answered.
			terminal := cmd.OutOrStdout()
			s := ui.For(terminal)

			var block strings.Builder

			out := io.Writer(&block)

			switch {
			case generates:
				fmt.Fprintf(
					out,
					"%s %s runs its download on your machine to generate completions:\n\n    %s\n"+
						"  %s\n",
					s.Bold(m.Package.Name), m.Version.Value, ui.Clean(a.Completions.Generate),
					s.Warn("(once for each of "+strings.Join(manifest.Shells, ", ")+")"),
				)
			case len(steps) > 0:
				fmt.Fprintf(
					out,
					"%s %s builds from source and runs these commands on your machine:\n\n",
					s.Bold(m.Package.Name),
					m.Version.Value,
				)
			}

			for _, i := range slices.Sorted(maps.Keys(steps)) {
				text, note := "", ""

				switch {
				case steps[i].Generates():
					text = steps[i].Install.Completions.Generate
					note = "  (generates completions, once for each of " +
						strings.Join(manifest.Shells, ", ") + ")"
				case steps[i].Vendor != nil:
					text = "vendor " + *steps[i].Vendor
					note = "  (downloads packages, checked against oku.lock)"

					switch {
					case steps[i].Package == "":
					case len(steps[i].Scripts) > 0:
						text += ", which installs " + steps[i].Package +
							" and its dependencies and runs the install scripts of " +
							strings.Join(steps[i].Scripts, ", ")
						note = "  (runs install scripts with network, not checked against oku.lock)"
					default:
						text += ", which installs " + steps[i].Package +
							" and its dependencies and runs none of their scripts"
					}
				case steps[i].Network:
					text, note = *steps[i].Run, "  (wants network)"
				default:
					text = *steps[i].Run
				}

				// A control character in a command could hide part of it, so the user
				// sees it as \x1b and the like.
				text = ui.Clean(text)

				// Steps count from 1, as the wait line of a build does. A command
				// line that wraps goes on further in, so it reads apart from the next.
				fmt.Fprintf(
					out,
					"  %s%s\n%s\n",
					s.Accent(fmt.Sprintf("step %d", i+1)),
					s.Warn(note),
					s.Wrap("    "+strings.ReplaceAll(text, "\n", "\n    "), 6),
				)
			}

			// A variable such as GIT_CONFIG_* or PAGER can run a program in every
			// shell, so the user sees each one before it applies.
			if len(m.Env) > 0 {
				if runs {
					fmt.Fprintln(out)
				}

				fmt.Fprintf(
					out, "%s %s sets these variables in your shell while it is installed:\n\n",
					s.Bold(m.Package.Name), m.Version.Value,
				)

				for _, name := range slices.Sorted(maps.Keys(m.Env)) {
					fmt.Fprintf(out, "%s\n", s.Wrap("    "+name+" = "+ui.Clean(m.Env[name]), 6))
				}
			}

			if unsandboxed != "" {
				fmt.Fprintf(
					out, "\n%s\n",
					s.Warn("this host cannot sandbox commands, because "+unsandboxed+
						", so they can use the network and read your files"),
				)
			}

			question, action := "run them?", "run them"

			switch {
			case generates:
				question, action = "run it?", "run it"
			case !runs:
				question, action = "set them?", "set its variables"
			}

			if runs && len(m.Env) > 0 {
				question = strings.TrimSuffix(question, "?") + " and set the variables?"
				action += " and set its variables"
			}

			if !interactive(cmd, opts) {
				fmt.Fprint(terminal, block.String())

				return notApprovedError{
					version: m.Version.Value,
					text: fmt.Sprintf(
						"%s %s needs approval to %s, and this is not a terminal\npass --yes to approve",
						m.Package.Name, m.Version.Value, action,
					),
					why: "oku cannot ask without a terminal, pass --yes to approve it",
				}
			}

			fmt.Fprintln(out)
			fmt.Fprint(terminal, block.String())

			approved := confirm(cmd.InOrStdin(), terminal, question)

			// Once the user answers, oku erases the block and the question, and one
			// line records what the user decided.
			s.Erase(terminal, s.Lines(block.String())+1)

			if !approved {
				fmt.Fprintf(
					terminal, "%s rejected %s %s\n",
					s.Bad(s.Pick("✗", "x")), m.Package.Name, m.Version.Value,
				)

				return notApprovedError{
					version: m.Version.Value,
					text:    "not approved, nothing was built",
					why:     "you did not approve it",
				}
			}

			fmt.Fprintf(
				terminal, "%s approved %s %s\n",
				s.Good(s.Pick("✓", "ok")), m.Package.Name, m.Version.Value,
			)
		}

		return approvals.Add(m.Package.Name, m.SHA256)
	}
}

// errNotApproved reports a build or a completions command that the user did not
// approve, or that oku could not ask about without a terminal.
var errNotApproved = errors.New("not approved")

// notApprovedError is errNotApproved for one version, and why. kept says why
// the package keeps its locked version, when that is not a declined build.
type notApprovedError struct {
	version, text, why, kept string
}

func (n notApprovedError) Error() string { return n.text }

func (n notApprovedError) Is(target error) bool { return target == errNotApproved }

// interactive reports whether oku can ask the user a question.
func interactive(cmd *cobra.Command, opts Options) bool {
	if opts.Interactive != nil {
		return *opts.Interactive
	}

	stdin, ok := cmd.InOrStdin().(*os.File)

	return ok && term.IsTerminal(int(stdin.Fd()))
}
