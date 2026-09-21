// Package list reads and edits oku.toml, the user's declared package list.
package list

import (
	"errors"
	"fmt"
	"io/fs"
	"maps"
	"os"
	"path/filepath"
	"regexp"
	"slices"
	"strconv"
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
	// Service enables the package's services, so they start now and at login.
	Service bool
	// System puts the package's apps, fonts and services in system scope, for
	// every user of the machine. Applying it needs administrator rights.
	System bool
}

// File is one entry of [files]: a path in the home directory that oku writes.
type File struct {
	// Target is the path as the list has it, starting with a location variable
	// such as {{home}}.
	Target string
	// Link is the path the target links to. Text is the content of the target,
	// and Render is the path of a template that gives the content. An entry has
	// one of the three, and HasText tells an empty text from none.
	Link    string
	Text    string
	HasText bool
	Render  string
	// Mode is the permission of a file with content. Zero means read-only.
	Mode fs.FileMode
	// When limits the entry to matching platforms. The zero value matches all.
	When platform.Selector
}

// List is a parsed oku.toml.
type List struct {
	// Include holds refs of other lists to merge under this one.
	Include  []string
	Packages map[string]Entry
	// Files is sorted by target.
	Files []File
	// Vars holds [vars]. A nested table becomes names joined by a dot, so
	// [vars.theme] with base00 is "theme.base00".
	Vars map[string]string
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
		Files    map[string]any `toml:"files"`
		Vars     map[string]any `toml:"vars"`
	}

	if err := toml.Unmarshal(data, &raw); err != nil {
		return nil, fmt.Errorf("parse %s: %w", origin, err)
	}

	l := &List{Include: raw.Include, Packages: map[string]Entry{}, Vars: map[string]string{}}

	if err := flattenVars(l.Vars, "", raw.Vars); err != nil {
		return nil, fmt.Errorf("%s: %w", origin, err)
	}

	for name, value := range raw.Packages {
		entry, err := toEntry(value)
		if err != nil {
			return nil, fmt.Errorf("%s: packages.%s: %w", origin, name, err)
		}

		l.Packages[name] = entry
	}

	for _, target := range slices.Sorted(maps.Keys(raw.Files)) {
		file, err := toFile(raw.Files[target])
		if err != nil {
			return nil, fmt.Errorf("%s: files.%q: %w", origin, target, err)
		}

		file.Target = target
		l.Files = append(l.Files, file)
	}

	return l, nil
}

// flattenVars adds the strings of table to vars, with prefix before each name.
func flattenVars(vars map[string]string, prefix string, table map[string]any) error {
	for key, value := range table {
		switch v := value.(type) {
		case string:
			vars[prefix+key] = v
		case map[string]any:
			if err := flattenVars(vars, prefix+key+".", v); err != nil {
				return err
			}
		default:
			return fmt.Errorf("vars.%s must be a string or a table of strings", prefix+key)
		}
	}

	return nil
}

// toFile reads the table of one [files] entry.
func toFile(value any) (File, error) {
	table, ok := value.(map[string]any)
	if !ok {
		return File{}, errors.New("want a table with link, text or render")
	}

	var f File

	for key, v := range table {
		text, isText := v.(string)

		switch key {
		case "link":
			f.Link = text
		case "text":
			f.Text, f.HasText = text, isText
		case "render":
			f.Render = text
		case "mode":
			mode, err := strconv.ParseUint(text, 8, 32)
			if err != nil || !isText || mode > 0o777 {
				return f, fmt.Errorf("mode %v is not a permission such as \"0600\"", v)
			}

			f.Mode = fs.FileMode(mode)
		case "when":
			continue
		default:
			return f, fmt.Errorf(
				"%s is not a key of a file, use link, text, render, mode or when", key,
			)
		}

		if !isText {
			return f, fmt.Errorf("%s must be a string", key)
		}
	}

	kinds := 0

	for _, set := range []bool{f.Link != "", f.HasText, f.Render != ""} {
		if set {
			kinds++
		}
	}

	switch {
	case kinds != 1:
		return f, errors.New("a file needs exactly one of link, text and render")
	case f.Link != "" && f.Mode != 0:
		return f, errors.New(
			"mode does not apply to a link, which has the permissions of its source",
		)
	}

	var err error

	f.When, err = toSelector(table["when"])

	return f, err
}

// toSelector reads a when table. A missing one matches every platform.
func toSelector(value any) (platform.Selector, error) {
	var sel platform.Selector

	when, _ := value.(map[string]any)
	for key, field := range map[string]*string{
		"os": &sel.OS, "arch": &sel.Arch, "libc": &sel.Libc,
	} {
		*field, _ = when[key].(string)
	}

	for key := range when {
		if key != "os" && key != "arch" && key != "libc" {
			return sel, fmt.Errorf("when.%s is not a selector key, use os, arch or libc", key)
		}
	}

	return sel, nil
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
		e.Service, _ = v["service"].(bool)
		e.System, _ = v["system"].(bool)

		if e.Ref == "" {
			return e, errors.New("ref is required")
		}

		var err error

		e.When, err = toSelector(v["when"])

		return e, err
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

	if entry.Version != "" || entry.Service || entry.System {
		fields := []string{fmt.Sprintf("ref = %q", entry.Ref)}

		if entry.Version != "" {
			fields = append(fields, fmt.Sprintf("version = %q", entry.Version))
		}

		if entry.Service {
			fields = append(fields, "service = true")
		}

		if entry.System {
			fields = append(fields, "system = true")
		}

		value = "{ " + strings.Join(fields, ", ") + " }"
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
