package list

import (
	"cmp"
	"errors"
	"fmt"
	"maps"
	"path/filepath"
	"regexp"
	"slices"
	"strings"

	"github.com/y3owk1n/oku/internal/manifest"
)

// EnvValue is one variable of [env]. It has one of a value, an unset, entries
// to prepend, or a required hint.
type EnvValue struct {
	// Value may name other variables as ${NAME} or ${NAME:-default}.
	Value string
	// Unset removes the variable.
	Unset bool
	// Prepend holds entries to put in front of a list variable, such as PATH. An
	// entry may name variables as Value does, and a relative one starts at the
	// list's directory.
	Prepend []string
	// Required is what to tell the user when nothing sets the variable.
	Required string
}

// isValue reports whether v sets a value, which may be empty.
func (v EnvValue) isValue() bool {
	return !v.Unset && v.Prepend == nil && v.Required == ""
}

// EnvFile is one entry of [[env.file]], a .env file to load.
type EnvFile struct {
	// Path starts at the list's directory when it is relative.
	Path string
	// Optional lets the file be missing.
	Optional bool
	// Unless names variables that skip the file when one of them is set and
	// not empty.
	Unless []string
	// Secret says the file is encrypted with sops or age.
	Secret bool
	// ExecOnly says only oku exec loads the file, and never the shell.
	ExecOnly bool
}

// toEnv reads [env]. Its key "file", when it holds tables, is [[env.file]].
func toEnv(raw map[string]any) (map[string]EnvValue, []EnvFile, error) {
	env := map[string]EnvValue{}

	var files []EnvFile

	for name, value := range raw {
		if tables, ok := value.([]any); ok && name == "file" {
			for i, table := range tables {
				f, err := toEnvFile(table)
				if err != nil {
					return nil, nil, fmt.Errorf("env.file[%d]: %w", i, err)
				}

				files = append(files, f)
			}

			continue
		}

		v, err := toEnvValue(value)
		if err != nil {
			return nil, nil, fmt.Errorf("env.%s: %w", name, err)
		}

		if err := CheckEnvName(name, v.Prepend != nil); err != nil {
			return nil, nil, fmt.Errorf("env.%w", err)
		}

		env[name] = v
	}

	return env, files, nil
}

// CheckEnvName fails when a list may not set name. prepend says whether the
// list only puts entries in front of it.
func CheckEnvName(name string, prepend bool) error {
	switch {
	case !manifest.ValidEnvName(name):
		return fmt.Errorf("%s: a variable name is letters, digits and _", name)
	case manifest.ReservedEnv(name) && (name != "PATH" || !prepend):
		return fmt.Errorf(
			"%s: oku does not let a list set %s, which controls the shell. PATH takes { prepend = [...] }",
			name, name,
		)
	}

	return nil
}

func toEnvFile(value any) (EnvFile, error) {
	table, ok := value.(map[string]any)
	if !ok {
		return EnvFile{}, errors.New("give a table with path")
	}

	var f EnvFile

	for key, field := range table {
		switch key {
		case "path":
			f.Path, _ = field.(string)
		case "optional":
			if f.Optional, ok = field.(bool); !ok {
				return EnvFile{}, errors.New("optional is true or false")
			}
		case "secret":
			if f.Secret, ok = field.(bool); !ok {
				return EnvFile{}, errors.New("secret is true or false")
			}
		case "scope":
			switch field {
			case "shell":
			case "exec":
				f.ExecOnly = true
			default:
				return EnvFile{}, errors.New(`scope is "shell", the default, or "exec"`)
			}
		case "unless":
			names, ok := field.([]any)
			if !ok {
				return EnvFile{}, errors.New("unless is a list of variable names")
			}

			for _, name := range names {
				s, ok := name.(string)
				if !ok || !manifest.ValidEnvName(s) {
					return EnvFile{}, errors.New("unless is a list of variable names")
				}

				f.Unless = append(f.Unless, s)
			}
		default:
			return EnvFile{}, fmt.Errorf("%s is not a key of env.file, which takes path, optional, unless, secret and scope", key)
		}
	}

	if f.Path == "" {
		return EnvFile{}, errors.New("path is the file to load, a string")
	}

	return f, nil
}

// Skipped reports whether one of the variables of Unless is set and not empty.
func (f EnvFile) Skipped(lookup func(string) (string, bool)) bool {
	for _, name := range f.Unless {
		if value, _ := lookup(name); value != "" {
			return true
		}
	}

	return false
}

// Abs returns the file's path, from dir when it is relative.
func (f EnvFile) Abs(dir string) string {
	if filepath.IsAbs(f.Path) {
		return f.Path
	}

	return filepath.Join(dir, filepath.FromSlash(f.Path))
}

func toEnvValue(value any) (EnvValue, error) {
	switch v := value.(type) {
	case string:
		return EnvValue{Value: v}, nil
	case bool:
		if v {
			return EnvValue{}, errors.New("true is no value, give a string, or false to unset the variable")
		}

		return EnvValue{Unset: true}, nil
	case map[string]any:
		var out EnvValue

		for key, field := range v {
			switch key {
			case "prepend":
				entries, ok := field.([]any)
				if !ok || len(entries) == 0 {
					return EnvValue{}, errors.New("prepend is a list of paths")
				}

				for _, entry := range entries {
					s, ok := entry.(string)
					if !ok || s == "" {
						return EnvValue{}, errors.New("prepend is a list of paths")
					}

					out.Prepend = append(out.Prepend, s)
				}
			case "required":
				hint, ok := field.(string)
				if !ok {
					return EnvValue{}, errors.New("required is what to tell the user, a string")
				}

				out.Required = cmp.Or(hint, "set it before you use this project")
			default:
				return EnvValue{}, fmt.Errorf("%s is not a key of an env table, which takes prepend or required", key)
			}
		}

		if (out.Prepend != nil) == (out.Required != "") {
			return EnvValue{}, errors.New("an env table takes prepend or required, one of them")
		}

		return out, nil
	default:
		return EnvValue{}, errors.New("give a string, false, { prepend = [...] } or { required = \"...\" }")
	}
}

// EnvChange is what the [env] of a list does to an environment.
type EnvChange struct {
	// Set holds the variables to export.
	Set map[string]string
	// Unset holds the variables to remove.
	Unset []string
	// Prepend holds, for each list variable, the entries to put in front of it,
	// as absolute paths.
	Prepend map[string][]string
	// Missing holds the required variables that nothing sets, with the hint for
	// each.
	Missing map[string]string
}

var referenceRe = regexp.MustCompile(`\$\{([A-Za-z_][A-Za-z0-9_]*)(:-([^}]*))?\}`)

// ResolveEnv works out what env, the [env] of the list in dir, does. lookup
// reads a variable as it is before the list applies. A value reads another
// variable of env as env sets it, in any order, and its own variable as lookup
// has it.
func ResolveEnv(env map[string]EnvValue, dir string, lookup func(string) (string, bool)) (EnvChange, error) {
	change := EnvChange{
		Set: map[string]string{}, Prepend: map[string][]string{}, Missing: map[string]string{},
	}

	resolved := map[string]string{}

	var (
		resolve func(name string, seen []string) (string, bool, error)
		expand  func(text string, seen []string) (string, error)
	)

	resolve = func(name string, seen []string) (string, bool, error) {
		v, own := env[name]

		// A value that names its own variable reads the value from before.
		self := len(seen) > 0 && seen[len(seen)-1] == name

		switch {
		case own && v.Unset:
			return "", false, nil
		case !own || !v.isValue() || self:
			value, ok := lookup(name)

			return value, ok, nil
		}

		if value, done := resolved[name]; done {
			return value, true, nil
		}

		if slices.Contains(seen, name) {
			return "", false, fmt.Errorf("env.%s: its value names itself through %s", name, strings.Join(seen, ", "))
		}

		value, err := expand(v.Value, slices.Concat(seen, []string{name}))
		if err != nil {
			return "", false, err
		}

		resolved[name] = value

		return value, true, nil
	}

	expand = func(text string, seen []string) (string, error) {
		var failed error

		value := referenceRe.ReplaceAllStringFunc(text, func(m string) string {
			ref := referenceRe.FindStringSubmatch(m)

			got, ok, err := resolve(ref[1], seen)
			failed = cmp.Or(failed, err)

			// ${NAME:-default} takes the default when NAME is unset or empty.
			if (!ok || got == "") && ref[2] != "" {
				return ref[3]
			}

			return got
		})

		return value, failed
	}

	for _, name := range slices.Sorted(maps.Keys(env)) {
		v := env[name]

		switch {
		case v.Unset:
			change.Unset = append(change.Unset, name)
		case v.Prepend != nil:
			for _, entry := range v.Prepend {
				entry, err := expand(entry, []string{name})
				if err != nil {
					return EnvChange{}, err
				}

				if !filepath.IsAbs(entry) {
					entry = filepath.Join(dir, filepath.FromSlash(entry))
				}

				change.Prepend[name] = append(change.Prepend[name], entry)
			}
		case v.Required != "":
			if value, ok := lookup(name); !ok || value == "" {
				change.Missing[name] = v.Required
			}
		default:
			value, _, err := resolve(name, nil)
			if err != nil {
				return EnvChange{}, err
			}

			change.Set[name] = value
		}
	}

	return change, nil
}
