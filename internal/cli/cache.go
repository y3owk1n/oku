package cli

import (
	"context"
	"crypto/rand"
	"errors"
	"fmt"
	"io/fs"
	"os"
	"path/filepath"
	"slices"
	"strings"

	"aead.dev/minisign"
	"github.com/spf13/cobra"

	"github.com/y3owk1n/oku/internal/profile"
	"github.com/y3owk1n/oku/internal/source"
	"github.com/y3owk1n/oku/internal/status"
	"github.com/y3owk1n/oku/internal/store"
	"github.com/y3owk1n/oku/internal/ui"
)

// signingKeyFile holds the secret key that "oku cache push" signs with.
const signingKeyFile = "signing.key"

// substitute fills the store path prefix from the user's caches. It reports
// whether the path is now in the store, and the entries it ignored.
func (e env) substitute(ctx context.Context, prefix string) (bool, []string, error) {
	config, err := source.Read(e.configPath())
	if err != nil {
		return false, nil, err
	}

	if len(config.Caches) == 0 {
		return false, nil, nil
	}

	keys := make([]minisign.PublicKey, 0, len(config.TrustedKeys))

	for _, text := range config.TrustedKeys {
		var key minisign.PublicKey
		if err := key.UnmarshalText([]byte(text)); err != nil {
			return false, nil, fmt.Errorf("trusted key %s in %s: %w", text, e.configPath(), err)
		}

		keys = append(keys, key)
	}

	return e.store().Substitute(ctx, prefix, config.Caches, keys)
}

// editConfig reads config.toml, applies change and writes it back.
func editConfig(change func(*source.Config) error) error {
	e, err := loadEnv()
	if err != nil {
		return err
	}

	config, err := source.Read(e.configPath())
	if err != nil {
		return err
	}

	if err := change(config); err != nil {
		return err
	}

	return config.Write(e.configPath())
}

func newCacheCmd() *cobra.Command {
	cmd := &cobra.Command{
		Use:   "cache",
		Short: "Use and fill caches of built packages",
		Long: `Use and fill caches of built packages.

A cache is a directory, or the same directory served over http(s). Before oku
builds a package it looks in each cache for the package's store path. It uses
an entry only when a key from "oku key trust" signed it.`,
	}

	cmd.AddCommand(
		&cobra.Command{
			Use:   "add <directory-or-url>",
			Short: "Look in this cache before building",
			Args:  exactArgs(1),
			RunE: func(cmd *cobra.Command, args []string) error {
				location := args[0]
				if !strings.Contains(location, "://") {
					abs, err := filepath.Abs(location)
					if err != nil {
						return err
					}

					location = abs
				}

				err := editConfig(func(c *source.Config) error {
					if !slices.Contains(c.Caches, location) {
						c.Caches = append(c.Caches, location)
					}

					return nil
				})
				if err != nil {
					return err
				}

				fmt.Fprintf(cmd.OutOrStdout(), "added cache %s\n", location)

				return nil
			},
		},
		&cobra.Command{
			Use:   "remove <directory-or-url>",
			Short: "Stop looking in this cache",
			Args:  exactArgs(1),
			RunE: func(cmd *cobra.Command, args []string) error {
				return editConfig(func(c *source.Config) error {
					abs, _ := filepath.Abs(args[0])
					before := len(c.Caches)

					c.Caches = slices.DeleteFunc(c.Caches, func(have string) bool {
						return have == args[0] || have == abs
					})
					if len(c.Caches) == before {
						return fmt.Errorf(
							"%s is not one of your caches, see \"oku cache list\"",
							args[0],
						)
					}

					fmt.Fprintf(cmd.OutOrStdout(), "removed cache %s\n", args[0])

					return nil
				})
			},
		},
		&cobra.Command{
			Use:   "list",
			Short: "List your caches",
			Args:  cobra.NoArgs,
			RunE: func(cmd *cobra.Command, _ []string) error {
				e, err := loadEnv()
				if err != nil {
					return err
				}

				config, err := source.Read(e.configPath())
				if err != nil {
					return err
				}

				if wantJSON(cmd) {
					return printJSON(cmd, append([]string{}, config.Caches...))
				}

				if len(config.Caches) == 0 {
					fmt.Fprintln(cmd.OutOrStdout(), "no caches, add one with \"oku cache add\"")
				}

				for _, location := range config.Caches {
					fmt.Fprintln(cmd.OutOrStdout(), location)
				}

				return nil
			},
		},
		&cobra.Command{
			Use:   "push <directory> [name...]",
			Short: "Write signed entries for built packages and their deps into a directory",
			Long: `Write signed entries for built packages and their deps into a directory.

Without names it pushes every built package of the global profile. Serve the
directory over http(s), or share it, and it is a cache. oku signs with the key
from "oku key generate".`,
			Args: cobra.MinimumNArgs(1),
			RunE: func(cmd *cobra.Command, args []string) error {
				return runPush(cmd, args[0], args[1:])
			},
		},
	)

	return cmd
}

// readSigningKey reads the secret key, which oku stores without a password so
// that a push needs no prompt.
func readSigningKey(path string) (minisign.PrivateKey, error) {
	var key minisign.PrivateKey

	text, err := os.ReadFile(path)
	if err != nil {
		return key, err
	}

	return key, key.UnmarshalText(text)
}

func runPush(cmd *cobra.Command, dir string, names []string) error {
	e, err := loadEnv()
	if err != nil {
		return err
	}

	keyPath := filepath.Join(e.config, signingKeyFile)

	key, err := readSigningKey(keyPath)
	if errors.Is(err, fs.ErrNotExist) {
		return errors.New("there is no signing key yet, create one with \"oku key generate\"")
	}

	if err != nil {
		return fmt.Errorf("read %s: %w", keyPath, err)
	}

	pkgs, err := e.globalProfile().Packages()
	if err != nil {
		return err
	}

	for _, name := range names {
		if !slices.ContainsFunc(pkgs, func(p profile.Package) bool { return p.Name == name }) {
			return fmt.Errorf("%s is not installed in the global profile", name)
		}
	}

	var paths []string

	for _, pkg := range pkgs {
		if len(names) > 0 && !slices.Contains(names, pkg.Name) {
			continue
		}

		for _, path := range append([]string{pkg.StorePath}, pkg.Closure...) {
			if !slices.Contains(paths, path) {
				paths = append(paths, path)
			}
		}
	}

	pushed := 0

	for _, path := range paths {
		meta, err := store.ReadMeta(path)
		if err != nil {
			return err
		}

		// A package with a URL is a download, which every machine can fetch itself.
		if meta.URL != "" {
			continue
		}

		done := status.Start(cmd.Context(), "packing %s", filepath.Base(path))
		err = e.store().Pack(path, dir, key)

		done()

		if err != nil {
			return fmt.Errorf("%s: %w", filepath.Base(path), err)
		}

		pushed++

		fmt.Fprintf(cmd.OutOrStdout(), "pushed %s\n", filepath.Base(path))
	}

	if pushed == 0 {
		fmt.Fprintln(
			cmd.OutOrStdout(),
			"nothing to push, none of these packages was built from source",
		)
	}

	return nil
}

func newKeyCmd() *cobra.Command {
	cmd := &cobra.Command{
		Use:   "key",
		Short: "Manage the keys that sign and verify cache entries",
	}

	cmd.AddCommand(
		&cobra.Command{
			Use:   "generate",
			Short: "Create the key that \"oku cache push\" signs with",
			Args:  cobra.NoArgs,
			RunE: func(cmd *cobra.Command, _ []string) error {
				e, err := loadEnv()
				if err != nil {
					return err
				}

				path := filepath.Join(e.config, signingKeyFile)
				if _, err := os.Stat(path); err == nil {
					return fmt.Errorf(
						"%s already exists, delete it first to replace your key",
						path,
					)
				}

				public, private, err := minisign.GenerateKey(rand.Reader)
				if err != nil {
					return err
				}

				text, err := private.MarshalText()
				if err != nil {
					return err
				}

				if err := os.MkdirAll(e.config, 0o755); err != nil {
					return err
				}

				if err := os.WriteFile(path, text, 0o600); err != nil {
					return err
				}

				out := cmd.OutOrStdout()
				fmt.Fprintf(out, "wrote the secret key to %s, it has no password\n", path)
				fmt.Fprintf(out, "people who use your cache run:\n  oku key trust %s\n", public)

				return nil
			},
		},
		&cobra.Command{
			Use:   "trust <public-key>",
			Short: "Accept cache entries that this key signed",
			Args:  exactArgs(1),
			RunE: func(cmd *cobra.Command, args []string) error {
				var key minisign.PublicKey
				if err := key.UnmarshalText([]byte(args[0])); err != nil {
					return fmt.Errorf("%s is not a minisign public key: %w", args[0], err)
				}

				return editConfig(func(c *source.Config) error {
					if !slices.Contains(c.TrustedKeys, key.String()) {
						c.TrustedKeys = append(c.TrustedKeys, key.String())
					}

					fmt.Fprintf(cmd.OutOrStdout(), "trusted %s\n", key)

					return nil
				})
			},
		},
		&cobra.Command{
			Use:   "revoke <public-key>",
			Short: "Stop accepting cache entries that this key signed",
			Args:  exactArgs(1),
			RunE: func(cmd *cobra.Command, args []string) error {
				return editConfig(func(c *source.Config) error {
					before := len(c.TrustedKeys)

					c.TrustedKeys = slices.DeleteFunc(c.TrustedKeys, func(have string) bool {
						return have == args[0]
					})
					if len(c.TrustedKeys) == before {
						return fmt.Errorf("%s is not a trusted key, see \"oku key list\"", args[0])
					}

					fmt.Fprintf(cmd.OutOrStdout(), "revoked %s\n", args[0])

					return nil
				})
			},
		},
		&cobra.Command{
			Use:   "list",
			Short: "List the trusted keys, and your own public key",
			Args:  cobra.NoArgs,
			RunE: func(cmd *cobra.Command, _ []string) error {
				e, err := loadEnv()
				if err != nil {
					return err
				}

				out := cmd.OutOrStdout()
				yours := ""

				if key, err := readSigningKey(filepath.Join(e.config, signingKeyFile)); err == nil {
					yours = key.Public().(minisign.PublicKey).String()
				}

				config, err := source.Read(e.configPath())
				if err != nil {
					return err
				}

				if wantJSON(cmd) {
					return printJSON(cmd, struct {
						Yours   string   `json:"yours"`
						Trusted []string `json:"trusted"`
					}{yours, append([]string{}, config.TrustedKeys...)})
				}

				if yours == "" && len(config.TrustedKeys) == 0 {
					fmt.Fprintln(
						out,
						"no keys yet, `oku key generate` makes yours and `oku key trust` adds another",
					)

					return nil
				}

				pairs := [][2]string{{"yours", yours}}
				for _, key := range config.TrustedKeys {
					pairs = append(pairs, [2]string{"trusted", key})
				}

				return ui.For(out).KV(out, pairs...)
			},
		},
	)

	return cmd
}
