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

// screen draws r and returns what the terminal shows after the last erase.
func screen(r *Reporter, out *bytes.Buffer) string {
	r.mu.Lock()
	defer r.mu.Unlock()

	r.draw()

	shown := out.String()
	if i := strings.LastIndex(shown, "\x1b[2K"); i >= 0 {
		shown = shown[i+len("\x1b[2K"):]
	}

	return shown
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

	time.Sleep(grace)

	lines := strings.Split(screen(r, &out), "\n")
	if len(lines) != 2 {
		t.Fatalf("two packages should take two lines:\n%q", lines)
	}

	if !strings.Contains(lines[0], "jq 1.8.2: downloading jq-macos-arm64 512 B of 2.0 KiB, 25%") {
		t.Fatalf("the download should show its file and how far it got:\n%q", lines[0])
	}

	if !strings.Contains(lines[1], "fd: reading github:sharkdp/fd") {
		t.Fatalf("fd should keep its own line:\n%q", lines[1])
	}
}

func TestB554AWaitShowsOnceItRunsForTheGraceAndKeepsItsLine(t *testing.T) {
	var out bytes.Buffer

	r := live(&out)
	ctx := With(context.Background(), r)

	// A lookup that the cache answers ends before its line would show.
	Start(Scope(ctx, "uts"), "reading github:y3owk1n/uts#uts-main")()

	if out.Len() != 0 {
		t.Fatalf("a short wait drew on the terminal:\n%q", out.String())
	}

	defer Start(Scope(ctx, "fd"), "reading github:sharkdp/fd")()

	jq := Scope(ctx, "jq")
	reading := Start(jq, "reading github:jqlang/jq")

	if got := screen(r, &out); strings.Contains(got, "jq") || strings.Contains(got, "fd") {
		t.Fatalf("waits younger than the grace should not show yet:\n%q", got)
	}

	time.Sleep(grace)

	if got := screen(r, &out); !strings.Contains(got, "jq: reading") || !strings.Contains(got, "fd: reading") {
		t.Fatalf("waits past the grace should show:\n%q", got)
	}

	// jq keeps its line from one wait to the next.
	reading()
	defer Start(jq, "downloading jq-macos-arm64")()

	if got := screen(r, &out); !strings.Contains(got, "jq: downloading jq-macos-arm64") {
		t.Fatalf("the next wait of a shown package should show at once:\n%q", got)
	}
}

func TestB559ARequestNoOtherWaitNamesShowsItsHostAfterASecond(t *testing.T) {
	var out bytes.Buffer

	r := live(&out)
	jq := Scope(With(context.Background(), r), "jq")

	defer Idle(jq, "waiting for api.osv.dev")()

	time.Sleep(grace)

	if got := screen(r, &out); strings.Contains(got, "api.osv.dev") {
		t.Fatalf("a request younger than a second should not show yet:\n%q", got)
	}

	time.Sleep(idleGrace)

	if got := screen(r, &out); !strings.Contains(got, "jq: waiting for api.osv.dev") {
		t.Fatalf("a long request should name its host:\n%q", got)
	}

	// The package's own wait names more than the host, so its line shows that
	// wait.
	defer Start(jq, "downloading jq-macos-arm64")()

	if got := screen(r, &out); !strings.Contains(got, "jq: downloading jq-macos-arm64") ||
		strings.Contains(got, "api.osv.dev") {
		t.Fatalf("the package's own wait should take the line:\n%q", got)
	}

	// A pipe gets a line for each wait, and none for a request.
	var piped bytes.Buffer

	Idle(With(context.Background(), New(&piped)), "waiting for api.osv.dev")()

	if piped.Len() != 0 {
		t.Fatalf("a pipe should get no line for a request:\n%q", piped.String())
	}
}
