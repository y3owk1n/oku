package store

import (
	"bytes"
	"context"
	"errors"
	"fmt"
	"io"
	"os"
	"os/exec"
	"path"
	"path/filepath"
	"runtime"
	"slices"
	"strings"

	"github.com/y3owk1n/oku/internal/manifest"
	"github.com/y3owk1n/oku/internal/sandbox"
)

// generateCompletions runs c.Generate once per shell in dir, with env, and
// writes what each run prints under prefix/share/completions. The string says
// why the command ran without the sandbox, and is empty when it was sandboxed.
func generateCompletions(
	ctx context.Context,
	c manifest.Completions,
	dir, prefix string,
	env []string,
	box sandbox.Spec,
	log io.Writer,
) (string, error) {
	shell, args := "sh", []string{"-c"}
	if runtime.GOOS == "windows" {
		shell, args = "cmd", []string{"/C"}
	}

	var why string

	for _, name := range manifest.Shells {
		command, err := manifest.Expand(c.Generate, map[string]string{"shell": name})
		if err != nil {
			return "", fmt.Errorf("completions.generate: %w", err)
		}

		box.Argv = append(append([]string{shell}, args...), command)
		box.Dir = dir
		box.Env = slices.Clone(env)

		var cmd *exec.Cmd

		cmd, why = sandbox.Command(ctx, box)

		var stdout, stderr bytes.Buffer

		cmd.Stdout, cmd.Stderr = &stdout, &stderr
		if log != nil {
			cmd.Stderr = io.MultiWriter(&stderr, log)
		}

		err = cmd.Run()
		if err == nil && stdout.Len() == 0 {
			err = errors.New("printed nothing")
		}

		if err != nil {
			lines := strings.Split(strings.TrimRight(stderr.String(), "\n"), "\n")
			if len(lines) > outputTail {
				lines = lines[len(lines)-outputTail:]
			}

			return why, fmt.Errorf(
				"completions.generate for %s failed: %s: %w\n%s",
				name, command, err, strings.Join(lines, "\n"),
			)
		}

		to := filepath.Join(
			prefix, "share", "completions", name, manifest.File(name, c.Name),
		)
		if err := os.MkdirAll(filepath.Dir(to), 0o755); err != nil {
			return why, err
		}

		if err := os.WriteFile(to, stdout.Bytes(), 0o644); err != nil {
			return why, err
		}
	}

	return why, nil
}

// completionDest names where the completion file of shell goes in a package.
func completionDest(shell, file string) string {
	return path.Join("share", "completions", shell, path.Base(file))
}

// completionPaths returns the files of the path form of c that exist under
// root. A directory shorthand skips the shells the package lacks, and fails
// only when it holds none of them.
func completionPaths(c manifest.Completions, root string) (map[string]string, error) {
	if !c.FromDir {
		return c.Paths, nil
	}

	found := map[string]string{}

	for shell, file := range c.Paths {
		if exists(filepath.Join(root, filepath.FromSlash(file))) {
			found[shell] = file
		}
	}

	if len(found) == 0 {
		return nil, fmt.Errorf(
			"completions %q: no %s, %s or %s in the package",
			path.Dir(c.Paths["fish"]),
			path.Base(c.Paths["fish"]), path.Base(c.Paths["zsh"]), path.Base(c.Paths["bash"]),
		)
	}

	return found, nil
}

// withBinFirst returns env with dir put first on PATH.
func withBinFirst(env []string, dir string) []string {
	out := slices.Clone(env)

	for i, entry := range out {
		if strings.HasPrefix(entry, "PATH=") {
			out[i] = "PATH=" + joinPaths([]string{dir, strings.TrimPrefix(entry, "PATH=")})
		}
	}

	return out
}
