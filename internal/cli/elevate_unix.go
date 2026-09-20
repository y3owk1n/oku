//go:build !windows

package cli

import (
	"context"
	"os"
	"os/exec"
	"os/user"
)

// elevatedCommand returns the command that runs argv with administrator rights.
func elevatedCommand(ctx context.Context, argv []string) *exec.Cmd {
	if os.Geteuid() != 0 {
		argv = append([]string{"sudo"}, argv...)
	}

	return exec.CommandContext(ctx, argv[0], argv[1:]...)
}

// createRootArgv creates dir and makes owner its owner.
func createRootArgv(dir string, owner *user.User) []string {
	return []string{"install", "-d", "-m", "0755", "-o", owner.Uid, "-g", owner.Gid, dir}
}

func removeDirArgv(dir string) []string { return []string{"rmdir", dir} }
