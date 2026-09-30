package cli

import (
	"errors"
	"fmt"
	"io"
	"strings"

	"github.com/spf13/cobra"

	"github.com/y3owk1n/oku/internal/ref"
	"github.com/y3owk1n/oku/internal/status"
	"github.com/y3owk1n/oku/internal/ui"
)

// listReviewer shows what changed in an included list since oku.lock pinned
// it, and asks before update takes it. A list can place files and change
// settings, so its author's later edits need the user's yes as much as the
// first include did. yes takes the change without asking, and without a
// terminal oku refuses.
func (e env) listReviewer(cmd *cobra.Command, opts Options, yes bool) listReview {
	return func(r ref.Ref, before, after []byte) error {
		if yes {
			fmt.Fprintf(cmd.ErrOrStderr(), "--yes took the changed included list %s\n", r)

			return nil
		}

		defer status.Pause(cmd.Context())()

		terminal := cmd.ErrOrStderr()
		fmt.Fprintf(terminal, "the included list %s changed since oku.lock was written:\n", r)
		writeListDiff(terminal, before, after)

		if !interactive(cmd, opts) {
			return fmt.Errorf(
				"include %s changed, and this is not a terminal\npass --yes to take the change", r,
			)
		}

		if !confirm(cmd.InOrStdin(), terminal, "take the change?") {
			return errors.New("not taken, nothing was changed")
		}

		return nil
	}
}

// writeListDiff prints the lines of after that are new and the lines of before
// that are gone, each under the table it is in. With no before it prints all
// of after, since oku cannot read what it pinned.
func writeListDiff(w io.Writer, before, after []byte) {
	s := ui.For(w)

	var old []string
	if before == nil {
		fmt.Fprintln(w, s.Dim("  oku cannot read the version oku.lock pinned, so this is the whole list:"))
	} else {
		old = strings.Split(strings.TrimRight(string(before), "\n"), "\n")
	}

	lines := strings.Split(strings.TrimRight(string(after), "\n"), "\n")

	// Each side has its own table, since a line may be gone from one table and
	// added to another.
	var oldTable, newTable, shown string

	for _, d := range diffLines(old, lines) {
		header := strings.TrimSpace(d.line)
		if strings.HasPrefix(header, "[") {
			if d.op != '+' {
				oldTable = header
			}

			if d.op != '-' {
				newTable = header
			}

			// A header that changed names its own table.
			if d.op != ' ' {
				shown = header
			}
		}

		if d.op == ' ' || header == "" {
			continue
		}

		table := newTable
		if d.op == '-' {
			table = oldTable
		}

		if table != shown {
			fmt.Fprintln(w, s.Dim("  "+table))
			shown = table
		}

		if d.op == '+' {
			fmt.Fprintln(w, s.Good("  + "+d.line))
		} else {
			fmt.Fprintln(w, s.Bad("  - "+d.line))
		}
	}
}

type diffLine struct {
	op   byte
	line string
}

// diffLines is the shortest edit from a to b, by their longest common
// subsequence. Lists are a few hundred lines, so the table fits in memory.
func diffLines(a, b []string) []diffLine {
	common := make([][]int, len(a)+1)
	for i := range common {
		common[i] = make([]int, len(b)+1)
	}

	for i := len(a) - 1; i >= 0; i-- {
		for j := len(b) - 1; j >= 0; j-- {
			if a[i] == b[j] {
				common[i][j] = common[i+1][j+1] + 1
			} else {
				common[i][j] = max(common[i+1][j], common[i][j+1])
			}
		}
	}

	var out []diffLine

	i, j := 0, 0
	for i < len(a) || j < len(b) {
		switch {
		case i < len(a) && j < len(b) && a[i] == b[j]:
			out = append(out, diffLine{' ', a[i]})
			i, j = i+1, j+1
		// A removed line comes before the line that replaces it.
		case i < len(a) && (j == len(b) || common[i+1][j] >= common[i][j+1]):
			out = append(out, diffLine{'-', a[i]})
			i++
		default:
			out = append(out, diffLine{'+', b[j]})
			j++
		}
	}

	return out
}
