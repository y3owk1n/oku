package cli

import (
	"regexp"
	"slices"
	"strings"

	"github.com/y3owk1n/oku/internal/profile"
)

const (
	// diffFlag makes a dry run print how the text of each file it would write
	// changes.
	diffFlag  = "diff"
	diffUsage = "with --dry-run, print how the text of each file would change, with secrets left out"
	// diffContext is how many unchanged lines a diff keeps around a change.
	diffContext = 2
	// diffCells caps the lines of the old text times those of the new, since
	// diffLines keeps a table of that size.
	diffCells = 4 << 20
)

// placeholders finds the place of each secret in a generation's text.
var placeholders = regexp.MustCompile("\x00oku-secret:[^\x00]*\x00")

// contentDiffs returns, by target, how the text of each file with content
// changes from the active generation to generation to. A file oku did not
// write before compares with nothing. A secret reads as {{secret.<name>}}, or
// {{secret}} for a file that is one secret, since a generation never holds its
// value.
func (e env) contentDiffs(to int) (map[string][]string, error) {
	prof := e.profile()

	before, err := prof.FilesWithContent(prof.Current())
	if err != nil {
		return nil, err
	}

	after, err := prof.FilesWithContent(to)
	if err != nil {
		return nil, err
	}

	diffs := map[string][]string{}

	for _, f := range after {
		if f.Content == "" {
			continue
		}

		var old []byte

		i := slices.IndexFunc(before, func(b profile.File) bool { return b.Target == f.Target && b.Content != "" })
		if i >= 0 {
			if before[i].Hash == f.Hash {
				continue
			}

			old = before[i].Text
		}

		if lines := textDiff(masked(old), masked(f.Text)); len(lines) > 0 {
			diffs[f.Target] = lines
		}
	}

	return diffs, nil
}

// masked returns text with each secret's placeholder written as the template
// names it.
func masked(text []byte) string {
	return placeholders.ReplaceAllStringFunc(string(text), func(p string) string {
		if name := strings.Trim(strings.TrimPrefix(p, "\x00oku-secret:"), "\x00"); name != inline {
			return "{{" + secretPrefix + name + "}}"
		}

		return "{{secret}}"
	})
}

// textDiff returns the lines that change from a to b, each after "+" or "-",
// with diffContext unchanged lines around each change after " ", and "..."
// where it skips lines.
func textDiff(a, b string) []string {
	split := func(s string) []string {
		if s == "" {
			return nil
		}

		return strings.Split(strings.TrimSuffix(s, "\n"), "\n")
	}

	old, now := split(a), split(b)
	if len(old)*len(now) > diffCells {
		return []string{"...the file is too long to compare line by line"}
	}

	ops := diffLines(old, now)

	// keep marks the lines within diffContext of a change.
	keep := make([]bool, len(ops))

	for i, d := range ops {
		if d.op == ' ' {
			continue
		}

		for j := max(0, i-diffContext); j <= min(len(ops)-1, i+diffContext); j++ {
			keep[j] = true
		}
	}

	var lines []string

	for i, d := range ops {
		switch {
		case !keep[i]:
			continue
		case i > 0 && !keep[i-1] && len(lines) > 0:
			lines = append(lines, "...")
		}

		lines = append(lines, string(d.op)+d.line)
	}

	return lines
}
