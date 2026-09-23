// Package crates reads a crate's versions from crates.io.
package crates

import (
	"context"
	"encoding/json"
	"errors"
	"fmt"
	"io"
	"net/http"
	"regexp"
	"strings"
)

// API is where crates.io answers questions about crates.
const API = "https://crates.io/api/v1"

// Downloads is where crates.io serves the .crate file of each version.
const Downloads = "https://static.crates.io/crates"

// userAgent names oku, which the crates.io crawler policy asks every client to.
const userAgent = "oku (https://github.com/y3owk1n/oku)"

// maxBody is the most bytes read from one answer of the API.
const maxBody = 32 << 20

// ErrNotFound reports that crates.io has no such crate.
var ErrNotFound = errors.New("crates.io has no such crate")

// nameRe is a crate name as crates.io accepts it.
var nameRe = regexp.MustCompile(`^[A-Za-z][A-Za-z0-9_-]{0,63}$`)

// ValidName reports whether name is a crate name.
func ValidName(name string) bool {
	return nameRe.MatchString(name)
}

// Version is one published version.
type Version struct {
	Number string
	// SHA256 is the digest of the version's .crate file.
	SHA256 string
	Yanked bool
	// Programs are the names of the version's binaries.
	Programs []string
}

// Crate is what crates.io lists for one crate, newest version first.
type Crate struct {
	Name        string
	Description string
	Versions    []Version
}

// URL returns where crates.io serves the .crate file of version. An empty
// downloads means Downloads.
func URL(downloads, name, version string) string {
	if downloads == "" {
		downloads = Downloads
	}

	return strings.TrimRight(downloads, "/") + "/" + name + "/" + name + "-" + version + ".crate"
}

// Read returns the versions of the crate called name. An empty api means API.
func Read(ctx context.Context, client *http.Client, api, name string) (Crate, error) {
	if api == "" {
		api = API
	}

	req, err := http.NewRequestWithContext(
		ctx, http.MethodGet, strings.TrimRight(api, "/")+"/crates/"+name, nil,
	)
	if err != nil {
		return Crate{}, err
	}

	req.Header.Set("User-Agent", userAgent)

	resp, err := client.Do(req)
	if err != nil {
		return Crate{}, err
	}
	defer resp.Body.Close()

	switch {
	case resp.StatusCode == http.StatusNotFound:
		return Crate{}, ErrNotFound
	case resp.StatusCode != http.StatusOK:
		return Crate{}, fmt.Errorf("crates.io returned %s", resp.Status)
	}

	body, err := io.ReadAll(io.LimitReader(resp.Body, maxBody+1))
	if err != nil {
		return Crate{}, err
	}

	if len(body) > maxBody {
		return Crate{}, fmt.Errorf("crates.io answered with more than %d bytes", maxBody)
	}

	var found struct {
		Crate struct {
			Name        string `json:"name"`
			Description string `json:"description"`
		} `json:"crate"`
		Versions []struct {
			Number   string   `json:"num"`
			Checksum string   `json:"checksum"`
			Yanked   bool     `json:"yanked"`
			Bins     []string `json:"bin_names"`
		} `json:"versions"`
	}

	if err := json.Unmarshal(body, &found); err != nil {
		return Crate{}, fmt.Errorf("read the answer of crates.io: %w", err)
	}

	c := Crate{Name: found.Crate.Name, Description: strings.TrimSpace(found.Crate.Description)}

	for _, v := range found.Versions {
		c.Versions = append(c.Versions, Version{
			Number: v.Number, SHA256: v.Checksum, Yanked: v.Yanked, Programs: v.Bins,
		})
	}

	return c, nil
}
