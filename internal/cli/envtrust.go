package cli

import (
	"crypto/sha256"
	"encoding/hex"
	"os"
	"os/exec"
	"path/filepath"
	"slices"

	"github.com/y3owk1n/oku/internal/list"
	"github.com/y3owk1n/oku/internal/trust"
)

// newAllow returns the allow of the project in e as it is now. It covers the
// oku.toml, and each overlay and .env file that git tracks, since a pull can
// change those. The files that git does not track are the user's own.
func (e env) newAllow() (*trust.Allow, []string, error) {
	listed, paths, err := e.envFilesOfList()
	if err != nil {
		return nil, nil, err
	}

	allow := &trust.Allow{}

	var tracked []string

	for _, path := range paths {
		if gitTracks(e.project, path) {
			tracked = append(tracked, path)
		} else {
			allow.Untracked = append(allow.Untracked, trust.Untracked{Path: path, ModTime: modTime(path)})
		}
	}

	allow.ListSHA256 = allowDigest(listed, tracked)

	return allow, tracked, nil
}

// allowHolds reports whether the allow of the project in e still covers its
// oku.toml and the files that git tracks. oku asks git about a file that is
// new to the allow, or that git did not track and that changed since. The
// allow keeps a file that git does not track, with its new time.
func (e env) allowHolds(allowed *trust.Allowed) (bool, error) {
	allow, ok := allowed.Get(e.project)
	if !ok {
		return false, nil
	}

	listed, paths, err := e.envFilesOfList()
	if err != nil {
		return false, err
	}

	var tracked []string

	changed := false

	for _, path := range paths {
		i := slices.IndexFunc(allow.Untracked, func(u trust.Untracked) bool { return u.Path == path })

		switch {
		case i < 0 && gitTracks(e.project, path):
			tracked = append(tracked, path)
		case i < 0:
			allow.Untracked = append(allow.Untracked, trust.Untracked{Path: path, ModTime: modTime(path)})
			changed = true
		case modTime(path) == allow.Untracked[i].ModTime:
		case gitTracks(e.project, path):
			tracked = append(tracked, path)
		default:
			allow.Untracked[i].ModTime = modTime(path)
			changed = true
		}
	}

	holds := allowDigest(listed, tracked) == allow.ListSHA256
	if holds && changed {
		// Saving the new time only spares git the next time, so a failure is fine.
		_ = allowed.Set(e.project, &allow)
	}

	return holds, nil
}

// envFilesOfList returns the oku.toml of the project in e and the paths of
// the other files the hook may read: every overlay, whatever OKU_ENV names,
// and the .env files of the list and of each overlay.
func (e env) envFilesOfList() ([]byte, []string, error) {
	listed, err := os.ReadFile(e.listPath())
	if err != nil {
		return nil, nil, err
	}

	l, err := list.Parse(listed, e.listPath())
	if err != nil {
		return nil, nil, err
	}

	overlays, err := filepath.Glob(filepath.Join(e.project, "oku.*.toml"))
	if err != nil {
		return nil, nil, err
	}

	var paths []string

	add := func(files []list.EnvFile) {
		for _, f := range files {
			if path := f.Abs(e.project); !slices.Contains(paths, path) {
				paths = append(paths, path)
			}
		}
	}

	add(l.EnvFiles)

	for _, path := range overlays {
		if filepath.Base(path) == "oku.pkg.toml" {
			continue
		}

		paths = append(paths, path)

		// An overlay that does not parse is refused when the hook reads it.
		if o, err := list.ReadOverlay(path); err == nil {
			add(o.EnvFiles)
		}
	}

	return listed, paths, nil
}

// allowDigest is the sha256 of listed and of each file of tracked, with its
// path.
func allowDigest(listed []byte, tracked []string) string {
	sum := sha256.New()
	sum.Write(listed)

	for _, path := range tracked {
		data, err := os.ReadFile(path)
		if err != nil {
			data = []byte("\x01missing")
		}

		sum.Write([]byte("\x00" + path + "\x00"))
		sum.Write(data)
	}

	return hex.EncodeToString(sum.Sum(nil))
}

// gitTracks reports whether git tracks path in the repo of dir. Without git,
// or outside a repo, nothing is tracked.
func gitTracks(dir, path string) bool {
	return exec.Command("git", "-C", dir, "ls-files", "--error-unmatch", "--", path).Run() == nil
}

// modTime returns the modification time of path in Unix nanoseconds, or 0 when
// it does not exist.
func modTime(path string) int64 {
	info, err := os.Stat(path)
	if err != nil {
		return 0
	}

	return info.ModTime().UnixNano()
}
