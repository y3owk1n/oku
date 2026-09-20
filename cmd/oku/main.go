package main

import (
	"fmt"
	"os"
	"path/filepath"

	"github.com/y3owk1n/oku/internal/cli"
)

// The build sets version through -ldflags "-X main.version=...".
var version = "dev"

func main() {
	executable, err := os.Executable()
	if err == nil {
		executable, err = filepath.EvalSymlinks(executable)
	}

	if err != nil {
		fmt.Fprintln(os.Stderr, "oku: locate the running binary:", err)
		os.Exit(1)
	}

	if err := cli.NewRootCmd(version, executable).Execute(); err != nil {
		fmt.Fprintln(os.Stderr, "oku:", err)
		os.Exit(1)
	}
}
