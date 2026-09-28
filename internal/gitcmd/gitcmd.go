// Package gitcmd runs git so that it fails rather than waits on a prompt.
package gitcmd

import (
	"context"
	"os"
	"os/exec"
	"path/filepath"
	"strings"
)

// Command returns git with args, set so that neither git nor ssh asks a
// question. A URL goes over ssh when the user's git config rewrites it. ssh then
// asks on the terminal, where oku's progress line hides the question, and waits
// forever.
func Command(ctx context.Context, args ...string) *exec.Cmd {
	cmd := exec.CommandContext(ctx, "git", args...)
	cmd.Env = append(os.Environ(), "GIT_TERMINAL_PROMPT=0")

	if ssh := batchSSH(ctx); ssh != "" {
		cmd.Env = append(cmd.Env, "GIT_SSH_COMMAND="+ssh)
	}

	return cmd
}

// batchSSH returns the ssh command git would run, with BatchMode added. It
// returns "" when GIT_SSH names the program, or when the command is not
// OpenSSH, such as plink, which takes other options. Git then runs its own
// choice.
func batchSSH(ctx context.Context) string {
	if os.Getenv("GIT_SSH") != "" {
		return ""
	}

	ssh := os.Getenv("GIT_SSH_COMMAND")
	if ssh == "" {
		out, _ := exec.CommandContext(ctx, "git", "config", "--get", "core.sshCommand").Output()
		ssh = strings.TrimSpace(string(out))
	}

	if ssh == "" {
		return "ssh -o BatchMode=yes"
	}

	if strings.TrimSuffix(filepath.Base(program(ssh)), ".exe") != "ssh" {
		return ""
	}

	return ssh + " -o BatchMode=yes"
}

// program is the first word of a shell command, which may be quoted.
func program(command string) string {
	if q := command[0]; q == '"' || q == '\'' {
		if end := strings.IndexByte(command[1:], q); end >= 0 {
			return command[1 : end+1]
		}
	}

	name, _, _ := strings.Cut(command, " ")

	return name
}
