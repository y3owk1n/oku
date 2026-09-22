// Package status tells the user what oku is waiting for. On a terminal it keeps
// one line that it redraws. Anywhere else it prints a line when a wait starts.
package status

import (
	"bytes"
	"context"
	"fmt"
	"io"
	"os"
	"slices"
	"sync"
	"time"

	"golang.org/x/term"

	"github.com/y3owk1n/oku/internal/ui"
)

const (
	frames   = "⠋⠙⠹⠸⠼⠴⠦⠧⠇⠏"
	interval = 100 * time.Millisecond
	// clearLine returns to the first column and erases the line.
	clearLine = "\r\x1b[2K"
)

// Reporter shows the waits of one command.
type Reporter struct {
	mu    sync.Mutex
	out   io.Writer
	live  bool
	style ui.Style
	width func() int
	tasks []*task
	stop  chan struct{}
	done  chan struct{}
	frame int
	// paused stops the redraw while another program uses the terminal.
	paused bool
}

type task struct {
	text string
	// scope is the Scope of the context that started the wait.
	scope   string
	started time.Time
	// read and total count the bytes of a download. total is -1 when unknown.
	read, total int64
}

// New returns a reporter that writes to out. It redraws a line when out is a
// terminal that understands escape codes.
func New(out io.Writer) *Reporter {
	r := &Reporter{out: out, style: ui.For(out)}

	file, ok := out.(*os.File)
	if ok && term.IsTerminal(int(file.Fd())) && os.Getenv("TERM") != "dumb" {
		r.live = true
		r.width = func() int {
			width, _, err := term.GetSize(int(file.Fd()))
			if err != nil || width <= 0 {
				return 80
			}

			return width
		}
	}

	return r
}

type reporterKey struct{}

type scopeKey struct{}

// With returns a context that carries r.
func With(ctx context.Context, r *Reporter) context.Context {
	return context.WithValue(ctx, reporterKey{}, r)
}

// Scope returns a context whose waits Start shows as "name: wait".
func Scope(ctx context.Context, name string) context.Context {
	return context.WithValue(ctx, scopeKey{}, name)
}

// Start shows a wait until the caller calls the returned function. A wait that
// starts inside another one replaces it until it ends.
func Start(ctx context.Context, format string, args ...any) func() {
	r, _ := ctx.Value(reporterKey{}).(*Reporter)
	if r == nil {
		return func() {}
	}

	text := fmt.Sprintf(format, args...)

	scope, _ := ctx.Value(scopeKey{}).(string)
	if scope != "" {
		text = scope + ": " + text
	}

	return r.start(text, scope)
}

// Reader adds the bytes read from in to the innermost wait of ctx. total is the
// expected size, or -1. Packages install in parallel, so the wait is the newest
// one with the Scope of ctx.
func Reader(ctx context.Context, in io.Reader, total int64) io.Reader {
	r, _ := ctx.Value(reporterKey{}).(*Reporter)
	if r == nil || !r.live {
		return in
	}

	r.mu.Lock()
	defer r.mu.Unlock()

	scope, _ := ctx.Value(scopeKey{}).(string)

	for _, t := range slices.Backward(r.tasks) {
		if t.scope == scope {
			t.total = total

			return &countingReader{in: in, r: r, t: t}
		}
	}

	return in
}

// Writer returns a writer for output that may arrive during a wait, such as a
// build log. It keeps that output off the redrawn line.
func Writer(ctx context.Context, out io.Writer) io.Writer {
	r, _ := ctx.Value(reporterKey{}).(*Reporter)
	if r == nil || !r.live {
		return out
	}

	return &lineWriter{r: r, out: out}
}

// Pause hides the redrawn line until the caller calls the returned function, so
// that another program can ask the user something.
func Pause(ctx context.Context) func() {
	r, _ := ctx.Value(reporterKey{}).(*Reporter)
	if r == nil || !r.live {
		return func() {}
	}

	r.mu.Lock()
	r.paused = true
	fmt.Fprint(r.out, clearLine)
	r.mu.Unlock()

	return func() {
		r.mu.Lock()
		r.paused = false
		r.mu.Unlock()
	}
}

func (r *Reporter) start(text, scope string) func() {
	t := &task{text: text, scope: scope, started: time.Now(), total: -1}

	r.mu.Lock()
	defer r.mu.Unlock()

	r.tasks = append(r.tasks, t)

	if !r.live {
		fmt.Fprintln(r.out, text)
	} else if len(r.tasks) == 1 {
		r.stop, r.done = make(chan struct{}), make(chan struct{})
		r.draw()

		go r.spin(r.stop, r.done)
	}

	return func() { r.end(t) }
}

func (r *Reporter) end(t *task) {
	r.mu.Lock()

	for i, other := range r.tasks {
		if other == t {
			r.tasks = append(r.tasks[:i], r.tasks[i+1:]...)

			break
		}
	}

	if !r.live || len(r.tasks) > 0 {
		r.mu.Unlock()

		return
	}

	stop, done := r.stop, r.done
	r.mu.Unlock()

	close(stop)
	<-done

	r.mu.Lock()
	fmt.Fprint(r.out, clearLine)
	r.mu.Unlock()
}

func (r *Reporter) spin(stop, done chan struct{}) {
	defer close(done)

	ticker := time.NewTicker(interval)
	defer ticker.Stop()

	for {
		select {
		case <-stop:
			return
		case <-ticker.C:
			r.mu.Lock()
			r.frame++
			r.draw()
			r.mu.Unlock()
		}
	}
}

// draw writes the innermost wait. The caller holds mu.
func (r *Reporter) draw() {
	if len(r.tasks) == 0 || r.paused {
		return
	}

	t := r.tasks[len(r.tasks)-1]
	spinner := []rune(frames)
	tail := ""

	switch {
	case t.total > 0:
		tail = fmt.Sprintf(" %s of %s", Size(t.read), Size(t.total))
	case t.read > 0:
		tail = " " + Size(t.read)
	}

	if elapsed := time.Since(t.started); elapsed >= time.Second {
		tail += " " + elapsed.Truncate(time.Second).String()
	}

	// The spinner and its space take two columns, and a full line would wrap.
	room := r.width() - 3 - len(tail)
	text := []rune(t.text)

	if room < 10 {
		room = 10
	}

	// The end of a URL names the file, so draw cuts the middle.
	if len(text) > room {
		head := (room - 3) / 2
		text = append(append(text[:head:head], []rune("...")...), text[len(text)-(room-3-head):]...)
	}

	fmt.Fprintf(
		r.out, "%s%s %s%s",
		clearLine, r.style.Accent(string(spinner[r.frame%len(spinner)])), string(text), r.style.Dim(tail),
	)
}

type countingReader struct {
	in io.Reader
	r  *Reporter
	t  *task
}

func (c *countingReader) Read(p []byte) (int, error) {
	n, err := c.in.Read(p)

	c.r.mu.Lock()
	c.t.read += int64(n)
	c.r.mu.Unlock()

	return n, err
}

// lineWriter passes on whole lines only, because the next redraw would erase
// the start of a line.
type lineWriter struct {
	r       *Reporter
	out     io.Writer
	pending []byte
}

func (w *lineWriter) Write(p []byte) (int, error) {
	w.r.mu.Lock()
	defer w.r.mu.Unlock()

	w.pending = append(w.pending, p...)

	end := bytes.LastIndexByte(w.pending, '\n') + 1
	if end == 0 {
		return len(p), nil
	}

	if len(w.r.tasks) > 0 {
		fmt.Fprint(w.r.out, clearLine)
	}

	_, err := w.out.Write(w.pending[:end])
	w.pending = w.pending[end:]

	w.r.draw()

	return len(p), err
}

// Size formats a byte count, such as "1.5 MiB".
func Size(n int64) string {
	const unit = 1024

	if n < unit {
		return fmt.Sprintf("%d B", n)
	}

	value, suffix := float64(n)/unit, "KiB"
	for _, next := range []string{"MiB", "GiB", "TiB"} {
		if value < unit {
			break
		}

		value, suffix = value/unit, next
	}

	return fmt.Sprintf("%.1f %s", value, suffix)
}
