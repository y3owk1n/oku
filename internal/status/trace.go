package status

import (
	"context"
	"fmt"
	"os"
	"sync"
	"time"
)

// TraceEnv names the file that oku appends a line to for each request and each
// wait, to show where a command spends its time.
const TraceEnv = "OKU_TRACE"

// started is when the process started, which each trace line counts from.
var started = time.Now()

var traceMu sync.Mutex

// Trace starts a trace line for what, in the package of ctx, and returns the
// function that ends it with the outcome. It does nothing unless TraceEnv
// names a file.
func Trace(ctx context.Context, format string, args ...any) func(outcome string) {
	path := os.Getenv(TraceEnv)
	if path == "" {
		return func(string) {}
	}

	what := fmt.Sprintf(format, args...)
	own, _ := ctx.Value(scopeKey{}).(scope)
	at := time.Now()

	return func(outcome string) {
		writeTrace(path, fmt.Sprintf("%8.3fs %8.3fs\t%s\t%s\t%s\n",
			at.Sub(started).Seconds(), time.Since(at).Seconds(), own.label, what, outcome))
	}
}

// writeTrace appends line to the trace file at path. A trace only helps the
// user find a slow step, so a file oku cannot write is no error.
func writeTrace(path, line string) {
	traceMu.Lock()
	defer traceMu.Unlock()

	f, err := os.OpenFile(path, os.O_CREATE|os.O_WRONLY|os.O_APPEND, 0o600)
	if err != nil {
		return
	}

	_, _ = f.WriteString(line)
	_ = f.Close()
}
