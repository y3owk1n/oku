// Package list reads and edits oku.toml, the user's declared package list.
package list

import (
	"bytes"
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
	"time"

	"github.com/pelletier/go-toml/v2"

	"github.com/y3owk1n/oku/internal/host"
	"github.com/y3owk1n/oku/internal/manifest"
	"github.com/y3owk1n/oku/internal/platform"
	"github.com/y3owk1n/oku/internal/settings"
)

// FileName is the list's file name.
const FileName = "oku.toml"

// Entry is one package of a list.
type Entry struct {
	Ref     string
	Version string
	// When limits the package to matching platforms. The empty When matches all.
	When platform.When
	// Service enables the package's services, so they start now and at login.
	Service bool
	// System puts the package's apps, fonts and services in system scope, for
	// every user of the machine. Applying it needs administrator rights.
	System bool
	// RunAs is "root" for a system service that must run as root. Empty runs it
	// as the user, since its files are in the store that the user owns.
	RunAs string
	// Asset and Bins are the "--asset" and "--bin" oku infers the package's
	// manifest with.
	Asset string
	Bins  []string
	// MinReleaseAge replaces the list's minimum release age for this package, as
	// ParseAge reads it. Empty keeps the list's.
	MinReleaseAge string
	// SigningKey and SignerWorkflow pin who signs the package, in place of what
	// its manifest names at the first install.
	SigningKey     string
	SignerWorkflow string
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
	// Secret is the path of an encrypted file whose value is the content, and Key
	// the path of one value in it.
	Secret string
	Key    string
	// Mode is the permission of a file with content. Zero means read-only.
	Mode fs.FileMode
	// When limits the entry to matching platforms. The empty When matches all.
	When platform.When
	// Vars overrides [vars] for a text or a render entry.
	Vars map[string]string
}

// Secret is one entry of [secrets]: a name for one encrypted value.
type Secret struct {
	// File is the path of a sops or an age file.
	File string
	// Key is the path of one value in a sops file, with "/" between its parts.
	Key string
}

// Setting is one key of a settings table, such as [defaults."com.apple.dock"].
type Setting struct {
	// Backend is the table's name, which is the mechanism of one OS: "defaults",
	// "registry" or "dconf".
	Backend string
	Domain  string
	Key     string
	Value   any
}

// Backends are the settings tables, each for one OS.
var Backends = map[string]string{"defaults": "darwin", "registry": "windows", "dconf": "linux"}

// List is a parsed oku.toml.
type List struct {
	// Include holds refs of other lists to merge under this one.
	Include  []string
	Packages map[string]Entry
	// Files is sorted by target.
	Files []File
	// Settings is sorted by backend, domain and key.
	Settings []Setting
	// Secrets holds [secrets] by name.
	Secrets map[string]Secret
	// Vars holds [vars]. A nested table becomes names joined by a dot, so
	// [vars.theme] with base00 is "theme.base00".
	Vars map[string]string
	// LockPlatforms holds the platforms of [lock], which oku.lock pins every
	// package for besides the host.
	LockPlatforms []platform.Platform
	// MinReleaseAge is [lock] min_release_age, as ParseAge reads it. Empty means
	// DefaultReleaseAge.
	MinReleaseAge string
	// UnknownReleaseAge is [lock] unknown_release_age: what oku does with a
	// version whose source gives no release time. Empty means UnknownWarn.
	UnknownReleaseAge string
	// Unverified is [lock] unverified: what oku does with a download that
	// nothing states a digest for, which it would trust on first use. It takes
	// the values of UnknownReleaseAge. Empty means UnknownAllow.
	Unverified string
	// Env holds [env], the variables the list sets in the shell and for oku exec.
	Env map[string]EnvValue
	// EnvFiles holds [[env.file]], the .env files the list loads before Env, in
	// order.
	EnvFiles []EnvFile
	// Runtimes maps an interpreter, such as "node", to the package that
	// provides it, for the packages that run through one. A version constraint
	// limits which versions of that package oku picks.
	Runtimes map[string]manifest.Dep
	// Host holds [host] by name: what the machine must have that oku does not
	// install.
	Host map[string]host.Requirement
}

// Empty reports whether l holds nothing: no include, package, file, setting,
// secret, variable, [lock] key, [env], runtime or [host] entry.
func (l *List) Empty() bool {
	return len(l.Include) == 0 && len(l.Packages) == 0 && len(l.Files) == 0 && len(l.Settings) == 0 &&
		len(l.Secrets) == 0 && len(l.Vars) == 0 && len(l.LockPlatforms) == 0 && l.MinReleaseAge == "" &&
		l.UnknownReleaseAge == "" && l.Unverified == "" && len(l.Env) == 0 && len(l.EnvFiles) == 0 &&
		len(l.Runtimes) == 0 && len(l.Host) == 0
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
		Secrets  map[string]any `toml:"secrets"`
		Defaults map[string]any `toml:"defaults"`
		// ThisMac holds the macOS preferences of this one Mac, which go to the same
		// store as Defaults with a marked domain.
		ThisMac  map[string]any `toml:"defaults-currenthost"`
		Registry map[string]any `toml:"registry"`
		Dconf    map[string]any `toml:"dconf"`
		Lock     map[string]any `toml:"lock"`
		Runtimes map[string]any `toml:"runtimes"`
		Env      map[string]any `toml:"env"`
		Host     map[string]any `toml:"host"`
	}

	// A misspelt table would be left out, and [package] for [packages] would
	// empty the list, so a sync would remove every package.
	decoder := toml.NewDecoder(bytes.NewReader(data))
	decoder.DisallowUnknownFields()

	var strict *toml.StrictMissingError
	if err := decoder.Decode(&raw); errors.As(err, &strict) {
		var keys []string
		for _, missing := range strict.Errors {
			row, _ := missing.Position()
			keys = append(keys, fmt.Sprintf("line %d: unknown table or key %s", row, strings.Join(missing.Key(), ".")))
		}

		return nil, fmt.Errorf("%s: %s", origin, strings.Join(keys, ", "))
	} else if err != nil {
		return nil, fmt.Errorf("parse %s: %s%w", origin, position(err), err)
	}

	l := &List{
		Include: raw.Include, Packages: map[string]Entry{}, Vars: map[string]string{},
		Runtimes: map[string]manifest.Dep{},
	}

	for name, value := range raw.Runtimes {
		d, err := manifest.ParseDep(value)
		if err != nil {
			return nil, fmt.Errorf("%s: runtimes.%s: %w", origin, name, err)
		}

		l.Runtimes[name] = d
	}

	if err := flattenVars(l.Vars, "", raw.Vars); err != nil {
		return nil, fmt.Errorf("%s: %w", origin, err)
	}

	var err error
	if l.Env, l.EnvFiles, err = toEnv(raw.Env); err != nil {
		return nil, fmt.Errorf("%s: %w", origin, err)
	}

	if err = l.readLock(raw.Lock); err != nil {
		return nil, fmt.Errorf("%s: %w", origin, err)
	}

	for name, value := range raw.Secrets {
		table, _ := value.(map[string]any)

		var secret Secret

		secret.File, _ = table["file"].(string)
		secret.Key, _ = table["key"].(string)

		if secret.File == "" || len(table) > 2 || (len(table) == 2 && secret.Key == "") {
			return nil, fmt.Errorf(
				"%s: secrets.%s wants a table with file, and key when needed",
				origin,
				name,
			)
		}

		if l.Secrets == nil {
			l.Secrets = map[string]Secret{}
		}

		l.Secrets[name] = secret
	}

	for name, value := range raw.Host {
		r, err := host.Parse(name, value)
		if err != nil {
			return nil, fmt.Errorf("%s: %w", origin, err)
		}

		if l.Host == nil {
			l.Host = map[string]host.Requirement{}
		}

		l.Host[name] = r
	}

	for name, value := range raw.Packages {
		entry, err := toEntry(value)
		if err != nil {
			return nil, fmt.Errorf("%s: packages.%s: %w", origin, name, err)
		}

		l.Packages[name] = entry
	}

	thisMac := map[string]any{}
	for domain, keys := range raw.ThisMac {
		thisMac[settings.CurrentHost+domain] = keys
	}

	for _, table := range []struct {
		backend string
		domains map[string]any
	}{
		{"defaults", raw.Defaults},
		{"defaults", thisMac},
		{"registry", raw.Registry},
		{"dconf", raw.Dconf},
	} {
		backend, domains := table.backend, table.domains

		settings, err := toSettings(backend, domains)
		if err != nil {
			return nil, fmt.Errorf("%s: %w", origin, err)
		}

		l.Settings = append(l.Settings, settings...)
	}

	slices.SortFunc(l.Settings, func(a, b Setting) int {
		return strings.Compare(
			a.Backend+"\x00"+a.Domain+"\x00"+a.Key,
			b.Backend+"\x00"+b.Domain+"\x00"+b.Key,
		)
	})

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

// DefaultReleaseAge is the minimum release age of a list that sets none.
const DefaultReleaseAge = 24 * time.Hour

// ParseAge reads a minimum release age: a whole number of hours, days or weeks,
// such as 12h, 1d or 2w, or 0 for none.
func ParseAge(text string) (time.Duration, error) {
	if text == "0" {
		return 0, nil
	}

	units := map[string]time.Duration{"h": time.Hour, "d": 24 * time.Hour, "w": 7 * 24 * time.Hour}

	n, err := strconv.Atoi(text[:max(len(text)-1, 0)])
	unit, ok := units[text[max(len(text)-1, 0):]]

	if err != nil || !ok || n < 1 {
		return 0, fmt.Errorf("want a number of hours, days or weeks, such as 12h, 1d or 2w, or 0, got %q", text)
	}

	return time.Duration(n) * unit, nil
}

// What oku does with a version whose source gives no release time, as
// [lock] unknown_release_age names it, and with a download it would trust on
// first use, as [lock] unverified names it.
const (
	// UnknownAllow takes the version, or trusts the download, and says so.
	UnknownAllow = "allow"
	// UnknownWarn asks on a terminal, and refuses without one.
	UnknownWarn = "warn"
	// UnknownRefuse refuses the version or the download.
	UnknownRefuse = "refuse"
)

// readLock reads the [lock] table into l: its platforms, min_release_age,
// unknown_release_age and unverified.
func (l *List) readLock(table map[string]any) error {
	keys := []string{"platforms", "min_release_age", "unknown_release_age", "unverified"}
	for key := range table {
		if !slices.Contains(keys, key) {
			return fmt.Errorf(
				"lock.%s is not a key of [lock], use platforms, min_release_age, unknown_release_age or unverified",
				key,
			)
		}
	}

	age, ok := table["min_release_age"].(string)
	if !ok && table["min_release_age"] != nil {
		return errors.New("lock.min_release_age wants a string such as \"1d\"")
	}

	if age != "" {
		if _, err := ParseAge(age); err != nil {
			return fmt.Errorf("lock.min_release_age: %w", err)
		}
	}

	for _, key := range []string{"unknown_release_age", "unverified"} {
		value, _ := table[key].(string)
		if table[key] != nil && !slices.Contains([]string{UnknownAllow, UnknownWarn, UnknownRefuse}, value) {
			return fmt.Errorf("lock.%s wants \"allow\", \"warn\" or \"refuse\", got %v", key, table[key])
		}
	}

	names, ok := table["platforms"].([]any)
	if !ok && table["platforms"] != nil {
		return errors.New("lock.platforms wants an array of platform names")
	}

	l.LockPlatforms = make([]platform.Platform, 0, len(names))

	for _, name := range names {
		text, _ := name.(string)

		p, err := platform.Parse(text)
		if err != nil {
			return fmt.Errorf("lock.platforms: %w", err)
		}

		l.LockPlatforms = append(l.LockPlatforms, p)
	}

	l.MinReleaseAge = age
	l.UnknownReleaseAge, _ = table["unknown_release_age"].(string)
	l.Unverified, _ = table["unverified"].(string)

	return nil
}

// toSettings reads the domains of one settings table.
func toSettings(backend string, domains map[string]any) ([]Setting, error) {
	var out []Setting

	for domain, value := range domains {
		keys, ok := value.(map[string]any)
		if !ok {
			return nil, fmt.Errorf("%s.%q must be a table of keys", backend, domain)
		}

		// A domain that is a path would let a list edit any plist, such as another
		// app's launch agent, which oku did not place.
		if name := strings.TrimPrefix(domain, settings.CurrentHost); backend == "defaults" &&
			(strings.ContainsAny(name, `/\`) || strings.HasPrefix(name, "~")) {
			return nil, fmt.Errorf("defaults.%q must be a domain such as com.apple.dock, not a path", domain)
		}

		// oku writes settings of the current user only.
		if backend == "registry" && !strings.HasPrefix(strings.ToUpper(domain), `HKCU\`) &&
			!strings.HasPrefix(strings.ToUpper(domain), `HKEY_CURRENT_USER\`) {
			return nil, fmt.Errorf(
				"registry.%q is outside HKCU, and oku writes settings of the current user only",
				domain,
			)
		}

		for key, v := range keys {
			out = append(
				out,
				Setting{Backend: backend, Domain: domain, Key: key, Value: v},
			)
		}
	}

	return out, nil
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
		return File{}, errors.New("want a table with link, text, render or secret")
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
		case "secret":
			f.Secret = text
		case "key":
			f.Key = text
		case "mode":
			mode, err := strconv.ParseUint(text, 8, 32)
			if err != nil || !isText || mode > 0o777 {
				return f, fmt.Errorf("mode %v is not a permission such as \"0600\"", v)
			}

			f.Mode = fs.FileMode(mode)
		case "when":
			continue
		case "vars":
			f.Vars = map[string]string{}

			table, isTable := v.(map[string]any)
			if !isTable {
				return f, errors.New("vars wants a table of strings")
			}

			for name, value := range table {
				text, isText := value.(string)
				if !isText {
					return f, fmt.Errorf("vars.%s must be a string", name)
				}

				f.Vars[name] = text
			}

			continue
		default:
			return f, fmt.Errorf(
				"%s is not a key of a file, use link, text, render, secret, key, mode, when or vars",
				key,
			)
		}

		if !isText {
			return f, fmt.Errorf("%s must be a string", key)
		}
	}

	kinds := 0

	for _, set := range []bool{f.Link != "", f.HasText, f.Render != "", f.Secret != ""} {
		if set {
			kinds++
		}
	}

	switch {
	case kinds != 1:
		return f, errors.New("a file needs exactly one of link, text, render and secret")
	case f.Key != "" && f.Secret == "":
		return f, errors.New("key names a value of a secret, so it needs secret")
	case f.Link != "" && f.Mode != 0:
		return f, errors.New(
			"mode does not apply to a link, which has the permissions of its source",
		)
	case f.Vars != nil && (f.Link != "" || f.Secret != ""):
		return f, errors.New("vars applies to text and render, which fill in variables")
	}

	var err error

	f.When, err = platform.ParseWhen(table["when"])

	return f, err
}

// toEntry accepts the short form "ref" and the table form { ref, version, when, ... }.
// entryKeys are the keys of a package entry, each with the Go type its value
// must have, or "" for one that toEntry checks itself.
var entryKeys = map[string]string{
	"ref": "string", "version": "string", "service": "bool", "system": "bool", "asset": "string",
	"run_as": "string", "min_release_age": "", "signing_key": "string", "signer_workflow": "string",
	"bin": "", "when": "",
}

func toEntry(value any) (Entry, error) {
	switch v := value.(type) {
	case string:
		return Entry{Ref: v}, nil
	case map[string]any:
		var e Entry

		// A misspelt or mistyped key would be left out, and with it a pin the user
		// asked for, such as version or signing_key.
		for _, key := range slices.Sorted(maps.Keys(v)) {
			want, known := entryKeys[key]

			switch {
			case !known:
				return e, fmt.Errorf("unknown key %s, an entry takes %s", key, strings.Join(slices.Sorted(maps.Keys(entryKeys)), ", "))
			case want != "" && fmt.Sprintf("%T", v[key]) != want:
				return e, fmt.Errorf("%s wants a %s", key, map[string]string{"string": "string", "bool": "true or false"}[want])
			}
		}

		e.Ref, _ = v["ref"].(string)
		e.Version, _ = v["version"].(string)
		e.Service, _ = v["service"].(bool)
		e.System, _ = v["system"].(bool)
		e.Asset, _ = v["asset"].(string)
		e.RunAs, _ = v["run_as"].(string)

		if e.Ref == "" {
			return e, errors.New("ref is required")
		}

		if e.RunAs != "" && e.RunAs != "root" {
			return e, fmt.Errorf("run_as is %q, and the one value it takes is \"root\"", e.RunAs)
		}

		age, ok := v["min_release_age"].(string)
		if !ok && v["min_release_age"] != nil {
			return e, errors.New("min_release_age wants a string such as \"1d\" or \"0\"")
		}

		if age != "" {
			if _, err := ParseAge(age); err != nil {
				return e, fmt.Errorf("min_release_age: %w", err)
			}
		}

		e.MinReleaseAge = age

		e.SigningKey, _ = v["signing_key"].(string)
		if e.SigningKey != "" {
			if err := manifest.CheckSigningKey(e.SigningKey); err != nil {
				return e, fmt.Errorf("signing_key %w", err)
			}
		}

		e.SignerWorkflow, _ = v["signer_workflow"].(string)
		if e.SignerWorkflow != "" {
			if err := manifest.CheckSignerWorkflow(e.SignerWorkflow); err != nil {
				return e, fmt.Errorf("signer_workflow %w", err)
			}
		}

		bins, ok := v["bin"].([]any)
		if !ok && v["bin"] != nil {
			return e, errors.New("bin wants an array of program names")
		}

		for _, bin := range bins {
			name, ok := bin.(string)
			if !ok {
				return e, errors.New("bin wants an array of program names")
			}

			e.Bins = append(e.Bins, name)
		}

		var err error

		e.When, err = platform.ParseWhen(v["when"])

		return e, err
	default:
		return Entry{}, errors.New("want a ref string or a table with ref")
	}
}

// Set writes entry under name in the list at path. It edits the file's text, so
// the user's comments and ordering stay.
func Set(path, name string, entry Entry) error {
	return edit(path, name, Line(name, entry))
}

// Delete removes name from the list at path.
func Delete(path, name string) error {
	return edit(path, name, "")
}

// Line is the line of oku.toml that declares entry under name.
func Line(name string, entry Entry) string {
	value := fmt.Sprintf("%q", entry.Ref)

	if entry.Version != "" || entry.Service || entry.System || entry.RunAs != "" ||
		len(entry.When) > 0 || entry.Asset != "" || len(entry.Bins) > 0 || entry.MinReleaseAge != "" ||
		entry.SigningKey != "" || entry.SignerWorkflow != "" {
		fields := []string{fmt.Sprintf("ref = %q", entry.Ref)}

		if entry.Version != "" {
			fields = append(fields, fmt.Sprintf("version = %q", entry.Version))
		}

		if entry.Asset != "" {
			fields = append(fields, fmt.Sprintf("asset = %q", entry.Asset))
		}

		if len(entry.Bins) > 0 {
			quoted := make([]string, len(entry.Bins))
			for i, bin := range entry.Bins {
				quoted[i] = fmt.Sprintf("%q", bin)
			}

			fields = append(fields, "bin = ["+strings.Join(quoted, ", ")+"]")
		}

		if entry.Service {
			fields = append(fields, "service = true")
		}

		if entry.System {
			fields = append(fields, "system = true")
		}

		if entry.RunAs != "" {
			fields = append(fields, fmt.Sprintf("run_as = %q", entry.RunAs))
		}

		if len(entry.When) > 0 {
			fields = append(fields, "when = "+entry.When.TOML())
		}

		if entry.MinReleaseAge != "" {
			fields = append(fields, fmt.Sprintf("min_release_age = %q", entry.MinReleaseAge))
		}

		if entry.SigningKey != "" {
			fields = append(fields, fmt.Sprintf("signing_key = %q", entry.SigningKey))
		}

		if entry.SignerWorkflow != "" {
			fields = append(fields, fmt.Sprintf("signer_workflow = %q", entry.SignerWorkflow))
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

// WriteFile replaces path in one rename, so a crash or a power loss leaves the
// old file or the new one. A symlink at path stays, and the file it points at
// gets the data, as for an oku.toml that lives in a dotfiles repo. The file
// keeps its mode, and a new one gets 0644.
func WriteFile(path string, data []byte) error {
	mode := fs.FileMode(0o644)

	if target, err := filepath.EvalSymlinks(path); err == nil {
		path = target
	} else if link, linkErr := os.Readlink(path); linkErr == nil {
		// A link to a file that does not exist yet.
		if !filepath.IsAbs(link) {
			link = filepath.Join(filepath.Dir(path), link)
		}

		path = link
	}

	if info, err := os.Stat(path); err == nil {
		mode = info.Mode().Perm()
	}

	if err := os.MkdirAll(filepath.Dir(path), 0o755); err != nil {
		return err
	}

	tmp, err := os.CreateTemp(filepath.Dir(path), ".oku-*")
	if err != nil {
		return err
	}
	defer os.Remove(tmp.Name())

	_, err = tmp.Write(data)
	if err == nil {
		err = tmp.Sync()
	}

	if closeErr := tmp.Close(); err == nil {
		err = closeErr
	}

	if err != nil {
		return err
	}

	if err := os.Chmod(tmp.Name(), mode); err != nil {
		return err
	}

	if err := os.Rename(tmp.Name(), path); err != nil {
		return err
	}

	// The rename lasts a power loss once the directory is on disk. Windows cannot
	// open a directory for this.
	if dir, err := os.Open(filepath.Dir(path)); err == nil {
		_ = dir.Sync()
		dir.Close()
	}

	return nil
}

// ReadOverlay parses the list at path that sets variables over a project's
// oku.toml, such as oku.local.toml. It holds [env] and nothing else.
func ReadOverlay(path string) (*List, error) {
	data, err := os.ReadFile(path)
	if err != nil {
		return nil, fmt.Errorf("read %s: %w", path, err)
	}

	var raw map[string]any
	if err := toml.Unmarshal(data, &raw); err != nil {
		return nil, fmt.Errorf("parse %s: %s%w", path, position(err), err)
	}

	for key := range raw {
		if key != "env" {
			return nil, fmt.Errorf("%s holds %s, and a list over oku.toml holds [env] alone", path, key)
		}
	}

	return Parse(data, path)
}

// position returns "line N, column M: " for a TOML syntax error, so the user
// finds the mistake, or "" for any other error.
func position(err error) string {
	var syntax *toml.DecodeError
	if !errors.As(err, &syntax) {
		return ""
	}

	row, column := syntax.Position()

	return fmt.Sprintf("line %d, column %d: ", row, column)
}
