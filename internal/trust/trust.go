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
