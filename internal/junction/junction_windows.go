package junction

import (
	"fmt"
	"os"
	"os/exec"
	"strings"
	"syscall"
)

// Make makes link a junction to the directory target. mklink is part of cmd,
// which reads &, ^ and % in a path as commands, so the paths go through the
// environment and cmd expands them inside quotes, where they stay paths.
func Make(link, target string) error {
	cmd := exec.Command("cmd")
	cmd.SysProcAttr = &syscall.SysProcAttr{
		CmdLine: `cmd /c mklink /J "%OKU_JUNCTION_LINK%" "%OKU_JUNCTION_TARGET%"`,
	}
	cmd.Env = append(os.Environ(), "OKU_JUNCTION_LINK="+link, "OKU_JUNCTION_TARGET="+target)

	if out, err := cmd.CombinedOutput(); err != nil {
		return fmt.Errorf("mklink /J: %w: %s", err, strings.TrimSpace(string(out)))
	}

	return nil
}
