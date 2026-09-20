package main

import (
	"fmt"
	"os"

	"github.com/y3owk1n/oku/internal/cli"
)

// The build sets version through -ldflags "-X main.version=...".
var version = "dev"

func main() {
	if err := cli.NewRootCmd(version).Execute(); err != nil {
		fmt.Fprintln(os.Stderr, "oku:", err)
		os.Exit(1)
	}
}
