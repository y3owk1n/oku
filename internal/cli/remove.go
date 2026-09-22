package cli

import (
	"fmt"
	"strings"

	"github.com/spf13/cobra"

	"github.com/y3owk1n/oku/internal/list"
	"github.com/y3owk1n/oku/internal/lock"
	"github.com/y3owk1n/oku/internal/ui"
)

func newRemoveCmd(opts Options) *cobra.Command {
	return &cobra.Command{
		Use:   "remove <name>...",
		Short: "Remove packages from the profile",
		Args:  cobra.MinimumNArgs(1),
		RunE: func(cmd *cobra.Command, args []string) error {
			e, err := scopedEnv(cmd, opts)
			if err != nil {
				return err
			}

			if err := e.recoverPending(cmd, opts); err != nil {
				return err
			}

			listed, err := list.Read(e.listPath())
			if err != nil {
				return err
			}

			locked, err := lock.Read(e.lockPath())
			if err != nil {
				return err
			}

			// One generation drops every name, so a name that cannot go stops the
			// command before anything changes.
			var installed, unknown []string

			for _, name := range args {
				_, inList := listed.Packages[name]
				_, inLock := locked.Find(name)

				if !inList && len(listed.Include) > 0 && inLock {
					return fmt.Errorf(
						"%s is not in %s, so it comes from an include\n"+
							"oku does not edit included lists, so remove it there",
						name, e.listPath(),
					)
				}

				switch {
				case e.profile().Has(name):
					installed = append(installed, name)
				case inList || inLock:
					// A package can be listed without being in the profile, for
					// example after the data directory was deleted.
				default:
					unknown = append(unknown, name)
				}

				locked.Delete(name)
			}

			if len(unknown) > 0 {
				return fmt.Errorf("%s: not installed", strings.Join(unknown, ", "))
			}

			lockData, err := locked.Bytes(e.lockPath())
			if err != nil {
				return err
			}

			// Nothing to take out of the profile, so the active generation stays.
			c := change{to: e.profile().Current()}

			if len(installed) > 0 {
				staged, err := e.profile().Remove(installed, lockData)
				if err != nil {
					return err
				}

				c = change{to: staged, staged: true}
			}

			c.commit = func() error {
				for _, name := range args {
					if err := list.Delete(e.listPath(), name); err != nil {
						return err
					}
				}

				return locked.Write(e.lockPath())
			}

			if err := e.apply(cmd, opts, c); err != nil {
				return err
			}

			s := ui.For(cmd.OutOrStdout())
			fmt.Fprintln(cmd.OutOrStdout(), s.Done("removed "+s.Bold(strings.Join(args, " "))))

			return nil
		},
	}
}
