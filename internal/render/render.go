// Package render fills the variables of the list into a text. A template has
// variables and nothing else: no conditionals, no loops.
package render

import (
	"fmt"
	"regexp"
	"strings"
)

// open starts a variable. A backslash before it writes it literally.
const open = "{{"

// name is what may stand between the braces. Spaces around it are allowed, which
// is how base16 templates are written.
var name = regexp.MustCompile(`^\s*([A-Za-z0-9][A-Za-z0-9_.-]*)\s*$`)

// Text replaces each {{name}} in text with vars[name]. origin names the text in
// an error, which also gives the line.
func Text(text string, vars map[string]string, origin string) (string, error) {
	var out strings.Builder

	rest := text

	for {
		at := strings.Index(rest, open)
		if at < 0 {
			out.WriteString(rest)

			return out.String(), nil
		}

		line := 1 + strings.Count(text[:len(text)-len(rest)+at], "\n")

		if at > 0 && rest[at-1] == '\\' {
			out.WriteString(rest[:at-1] + open)
			rest = rest[at+len(open):]

			continue
		}

		inner, after, closed := strings.Cut(rest[at+len(open):], "}}")

		match := name.FindStringSubmatch(inner)
		if !closed || match == nil {
			return "", fmt.Errorf(
				"%s:%d: {{ does not start a variable here, write \\{{ for the two braces themselves",
				origin,
				line,
			)
		}

		value, set := vars[match[1]]
		if !set {
			return "", fmt.Errorf("%s:%d: %s is not set in [vars]", origin, line, match[1])
		}

		out.WriteString(rest[:at] + value)
		rest = after
	}
}
