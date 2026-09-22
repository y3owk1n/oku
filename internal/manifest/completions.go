package manifest

import (
	"errors"
	"fmt"
	"path"
	"slices"
	"strings"
)

// Shells are the shells a completions key covers, in the order oku generates
// them.
var Shells = []string{"fish", "zsh", "bash"}

// Completions is a "completions" key. It is a map of shell to file in the
// package, a directory that holds the conventional file of each shell, or a
// table with generate, a command that prints the completions of a shell.
type Completions struct {
	// Paths maps a shell to a file. A directory shorthand fills it with the
	// conventional file of every shell, and the store skips the ones the package
	// lacks.
	Paths map[string]string
	// FromDir reports that Paths came from a directory shorthand.
	FromDir bool
	// Generate is a command template. {{shell}} expands to each shell in turn.
	Generate string
	// Name names the program the files are for. Parse fills it from the first
	// bin entry unless the table sets name.
	Name string
}

// Empty reports whether the key was absent.
func (c Completions) Empty() bool {
	return len(c.Paths) == 0 && c.Generate == ""
}

// File is the conventional completion file name of shell for program name.
func File(shell, name string) string {
	switch shell {
	case "zsh":
		return "_" + name
	default:
		return name + "." + shell
	}
}

// parseCompletions reads a "completions" key. bins are the program names the
// package exposes, which name the files unless the table has name.
func parseCompletions(raw any, bins []string) (Completions, error) {
	var c Completions

	switch v := raw.(type) {
	case nil:
		return c, nil
	case string:
		c.FromDir = true
		c.Name = firstBin(bins)

		if c.Name == "" {
			return c, errors.New("completions: a directory needs bin to name the files")
		}

		c.Paths = map[string]string{}
		for _, shell := range Shells {
			c.Paths[shell] = path.Join(v, File(shell, c.Name))
		}

		return c, nil
	case map[string]any:
		paths := map[string]string{}

		for key, value := range v {
			text, ok := value.(string)
			if !ok {
				return c, fmt.Errorf("completions.%s: want a string", key)
			}

			switch key {
			case "generate":
				c.Generate = text
			case "name":
				c.Name = text
			default:
				paths[key] = text
			}
		}

		switch {
		case c.Generate != "" && len(paths) > 0:
			return c, errors.New(
				"completions: generate runs a command for every shell, so it takes no shell paths",
			)
		case c.Generate == "" && c.Name != "":
			return c, errors.New("completions: name goes with generate")
		case c.Generate == "" && len(paths) == 0:
			return c, errors.New("completions: set a shell path, or generate")
		case c.Generate != "":
			if strings.ContainsAny(c.Generate, "\r\n") {
				return c, errors.New("completions: generate must not hold a line break")
			}

			if c.Name == "" {
				c.Name = firstBin(bins)
			}

			switch {
			case c.Name == "":
				return c, errors.New("completions: generate needs bin, or name, to name the files")
			case !nameRe.MatchString(c.Name):
				return c, fmt.Errorf(
					"completions: name %q must be lowercase letters, digits, '.', '_' or '-'",
					c.Name,
				)
			}

			return c, nil
		default:
			c.Paths = paths

			return c, nil
		}
	default:
		return c, errors.New("completions: want a table of shell paths, a directory, or generate")
	}
}

func firstBin(bins []string) string {
	if len(bins) == 0 {
		return ""
	}

	return path.Base(bins[0])
}

// binNames lists the names that bin entries and wrappers expose, bins first.
func binNames(bins []string, wraps []Wrapper) []string {
	names := slices.Clone(bins)
	for _, w := range wraps {
		names = append(names, w.Name)
	}

	return names
}
