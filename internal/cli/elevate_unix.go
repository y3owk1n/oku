//go:build !windows

package cli

import (
	"context"
	"errors"
	"fmt"
	"io/fs"
	"os"
	"os/exec"
	"os/user"
	"syscall"
)

// elevatedCommand returns the command that runs argv with administrator rights.
func elevatedCommand(ctx context.Context, argv []string) *exec.Cmd {
	// A program on the user's PATH called sudo, which a package could ship,
	// would get the password.
	if os.Geteuid() != 0 {
		argv = append([]string{"/usr/bin/sudo"}, argv...)
	}

	return exec.CommandContext(ctx, argv[0], argv[1:]...)
}

// createRootArgv creates dir and makes owner its owner.
func createRootArgv(dir string, owner *user.User) []string {
	return []string{"install", "-d", "-m", "0755", "-o", owner.Uid, "-g", owner.Gid, dir}
}

func removeDirArgv(dir string) []string { return []string{"rmdir", dir} }

// checkRootOwner fails when dir exists and belongs to someone other than root
// or the user, who could have put files in it that oku would then run.
func checkRootOwner(dir string) error {
	info, err := os.Lstat(dir)
	if errors.Is(err, fs.ErrNotExist) {
		return nil
	}

	if err != nil {
		return err
	}

	stat, ok := info.Sys().(*syscall.Stat_t)
	if info.Mode()&fs.ModeSymlink != 0 || !ok || stat.Uid != 0 && int(stat.Uid) != os.Getuid() {
		return fmt.Errorf("%s exists and belongs to another user, remove it or give it to root first", dir)
	}

	return nil
}
