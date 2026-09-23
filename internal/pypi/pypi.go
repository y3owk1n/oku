// Package pypi reads a package's versions from the Python Package Index.
package pypi

import (
	"context"
	"encoding/json"
	"errors"
	"fmt"
	"io"
	"net/http"
	"regexp"
	"strings"
	"time"
)

// Index is where Python packages are published.
const Index = "https://pypi.org"

// maxBody is the most bytes read from the index. A package with hundreds of
// versions, each with dozens of wheels, has a list of several MiB.
const maxBody = 64 << 20

// ErrNotFound reports that the index has no such package.
var ErrNotFound = errors.New("the index has no such package")

// Version is one published version.
type Version struct {
	// Uploaded is when the last file of the version was uploaded.
	Uploaded time.Time
	// Yanked reports that every file of the version was withdrawn.
	Yanked bool
}

// Package is what the index lists for one package.
type Package struct {
	// Name is the package's name as its author wrote it.
	Name string
	// Summary is the package's one-line description.
	Summary  string
	Latest   string
	Versions map[string]Version
}

// Read returns the versions of the package called name. An empty index means
// Index.
func Read(ctx context.Context, client *http.Client, index, name string) (Package, error) {
	if index == "" {
		index = Index
	}

	url := strings.TrimRight(index, "/") + "/pypi/" + Normalize(name) + "/json"

	req, err := http.NewRequestWithContext(ctx, http.MethodGet, url, nil)
	if err != nil {
		return Package{}, err
	}

	req.Header.Set("User-Agent", "oku")

	resp, err := client.Do(req)
	if err != nil {
		return Package{}, err
	}
	defer resp.Body.Close()

	switch {
	case resp.StatusCode == http.StatusNotFound:
		return Package{}, ErrNotFound
	case resp.StatusCode != http.StatusOK:
		return Package{}, fmt.Errorf("the index returned %s", resp.Status)
	}

	var found struct {
		Info struct {
			Name    string `json:"name"`
			Summary string `json:"summary"`
			Version string `json:"version"`
		} `json:"info"`
		Releases map[string][]struct {
			Uploaded time.Time `json:"upload_time_iso_8601"`
			Yanked   bool      `json:"yanked"`
		} `json:"releases"`
	}

	body, err := io.ReadAll(io.LimitReader(resp.Body, maxBody+1))
	if err != nil {
		return Package{}, err
	}

	if len(body) > maxBody {
		return Package{}, fmt.Errorf("the index answered with more than %d bytes", maxBody)
	}

	if err := json.Unmarshal(body, &found); err != nil {
		return Package{}, fmt.Errorf("read the index's answer: %w", err)
	}

	pkg := Package{
		Name: found.Info.Name, Summary: found.Info.Summary, Latest: found.Info.Version,
		Versions: map[string]Version{},
	}

	for version, files := range found.Releases {
		// A version with no files cannot be installed.
		if len(files) == 0 {
			continue
		}

		v := Version{Yanked: true}

		for _, file := range files {
			v.Yanked = v.Yanked && file.Yanked

			if file.Uploaded.After(v.Uploaded) {
				v.Uploaded = file.Uploaded
			}
		}

		pkg.Versions[version] = v
	}

	return pkg, nil
}

var (
	separators = regexp.MustCompile(`[-_.]+`)
	// nameRe is a project name as PEP 508 allows it.
	nameRe = regexp.MustCompile(`^[A-Za-z0-9]([A-Za-z0-9._-]*[A-Za-z0-9])?$`)
	// prereleaseRe matches a PEP 440 prerelease or development version, such as
	// 1.0a1, 2.0b3, 3.1rc1 or 4.0.dev2.
	prereleaseRe = regexp.MustCompile(`(?i)\d[._-]?(a|alpha|b|beta|c|rc|pre|preview)\d*|dev\d*$`)
)

// Normalize returns name as the index keys it, with PEP 503: lower case, and
// each run of "-", "_" and "." one "-".
func Normalize(name string) string {
	return separators.ReplaceAllString(strings.ToLower(name), "-")
}

// ValidName reports whether name is a project name the index can hold.
func ValidName(name string) bool {
	return nameRe.MatchString(name)
}

// IsPrerelease reports whether version is a PEP 440 prerelease or development
// version.
func IsPrerelease(version string) bool {
	return prereleaseRe.MatchString(version)
}
