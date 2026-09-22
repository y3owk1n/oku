package ui_test

import (
	"bytes"
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
