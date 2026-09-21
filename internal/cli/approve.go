package cli

import (
	"bufio"
	"errors"
	"fmt"
	"maps"
	"os"
	"slices"
	"strings"

	"github.com/spf13/cobra"
	"golang.org/x/term"

	"github.com/y3owk1n/oku/internal/manifest"
	"github.com/y3owk1n/oku/internal/platform"
	"github.com/y3owk1n/oku/internal/status"
	"github.com/y3owk1n/oku/internal/trust"
)

// buildFlags are the flags of every command that may build from source.
type buildFlags struct {
	yes     bool
	verbose bool
	// acceptKey accepts a signing key that differs from the one in oku.lock.
	acceptKey bool
}

func (f *buildFlags) register(cmd *cobra.Command) {
	cmd.Flags().
		BoolVarP(&f.yes, "yes", "y", false, "run a manifest's build commands without asking")
	cmd.Flags().
		BoolVarP(&f.verbose, "verbose", "v", false, "show the output of build commands, and a manifest that oku inferred")
	cmd.Flags().
		BoolVar(&f.acceptKey, "accept-key", false, "accept a signing key that differs from the one in oku.lock")
}

// approver returns the check that install runs before a build. A manifest with
// run steps needs the user's approval once per manifest hash.
func (e env) approver(
	cmd *cobra.Command,
	opts Options,
	flags *buildFlags,
) func(*manifest.Manifest, platform.Platform) error {
	return func(m *manifest.Manifest, host platform.Platform) error {
		steps := m.Build.CommandSteps(host)
		if len(steps) == 0 {
			return nil
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

			out := cmd.OutOrStdout()
			fmt.Fprintf(
				out,
				"%s %s builds from source and runs these commands on your machine:\n\n",
				m.Package.Name,
				m.Version.Value,
			)

			for _, i := range slices.Sorted(maps.Keys(steps)) {
				text, note := "", ""

				switch {
				case steps[i].Vendor != nil:
					text = "vendor " + *steps[i].Vendor
					if steps[i].Package != "" {
						text += ", which installs " + steps[i].Package + " and its dependencies and runs none of their scripts"
					}
					note = "  (downloads packages, checked against oku.lock)"
				case steps[i].Network:
					text, note = *steps[i].Run, "  (wants network)"
				default:
					text = *steps[i].Run
				}

				fmt.Fprintf(
					out,
					"  step %d%s\n    %s\n",
					i,
					note,
					strings.ReplaceAll(text, "\n", "\n    "),
				)
			}

			if !interactive(cmd, opts) {
				return fmt.Errorf(
					"%s needs approval to run them, and this is not a terminal\npass --yes to approve",
					m.Package.Name,
				)
			}

			fmt.Fprint(out, "\nrun them? [y/N] ")

			answer, _ := bufio.NewReader(cmd.InOrStdin()).ReadString('\n')
			if a := strings.ToLower(strings.TrimSpace(answer)); a != "y" && a != "yes" {
				return errors.New("not approved, nothing was built")
			}
		}

		return approvals.Add(m.Package.Name, m.SHA256)
	}
}

// interactive reports whether oku can ask the user a question.
func interactive(cmd *cobra.Command, opts Options) bool {
	if opts.Interactive != nil {
		return *opts.Interactive
	}

	stdin, ok := cmd.InOrStdin().(*os.File)

	return ok && term.IsTerminal(int(stdin.Fd()))
}
