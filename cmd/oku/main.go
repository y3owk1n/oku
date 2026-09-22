package main

import (
	"errors"
	"fmt"
	"os"
	"path/filepath"
	"strings"

	"github.com/y3owk1n/oku/internal/cli"
	"github.com/y3owk1n/oku/internal/shim"
	"github.com/y3owk1n/oku/internal/ui"
)

// The build sets version through -ldflags "-X main.version=...".
var version = "dev"

func main() {
	executable, err := os.Executable()
	if err == nil {
		// On Windows a profile's bin holds copies of oku under other names. The
		// check uses the path as started, because the shim file is beside that name.
		if code, handled := shim.Run(executable, os.Args[1:]); handled {
			os.Exit(code)
		}

		executable, err = filepath.EvalSymlinks(executable)
	}

	if err != nil {
		fmt.Fprintln(os.Stderr, "oku: locate the running binary:", err)
		os.Exit(1)
	}

	if err := cli.NewRootCmd(cli.Options{Version: version, Executable: executable}).
		Execute(); err != nil {
		// "oku shell -- command" exits with the command's code and adds no message.
		var exit cli.ExitError
		if errors.As(err, &exit) {
			os.Exit(exit.Code)
		}

		fail(err)
	}
}

// fail prints err as "oku: <reason>" and exits with 1. On a terminal the
// prefix is red, the lines after the first, which say what to do about it,
// are indented, and each `command` to type is in colour.
func fail(err error) {
	s := ui.For(os.Stderr)
	first, rest, more := strings.Cut(suggest(err.Error()), "\n")

	fmt.Fprintln(os.Stderr, s.Wrap(s.Alert("oku:")+" "+s.Code(s.Homes(first)), 2))

	if more {
		if s.On() {
			rest = s.Wrap("  "+strings.ReplaceAll(s.Code(s.Homes(rest)), "\n", "\n  "), 2)
		}

		fmt.Fprintln(os.Stderr, rest)
	}

	os.Exit(1)
}

// suggest puts cobra's "Did you mean this?" list for a mistyped command on the
// line of the error: unknown command "lsit" for "oku", did you mean list?
func suggest(message string) string {
	first, list, ok := strings.Cut(message, "\n\nDid you mean this?\n")
	if !ok {
		return message
	}

	return first + ", did you mean " + strings.Join(strings.Fields(list), " or ") + "?"
}
