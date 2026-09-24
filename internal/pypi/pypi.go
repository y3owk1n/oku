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

	"github.com/y3owk1n/oku/internal/shape"
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

// simpleType is the media type of version 1 of the Simple API in JSON, PEP 691.
const simpleType = "application/vnd.pypi.simple.v1+json"

// Read returns the versions of the package called name. An empty index means
// Index. The versions and their files come from the Simple API in JSON, which
// has a format version. The name, summary and latest version come from the
// JSON API, which deprecates its own list of versions.
func Read(ctx context.Context, client *http.Client, index, name string) (Package, error) {
	if index == "" {
		index = Index
	}

	base := strings.TrimRight(index, "/")

	var info struct {
		Info struct {
			Name    string `json:"name"`
			Summary string `json:"summary"`
			Version string `json:"version"`
		} `json:"info"`
	}

	if _, err := get(ctx, client, base+"/pypi/"+Normalize(name)+"/json", "", &info); err != nil {
		return Package{}, err
	}

	var simple struct {
		Meta struct {
			APIVersion string `json:"api-version"`
		} `json:"meta"`
		Versions []string `json:"versions"`
		Files    []struct {
			Filename string          `json:"filename"`
			Uploaded time.Time       `json:"upload-time"`
			Yanked   json.RawMessage `json:"yanked"`
		} `json:"files"`
	}

	kind, err := get(ctx, client, base+"/simple/"+Normalize(name)+"/", simpleType, &simple)
	if err != nil {
		return Package{}, err
	}

	// PEP 691 has a client fail on a major version it does not know. An index
	// that does not know the type answers with another one, such as HTML.
	major, _, _ := strings.Cut(simple.Meta.APIVersion, ".")

	if err := shape.Check(
		"the index's answer for "+name,
		shape.Field{Name: "the name", Has: info.Info.Name != ""},
		shape.Field{Name: "the latest version", Has: info.Info.Version != ""},
		shape.Field{Name: "the Simple API's version 1 in JSON", Has: strings.HasPrefix(kind, simpleType) && major == "1"},
	); err != nil {
		return Package{}, err
	}

	pkg := Package{
		Name: info.Info.Name, Summary: info.Info.Summary, Latest: info.Info.Version,
		Versions: map[string]Version{},
	}

	files := map[string][]Version{}

	for _, f := range simple.Files {
		version := fileVersion(f.Filename)
		if version == "" {
			continue
		}

		// yanked is false, or true or the reason when the file was yanked.
		yanked := len(f.Yanked) > 0 && string(f.Yanked) != "false"
		files[version] = append(files[version], Version{Uploaded: f.Uploaded, Yanked: yanked})
	}

	versions := simple.Versions
	if versions == nil {
		// An index at version 1.0 lists no versions, only files.
		for version := range files {
			versions = append(versions, version)
		}
	}

	for _, version := range versions {
		// A version with no files cannot be installed. A wheel spells a version
		// with "_" for "-".
		found := files[version]
		if found == nil {
			found = files[strings.ReplaceAll(version, "-", "_")]
		}

		if len(found) == 0 {
			continue
		}

		v := Version{Yanked: true}

		for _, file := range found {
			v.Yanked = v.Yanked && file.Yanked

			if file.Uploaded.After(v.Uploaded) {
				v.Uploaded = file.Uploaded
			}
		}

		pkg.Versions[version] = v
	}

	return pkg, nil
}

// fileVersion returns the version a file of a release names: the second part
// of a wheel's name, or what follows the last "-" of a source archive's.
func fileVersion(filename string) string {
	if stem, ok := strings.CutSuffix(filename, ".whl"); ok {
		if parts := strings.Split(stem, "-"); len(parts) >= 5 {
			return parts[1]
		}

		return ""
	}

	for _, ext := range []string{".tar.gz", ".zip", ".tar.bz2", ".tar.xz", ".tgz"} {
		if stem, ok := strings.CutSuffix(filename, ext); ok {
			if i := strings.LastIndex(stem, "-"); i > 0 {
				return stem[i+1:]
			}
		}
	}

	return ""
}

// get reads url into into, asking for the media type accept when it is set,
// and returns the media type of the answer.
func get(ctx context.Context, client *http.Client, url, accept string, into any) (string, error) {
	req, err := http.NewRequestWithContext(ctx, http.MethodGet, url, nil)
	if err != nil {
		return "", err
	}

	req.Header.Set("User-Agent", "oku")

	if accept != "" {
		req.Header.Set("Accept", accept)
	}

	resp, err := client.Do(req)
	if err != nil {
		return "", err
	}
	defer resp.Body.Close()

	switch {
	case resp.StatusCode == http.StatusNotFound:
		return "", ErrNotFound
	case resp.StatusCode != http.StatusOK:
		return "", fmt.Errorf("the index returned %s", resp.Status)
	}

	body, err := io.ReadAll(io.LimitReader(resp.Body, maxBody+1))
	if err != nil {
		return "", err
	}

	if len(body) > maxBody {
		return "", fmt.Errorf("the index answered with more than %d bytes", maxBody)
	}

	kind := resp.Header.Get("Content-Type")

	// An answer in another media type has nothing to decode.
	if accept != "" && !strings.HasPrefix(kind, accept) {
		return kind, nil
	}

	if err := json.Unmarshal(body, into); err != nil {
		return "", fmt.Errorf("read the index's answer: %w", err)
	}

	return kind, nil
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
