// Package list reads and edits oku.toml, the user's declared package list.
package list

import (
	"errors"
	"fmt"
	"io/fs"
	"os"
	"path/filepath"
	"regexp"
	"strings"

	"github.com/pelletier/go-toml/v2"

	"github.com/y3owk1n/oku/internal/platform"
)

// FileName is the list's file name.
const FileName = "oku.toml"

// Entry is one package of a list.
type Entry struct {
	Ref     string
	Version string
	// When limits the package to matching platforms. The zero value matches all.
	When platform.Selector
}

// List is a parsed oku.toml.
type List struct {
	// Include holds refs of other lists to merge under this one.
	Include  []string
	Packages map[string]Entry
}

// Read parses the list at path. A missing file is an empty list.
func Read(path string) (*List, error) {
	data, err := os.ReadFile(path)
	if err != nil && !errors.Is(err, fs.ErrNotExist) {
		return nil, fmt.Errorf("read %s: %w", path, err)
	}

	return Parse(data, path)
}

// Parse reads list data. origin names the data in error messages.
func Parse(data []byte, origin string) (*List, error) {
	var raw struct {
		Include  []string       `toml:"include"`
		Packages map[string]any `toml:"packages"`
	}

	if err := toml.Unmarshal(data, &raw); err != nil {
		return nil, fmt.Errorf("parse %s: %w", origin, err)
	}

	l := &List{Include: raw.Include, Packages: map[string]Entry{}}

	for name, value := range raw.Packages {
		entry, err := toEntry(value)
		if err != nil {
			return nil, fmt.Errorf("%s: packages.%s: %w", origin, name, err)
		}

		l.Packages[name] = entry
	}

	return l, nil
}

// toEntry accepts the short form "ref" and the table form { ref, version, when }.
func toEntry(value any) (Entry, error) {
	switch v := value.(type) {
	case string:
		return Entry{Ref: v}, nil
	case map[string]any:
		var e Entry

		e.Ref, _ = v["ref"].(string)
		e.Version, _ = v["version"].(string)

		if e.Ref == "" {
			return e, errors.New("ref is required")
		}

		when, _ := v["when"].(map[string]any)
		for key, field := range map[string]*string{
			"os": &e.When.OS, "arch": &e.When.Arch, "libc": &e.When.Libc,
		} {
			*field, _ = when[key].(string)
		}

		for key := range when {
			if key != "os" && key != "arch" && key != "libc" {
				return e, fmt.Errorf("when.%s is not a selector key, use os, arch or libc", key)
			}
		}

		return e, nil
	default:
		return Entry{}, errors.New("want a ref string or a table with ref")
	}
}

// Set writes entry under name in the list at path. It edits the file's text, so
// the user's comments and ordering stay.
func Set(path, name string, entry Entry) error {
	return edit(path, name, formatLine(name, entry))
}

// Delete removes name from the list at path.
func Delete(path, name string) error {
	return edit(path, name, "")
}

func formatLine(name string, entry Entry) string {
	value := fmt.Sprintf("%q", entry.Ref)
	if entry.Version != "" {
		value = fmt.Sprintf("{ ref = %q, version = %q }", entry.Ref, entry.Version)
	}

	return formatKey(name) + " = " + value
}

// formatKey quotes a name that holds ".", which TOML would read as a nested key.
func formatKey(name string) string {
	if strings.Contains(name, ".") {
		return fmt.Sprintf("%q", name)
	}

	return name
}

var (
	headerRe   = regexp.MustCompile(`^\s*\[`)
	packagesRe = regexp.MustCompile(`^\s*\[\s*packages\s*\]`)
)

// edit replaces the line that defines name inside [packages] with line, adds
// line when name is new, and drops the line when line is empty.
func edit(path, name, line string) error {
	data, err := os.ReadFile(path)
	if err != nil && !errors.Is(err, fs.ErrNotExist) {
		return fmt.Errorf("read %s: %w", path, err)
	}

	lines := strings.Split(strings.TrimRight(string(data), "\n"), "\n")
	if len(data) == 0 {
		lines = nil
	}

	subTable := regexp.MustCompile(
		`^\s*\[\s*packages\s*\.\s*"?` + regexp.QuoteMeta(name) + `"?\s*\]`,
	)
	keyLine := regexp.MustCompile(`^\s*"?` + regexp.QuoteMeta(name) + `"?\s*=`)

	start, end, found := -1, len(lines), -1

	for i, text := range lines {
		switch {
		case subTable.MatchString(text):
			return fmt.Errorf(
				"%s defines %s as a [packages.%s] table, edit it by hand", path, name, name,
			)
		case packagesRe.MatchString(text):
			start = i
		case start >= 0 && end == len(lines) && headerRe.MatchString(text):
			end = i
		case start >= 0 && end == len(lines) && keyLine.MatchString(text):
			found = i
		}
	}

	switch {
	case found >= 0 && line == "":
		lines = append(lines[:found], lines[found+1:]...)
	case found >= 0:
		lines[found] = line
	case line == "":
		return nil
	case start < 0:
		if len(lines) > 0 {
			lines = append(lines, "")
		}

		lines = append(lines, "[packages]", line)
	default:
		// The new line goes after the last non-blank line of the section.
		at := end
		for at > start+1 && strings.TrimSpace(lines[at-1]) == "" {
			at--
		}

		lines = append(lines[:at], append([]string{line}, lines[at:]...)...)
	}

	return WriteFile(path, []byte(strings.Join(lines, "\n")+"\n"))
}

// WriteFile replaces path in one rename, so a crash leaves the old file or the
// new one.
func WriteFile(path string, data []byte) error {
	if err := os.MkdirAll(filepath.Dir(path), 0o755); err != nil {
		return err
	}

	tmp, err := os.CreateTemp(filepath.Dir(path), ".oku-*")
	if err != nil {
		return err
	}
	defer os.Remove(tmp.Name())

	_, err = tmp.Write(data)
	if closeErr := tmp.Close(); err == nil {
		err = closeErr
	}

	if err != nil {
		return err
	}

	if err := os.Chmod(tmp.Name(), 0o644); err != nil {
		return err
	}

	return os.Rename(tmp.Name(), path)
}
