package cli

import (
	"errors"
	"fmt"

	"github.com/spf13/cobra"

	"github.com/y3owk1n/oku/internal/list"
	"github.com/y3owk1n/oku/internal/lock"
	"github.com/y3owk1n/oku/internal/profile"
	"github.com/y3owk1n/oku/internal/ui"
)

func newRemoveCmd(opts Options) *cobra.Command {
	return &cobra.Command{
		Use:   "remove <name>",
		Short: "Remove a package from the profile",
		Args:  exactArgs(1),
		RunE: func(cmd *cobra.Command, args []string) error {
			e, err := scopedEnv(cmd, opts)
			if err != nil {
				return err
			}

			if err := e.recoverPending(cmd, opts); err != nil {
				return err
			}

			name := args[0]

			listed, err := list.Read(e.listPath())
			if err != nil {
				return err
			}

			locked, err := lock.Read(e.lockPath())
			if err != nil {
				return err
			}

			_, inList := listed.Packages[name]
			_, inLock := locked.Find(name)

			if !inList && len(listed.Include) > 0 && inLock {
				return fmt.Errorf(
					"%s is not in %s, so it comes from an include\n"+
						"oku does not edit included lists, so remove it there",
					name, e.listPath(),
				)
			}

			locked.Delete(name)

			lockData, err := locked.Bytes(e.lockPath())
			if err != nil {
				return err
			}

			// A package can be listed without being in the profile, for example
			// after the data directory was deleted. Remove still has to clear it.
			staged, err := e.profile().Remove(name, lockData)
			if err != nil && (!errors.Is(err, profile.ErrNotInstalled) || (!inList && !inLock)) {
				return err
			}

			c := change{to: staged, staged: true}

			// Nothing to take out of the profile, so the active generation stays.
			if errors.Is(err, profile.ErrNotInstalled) {
				c = change{to: e.profile().Current()}
			}

			c.commit = func() error {
				if err := list.Delete(e.listPath(), name); err != nil {
					return err
				}

				return locked.Write(e.lockPath())
			}

			if err := e.apply(cmd, opts, c); err != nil {
				return err
			}

			s := ui.For(cmd.OutOrStdout())
			fmt.Fprintf(cmd.OutOrStdout(), "removed %s\n", s.Bold(args[0]))

			return nil
		},
	}
}
