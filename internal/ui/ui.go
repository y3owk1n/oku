// Package ui styles the text that oku prints. On a terminal that accepts colour
// it adds ANSI codes, unicode glyphs and column headers. Anywhere else, such as
// a pipe, a CI log or a test, every function returns its input unchanged, so
// the plain text a script reads never depends on the terminal.
package ui

import (
	"fmt"
	"io"
	"os"
	"strings"
	"unicode/utf8"

	"golang.org/x/term"
)

// Style renders text for one writer.
type Style struct {
	on bool
}

// For returns the Style of w. Colour is on when w is a terminal, TERM is not
// "dumb" and NO_COLOR is unset. FORCE_COLOR turns it on for any writer, so a
// pager or a screenshot script can ask for it.
func For(w io.Writer) Style {
	if os.Getenv("NO_COLOR") != "" {
		return Style{}
	}

	if os.Getenv("FORCE_COLOR") != "" {
		return Style{on: true}
	}

	file, ok := w.(*os.File)
	if !ok || !term.IsTerminal(int(file.Fd())) || os.Getenv("TERM") == "dumb" {
		return Style{}
	}

	return Style{on: true}
}

// On reports whether the style adds anything.
func (s Style) On() bool { return s.on }

const (
	reset  = "\x1b[0m"
	bold   = "\x1b[1m"
	dim    = "\x1b[2m"
	red    = "\x1b[31m"
	green  = "\x1b[32m"
	yellow = "\x1b[33m"
	blue   = "\x1b[34m"
	cyan   = "\x1b[36m"
)

func (s Style) wrap(code, text string) string {
	if !s.on || text == "" {
		return text
	}

	return code + text + reset
}

// Bold marks the main item of a line, such as a package name.
func (s Style) Bold(text string) string { return s.wrap(bold, text) }

// Dim is for context the reader can skip: paths, refs, hints, headers.
func (s Style) Dim(text string) string { return s.wrap(dim, text) }

// Accent marks a name, a number, or a value that matters.
func (s Style) Accent(text string) string { return s.wrap(cyan, text) }

// Good marks what worked or is new.
func (s Style) Good(text string) string { return s.wrap(green, text) }

// Warn marks what the user should look at, but need not act on.
func (s Style) Warn(text string) string { return s.wrap(yellow, text) }

// Bad marks a problem or a removal.
func (s Style) Bad(text string) string { return s.wrap(red, text) }

// Alert is the "oku:" that starts an error.
func (s Style) Alert(text string) string { return s.wrap(bold+red, text) }

// Heading is a section title, such as "Commands" in the help.
func (s Style) Heading(text string) string { return s.wrap(bold+blue, text) }

// Pick returns fancy on a styled terminal and plain anywhere else. Callers
// pass a unicode glyph and its ASCII stand-in.
func (s Style) Pick(fancy, plain string) string {
	if s.on {
		return fancy
	}

	return plain
}

// Glyphs, each one column wide, for the start of a line.
func (s Style) Check() string  { return s.Good(s.Pick("✓", "ok")) }
func (s Style) Cross() string  { return s.Bad(s.Pick("✗", "problem")) }
func (s Style) Note() string   { return s.Warn(s.Pick("!", "note")) }
func (s Style) Bullet() string { return s.Dim(s.Pick("•", "-")) }
func (s Style) Arrow() string  { return s.Pick("→", "->") }

// Home shortens the user's home directory to "~" on a styled terminal, where a
// path is read and not copied.
func (s Style) Home(path string) string {
	home, err := os.UserHomeDir()
	if !s.on || err != nil || home == "" || !strings.HasPrefix(path, home) {
		return path
	}

	rest := path[len(home):]
	if rest != "" && rest[0] != os.PathSeparator {
		return path
	}

	return "~" + rest
}

// Homes is Home for every path inside text.
func (s Style) Homes(text string) string {
	home, err := os.UserHomeDir()
	if !s.on || err != nil || home == "" {
		return text
	}

	return strings.ReplaceAll(text, home+string(os.PathSeparator), "~"+string(os.PathSeparator))
}

// Table lays out rows in aligned columns. A styled terminal gets a dim header
// row. Anywhere else the rows print with two spaces between columns and no
// header, as tabwriter did.
type Table struct {
	style  Style
	header []string
	rows   [][]cell
}

type cell struct {
	text  string
	style func(string) string
}

// Table starts a table with one column per name in header.
func (s Style) Table(header ...string) *Table {
	return &Table{style: s, header: header}
}

// Row adds a row of plain cells.
func (t *Table) Row(cells ...string) {
	row := make([]cell, len(cells))
	for i, text := range cells {
		row[i] = cell{text: text}
	}

	t.rows = append(t.rows, row)
}

// Styled adds a row and styles each cell with the function at its index. A nil
// function leaves that cell plain.
func (t *Table) Styled(cells []string, styles ...func(string) string) {
	row := make([]cell, len(cells))
	for i, text := range cells {
		row[i] = cell{text: text}
		if i < len(styles) && styles[i] != nil {
			row[i].style = styles[i]
		}
	}

	t.rows = append(t.rows, row)
}

// Write prints the table. A table with no rows prints nothing, not even the
// header, because a header over nothing reads as a bug.
func (t *Table) Write(w io.Writer) error {
	if len(t.rows) == 0 {
		return nil
	}

	widths := make([]int, len(t.header))

	grow := func(i, n int) {
		for len(widths) <= i {
			widths = append(widths, 0)
		}

		widths[i] = max(widths[i], n)
	}

	if t.style.on {
		for i, h := range t.header {
			grow(i, utf8.RuneCountInString(h))
		}
	}

	for _, row := range t.rows {
		for i, c := range row {
			grow(i, utf8.RuneCountInString(c.text))
		}
	}

	var b strings.Builder

	if t.style.on && len(t.header) > 0 {
		for i, h := range t.header {
			b.WriteString(t.style.Dim(pad(strings.ToUpper(h), widths[i], i == len(t.header)-1)))
		}

		b.WriteByte('\n')
	}

	for _, row := range t.rows {
		for i, c := range row {
			text := pad(c.text, widths[i], i == len(row)-1)
			if c.style != nil {
				// Style the text, not the padding, so the codes never widen a column.
				text = c.style(strings.TrimRight(text, " ")) + text[len(strings.TrimRight(text, " ")):]
			}

			b.WriteString(text)
		}

		b.WriteByte('\n')
	}

	_, err := io.WriteString(w, b.String())

	return err
}

// pad widens text to width and adds the column gap, except on the last column.
func pad(text string, width int, last bool) string {
	if last {
		return text
	}

	return text + strings.Repeat(" ", width-utf8.RuneCountInString(text)+2)
}

// KV prints label and value pairs, the labels aligned and dim. A pair with an
// empty value is left out.
func (s Style) KV(w io.Writer, pairs ...[2]string) error {
	width := 0

	for _, p := range pairs {
		if p[1] != "" {
			width = max(width, utf8.RuneCountInString(p[0]))
		}
	}

	var b strings.Builder

	for _, p := range pairs {
		if p[1] == "" {
			continue
		}

		fmt.Fprintf(&b, "%s  %s\n", s.Dim(fmt.Sprintf("%-*s", width, p[0])), p[1])
	}

	_, err := io.WriteString(w, b.String())

	return err
}
