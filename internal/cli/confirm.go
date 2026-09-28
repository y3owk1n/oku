package cli

import (
	"fmt"
	"io"
	"os"
	"strings"

	"golang.org/x/term"

	"github.com/y3owk1n/oku/internal/ui"
)

// confirm asks a yes or no question, where no is the default. On a terminal one
// key answers it. y is yes, and n, Enter, Esc, Ctrl-C and Ctrl-D are no. From
// anything else confirm reads one line, a byte at a time, so that a later
// question still finds its own answer. The question and its answer end on their
// own line, which a styled terminal redraws as a mark, the question and the
// answer.
func confirm(in io.Reader, out io.Writer, question string) bool {
	s := ui.For(out)

	if s.On() {
		fmt.Fprintf(out, "%s %s %s ", s.Accent("?"), s.Bold(question), s.Dim("y/N"))
	} else {
		fmt.Fprint(out, question+" [y/N] ")
	}

	yes, ok := readKey(in)
	if !ok {
		yes = readLine(in)
	}

	answer, mark := "no", s.Cross()
	if yes {
		answer, mark = "yes", s.Check()
	}

	if s.On() {
		fmt.Fprintf(out, "\r\x1b[2K%s %s %s\n", mark, question, s.Dim(answer))
	} else {
		fmt.Fprintln(out, answer)
	}

	return yes
}

// readKey reads one key from a terminal in raw mode. ok is false when in is
// not a terminal. Keys other than an answer, such as an arrow, are skipped.
func readKey(in io.Reader) (yes, ok bool) {
	file, ok := in.(*os.File)
	if !ok || !term.IsTerminal(int(file.Fd())) {
		return false, false
	}

	state, err := term.MakeRaw(int(file.Fd()))
	if err != nil {
		return false, false
	}
	defer term.Restore(int(file.Fd()), state) //nolint:errcheck

	var buf [16]byte

	for {
		n, err := file.Read(buf[:])
		if err != nil {
			return false, true
		}

		// A lone Esc declines, but Esc followed by more bytes is a key such as an
		// arrow.
		if n != 1 {
			continue
		}

		switch buf[0] {
		case 'y', 'Y':
			return true, true
		case 'n', 'N', '\r', '\n', 0x1b, 0x03, 0x04:
			return false, true
		}
	}
}

// readLine reads up to a newline and reports whether the line is y or yes.
func readLine(in io.Reader) bool {
	var line []byte

	var b [1]byte

	for {
		n, err := in.Read(b[:])
		if n == 1 && b[0] == '\n' {
			break
		}

		line = append(line, b[:n]...)

		if err != nil {
			break
		}
	}

	a := strings.ToLower(strings.TrimSpace(string(line)))

	return a == "y" || a == "yes"
}
