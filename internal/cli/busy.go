package cli

import (
	"fmt"

	"github.com/spf13/cobra"

	"github.com/y3owk1n/oku/internal/busy"
	"github.com/y3owk1n/oku/internal/dirs"
)

// exclusive are the commands that change what oku keeps. Two at once would
// undo each other's change or delete a package the other one is building, so
// each waits for the other. "shell" and "self uninstall" take the lock for part
// of their run.
var exclusive = [][]string{
	{"add"},
	{"remove"},
	{"sync"},
	{"update"},
	{"rollback"},
	{"gc"},
	{"allow"},
	{"deny"},
	{"source", "add"},
	{"source", "remove"},
	{"cache", "add"},
	{"cache", "remove"},
	{"cache", "push"},
	{"key", "generate"},
	{"key", "trust"},
	{"key", "revoke"},
}

// oneAtATime makes cmd hold the lock for its whole run.
func oneAtATime(cmd *cobra.Command) {
	run := cmd.RunE
	cmd.RunE = func(cmd *cobra.Command, args []string) error {
		// "add --plan" and "add --manifest" only read, so they do not wait.
		plan, _ := cmd.Flags().GetBool("plan")
		printed, _ := cmd.Flags().GetBool("manifest")

		if plan || printed {
			return run(cmd, args)
		}

		release, err := lockMachine(cmd)
		if err != nil {
			return err
		}
		defer release()

		return run(cmd, args)
	}
}

// lockMachine waits until no other oku process changes the machine, and
// returns the function that releases the lock.
func lockMachine(cmd *cobra.Command) (func(), error) {
	data, err := dirs.Data()
	if err != nil {
		return nil, err
	}

	return busy.Lock(cmd.Context(), data, func(pid int) {
		holder := "another oku process"
		if pid > 0 {
			holder = fmt.Sprintf("oku process %d", pid)
		}

		fmt.Fprintf(cmd.ErrOrStderr(), "waiting for %s to finish\n", holder)
	})
}
