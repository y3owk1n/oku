// Package busy lets one oku process at a time change the machine. Two
// changes at once would read each other's pending change as a crash and undo it.
package busy

import (
	"context"
	"errors"
	"fmt"
	"os"
	"path/filepath"
	"strconv"
	"strings"
	"time"
)

// fileName is the file in the data directory that oku locks.
const fileName = "busy"

// errHeld is what tryLock returns while another process holds the lock.
var errHeld = errors.New("held by another process")

// poll is how often a waiting process tries the lock again.
const poll = 100 * time.Millisecond

// Lock takes the lock in dir, waiting while another process holds it. It calls
// waiting once, with the pid of the holder, before the first wait. The OS
// releases the lock when the process exits, so a killed process leaves none.
func Lock(ctx context.Context, dir string, waiting func(pid int)) (release func(), err error) {
	if err := os.MkdirAll(dir, 0o755); err != nil {
		return nil, err
	}

	path := filepath.Join(dir, fileName)

	f, err := os.OpenFile(path, os.O_RDWR|os.O_CREATE, 0o644)
	if err != nil {
		return nil, fmt.Errorf("open %s: %w", path, err)
	}

	told := false

	for {
		err := tryLock(f)
		if err == nil {
			break
		}

		if !errors.Is(err, errHeld) {
			f.Close()

			return nil, fmt.Errorf("lock %s: %w", path, err)
		}

		if !told {
			told = true

			waiting(holder(path))
		}

		select {
		case <-ctx.Done():
			f.Close()

			return nil, ctx.Err()
		case <-time.After(poll):
		}
	}

	// The pid is only for the message of a process that waits.
	if err := f.Truncate(0); err == nil {
		_, _ = f.WriteAt([]byte(strconv.Itoa(os.Getpid())), 0)
	}

	return func() { f.Close() }, nil
}

// holder returns the pid written by the process that holds the lock, or 0.
func holder(path string) int {
	data, err := os.ReadFile(path)
	if err != nil {
		return 0
	}

	pid, _ := strconv.Atoi(strings.TrimSpace(string(data)))

	return pid
}
