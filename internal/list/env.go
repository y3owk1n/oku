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

// toEnv reads [env].
func toEnv(raw map[string]any) (map[string]EnvValue, error) {
	env := map[string]EnvValue{}

	for name, value := range raw {
		v, err := toEnvValue(value)
		if err != nil {
			return nil, fmt.Errorf("env.%s: %w", name, err)
		}

		switch {
		case !manifest.ValidEnvName(name):
			return nil, fmt.Errorf("env.%s: a variable name is letters, digits and _", name)
		case manifest.ReservedEnv(name) && (name != "PATH" || v.Prepend == nil):
			return nil, fmt.Errorf(
				"env.%s: oku does not let a list set %s, which controls the shell. PATH takes { prepend = [...] }",
				name, name,
			)
		}

		env[name] = v
	}

	return env, nil
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
