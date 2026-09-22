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
// prefix is red, and the lines after the first, which say what to do about
// it, are indented and dim.
func fail(err error) {
	s := ui.For(os.Stderr)
	first, rest, more := strings.Cut(err.Error(), "\n")

	fmt.Fprintln(os.Stderr, s.Alert("oku:"), first)

	if more {
		if s.On() {
			rest = "  " + strings.ReplaceAll(rest, "\n", "\n  ")
		}

		fmt.Fprintln(os.Stderr, s.Dim(rest))
	}

	os.Exit(1)
}
