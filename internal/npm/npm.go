// Package npm reads a package's versions from the npm registry.
package npm

import (
	"context"
	"encoding/json"
	"errors"
	"fmt"
	"io"
	"maps"
	"net/http"
	"slices"
	"time"

	"github.com/y3owk1n/oku/internal/shape"
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
	// Modified is when the package last changed, so every version was published
	// no later than it.
	Modified time.Time
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
	req.Header.Set("Accept", "application/vnd.npm.install-v1+json; q=1.0, application/json; q=0.8, */*")

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
		Modified time.Time         `json:"modified"`
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

	pkg := Package{
		Latest: found.Tags["latest"], Versions: map[string]Version{}, Modified: found.Modified,
	}
	downloads := true

	for _, published := range found.Versions {
		downloads = downloads && published.Dist.Tarball != ""
	}

	if err := shape.Check(
		"the npm registry's answer for "+name,
		shape.Field{Name: "the latest version", Has: pkg.Latest != ""},
		shape.Field{Name: "the versions", Has: len(found.Versions) > 0},
		shape.Field{Name: "a version's download", Has: downloads},
	); err != nil {
		return Package{}, err
	}

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

// Publication is what the registry's full answer says about one version.
type Publication struct {
	// At is when the version was published.
	At time.Time
	// Dependencies are the packages the version needs at run time, optional ones
	// included, sorted.
	Dependencies []string
}

// Published returns when a version of the package called name was published
// and what it depends on. Only the registry's full answer holds the time, so
// this reads more than Read does.
func Published(
	ctx context.Context,
	client *http.Client,
	registry, name, version string,
) (Publication, error) {
	found, err := readFull(ctx, client, registry, name)
	if err != nil {
		return Publication{}, err
	}

	at, ok := found.Time[version]
	if !ok {
		return Publication{}, fmt.Errorf(
			"the registry does not say when %s %s was published",
			name,
			version,
		)
	}

	published := Publication{At: at}
	for _, deps := range []map[string]string{
		found.Versions[version].Dependencies, found.Versions[version].Optional,
	} {
		published.Dependencies = slices.AppendSeq(published.Dependencies, maps.Keys(deps))
	}

	slices.Sort(published.Dependencies)

	return published, nil
}

// Times returns when each version of the package called name was published,
// from the registry's full answer.
func Times(
	ctx context.Context,
	client *http.Client,
	registry, name string,
) (map[string]time.Time, error) {
	found, err := readFull(ctx, client, registry, name)

	return found.Time, err
}

// full is the part of the registry's full answer that Published and Times read.
type full struct {
	Time     map[string]time.Time `json:"time"`
	Versions map[string]struct {
		Dependencies map[string]string `json:"dependencies"`
		Optional     map[string]string `json:"optionalDependencies"`
	} `json:"versions"`
}

func readFull(ctx context.Context, client *http.Client, registry, name string) (full, error) {
	if registry == "" {
		registry = Registry
	}

	req, err := http.NewRequestWithContext(ctx, http.MethodGet, registry+"/"+name, nil)
	if err != nil {
		return full{}, err
	}

	req.Header.Set("User-Agent", "oku")

	resp, err := client.Do(req)
	if err != nil {
		return full{}, err
	}
	defer resp.Body.Close()

	switch {
	case resp.StatusCode == http.StatusNotFound:
		return full{}, ErrNotFound
	case resp.StatusCode != http.StatusOK:
		return full{}, fmt.Errorf("the registry returned %s", resp.Status)
	}

	var found full
	if err := json.NewDecoder(io.LimitReader(resp.Body, maxBody)).Decode(&found); err != nil {
		return full{}, err
	}

	return found, nil
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
