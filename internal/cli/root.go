// Package cli holds the cobra commands.
package cli

import (
	"github.com/spf13/cobra"

	"github.com/y3owk1n/oku/internal/dirs"
	"github.com/y3owk1n/oku/internal/profile"
	"github.com/y3owk1n/oku/internal/store"
)

// NewRootCmd builds the oku command tree. executable is the path of the running
// binary, which "self uninstall" deletes.
func NewRootCmd(version, executable string) *cobra.Command {
	root := &cobra.Command{
		Use:           "oku",
		Short:         "A cross-platform package manager with no central registry",
		Version:       version,
		SilenceUsage:  true,
		SilenceErrors: true,
	}

	root.AddCommand(
		newAddCmd(),
		newRemoveCmd(),
		newListCmd(),
		newSelfCmd(executable),
	)

	return root
}

// env is where oku keeps its files on this machine.
type env struct {
	config string
	data   string
	cache  string
}

func loadEnv() (env, error) {
	var (
		e   env
		err error
	)

	if e.config, err = dirs.Config(); err != nil {
		return e, err
	}

	if e.data, err = dirs.Data(); err != nil {
		return e, err
	}

	e.cache, err = dirs.Cache()

	return e, err
}

func (e env) store() *store.Store {
	return store.New(e.data, e.cache)
}

func (e env) globalProfile() *profile.Profile {
	return profile.Open(e.data, "global")
}
