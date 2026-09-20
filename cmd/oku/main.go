package main

import (
	"errors"
	"fmt"
	"os"
	"path/filepath"

	"github.com/y3owk1n/oku/internal/cli"
	"github.com/y3owk1n/oku/internal/shim"
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

		fmt.Fprintln(os.Stderr, "oku:", err)
		os.Exit(1)
	}
}
