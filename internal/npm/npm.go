// Package npm reads a package's versions from the npm registry.
package npm

import (
	"context"
	"encoding/json"
	"errors"
	"fmt"
	"io"
	"net/http"
	"time"
)

// Registry is where npm packages are published.
const Registry = "https://registry.npmjs.org"

// maxBody is the most bytes read from the registry. A package with thousands of
// versions has a list of several MiB.
const maxBody = 64 << 20

// ErrNotFound reports that the registry has no such package.
var ErrNotFound = errors.New("the registry has no such package")

// Version is one published version.
type Version struct {
	// Tarball is the URL of the download.
	Tarball string
	// Integrity is the download's digest, such as "sha512-...".
	Integrity string
	// Bin maps a program's name to the path of its script in the download.
	Bin map[string]string
	// Dependencies reports whether the version lists packages it needs at run
	// time, optional ones included. The registry does not say whether the
	// download bundles them.
	Dependencies bool
}

// Package is what the registry lists for one package.
type Package struct {
	// Latest is the version the "latest" tag names.
	Latest   string
	Versions map[string]Version
}

// Read returns the versions of the package called name. An empty registry means
// Registry.
func Read(ctx context.Context, client *http.Client, registry, name string) (Package, error) {
	if registry == "" {
		registry = Registry
	}

	req, err := http.NewRequestWithContext(ctx, http.MethodGet, registry+"/"+name, nil)
	if err != nil {
		return Package{}, err
	}

	req.Header.Set("User-Agent", "oku")
	// This form leaves out each version's readme and scripts.
	req.Header.Set("Accept", "application/vnd.npm.install-v1+json")

	resp, err := client.Do(req)
	if err != nil {
		return Package{}, err
	}
	defer resp.Body.Close()

	switch {
	case resp.StatusCode == http.StatusNotFound:
		return Package{}, ErrNotFound
	case resp.StatusCode != http.StatusOK:
		return Package{}, fmt.Errorf("the registry returned %s", resp.Status)
	}

	var found struct {
		Tags     map[string]string `json:"dist-tags"`
		Versions map[string]struct {
			Name string `json:"name"`
			// Bin is a map, or one path for a program named after the package.
			Bin          json.RawMessage   `json:"bin"`
			Dependencies map[string]string `json:"dependencies"`
			Optional     map[string]string `json:"optionalDependencies"`
			Dist         struct {
				Tarball   string `json:"tarball"`
				Integrity string `json:"integrity"`
			} `json:"dist"`
		} `json:"versions"`
	}

	if err := json.NewDecoder(io.LimitReader(resp.Body, maxBody)).Decode(&found); err != nil {
		return Package{}, err
	}

	pkg := Package{Latest: found.Tags["latest"], Versions: map[string]Version{}}

	for version, published := range found.Versions {
		v := Version{
			Tarball:      published.Dist.Tarball,
			Integrity:    published.Dist.Integrity,
			Dependencies: len(published.Dependencies)+len(published.Optional) > 0,
			Bin:          map[string]string{},
		}

		var single string
		if json.Unmarshal(published.Bin, &single) == nil && single != "" {
			v.Bin[BaseName(name)] = single
		} else {
			_ = json.Unmarshal(published.Bin, &v.Bin)
		}

		pkg.Versions[version] = v
	}

	return pkg, nil
}

// Published returns when a version of the package called name was published.
// Only the registry's full answer holds that, so this reads more than Read
// does.
func Published(
	ctx context.Context,
	client *http.Client,
	registry, name, version string,
) (time.Time, error) {
	if registry == "" {
		registry = Registry
	}

	req, err := http.NewRequestWithContext(ctx, http.MethodGet, registry+"/"+name, nil)
	if err != nil {
		return time.Time{}, err
	}

	req.Header.Set("User-Agent", "oku")

	resp, err := client.Do(req)
	if err != nil {
		return time.Time{}, err
	}
	defer resp.Body.Close()

	switch {
	case resp.StatusCode == http.StatusNotFound:
		return time.Time{}, ErrNotFound
	case resp.StatusCode != http.StatusOK:
		return time.Time{}, fmt.Errorf("the registry returned %s", resp.Status)
	}

	var found struct {
		Time map[string]time.Time `json:"time"`
	}

	if err := json.NewDecoder(io.LimitReader(resp.Body, maxBody)).Decode(&found); err != nil {
		return time.Time{}, err
	}

	at, ok := found.Time[version]
	if !ok {
		return time.Time{}, fmt.Errorf("the registry does not say when %s %s was published", name, version)
	}

	return at, nil
}

// BaseName returns "name" for "@scope/name".
func BaseName(name string) string {
	for i := len(name) - 1; i >= 0; i-- {
		if name[i] == '/' {
			return name[i+1:]
		}
	}

	return name
}
