// Package source keeps the user's aliases for manifest collections and expands
// "alias/name" into the ref it stands for.
package source

import (
	"errors"
	"fmt"
	"io/fs"
	"os"
	"regexp"
	"strings"

	"github.com/pelletier/go-toml/v2"

	"github.com/y3owk1n/oku/internal/list"
	"github.com/y3owk1n/oku/internal/ref"
)

// FileName is the config file that holds the aliases.
const FileName = "config.toml"

var (
	aliasRe = regexp.MustCompile(`^[a-z0-9][a-z0-9_-]*$`)
	shortRe = regexp.MustCompile(`^([a-z0-9][a-z0-9_-]*)/([a-z0-9][a-z0-9._-]*)(@[^/@]+)?$`)
)

// Config is the part of config.toml that oku reads today.
type Config struct {
	// StoreRoot is the shared store root from "oku setup --system". Empty means
	// the store is in the user's data directory.
	StoreRoot string `toml:"store_root,omitempty"`
	// Caches are the directories and URLs oku looks in for a built package before
	// it builds one.
	Caches []string `toml:"caches,omitempty"`
	// TrustedKeys are the minisign public keys whose cache entries oku accepts.
	TrustedKeys []string          `toml:"trusted_keys,omitempty"`
	Sources     map[string]string `toml:"sources"`
	// Runtimes maps an interpreter, such as "node", to the ref of the package
	// that provides it. An inferred manifest that needs the interpreter depends
	// on that package.
	Runtimes map[string]string `toml:"runtimes,omitempty"`
}

// Read parses the config at path. A missing file is an empty config.
func Read(path string) (*Config, error) {
	data, err := os.ReadFile(path)
	if err != nil && !errors.Is(err, fs.ErrNotExist) {
		return nil, fmt.Errorf("read %s: %w", path, err)
	}

	c := &Config{}
	if err := toml.Unmarshal(data, c); err != nil {
		return nil, fmt.Errorf("parse %s: %w", path, err)
	}

	if c.Sources == nil {
		c.Sources = map[string]string{}
	}

	return c, nil
}

// Write saves the config to path.
func (c *Config) Write(path string) error {
	data, err := toml.Marshal(c)
	if err != nil {
		return fmt.Errorf("write %s: %w", path, err)
	}

	if err := list.WriteFile(path, data); err != nil {
		return fmt.Errorf("write %s: %w", path, err)
	}

	return nil
}

// Add records alias for the collection at target, which must be a ref without a
// fragment or a version.
func (c *Config) Add(alias, target string) error {
	if !aliasRe.MatchString(alias) {
		return fmt.Errorf("alias %q must be lowercase letters, digits, '_' or '-'", alias)
	}

	r, err := ref.Parse(target)
	if err != nil {
		return err
	}

	if r.Fragment != "" || r.Version != "" {
		return fmt.Errorf(
			"%s: a source is a whole collection, so it takes no #name or @version",
			target,
		)
	}

	c.Sources[alias] = r.String()

	return nil
}

// Expand turns "alias/name" or "alias/name@version" into the ref of that
// manifest. It returns arg unchanged when arg is not of that form or names a path
// that exists. An alias that is not defined is an error.
func (c *Config) Expand(arg string) (string, error) {
	parts := shortRe.FindStringSubmatch(arg)
	if parts == nil {
		return arg, nil
	}

	if _, err := os.Stat(arg); err == nil {
		return arg, nil
	}

	target, known := c.Sources[parts[1]]
	if !known {
		return "", fmt.Errorf(
			"%s is not a source and %s is not a file, see `oku source list`", parts[1], arg,
		)
	}

	return Member(target, parts[2]) + parts[3], nil
}

// Member returns the ref of the manifest called name in the collection at
// target. A forge or git collection is searched for "name.toml" and
// "packages/name.toml" when the ref is fetched. Member searches a directory
// itself. A URL holds "name.toml" only, because oku cannot list a URL.
func Member(target, name string) string {
	if r, err := ref.Parse(target); err == nil && (r.Kind == ref.Forge || r.Kind == ref.Git) {
		return target + "#" + name
	}

	root := strings.TrimRight(target, "/") + "/" + name + ".toml"
	nested := strings.TrimRight(target, "/") + "/" + ref.Manifest.Dir + "/" + name + ".toml"

	if _, err := os.Stat(root); err != nil {
		if _, err := os.Stat(nested); err == nil {
			return nested
		}
	}

	return root
}
