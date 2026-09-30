// Package source keeps the user's aliases for manifest collections and expands
// "alias/name" into the ref it stands for.
package source

import (
	"errors"
	"fmt"
	"io/fs"
	"net/url"
	"os"
	"regexp"
	"slices"
	"strings"

	"github.com/pelletier/go-toml/v2"

	"github.com/y3owk1n/oku/internal/list"
	"github.com/y3owk1n/oku/internal/netpolicy"
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
	// Runtimes maps an interpreter, such as "node", to the package that
	// provides it, as a ref or as a table with ref and version. An inferred
	// manifest that needs the interpreter depends on that package.
	Runtimes map[string]any `toml:"runtimes,omitempty"`
	// RequireSandbox refuses to run a package's commands on a host that cannot
	// sandbox them.
	RequireSandbox bool `toml:"require_sandbox,omitempty"`
	// Forge holds the user's own forge servers.
	Forge Forge `toml:"forge,omitempty"`
	// Network says where oku may connect.
	Network Network `toml:"network,omitempty"`
	// Trust names the origins whose refs a project may install.
	Trust Trust `toml:"trust,omitempty"`
}

// Network is the [network] table of config.toml.
type Network struct {
	// DenyPrivate refuses private addresses, unless the URL names this machine
	// or Private lists the host. Unset means true.
	DenyPrivate *bool `toml:"deny_private,omitempty"`
	// Allow lists the only hosts oku connects to. Empty means every host.
	Allow []string `toml:"allow,omitempty"`
	// Private lists the hosts that may resolve to a private address.
	Private []string `toml:"private,omitempty"`
}

// Policy is the network policy of the config. The hosts of the caches and of
// [forge] hosts are the user's own, so they may be private, and an allow list
// takes them too.
func (c *Config) Policy() netpolicy.Policy {
	var named []string

	for _, cache := range c.Caches {
		if u, err := url.Parse(cache); err == nil && u.Hostname() != "" {
			named = append(named, u.Hostname())
		}
	}

	for host := range c.Forge.Hosts {
		named = append(named, host)
	}

	p := netpolicy.Policy{
		AllowPrivate: c.Network.DenyPrivate != nil && !*c.Network.DenyPrivate,
		Private:      slices.Concat(c.Network.Private, named),
	}

	if len(c.Network.Allow) > 0 {
		p.Allow = slices.Concat(c.Network.Allow, named)
	}

	return p
}

// Trust is the [trust] table of config.toml.
type Trust struct {
	// Sources are trusted origins, such as "github:owner", "github:owner/repo",
	// "example.com" or "npm:@scope".
	Sources []string `toml:"sources,omitempty"`
}

// Forge is the [forge] table of config.toml.
type Forge struct {
	// Hosts maps a host to its kind, "github", "gitea" or "gitlab". oku sends that
	// kind's token for other hosts only to a host listed here.
	Hosts map[string]string `toml:"hosts,omitempty"`
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

	for host, kind := range c.Forge.Hosts {
		if kind != "github" && kind != "gitea" && kind != "gitlab" {
			return nil, fmt.Errorf("%s: forge.hosts.%q must be \"github\", \"gitea\" or \"gitlab\"", path, host)
		}
	}

	for key, patterns := range map[string][]string{"allow": c.Network.Allow, "private": c.Network.Private} {
		for _, pattern := range patterns {
			if err := netpolicy.ValidPattern(pattern); err != nil {
				return nil, fmt.Errorf("%s: network.%s: %w", path, key, err)
			}
		}
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
