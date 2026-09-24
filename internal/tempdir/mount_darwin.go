package tempdir

import (
	"fmt"
	"os/exec"
	"path/filepath"
	"strings"
	"syscall"
)

// unmount detaches the disk image mounted at path, when one is.
func unmount(path string) error {
	if !mounted(path) {
		return nil
	}

	out, err := exec.Command("/usr/bin/hdiutil", "detach", "-force", path).CombinedOutput()
	if err != nil || mounted(path) {
		return fmt.Errorf("detach the disk image at %s: %v: %s", path, err, strings.TrimSpace(string(out)))
	}

	return nil
}

// mounted reports whether a volume is mounted at path, which then sits on
// another device than its parent.
func mounted(path string) bool {
	var at, parent syscall.Stat_t
	if syscall.Stat(path, &at) != nil || syscall.Stat(filepath.Dir(path), &parent) != nil {
		return false
	}

	return at.Dev != parent.Dev
}

// Mounted returns where each image attached from the file at image is mounted.
func Mounted(image string) ([]string, error) {
	want, err := filepath.EvalSymlinks(image)
	if err != nil {
		return nil, err
	}

	out, err := exec.Command("/usr/bin/hdiutil", "info").Output()
	if err != nil {
		return nil, fmt.Errorf("hdiutil info: %w", err)
	}

	var points []string

	for _, block := range strings.Split(string(out), "\n=====") {
		matches := false

		for _, line := range strings.Split(block, "\n") {
			if key, value, ok := strings.Cut(line, ":"); ok && strings.TrimSpace(key) == "image-path" {
				got, err := filepath.EvalSymlinks(strings.TrimSpace(value))
				matches = err == nil && got == want
			}
		}

		if !matches {
			continue
		}

		for _, line := range strings.Split(block, "\n") {
			fields := strings.Split(line, "\t")
			if strings.HasPrefix(line, "/dev/") && len(fields) >= 3 && strings.TrimSpace(fields[2]) != "" {
				points = append(points, strings.TrimSpace(fields[2]))
			}
		}
	}

	return points, nil
}
