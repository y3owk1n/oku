// Package ui styles the text that oku prints. On a terminal that accepts colour
// it adds ANSI codes, unicode glyphs and column headers. Anywhere else, such as
// a pipe, a CI log or a test, every function returns its input unchanged, so
// the plain text a script reads never depends on the terminal.
package ui

import (
	"fmt"
	"io"
	"os"
	"strconv"
	"strings"
	"unicode/utf8"

	"golang.org/x/term"
)

// Style renders text for one writer.
type Style struct {
	on bool
	// width is the columns of the terminal, and 0 when w is not one.
	width int
}

// defaultWidth stands in when a styled writer reports no width, such as a
// buffer under FORCE_COLOR.
const defaultWidth = 80

// For returns the Style of w. Colour is on when w is a terminal, TERM is not
// "dumb" and NO_COLOR is unset. FORCE_COLOR turns it on for any writer, so a
// pager or a screenshot script can ask for it, and COLUMNS then sets the width.
func For(w io.Writer) Style {
	if os.Getenv("NO_COLOR") != "" {
		return Style{}
	}

	file, ok := w.(*os.File)
	if ok && term.IsTerminal(int(file.Fd())) && os.Getenv("TERM") != "dumb" {
		width, _, err := term.GetSize(int(file.Fd()))
		if err != nil || width <= 0 {
			width = defaultWidth
		}

		return Style{on: true, width: width}
	}

	if os.Getenv("FORCE_COLOR") != "" {
		width, err := strconv.Atoi(os.Getenv("COLUMNS"))
		if err != nil || width <= 0 {
			width = defaultWidth
		}

		return Style{on: true, width: width}
	}

	return Style{}
}

// On reports whether the style adds anything.
func (s Style) On() bool { return s.on }

// Width is the columns of the terminal, or 0 when the writer is not one.
func (s Style) Width() int { return s.width }

// Lines counts the rows text takes on the terminal, where a line longer than
// the width wraps. Escape codes take no room. Off a terminal it counts the
// newlines.
func (s Style) Lines(text string) int {
	n := 0

	for _, line := range strings.Split(strings.TrimSuffix(text, "\n"), "\n") {
		n++

		if s.width > 0 {
			n += (visible(line) - 1) / s.width
		}
	}

	return n
}

// Wrap fits each line of text to the terminal's width. Rows after the first
// start indent columns in, so they line up under the text after a glyph or a
// prefix. Off a terminal it returns text unchanged.
func (s Style) Wrap(text string, indent int) string {
	if s.width <= 0 {
		return text
	}

	var b strings.Builder

	for i, line := range strings.Split(text, "\n") {
		if i > 0 {
			b.WriteByte('\n')
		}

		lead := len(line) - len(strings.TrimLeft(line, " "))
		pad := strings.Repeat(" ", max(lead, indent))

		rows := wrapAt(line[lead:], max(s.width-lead, minLast), max(s.width-len(pad), minLast))

		for j, row := range rows {
			if j == 0 {
				b.WriteString(line[:lead] + row)
			} else {
				b.WriteString("\n" + pad + row)
			}
		}
	}

	return b.String()
}

// Erase moves the cursor up n lines and clears them, so that a prompt the user
// answered does not stay on screen. Off a terminal it does nothing.
func (s Style) Erase(w io.Writer, n int) {
	if !s.on || n <= 0 {
		return
	}

	fmt.Fprint(w, strings.Repeat("\x1b[1A\x1b[2K", n)+"\r")
}

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

// Code shows each `command` in text in the accent colour and drops its
// backticks. Off a terminal the backticks stay.
func (s Style) Code(text string) string {
	if !s.on {
		return text
	}

	// A backtick with no partner opens no command, so the text stays.
	parts := strings.Split(text, "`")
	if len(parts)%2 == 0 {
		return text
	}

	for i := 1; i < len(parts); i += 2 {
		parts[i] = s.Accent(parts[i])
	}

	return strings.Join(parts, "")
}

// Pick returns fancy on a styled terminal and plain anywhere else. Callers
// pass a unicode glyph and its ASCII stand-in.
func (s Style) Pick(fancy, plain string) string {
	if s.on {
		return fancy
	}

	return plain
}

// Glyphs, each one column wide, for the start of a line.
func (s Style) Check() string { return s.Good(s.Pick("✓", "ok")) }
func (s Style) Cross() string { return s.Bad(s.Pick("✗", "problem")) }
func (s Style) Note() string  { return s.Warn(s.Pick("!", "note")) }

// Done marks a line for something that finished, the way a package manager's
// install log does: a green check in front on a terminal, the text alone
// anywhere else.
func (s Style) Done(text string) string {
	if !s.on {
		return text
	}

	return s.Good("✓") + " " + text
}

// Gone marks a line for something oku removed, with a red minus in front
// on a terminal.
func (s Style) Gone(text string) string {
	if !s.on {
		return text
	}

	return s.Bad("-") + " " + text
}
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

const (
	// gap is the columns between two columns of a table.
	gap = 2
	// stackBelow is the terminal width under which a table prints one record
	// per block, because its columns cannot fit side by side.
	stackBelow = 60
	// minColumn is the narrowest a cut column gets.
	minColumn = 8
	// minLast is the narrowest the wrapped last column gets.
	minLast = 16
	// wrapFirst is the width the last column gives down to before any other
	// column is cut, enough for a path or a short phrase on one line.
	wrapFirst = 28
)

// Write prints the table. A table with no rows prints nothing, not even the
// header, because a header over nothing reads as a bug. On a terminal the
// table fits its width: the last column wraps, the others are cut with an
// ellipsis when they must give room, and a table that does not fit under
// stackBelow columns prints each row as a block of label and value lines.
func (t *Table) Write(w io.Writer) error {
	if len(t.rows) == 0 {
		return nil
	}

	if t.style.on {
		t.dropEmpty()
	}

	if t.Stacked() {
		return t.writeStacked(w)
	}

	widths := t.widths()

	if t.style.width > 0 {
		t.fit(widths)
	}

	var b strings.Builder

	if t.style.on && len(t.header) > 0 {
		cells := make([]cell, len(t.header))
		for i, h := range t.header {
			cells[i] = cell{text: strings.ToUpper(h), style: t.style.Dim}
		}

		b.WriteString(t.render(cells, widths))
	}

	for _, row := range t.rows {
		b.WriteString(t.render(row, widths))
	}

	_, err := io.WriteString(w, b.String())

	return err
}

// Stacked reports whether Write prints each row as a block, so that a caller
// can set a footer apart from the last block.
func (t *Table) Stacked() bool {
	widths := t.widths()
	if t.style.width <= 0 || t.style.width >= stackBelow || len(widths) <= 2 {
		return false
	}

	total := gap * (len(widths) - 1)
	for _, w := range widths {
		total += w
	}

	return total > t.style.width
}

// dropEmpty takes away the columns at the end that are empty in every row,
// so that no row ends in padding.
func (t *Table) dropEmpty() {
	keep := 0

	for _, row := range t.rows {
		for i, c := range row {
			if c.text != "" {
				keep = max(keep, i+1)
			}
		}
	}

	t.header = t.header[:min(keep, len(t.header))]
	for i, row := range t.rows {
		t.rows[i] = row[:min(keep, len(row))]
	}
}

// widths returns the natural width of each column, from the widest cell and
// on a terminal the header.
func (t *Table) widths() []int {
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
			grow(i, visible(c.text))
		}
	}

	return widths
}

// visible counts the columns text takes, without its escape codes. A cell
// may carry colour from the caller, as a glyph in a list of changes does.
func visible(text string) int {
	n := 0

	for _, part := range parts(text) {
		if !part.code {
			n += utf8.RuneCountInString(part.text)
		}
	}

	return n
}

// part is a run of text or one escape code.
type part struct {
	text string
	code bool
}

// parts splits text into its escape codes and the runs between them.
func parts(text string) []part {
	var out []part

	for text != "" {
		start := strings.Index(text, "\x1b[")
		if start < 0 {
			return append(out, part{text: text})
		}

		if start > 0 {
			out = append(out, part{text: text[:start]})
		}

		end := strings.IndexByte(text[start:], 'm')
		if end < 0 {
			return append(out, part{text: text[start:]})
		}

		out = append(out, part{text: text[start : start+end+1], code: true})
		text = text[start+end+1:]
	}

	return out
}

// take returns the first width columns of text with its escape codes kept, and
// the rest.
func take(text string, width int) (head, tail string) {
	var b strings.Builder

	left := width

	for i, p := range parts(text) {
		if p.code {
			b.WriteString(p.text)

			continue
		}

		runes := []rune(p.text)
		if len(runes) <= left {
			b.WriteString(p.text)
			left -= len(runes)

			continue
		}

		b.WriteString(string(runes[:left]))

		rest := string(runes[left:])
		for _, p := range parts(text)[i+1:] {
			rest += p.text
		}

		return b.String(), rest
	}

	return b.String(), ""
}

// fit shrinks widths until the table fits the terminal. The last column
// gives first, down to wrapFirst, because it wraps and the others are cut.
// Past that the widest column gives, one column at a time, so that no column
// keeps its full width while the table squeezes another. The last column stops at
// minLast and the others at minColumn.
func (t *Table) fit(widths []int) {
	last := len(widths) - 1
	room := t.style.width - gap*last

	total := func() int {
		n := 0
		for _, w := range widths {
			n += w
		}

		return n
	}

	if over := total() - room; over > 0 {
		widths[last] = max(widths[last]-over, min(widths[last], wrapFirst))
	}

	floor := func(i int) int {
		if i == last {
			return minLast
		}

		return minColumn
	}

	for {
		if total() <= room {
			return
		}

		widest := -1

		for i, w := range widths {
			if w > floor(i) && (widest < 0 || w > widths[widest]) {
				widest = i
			}
		}

		if widest < 0 {
			return
		}

		widths[widest]--
	}
}

// render lays out one row. A cell of a column that is not the last is cut to
// its width, and the last cell wraps with continuation lines under the column.
func (t *Table) render(row []cell, widths []int) string {
	var b strings.Builder

	last := len(row) - 1
	indent := 0

	for i, c := range row {
		if i < last {
			text := cut(c.text, widths[i])
			b.WriteString(styled(c.style, text))
			b.WriteString(strings.Repeat(" ", widths[i]-visible(text)+gap))
			indent += widths[i] + gap

			continue
		}

		lines := []string{c.text}
		if t.style.width > 0 {
			lines = wrap(c.text, widths[i])
		}

		for j, line := range lines {
			if j > 0 {
				b.WriteString(strings.Repeat(" ", indent))
			}

			b.WriteString(styled(c.style, line))
			b.WriteByte('\n')
		}
	}

	if last < 0 {
		b.WriteByte('\n')
	}

	// A row with an empty last cell would end in the padding of the cell
	// before it.
	if t.style.on && last >= 0 && row[last].text == "" {
		return strings.TrimRight(b.String(), " \n") + "\n"
	}

	return b.String()
}

// writeStacked prints each row as a block of "label  value" lines, one per
// column with a value, and a blank line between rows. A first column with no
// header, such as the number of a generation, is the title of its block.
func (t *Table) writeStacked(w io.Writer) error {
	width := 0
	for _, h := range t.header {
		width = max(width, utf8.RuneCountInString(h))
	}

	titled := len(t.header) > 0 && t.header[0] == ""

	var b strings.Builder

	for n, row := range t.rows {
		if n > 0 {
			b.WriteByte('\n')
		}

		for i, c := range row {
			if c.text == "" {
				continue
			}

			if i == 0 && titled {
				b.WriteString(styled(c.style, c.text) + "\n")

				continue
			}

			label := ""
			if i < len(t.header) {
				label = t.header[i]
			}

			b.WriteString(t.style.Dim(fmt.Sprintf("%-*s", width, label)) + "  ")

			for j, line := range wrap(c.text, max(t.style.width-width-gap, minLast)) {
				if j > 0 {
					b.WriteString(strings.Repeat(" ", width+gap))
				}

				b.WriteString(styled(c.style, line))
				b.WriteByte('\n')
			}
		}
	}

	_, err := io.WriteString(w, b.String())

	return err
}

func styled(style func(string) string, text string) string {
	if style == nil || text == "" {
		return text
	}

	return style(text)
}

// cut shortens text to width columns, with an ellipsis at the end.
func cut(text string, width int) string {
	if visible(text) <= width {
		return text
	}

	if width <= 1 {
		return "…"
	}

	head, _ := take(text, width-1)

	return head + "…"
}

// wrap breaks text into lines of at most width columns, at spaces, and breaks
// a word longer than the width.
func wrap(text string, width int) []string { return wrapAt(text, width, width) }

// wrapAt is wrap with first columns for the first line and rest for the
// others, for a line whose continuation rows are indented.
func wrapAt(text string, first, rest int) []string {
	if first <= 0 || visible(text) <= first {
		return []string{text}
	}

	var (
		lines []string
		line  string
	)

	width := func() int {
		if len(lines) == 0 {
			return first
		}

		return rest
	}

	for _, word := range strings.Fields(text) {
		// A word too long for any line, such as a path, fills the rest of
		// the line it starts on.
		for visible(word) > width() {
			var head string

			if room := width() - visible(line) - 1; line != "" && room >= minColumn {
				head, word = take(word, room)
				lines = append(lines, line+" "+head)
				line = ""

				continue
			}

			if line != "" {
				lines = append(lines, line)
				line = ""
			}

			head, word = take(word, width())
			lines = append(lines, head)
		}

		switch {
		case line == "":
			line = word
		case visible(line)+1+visible(word) <= width():
			line += " " + word
		default:
			lines = append(lines, line)
			line = word
		}
	}

	if line != "" || len(lines) == 0 {
		lines = append(lines, line)
	}

	return lines
}

// KV prints label and value pairs, the labels aligned and dim. A pair with an
// empty value is left out. On a terminal a long value wraps under itself.
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

		lines := []string{p[1]}
		if s.width > 0 {
			lines = wrap(p[1], max(s.width-width-gap, minLast))
		}

		fmt.Fprintf(&b, "%s  %s\n", s.Dim(fmt.Sprintf("%-*s", width, p[0])), lines[0])

		for _, line := range lines[1:] {
			fmt.Fprintf(&b, "%s%s\n", strings.Repeat(" ", width+gap), line)
		}
	}

	_, err := io.WriteString(w, b.String())

	return err
}
