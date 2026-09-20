package sandbox

import (
	"context"
	"errors"
	"fmt"
	"os/exec"
	"strings"
)

const sandboxExec = "/usr/bin/sandbox-exec"

func unavailable() string {
	if _, err := exec.LookPath(sandboxExec); err != nil {
		return sandboxExec + " is missing"
	}

	return ""
}

func command(ctx context.Context, spec Spec) (*exec.Cmd, string) {
	if why := unavailable(); why != "" {
		return nil, why
	}

	args := append([]string{"-p", profile(spec)}, spec.Argv...)

	return exec.CommandContext(ctx, sandboxExec, args...), ""
}

// profile writes the sandbox rules. The read rule is one deny that leaves out
// the readable paths, because newer macOS does not let a later allow override
// it. Reads of file names and sizes under Home stay allowed, because resolving
// a path to the store passes through Home. File contents and directory listings
// do not.
func profile(spec Spec) string {
	var b strings.Builder

	b.WriteString("(version 1)\n(allow default)\n")

	if !spec.Network {
		b.WriteString("(deny network*)\n")
	}

	if spec.Home != "" {
		fmt.Fprintf(&b, "(deny file-read-data (require-all (subpath %q)", spec.Home)

		for _, path := range append(append([]string{}, spec.Readable...), spec.Writable...) {
			fmt.Fprintf(&b, " (require-not (subpath %q))", path)
		}

		b.WriteString("))\n")
	}

	b.WriteString("(deny file-write*)\n")

	for _, path := range spec.Writable {
		fmt.Fprintf(&b, "(allow file-write* (subpath %q))\n", path)
	}

	// Shells and compilers write to these devices.
	b.WriteString(
		`(allow file-write* (literal "/dev/null") (literal "/dev/tty") (literal "/dev/dtracehelper") (subpath "/dev/fd"))` + "\n",
	)

	return b.String()
}

// Init only exists for the Linux sandbox.
func Init() error {
	return errors.New("sandbox-init is only used on Linux")
}
