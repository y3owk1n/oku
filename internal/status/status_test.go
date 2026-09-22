package status

import (
	"bytes"
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
