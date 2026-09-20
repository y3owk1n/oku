// Package cli holds the cobra commands.
package cli

import (
	"crypto/sha256"
	"encoding/hex"
	"fmt"
	"os"
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
	// WorkDir is where oku looks for a project list. Empty means the working
	// directory. Tests set it.
	WorkDir string
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

	root.PersistentFlags().BoolP(
		globalFlag, "g", false, "use the global list even inside a project",
	)

	root.AddCommand(
		newAddCmd(opts),
		newRemoveCmd(opts),
		newSyncCmd(opts),
		newUpdateCmd(opts),
		newGenerationsCmd(opts),
		newRollbackCmd(opts),
		newGCCmd(),
		newHookCmd(),
		newEnvCmd(opts),
		newAllowCmd(opts),
		newDenyCmd(opts),
		newManifestCmd(opts),
		newSourceCmd(),
		newSearchCmd(opts),
		newListCmd(opts),
		newWhyCmd(opts),
		newInfoCmd(opts),
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

const globalFlag = "global"

// env is where oku keeps its files on this machine, and which list it acts on.
type env struct {
	config string
	data   string
	cache  string
	// project is the directory of the project list in use, or empty for the
	// global list.
	project string
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

// scopedEnv is loadEnv for commands that act on a list. Inside a directory tree
// with an oku.toml they act on that project, unless --global is set.
func scopedEnv(cmd *cobra.Command, opts Options) (env, error) {
	e, err := loadEnv()
	if err != nil {
		return e, err
	}

	if global, _ := cmd.Flags().GetBool(globalFlag); global {
		return e, nil
	}

	dir := opts.WorkDir
	if dir == "" {
		if dir, err = os.Getwd(); err != nil {
			return e, err
		}
	}

	e.project = findProject(dir, e.config)
	if e.project != "" {
		fmt.Fprintf(cmd.ErrOrStderr(), "project %s\n", e.project)
	}

	return e, nil
}

// findProject returns the nearest directory at or above dir that holds an
// oku.toml. The config directory holds the global list and is not a project.
func findProject(dir, config string) string {
	for {
		if _, err := os.Stat(filepath.Join(dir, list.FileName)); err == nil && dir != config {
			return dir
		}

		parent := filepath.Dir(dir)
		if parent == dir {
			return ""
		}

		dir = parent
	}
}

func (e env) listDir() string {
	if e.project != "" {
		return e.project
	}

	return e.config
}

func (e env) listPath() string {
	return filepath.Join(e.listDir(), list.FileName)
}

func (e env) lockPath() string {
	return filepath.Join(e.listDir(), lock.FileName)
}

// profile returns the profile of the list in use. A project profile is named
// after a hash of its directory, so moving a project gives it a new profile.
func (e env) profile() *profile.Profile {
	if e.project == "" {
		return e.globalProfile()
	}

	sum := sha256.Sum256([]byte(e.project))

	return profile.Open(e.data, "project-"+hex.EncodeToString(sum[:])[:12])
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
