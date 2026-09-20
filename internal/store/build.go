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
	"strconv"
	"strings"

	"github.com/pelletier/go-toml/v2"

	"github.com/y3owk1n/oku/internal/manifest"
	"github.com/y3owk1n/oku/internal/platform"
)

// outputTail is how many lines of a failed step's output the error shows.
const outputTail = 40

// Build produces the package of m from source and returns its store path. It
// returns an existing store path untouched, and it removes a half-built one on
// any failure. log receives the output of run steps as they run, and may be nil.
//
// {{prefix}} is the final store path, because build systems write it into the
// files they install. The path counts as realized once its meta file exists.
func (s *Store) Build(
	ctx context.Context,
	m *manifest.Manifest,
	p platform.Platform,
	log io.Writer,
) (Realized, error) {
	build := m.Build
	prefix := s.pathFor(m, p, "build")

	if exists(filepath.Join(prefix, metaFile)) {
		return Realized{Path: prefix}, nil
	}

	if len(build.Deps) > 0 {
		return Realized{}, errors.New("build.deps are not supported yet")
	}

	toolDirs, err := findNeeds(build.Needs)
	if err != nil {
		return Realized{}, err
	}

	work, err := os.MkdirTemp("", "oku-build-")
	if err != nil {
		return Realized{}, err
	}
	defer os.RemoveAll(work)

	src := filepath.Join(work, "src")

	for _, dir := range []string{src, filepath.Join(work, "home"), filepath.Join(work, "tmp")} {
		if err := os.Mkdir(dir, 0o755); err != nil {
			return Realized{}, err
		}
	}

	vars := map[string]string{
		"version": m.Version.Value, "tag": m.Tag,
		"os": p.OS, "arch": p.Arch, "libc": p.Libc,
		"prefix": prefix, "src": src, "jobs": strconv.Itoa(runtime.NumCPU()),
	}

	if err := s.fetchSource(ctx, build.Source, src, vars); err != nil {
		return Realized{}, fmt.Errorf("fetch the source: %w", err)
	}

	// A crashed build may have left the prefix behind without a meta file.
	if err := os.RemoveAll(prefix); err != nil {
		return Realized{}, err
	}

	if err := os.MkdirAll(prefix, 0o755); err != nil {
		return Realized{}, fmt.Errorf("create store: %w", err)
	}

	env := []string{
		"PATH=" + strings.Join(append(toolDirs, "/usr/bin", "/bin"), string(os.PathListSeparator)),
		"HOME=" + filepath.Join(work, "home"),
		"TMPDIR=" + filepath.Join(work, "tmp"),
		"OKU_PREFIX=" + prefix, "OKU_SRC=" + src, "OKU_JOBS=" + vars["jobs"],
	}

	for i, step := range build.Steps {
		if !step.When.Matches(p) {
			continue
		}

		if err := s.runStep(ctx, step, src, prefix, vars, env, log); err != nil {
			os.RemoveAll(prefix)

			return Realized{}, fmt.Errorf(
				"build.step[%d] (%s) failed: %w",
				i,
				strings.Join(step.Kinds(), ","),
				err,
			)
		}
	}

	if entries, _ := os.ReadDir(prefix); len(entries) == 0 {
		os.RemoveAll(prefix)

		return Realized{}, errors.New("the build installed nothing, add an install step")
	}

	meta, err := toml.Marshal(
		Meta{Name: m.Package.Name, Version: m.Version.Value, Platform: p.String()},
	)
	if err == nil {
		err = os.WriteFile(filepath.Join(prefix, metaFile), meta, 0o644)
	}

	if err != nil {
		os.RemoveAll(prefix)

		return Realized{}, fmt.Errorf("write %s: %w", metaFile, err)
	}

	return Realized{Path: prefix}, nil
}

// findNeeds returns the directories of the needed tools, and fails on the first
// tool that is missing.
func findNeeds(needs []string) ([]string, error) {
	var dirs []string

	for _, tool := range needs {
		found, err := exec.LookPath(tool)
		if err != nil {
			return nil, fmt.Errorf("the build needs %q, which is not on PATH", tool)
		}

		if dir := filepath.Dir(found); !slices.Contains(dirs, dir) {
			dirs = append(dirs, dir)
		}
	}

	return dirs, nil
}

func (s *Store) fetchSource(
	ctx context.Context,
	source manifest.Source,
	src string,
	vars map[string]string,
) error {
	switch {
	case source.Git != "":
		tag, err := manifest.Expand(source.Tag, vars)
		if err != nil {
			return err
		}

		args := []string{"clone", "--quiet", "--depth", "1"}
		if tag != "" {
			args = append(args, "--branch", tag)
		}

		cmd := exec.CommandContext(ctx, "git", append(args, "--", source.Git, src)...)
		cmd.Env = append(os.Environ(), "GIT_TERMINAL_PROMPT=0")

		if out, err := cmd.CombinedOutput(); err != nil {
			return fmt.Errorf(
				"git clone %s: %w: %s",
				source.Git,
				err,
				strings.TrimSpace(string(out)),
			)
		}

		return nil
	case source.URL != "":
		if source.SHA256 == "" {
			return errors.New("build.source.url needs sha256")
		}

		url, err := manifest.Expand(source.URL, vars)
		if err != nil {
			return err
		}

		download, _, err := s.fetch(ctx, url, source.SHA256)
		if err != nil {
			return err
		}

		return extract(download, src, source.Strip)
	default:
		return nil
	}
}

func (s *Store) runStep(
	ctx context.Context,
	step manifest.Step,
	src, prefix string,
	vars map[string]string,
	env []string,
	log io.Writer,
) error {
	switch {
	case step.Run != nil:
		return runCommand(ctx, step, src, vars, env, log)
	case step.Install != nil:
		return installFiles(*step.Install, src, prefix)
	case step.Copy != nil:
		return copyInto(src, step.Copy.From, prefix, step.Copy.To, 0)
	case step.Fetch != nil:
		url, err := manifest.Expand(step.Fetch.URL, vars)
		if err != nil {
			return err
		}

		download, _, err := s.fetch(ctx, url, step.Fetch.SHA256)
		if err != nil {
			return err
		}

		return copyInto(filepath.Dir(download), filepath.Base(download), src, step.Fetch.To, 0)
	case step.Extract != nil:
		if !filepath.IsLocal(step.Extract.File) || !filepath.IsLocal(step.Extract.To) {
			return errors.New("extract paths must stay inside the source directory")
		}

		to := filepath.Join(src, step.Extract.To)
		if err := os.MkdirAll(to, 0o755); err != nil {
			return err
		}

		return extract(filepath.Join(src, step.Extract.File), to, step.Extract.Strip)
	default:
		return fmt.Errorf("%s steps are not supported yet", strings.Join(step.Kinds(), ","))
	}
}

func runCommand(
	ctx context.Context,
	step manifest.Step,
	src string,
	vars map[string]string,
	env []string,
	log io.Writer,
) error {
	script, err := manifest.Expand(*step.Run, vars)
	if err != nil {
		return err
	}

	shell := step.Shell
	if shell == "" {
		if runtime.GOOS == "windows" {
			return errors.New("a run step needs shell on Windows")
		}

		shell = "sh"
	}

	args := map[string][]string{
		"sh": {"-e", "-c"}, "bash": {"-e", "-c"}, "pwsh": {"-NoProfile", "-Command"}, "cmd": {"/C"},
	}[shell]
	if args == nil {
		return fmt.Errorf("shell %q must be sh, bash, pwsh or cmd", shell)
	}

	cmd := exec.CommandContext(ctx, shell, append(args, script)...)
	cmd.Dir = src
	cmd.Env = slices.Clone(env)

	for key, value := range step.Env {
		expanded, err := manifest.Expand(value, vars)
		if err != nil {
			return err
		}

		cmd.Env = append(cmd.Env, key+"="+expanded)
	}

	var output bytes.Buffer

	cmd.Stdout, cmd.Stderr = &output, &output
	if log != nil {
		cmd.Stdout, cmd.Stderr = io.MultiWriter(&output, log), io.MultiWriter(&output, log)
	}

	if err := cmd.Run(); err != nil {
		lines := strings.Split(strings.TrimRight(output.String(), "\n"), "\n")
		if len(lines) > outputTail {
			lines = lines[len(lines)-outputTail:]
		}

		return fmt.Errorf("%w\n%s", err, strings.Join(lines, "\n"))
	}

	return nil
}

// installFiles copies the named files from src into the package layout.
func installFiles(in manifest.Install, src, prefix string) error {
	groups := []struct {
		files []string
		dir   func(file string) (string, error)
		mode  os.FileMode
	}{
		{in.Bin, fixedDir("bin"), 0o755},
		{in.Lib, fixedDir("lib"), 0},
		{in.Include, fixedDir("include"), 0},
		{in.Share, fixedDir("share"), 0},
		{in.Man, func(file string) (string, error) {
			section := manSectionRe.FindStringSubmatch(file)
			if section == nil {
				return "", fmt.Errorf("man %q: the file name has no section such as .1", file)
			}

			return path.Join("share", "man", "man"+section[1]), nil
		}, 0},
	}

	for _, group := range groups {
		for _, file := range group.files {
			dir, err := group.dir(file)
			if err != nil {
				return err
			}

			if err := copyInto(
				src,
				file,
				prefix,
				path.Join(dir, path.Base(file)),
				group.mode,
			); err != nil {
				return err
			}
		}
	}

	for shell, file := range in.Completions {
		to := path.Join("share", "completions", shell, path.Base(file))
		if err := copyInto(src, file, prefix, to, 0); err != nil {
			return err
		}
	}

	return nil
}

func fixedDir(dir string) func(string) (string, error) {
	return func(string) (string, error) { return dir, nil }
}

// copyInto copies fromDir/from to toDir/to. Both relative paths must stay inside
// their directory. A zero mode keeps the source file's mode.
func copyInto(fromDir, from, toDir, to string, mode os.FileMode) error {
	if !filepath.IsLocal(filepath.FromSlash(from)) || !filepath.IsLocal(filepath.FromSlash(to)) {
		return fmt.Errorf("%q or %q points outside its directory", from, to)
	}

	source := filepath.Join(fromDir, filepath.FromSlash(from))

	info, err := os.Stat(source)
	if err != nil {
		return fmt.Errorf("%s: no such file in the source directory", from)
	}

	if !info.Mode().IsRegular() {
		return fmt.Errorf("%s is not a regular file", from)
	}

	if mode == 0 {
		mode = info.Mode().Perm() | 0o600
	}

	dest := filepath.Join(toDir, filepath.FromSlash(to))
	if err := copyFile(source, dest); err != nil {
		return err
	}

	return os.Chmod(dest, mode)
}
