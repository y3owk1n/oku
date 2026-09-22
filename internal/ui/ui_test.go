package ui_test

import (
	"bytes"
	"regexp"
	"strings"
	"testing"

	"github.com/y3owk1n/oku/internal/ui"
)

// A buffer is not a terminal, so the plain text is what a script reads.
func TestPlainWriterGetsPlainText(t *testing.T) {
	t.Setenv("FORCE_COLOR", "")

	var out bytes.Buffer

	s := ui.For(&out)
	if s.On() {
		t.Fatal("a buffer should not be styled")
	}

	if got := s.Bold("x") + s.Check() + s.Arrow(); got != "xok->" {
		t.Fatalf("plain style changed the text: %q", got)
	}

	tab := s.Table("name", "version", "ref")
	tab.Row("fd", "10.5.0", "github:sharkdp/fd")
	tab.Styled([]string{"ripgrep", "15.2.0", "local.toml"}, s.Bold, nil, s.Dim)

	if err := tab.Write(&out); err != nil {
		t.Fatal(err)
	}

	want := "fd       10.5.0  github:sharkdp/fd\nripgrep  15.2.0  local.toml\n"
	if out.String() != want {
		t.Fatalf("table:\n%q\nwant\n%q", out.String(), want)
	}
}

func TestForcedColourStylesAndKeepsColumns(t *testing.T) {
	t.Setenv("NO_COLOR", "")
	t.Setenv("FORCE_COLOR", "1")

	var out bytes.Buffer

	s := ui.For(&out)
	if !s.On() {
		t.Fatal("FORCE_COLOR should style a buffer")
	}

	tab := s.Table("name", "ref")
	tab.Styled([]string{"fd", "a"}, s.Bold, s.Dim)
	tab.Styled([]string{"ripgrep", "b"}, nil, nil)

	if err := tab.Write(&out); err != nil {
		t.Fatal(err)
	}

	lines := strings.Split(strings.TrimRight(out.String(), "\n"), "\n")
	if len(lines) != 3 || !strings.Contains(lines[0], "NAME") {
		t.Fatalf("want a header and two rows, got %q", lines)
	}

	// The codes wrap the text and never the padding, so the columns line up.
	if !strings.HasPrefix(lines[1], "\x1b[1mfd\x1b[0m       \x1b[2ma\x1b[0m") {
		t.Fatalf("styled row: %q", lines[1])
	}

	if lines[2] != "ripgrep  b" {
		t.Fatalf("plain row: %q", lines[2])
	}
}

func TestNoColorWins(t *testing.T) {
	t.Setenv("FORCE_COLOR", "1")
	t.Setenv("NO_COLOR", "1")

	if ui.For(&bytes.Buffer{}).On() {
		t.Fatal("NO_COLOR should turn styling off")
	}
}

func TestB227ATableFitsTheTerminalWidth(t *testing.T) {
	t.Setenv("NO_COLOR", "")
	t.Setenv("FORCE_COLOR", "1")

	t.Run("the last column wraps and a wide one is cut", func(t *testing.T) {
		t.Setenv("COLUMNS", "60")

		var out bytes.Buffer

		s := ui.For(&out)
		tab := s.Table("name", "path", "changes")
		tab.Row("fd", strings.Repeat("p", 40), "one two three four five six seven eight nine ten eleven twelve")

		if err := tab.Write(&out); err != nil {
			t.Fatal(err)
		}

		plain := regexp.MustCompile("\x1b\\[[0-9]+m").ReplaceAllString(out.String(), "")

		for _, line := range strings.Split(strings.TrimRight(plain, "\n"), "\n") {
			if n := len([]rune(line)); n > 60 {
				t.Fatalf("a line is %d columns wide:\n%s", n, out.String())
			}
		}

		if !strings.Contains(out.String(), "…") || strings.Count(out.String(), "\n") < 3 {
			t.Fatalf("the wide column should be cut and the last one wrapped:\n%s", out.String())
		}
	})

	t.Run("a narrow terminal stacks a table that does not fit", func(t *testing.T) {
		t.Setenv("COLUMNS", "30")

		var out bytes.Buffer

		s := ui.For(&out)
		tab := s.Table("name", "version", "ref")
		tab.Row("fd", "10.5.0", "github:sharkdp/fd")
		tab.Row("rg", "15.2.0", "github:BurntSushi/ripgrep")

		if err := tab.Write(&out); err != nil {
			t.Fatal(err)
		}

		plain := regexp.MustCompile("\x1b\\[[0-9]+m").ReplaceAllString(out.String(), "")
		want := "name     fd\nversion  10.5.0\nref      github:sharkdp/fd\n\nname     rg\n"

		if !strings.HasPrefix(plain, want) {
			t.Fatalf("stacked table:\n%s", plain)
		}
	})
}

// plain drops the escape codes, leaving what the terminal shows.
func plain(text string) string {
	return regexp.MustCompile("\x1b\\[[0-9;]*[A-Za-z]").ReplaceAllString(text, "")
}

func TestB233ATableStacksOnlyWhenItDoesNotFit(t *testing.T) {
	t.Setenv("NO_COLOR", "")
	t.Setenv("FORCE_COLOR", "1")
	t.Setenv("COLUMNS", "50")

	t.Run("a table that fits keeps its columns", func(t *testing.T) {
		var out bytes.Buffer

		s := ui.For(&out)
		tab := s.Table("name", "version", "ref", "")
		tab.Row("fd", "10.5.0", "github:sharkdp/fd", "")
		tab.Row("jq", "1.8.2", "github:jqlang/jq", "")

		if tab.Stacked() {
			t.Fatal("a table of 36 columns should not stack at 50")
		}

		if err := tab.Write(&out); err != nil {
			t.Fatal(err)
		}

		want := "NAME  VERSION  REF\nfd    10.5.0   github:sharkdp/fd\njq    1.8.2    github:jqlang/jq\n"
		if got := plain(out.String()); got != want {
			t.Fatalf("a row should end at its last value:\n%q\nwant\n%q", got, want)
		}
	})

	t.Run("a first column with no header titles each block", func(t *testing.T) {
		var out bytes.Buffer

		s := ui.For(&out)
		tab := s.Table("", "created", "holds", "changes")
		tab.Row("● 1", "2026-09-23 01:00", "4 packages", "+ fd 10.5.0, + jq 1.8.2, + just 1.58.0")
		tab.Row("  2", "2026-09-23 01:05", "3 packages", "- fd")

		if err := tab.Write(&out); err != nil {
			t.Fatal(err)
		}

		want := "● 1\ncreated  2026-09-23 01:00\nholds    4 packages\n"
		if got := plain(out.String()); !strings.HasPrefix(got, want) || !strings.Contains(got, "\n\n  2\n") {
			t.Fatalf("stacked generations:\n%s", got)
		}
	})
}

func TestB234LongLinesWrapUnderTheirTextOnATerminal(t *testing.T) {
	t.Setenv("NO_COLOR", "")
	t.Setenv("FORCE_COLOR", "1")
	t.Setenv("COLUMNS", "30")

	var out bytes.Buffer

	s := ui.For(&out)

	got := plain(s.Wrap(s.Note()+" fd publishes no checksum, so oku trusted this download", 2))
	want := "! fd publishes no checksum, so\n  oku trusted this download"

	if got != want {
		t.Fatalf("a note should wrap under its text:\n%q\nwant\n%q", got, want)
	}

	// A path longer than a line breaks, and each row stays inside the width.
	for _, line := range strings.Split(plain(s.Wrap(s.Cross()+" "+strings.Repeat("/dir", 20)+" is not on PATH", 2)), "\n") {
		if len([]rune(line)) > 30 {
			t.Fatalf("a row is %d columns wide: %q", len([]rune(line)), line)
		}
	}

	if err := s.KV(&out, [2]string{"store", "/data/oku/store/jq-1.8.2-6d574730d94c2a41"}); err != nil {
		t.Fatal(err)
	}

	for _, line := range strings.Split(strings.TrimRight(plain(out.String()), "\n"), "\n") {
		if len([]rune(line)) > 30 || !strings.HasPrefix(line, "store  ") && !strings.HasPrefix(line, "       ") {
			t.Fatalf("a long value should wrap under itself:\n%s", plain(out.String()))
		}
	}

	t.Setenv("FORCE_COLOR", "")

	if text := "a line longer than thirty columns stays whole"; ui.For(&out).Wrap(text, 2) != text {
		t.Fatal("a pipe should get the text unchanged")
	}
}

func TestB229LinesCountsWhatTheTerminalShows(t *testing.T) {
	t.Setenv("NO_COLOR", "")
	t.Setenv("FORCE_COLOR", "1")
	t.Setenv("COLUMNS", "20")

	s := ui.For(&bytes.Buffer{})

	// 18 columns on screen, with codes that would push the count past 20.
	if n := s.Lines(s.Accent("step 1") + s.Warn(" (wants net)")); n != 1 {
		t.Fatalf("a styled line of 18 columns takes %d rows, want 1", n)
	}

	if n := s.Lines(strings.Repeat("x", 21) + "\n"); n != 2 {
		t.Fatalf("21 columns take %d rows, want 2", n)
	}
}
