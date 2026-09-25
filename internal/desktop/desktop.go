// Package desktop reads the Linux desktop entries that a download ships, as
// share/applications/foo.desktop.
package desktop

import (
	"strings"
)

// Parse returns the keys of the [Desktop Entry] group of text. It leaves out
// localized keys such as Name[de].
func Parse(text string) map[string]string {
	fields := map[string]string{}
	inEntry := false

	for line := range strings.Lines(text) {
		line = strings.TrimSpace(line)

		switch {
		case line == "" || strings.HasPrefix(line, "#"):
		case strings.HasPrefix(line, "["):
			inEntry = line == "[Desktop Entry]"
		case inEntry:
			key, value, ok := strings.Cut(line, "=")
			key = strings.TrimSpace(key)

			if ok && !strings.Contains(key, "[") {
				fields[key] = strings.TrimSpace(value)
			}
		}
	}

	return fields
}

// Launches reports whether fields describe an app a desktop shows in its menu,
// which leaves out a terminal program and a hidden entry.
func Launches(fields map[string]string) bool {
	return fields["Type"] == "Application" && fields["Exec"] != "" &&
		fields["Terminal"] != "true" && fields["NoDisplay"] != "true" && fields["Hidden"] != "true"
}

// Program returns the program that an Exec value runs, without its arguments
// and quotes.
func Program(exec string) string {
	exec = strings.TrimSpace(exec)

	if rest, ok := strings.CutPrefix(exec, `"`); ok {
		program, _, _ := strings.Cut(rest, `"`)

		return program
	}

	program, _, _ := strings.Cut(exec, " ")

	return program
}
