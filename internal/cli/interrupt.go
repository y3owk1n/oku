package cli

import (
	"io"
	"os"
	"os/signal"
	"syscall"
)

// holdInterrupts keeps Ctrl-C and SIGTERM from killing oku until the returned
// func runs, so that a change it has begun finishes or is undone. A program
// that oku runs meanwhile, such as sudo, still gets Ctrl-C from the terminal.
func holdInterrupts(w io.Writer) func() {
	signals := make(chan os.Signal, 1)
	signal.Notify(signals, os.Interrupt, syscall.SIGTERM)

	done := make(chan struct{})

	go func() {
		select {
		case <-signals:
			warn(w, "oku finishes or undoes the change first")
		case <-done:
		}
	}()

	return func() {
		signal.Stop(signals)
		close(done)
	}
}
