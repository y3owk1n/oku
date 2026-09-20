package store

import (
	"bytes"
	"errors"
	"fmt"
	"os"
	"path/filepath"
	"strings"

	"github.com/bluekeyes/go-gitdiff/gitdiff"

	"github.com/y3owk1n/oku/internal/manifest"
)

// applyPatch applies a unified diff to the files under src. It is written in Go
// and does not call the patch program, so a step behaves the same on every OS.
// A hunk that does not fit fails the step. oku applies no hunk at an offset or
// with fuzz.
func applyPatch(src string, step manifest.Patch) error {
	if !filepath.IsLocal(filepath.FromSlash(step.File)) {
		return fmt.Errorf("patch file %s is outside the source directory", step.File)
	}

	data, err := os.ReadFile(filepath.Join(src, filepath.FromSlash(step.File)))
	if err != nil {
		return err
	}

	files, _, err := gitdiff.Parse(bytes.NewReader(data))
	if err != nil {
		return fmt.Errorf("read %s: %w", step.File, err)
	}

	if len(files) == 0 {
		return fmt.Errorf("%s changes no files, is it a unified diff?", step.File)
	}

	for _, file := range files {
		if err := applyFile(src, file, step.Strip); err != nil {
			return fmt.Errorf("%s: %w", step.File, err)
		}
	}

	return nil
}

func applyFile(src string, file *gitdiff.File, strip int) error {
	oldPath, err := patchPath(src, file.OldName, strip)
	if err != nil {
		return err
	}

	newPath, err := patchPath(src, file.NewName, strip)
	if err != nil {
		return err
	}

	if file.IsDelete {
		if err := os.Remove(oldPath); err != nil {
			return missing(err, file.OldName)
		}

		return nil
	}

	var (
		before []byte
		mode   os.FileMode = 0o644
	)

	if !file.IsNew {
		info, err := os.Stat(oldPath)
		if err != nil {
			return missing(err, file.OldName)
		}

		mode = info.Mode().Perm()

		if before, err = os.ReadFile(oldPath); err != nil {
			return err
		}
	}

	if file.NewMode != 0 {
		mode = file.NewMode.Perm()
	}

	var after bytes.Buffer
	if err := gitdiff.Apply(&after, bytes.NewReader(before), file); err != nil {
		var conflict *gitdiff.Conflict
		if errors.As(err, &conflict) {
			return fmt.Errorf("the patch does not fit %s: %w", file.NewName, err)
		}

		return fmt.Errorf("%s: %w", file.NewName, err)
	}

	if err := os.MkdirAll(filepath.Dir(newPath), 0o755); err != nil {
		return err
	}

	if err := os.WriteFile(newPath, after.Bytes(), mode); err != nil {
		return err
	}

	// WriteFile keeps the mode of a file that already exists.
	if err := os.Chmod(newPath, mode); err != nil {
		return err
	}

	if file.IsRename && oldPath != newPath {
		return os.Remove(oldPath)
	}

	return nil
}

// missing explains a file that the diff names and the source does not have. A
// wrong strip is the usual cause.
func missing(err error, name string) error {
	if !errors.Is(err, os.ErrNotExist) {
		return err
	}

	return fmt.Errorf(
		"%s is not in the source directory. A diff without a \"diff --git\" line keeps "+
			"its leading directories, and strip removes them, as -p does for patch", name,
	)
}

// patchPath turns a name from the diff into a path under src. An empty name
// belongs to the missing side of a new or a deleted file.
func patchPath(src, name string, strip int) (string, error) {
	// A patch file with Windows line endings leaves a carriage return on the names
	// in its header.
	name = strings.TrimRight(name, "\r")
	if name == "" {
		return "", nil
	}

	parts := strings.Split(name, "/")
	if strip >= len(parts) {
		return "", fmt.Errorf("strip = %d leaves nothing of %s", strip, name)
	}

	rel := filepath.FromSlash(strings.Join(parts[strip:], "/"))
	if !filepath.IsLocal(rel) {
		return "", fmt.Errorf("%s is outside the source directory", name)
	}

	return filepath.Join(src, rel), nil
}
