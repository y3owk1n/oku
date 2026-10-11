package cli

import (
	"bytes"
	"cmp"
	"context"
	"errors"
	"fmt"
	"io"
	"io/fs"
	"os"
	"os/exec"
	"path/filepath"
	"slices"
	"strings"
	"time"

	"github.com/spf13/cobra"
	"github.com/spf13/pflag"
)

// manManual is the manual that every page of oku names in its .TH line. oku
// removes a page only when it names this manual.
const manManual = "oku Manual"

func newSelfManCmd(opts Options) *cobra.Command {
	var dir string

	cmd := &cobra.Command{
		Use:   "man",
		Short: "Write oku's man pages",
		Args:  noArgs,
		RunE: func(cmd *cobra.Command, _ []string) error {
			return runSelfMan(cmd, opts.Version, cmp.Or(dir, manDir(opts.Executable)))
		},
	}
	cmd.Flags().
		StringVar(&dir, "dir", "", "write the pages into man1 under this directory, not ../share/man from the binary")

	return cmd
}

func runSelfMan(cmd *cobra.Command, version, dir string) error {
	// A command that a newer or older oku had keeps no page.
	if err := removeManPages(dir); err != nil {
		return err
	}

	section := filepath.Join(dir, "man1")
	if err := os.MkdirAll(section, 0o755); err != nil {
		return err
	}

	// The rest of each page's .TH line: the date, the version and the manual.
	// man parses the date only when it has no escapes.
	footer := strings.Join([]string{
		`"` + time.Now().Format(time.DateOnly) + `"`, roffQuote("oku " + version), roffQuote(manManual),
	}, " ")

	if err := writeManPages(cmd.Root(), footer, section); err != nil {
		return fmt.Errorf("write the man pages: %w", err)
	}

	pages, err := manPages(dir)
	if err != nil {
		return err
	}

	finished(cmd.OutOrStdout(), "wrote %d man pages to %s", len(pages), section)

	return nil
}

// writeManPages writes a page for c and one for each command under it, named
// after the command's path, such as oku-self-man.1. A page holds what --help
// prints, and keeps the line breaks and indents of the help text.
func writeManPages(c *cobra.Command, footer, section string) error {
	var subs []*cobra.Command

	for _, sub := range c.Commands() {
		if sub.IsAvailableCommand() {
			subs = append(subs, sub)
		}
	}

	for _, sub := range subs {
		if err := writeManPages(sub, footer, section); err != nil {
			return err
		}
	}

	// cobra adds --help only to the command that runs.
	c.InitDefaultHelpFlag()

	name := strings.ReplaceAll(c.CommandPath(), " ", "-")

	var b strings.Builder

	fmt.Fprintf(&b, ".TH %s 1 %s\n", roffQuote(strings.ToUpper(name)), footer)
	fmt.Fprintf(&b, ".SH NAME\n%s \\- %s\n", roffEscape(name), roffEscape(c.Short))
	fmt.Fprintf(&b, ".SH SYNOPSIS\n.nf\n\\fB%s\\fR\n.fi\n", roffEscape(c.UseLine()))
	fmt.Fprintf(&b, ".SH DESCRIPTION\n%s", roffText(cmp.Or(c.Long, c.Short)))

	if len(subs) > 0 {
		b.WriteString(".SH COMMANDS\n")

		for _, sub := range subs {
			fmt.Fprintf(&b, ".TP\n\\fB%s\\fR\n%s\n", roffEscape(sub.Name()), roffEscape(sub.Short))
		}
	}

	for _, flags := range []struct {
		title string
		set   *pflag.FlagSet
	}{{"OPTIONS", c.NonInheritedFlags()}, {"GLOBAL OPTIONS", c.InheritedFlags()}} {
		if !flags.set.HasAvailableFlags() {
			continue
		}

		fmt.Fprintf(&b, ".SH %s\n", flags.title)
		flags.set.VisitAll(func(f *pflag.Flag) {
			if f.Hidden || f.Deprecated != "" {
				return
			}

			// The value's name is what --help prints after the flag.
			value, usage := pflag.UnquoteUsage(f)

			names := "\\fB\\-\\-" + roffEscape(f.Name) + "\\fR"
			if f.Shorthand != "" && f.ShorthandDeprecated == "" {
				names = "\\fB\\-" + roffEscape(f.Shorthand) + "\\fR, " + names
			}

			if value != "" {
				names += " \\fI" + roffEscape(value) + "\\fR"
			}

			fmt.Fprintf(&b, ".TP\n%s\n%s\n", names, roffEscape(usage))
		})
	}

	var also []string
	if c.HasParent() {
		also = append(also, strings.ReplaceAll(c.Parent().CommandPath(), " ", "-"))
	}

	for _, sub := range subs {
		also = append(also, name+"-"+sub.Name())
	}

	if len(also) > 0 {
		for i, page := range also {
			also[i] = "\\fB" + roffEscape(page) + "\\fR(1)"
		}

		fmt.Fprintf(&b, ".SH SEE ALSO\n%s\n", strings.Join(also, ", "))
	}

	return os.WriteFile(filepath.Join(section, name+".1"), []byte(b.String()), 0o644)
}

// roffText turns help text into roff. Each paragraph of plain lines is filled
// to the width of the terminal, and one with an indented line keeps its lines
// as they are, as the tables in the help text need.
func roffText(text string) string {
	var b strings.Builder

	for i, paragraph := range strings.Split(strings.TrimSpace(text), "\n\n") {
		if i > 0 {
			b.WriteString(".PP\n")
		}

		lines := strings.Split(paragraph, "\n")
		indented := slices.ContainsFunc(lines, func(line string) bool {
			return strings.HasPrefix(line, " ") || strings.HasPrefix(line, "\t")
		})

		if indented {
			b.WriteString(".nf\n")
		}

		for _, line := range lines {
			b.WriteString(roffEscape(line) + "\n")
		}

		if indented {
			b.WriteString(".fi\n")
		}
	}

	return b.String()
}

// roffEscape makes text print as it is. A backslash starts an escape, a
// leading dot or quote a request, and a plain hyphen may print as a dash that
// does not copy as one.
func roffEscape(text string) string {
	text = strings.NewReplacer(`\`, `\e`, "-", `\-`).Replace(text)
	if strings.HasPrefix(text, ".") || strings.HasPrefix(text, "'") {
		text = `\&` + text
	}

	return text
}

// roffQuote makes text one argument of a request.
func roffQuote(text string) string {
	return `"` + strings.ReplaceAll(roffEscape(text), `"`, `\(dq`) + `"`
}

// manDir is share/man beside the bin directory that holds executable. man
// looks there for the pages of each program on PATH, so ~/.local/bin/oku gets
// ~/.local/share/man.
func manDir(executable string) string {
	return filepath.Join(filepath.Dir(filepath.Dir(executable)), "share", "man")
}

// manPages lists the pages in dir/man1 that "oku self man" wrote. It skips a
// page of another program, even one whose name starts with oku.
func manPages(dir string) ([]string, error) {
	section := filepath.Join(dir, "man1")

	entries, err := os.ReadDir(section)
	if errors.Is(err, fs.ErrNotExist) {
		return nil, nil
	}

	if err != nil {
		return nil, err
	}

	var pages []string

	for _, entry := range entries {
		name := entry.Name()
		if !entry.Type().IsRegular() ||
			name != "oku.1" && (!strings.HasPrefix(name, "oku-") || !strings.HasSuffix(name, ".1")) {
			continue
		}

		path := filepath.Join(section, name)

		ours, err := namesManual(path)
		if err != nil {
			return nil, err
		}

		if ours {
			pages = append(pages, path)
		}
	}

	return pages, nil
}

// namesManual reports whether the start of the page at path, where its .TH line
// is, names oku's manual.
func namesManual(path string) (bool, error) {
	f, err := os.Open(path)
	if err != nil {
		return false, err
	}
	defer f.Close()

	head := make([]byte, 512)

	n, err := io.ReadFull(f, head)
	if err != nil && !errors.Is(err, io.ErrUnexpectedEOF) && !errors.Is(err, io.EOF) {
		return false, err
	}

	return bytes.Contains(head[:n], []byte(`"`+manManual+`"`)), nil
}

// removeManPages deletes oku's pages in dir/man1, then dir/man1 and dir when
// that empties them.
func removeManPages(dir string) error {
	pages, err := manPages(dir)
	if err != nil {
		return err
	}

	for _, page := range pages {
		if err := os.Remove(page); err != nil {
			return err
		}
	}

	if len(pages) > 0 {
		// Either one may hold the pages of other programs, and then it stays.
		_ = os.Remove(filepath.Join(dir, "man1"))
		_ = os.Remove(dir)
	}

	return nil
}

// refreshManPages has the binary at executable write its own pages, so that
// they describe the version it is. The old pages go first, since a release
// older than "oku self man" writes none.
func refreshManPages(ctx context.Context, executable string) error {
	if err := removeManPages(manDir(executable)); err != nil {
		return err
	}

	if output, err := exec.CommandContext(ctx, executable, "self", "man").CombinedOutput(); err != nil {
		return fmt.Errorf("%w: %s", err, bytes.TrimSpace(output))
	}

	return nil
}
