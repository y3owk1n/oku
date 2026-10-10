package cli

import (
	"cmp"
	"errors"
	"fmt"
	"io"
	"os"
	"path/filepath"
	"slices"
	"strconv"
	"strings"

	"github.com/spf13/cobra"

	"github.com/y3owk1n/oku/internal/lock"
	"github.com/y3owk1n/oku/internal/profile"
	"github.com/y3owk1n/oku/internal/ui"
)

func newDiffCmd(opts Options) *cobra.Command {
	var markdown bool

	cmd := &cobra.Command{
		Use:   "diff [<from> [<to>]]",
		Short: "Show what changed between two generations or two locks",
		Long: `Show what changed between two generations or two oku.lock files: packages
and their versions, deps, files, settings and included lists.

A number names a generation, and anything else a lock file or the directory
that holds one. Both must be the same kind.

  oku diff                 the active generation against the one it replaced
  oku diff 4               generation 4 against the active one
  oku diff 4 7             generation 4 against generation 7
  oku diff old.lock        old.lock against the list's oku.lock
  oku diff a.lock b.lock   a.lock against b.lock

--markdown prints a table to paste in a pull request.`,
		Args:              maxArgs(2),
		ValidArgsFunction: completeGenerations(opts),
		RunE: func(cmd *cobra.Command, args []string) error {
			if markdown && wantJSON(cmd) {
				return errors.New("pass --markdown or --json, not both")
			}

			e, err := scopedEnv(cmd, opts)
			if err != nil {
				return err
			}

			deltas, err := e.diff(args)
			if err != nil {
				return err
			}

			switch {
			case wantJSON(cmd):
				if deltas == nil {
					deltas = []delta{}
				}

				return printJSON(cmd, deltas)
			case markdown:
				writeMarkdown(cmd.OutOrStdout(), deltas)

				return nil
			}

			return writeDeltas(cmd.OutOrStdout(), deltas)
		},
	}

	cmd.Flags().BoolVar(&markdown, "markdown", false, "print a Markdown table")

	return cmd
}

// diff compares the generations or the locks that args name.
func (e env) diff(args []string) ([]delta, error) {
	numbers := make([]int, 0, len(args))

	for _, arg := range args {
		if n, err := strconv.Atoi(arg); err == nil && n > 0 {
			numbers = append(numbers, n)
		}
	}

	switch {
	case len(numbers) == len(args):
		return e.diffGenerations(numbers)
	case len(numbers) > 0:
		return nil, errors.New("compare two generations or two locks, not one of each")
	}

	// One lock compares with the list's, which may not exist yet.
	paths := append(slices.Clone(args), e.lockPath())[:2]

	locks := make([]*lock.Lock, 2)

	for i, path := range paths {
		if i < len(args) {
			info, err := os.Stat(path)
			if err != nil {
				return nil, fmt.Errorf("%s is neither a generation number nor a lock file: %w", path, err)
			}

			if info.IsDir() {
				path = filepath.Join(path, lock.FileName)
			}
		}

		var err error
		if locks[i], err = lock.Read(path); err != nil {
			return nil, err
		}
	}

	return lockDeltas(locks[0], locks[1]), nil
}

// diffGenerations compares two generations. Without a second number it
// compares with the active one, and without any with the one the active one
// replaced, as `oku generations` does.
func (e env) diffGenerations(numbers []int) ([]delta, error) {
	gens, err := e.profile().Generations()
	if err != nil {
		return nil, err
	}

	at := slices.IndexFunc(gens, func(g profile.Generation) bool { return g.Current })
	if at < 0 {
		return nil, errors.New("the profile has no generations yet, the first `oku add` or `oku sync` makes one")
	}

	find := func(n int) (profile.Generation, error) {
		i := slices.IndexFunc(gens, func(g profile.Generation) bool { return g.Number == n })
		if i < 0 {
			return profile.Generation{}, fmt.Errorf("generation %d does not exist, see `oku generations`", n)
		}

		return gens[i], nil
	}

	to := gens[at]

	if len(numbers) == 0 {
		replaced := cmp.Or(to.From, to.Number-1)
		if replaced == 0 {
			return generationDeltas(profile.Generation{}, to), nil
		}

		from, err := find(replaced)
		if err != nil {
			return nil, fmt.Errorf("generation %d replaced %d, which is deleted", to.Number, replaced)
		}

		return generationDeltas(from, to), nil
	}

	from, err := find(numbers[0])
	if err != nil {
		return nil, err
	}

	if len(numbers) == 2 {
		if to, err = find(numbers[1]); err != nil {
			return nil, err
		}
	}

	return generationDeltas(from, to), nil
}

// text is the change column: a version, or two with an arrow, and what changed
// when the versions did not.
func (d delta) text(arrow string) string {
	var parts []string

	switch {
	case d.Change == wasAdded && d.After != "":
		parts = append(parts, d.After)
	case d.Change == wasRemoved && d.Before != "":
		parts = append(parts, d.Before)
	case d.Before != d.After:
		parts = append(parts, d.Before+" "+arrow+" "+d.After)
	case d.After != "" && d.Kind != "file":
		parts = append(parts, d.After)
	}

	if d.Note != "" {
		parts = append(parts, d.Note)
	}

	return strings.Join(parts, ", ")
}

// writeDeltas prints deltas as a table, with a mark for each change.
func writeDeltas(w io.Writer, deltas []delta) error {
	if len(deltas) == 0 {
		fmt.Fprintln(w, "no change")

		return nil
	}

	s := ui.For(w)
	tab := s.Table("", "kind", "name", "change")

	for _, d := range deltas {
		mark := map[string]string{wasAdded: s.Good("+"), wasRemoved: s.Bad("-"), wasChanged: s.Warn("~")}[d.Change]

		name := d.Name
		if d.Kind == "file" {
			name = s.Home(name)
		}

		tab.Styled([]string{mark, d.Kind, name, d.text(s.Arrow())}, nil, s.Dim, s.Bold, nil)
	}

	return tab.Write(w)
}

// writeMarkdown prints deltas as a Markdown table.
func writeMarkdown(w io.Writer, deltas []delta) {
	if len(deltas) == 0 {
		fmt.Fprintln(w, "No changes.")

		return
	}

	cell := func(s string) string { return strings.ReplaceAll(s, "|", `\|`) }

	fmt.Fprint(w, "| Change | Kind | Name | Before | After |\n|---|---|---|---|---|\n")

	for _, d := range deltas {
		after := d.After
		if d.Note != "" {
			after = strings.TrimPrefix(after+", "+d.Note, ", ")
		}

		fmt.Fprintf(w, "| %s | %s | `%s` | %s | %s |\n", d.Change, d.Kind, cell(d.Name), cell(d.Before), cell(after))
	}
}
