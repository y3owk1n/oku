// Package trust records which manifests the user allowed to run commands.
package trust

import (
	"errors"
	"fmt"
	"io/fs"
	"os"
	"path/filepath"
	"slices"
	"time"

	"github.com/pelletier/go-toml/v2"

	"github.com/y3owk1n/oku/internal/list"
)

// Approvals is the approvals file. An approval belongs to one manifest hash, so
// a changed manifest needs a new one.
type Approvals struct {
	path  string
	Items []Approval `toml:"approval"`
}

// Approval is one manifest the user allowed to run its build commands.
type Approval struct {
	Name           string    `toml:"name"`
	ManifestSHA256 string    `toml:"manifest_sha256"`
	ApprovedAt     time.Time `toml:"approved_at"`
}

// Read loads the approvals under dataDir. A missing file has no approvals.
func Read(dataDir string) (*Approvals, error) {
	a := &Approvals{path: filepath.Join(dataDir, "trust", "approvals.toml")}

	data, err := os.ReadFile(a.path)
	if err != nil && !errors.Is(err, fs.ErrNotExist) {
		return nil, fmt.Errorf("read %s: %w", a.path, err)
	}

	if err := toml.Unmarshal(data, a); err != nil {
		return nil, fmt.Errorf("parse %s: %w", a.path, err)
	}

	return a, nil
}

// Has reports whether the manifest with this hash is approved.
func (a *Approvals) Has(manifestSHA256 string) bool {
	return slices.ContainsFunc(a.Items, func(item Approval) bool {
		return item.ManifestSHA256 == manifestSHA256
	})
}

// Add records an approval and saves the file.
func (a *Approvals) Add(name, manifestSHA256 string) error {
	a.Items = append(a.Items, Approval{
		Name:           name,
		ManifestSHA256: manifestSHA256,
		ApprovedAt:     time.Now().UTC().Truncate(time.Second),
	})

	data, err := toml.Marshal(a)
	if err != nil {
		return fmt.Errorf("write %s: %w", a.path, err)
	}

	if err := list.WriteFile(a.path, data); err != nil {
		return fmt.Errorf("write %s: %w", a.path, err)
	}

	return nil
}

// Allowed is the list of projects whose environment the shell hook may apply.
// An entry belongs to one oku.toml content, so editing the list revokes it.
type Allowed struct {
	path  string
	Items []Allow `toml:"allow"`
}

// Allow is one project the user trusts.
type Allow struct {
	Dir string `toml:"dir"`
	// ListSHA256 covers the oku.toml and the .env files it loads that git
	// tracks.
	ListSHA256 string `toml:"list_sha256"`
	// Untracked holds the .env files the list loads that git did not track,
	// which the user changes without a new allow.
	Untracked []Untracked `toml:"untracked,omitempty"`
}

// Untracked is a .env file that git did not track when the user allowed the
// project. oku asks git again once the file changes.
type Untracked struct {
	Path string `toml:"path"`
	// ModTime is the file's modification time in Unix nanoseconds, or 0 when
	// the file did not exist.
	ModTime int64 `toml:"mod_time"`
}

// ReadAllowed loads the allow list under dataDir. A missing file allows nothing.
func ReadAllowed(dataDir string) (*Allowed, error) {
	a := &Allowed{path: filepath.Join(dataDir, "trust", "allow.toml")}

	data, err := os.ReadFile(a.path)
	if err != nil && !errors.Is(err, fs.ErrNotExist) {
		return nil, fmt.Errorf("read %s: %w", a.path, err)
	}

	if err := toml.Unmarshal(data, a); err != nil {
		return nil, fmt.Errorf("parse %s: %w", a.path, err)
	}

	return a, nil
}

// Get returns the allow of dir.
func (a *Allowed) Get(dir string) (Allow, bool) {
	i := slices.IndexFunc(a.Items, func(item Allow) bool { return item.Dir == dir })
	if i < 0 {
		return Allow{}, false
	}

	return a.Items[i], true
}

// Set records the allow of dir, or removes it when allow is nil, and saves the
// file.
func (a *Allowed) Set(dir string, allow *Allow) error {
	a.Items = slices.DeleteFunc(a.Items, func(item Allow) bool { return item.Dir == dir })

	if allow != nil {
		allow.Dir = dir
		a.Items = append(a.Items, *allow)
	}

	data, err := toml.Marshal(a)
	if err != nil {
		return fmt.Errorf("write %s: %w", a.path, err)
	}

	if err := list.WriteFile(a.path, data); err != nil {
		return fmt.Errorf("write %s: %w", a.path, err)
	}

	return nil
}
