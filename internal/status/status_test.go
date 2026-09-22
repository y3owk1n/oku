package status

import (
	"bytes"
	"context"
	"io"
	"strings"
	"testing"
	"time"

	"github.com/y3owk1n/oku/internal/ui"
)

// live returns a reporter that behaves as on a terminal, writing to a buffer.
func live(out *bytes.Buffer) *Reporter {
	return &Reporter{out: out, live: true, style: ui.Style{}, width: func() int { return 80 }}
}

func TestB232AQuestionHoldsTheOtherPackagesOutputUntilItIsAnswered(t *testing.T) {
	var out bytes.Buffer

	r := live(&out)
	rows := &lineWriter{r: r, out: &out}

	resume := r.pause()

	if _, err := rows.Write([]byte("✓ fd 10.5.0\n")); err != nil {
		t.Fatal(err)
	}

	if strings.Contains(out.String(), "fd") {
		t.Fatalf("a row wrote over the question:\n%q", out.String())
	}

	// The question and the answer take the terminal meanwhile.
	out.WriteString("run them? [y/N] y\n")
	resume()

	if got := out.String(); !strings.HasSuffix(got, "run them? [y/N] y\n✓ fd 10.5.0\n") {
		t.Fatalf("the held row should follow the answer:\n%q", got)
	}

	if _, err := rows.Write([]byte("✓ rg 15.2.0\n")); err != nil {
		t.Fatal(err)
	}

	if !strings.HasSuffix(out.String(), "✓ rg 15.2.0\n") {
		t.Fatalf("after the answer rows print at once:\n%q", out.String())
	}
}

func TestB232TwoQuestionsAskOneAfterTheOther(t *testing.T) {
	var out bytes.Buffer

	r := live(&out)
	first := r.pause()

	second := make(chan func(), 1)

	go func() { second <- r.pause() }()

	select {
	case <-second:
		t.Fatal("the second question should wait for the first")
	case <-time.After(50 * time.Millisecond):
	}

	first()

	select {
	case resume := <-second:
		resume()
	case <-time.After(time.Second):
		t.Fatal("the second question should ask once the first is answered")
	}
}

func TestB176APackageKeepsOneLineFromItsFirstWait(t *testing.T) {
	var out bytes.Buffer

	r := live(&out)
	ctx := With(context.Background(), r)
	jq, fd := Scope(ctx, "jq"), Scope(ctx, "fd")

	defer Start(jq, "reading github:jqlang/jq")()
	defer Start(fd, "reading github:sharkdp/fd")()

	// Once the version is known, the wait names it and stays on the line of jq.
	versioned := Scope(jq, "jq 1.8.2")
	defer Start(versioned, "downloading https://github.com/jqlang/jq/releases/download/jq-1.8.2/jq-macos-arm64")()

	body := Reader(versioned, strings.NewReader(strings.Repeat("x", 512)), 2048)
	if _, err := io.Copy(io.Discard, body); err != nil {
		t.Fatal(err)
	}

	r.mu.Lock()
	r.draw()
	screen := out.String()[strings.LastIndex(out.String(), "\x1b[2K")+len("\x1b[2K"):]
	r.mu.Unlock()

	lines := strings.Split(screen, "\n")
	if len(lines) != 2 {
		t.Fatalf("two packages should take two lines:\n%q", screen)
	}

	if !strings.Contains(lines[0], "jq 1.8.2: downloading jq-macos-arm64 512 B of 2.0 KiB, 25%") {
		t.Fatalf("the download should show its file and how far it got:\n%q", lines[0])
	}

	if !strings.Contains(lines[1], "fd: reading github:sharkdp/fd") {
		t.Fatalf("fd should keep its own line:\n%q", lines[1])
	}
}
