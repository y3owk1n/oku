// Package cli holds the cobra commands.
package cli

import (
	"path/filepath"

	"github.com/spf13/cobra"

	"github.com/y3owk1n/oku/internal/dirs"
	"github.com/y3owk1n/oku/internal/infer"
	"github.com/y3owk1n/oku/internal/list"
	"github.com/y3owk1n/oku/internal/lock"
	"github.com/y3owk1n/oku/internal/profile"
	"github.com/y3owk1n/oku/internal/ref"
	"github.com/y3owk1n/oku/internal/resolve"
	"github.com/y3owk1n/oku/internal/sandbox"
	"github.com/y3owk1n/oku/internal/store"
)

// Options are the values main and the tests pass to the command tree.
type Options struct {
	Version string
	// Executable is the path of the running binary, which "self uninstall"
	// deletes.
	Executable string
	// Interactive overrides the check for a terminal on stdin. Tests set it.
	Interactive *bool
	// GitHubAPI and GitHubRaw replace the github.com URLs when set.
	GitHubAPI string
	GitHubRaw string
}

// NewRootCmd builds the oku command tree.
func NewRootCmd(opts Options) *cobra.Command {
	root := &cobra.Command{
		Use:           "oku",
		Short:         "A cross-platform package manager with no central registry",
		Version:       opts.Version,
		SilenceUsage:  true,
		SilenceErrors: true,
	}

	root.AddCommand(
		newAddCmd(opts),
		newRemoveCmd(),
		newSyncCmd(opts),
		newUpdateCmd(opts),
		newGenerationsCmd(),
		newRollbackCmd(),
		newGCCmd(),
		newManifestCmd(opts),
		newSourceCmd(),
		newSearchCmd(opts),
		newListCmd(),
		newWhyCmd(),
		newSelfCmd(opts.Executable),
	)

	// The Linux sandbox re-runs oku inside new namespaces to finish the setup.
	root.AddCommand(&cobra.Command{
		Use:    sandbox.InitCommand,
		Hidden: true,
		Args:   cobra.NoArgs,
		RunE:   func(*cobra.Command, []string) error { return sandbox.Init() },
	})

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

func (e env) listPath() string {
	return filepath.Join(e.config, list.FileName)
}

func (e env) lockPath() string {
	return filepath.Join(e.config, lock.FileName)
}

func (e env) store() *store.Store {
	return store.New(e.data, e.cache)
}

func (e env) fetcher(opts Options) *ref.Fetcher {
	f := ref.NewFetcher(e.cache)

	if opts.GitHubAPI != "" {
		f.GitHubAPI = opts.GitHubAPI
	}

	if opts.GitHubRaw != "" {
		f.GitHubRaw = opts.GitHubRaw
	}

	return f
}

func (e env) resolver(opts Options) *resolve.Resolver {
	f := e.fetcher(opts)

	return &resolve.Resolver{HTTP: f.HTTP, GitHubAPI: f.GitHubAPI, Token: f.Token}
}

func (e env) inferrer(opts Options) *infer.Inferrer {
	f := e.fetcher(opts)

	return &infer.Inferrer{
		HTTP:      f.HTTP,
		GitHubAPI: f.GitHubAPI,
		Token:     f.Token,
		Inspect:   e.store().Inspect,
	}
}

func (e env) globalProfile() *profile.Profile {
	return profile.Open(e.data, "global")
}
