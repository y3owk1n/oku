package store

import (
	"bytes"
	"context"
	"crypto/sha256"
	"encoding/hex"
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
	"github.com/y3owk1n/oku/internal/sandbox"
)

// outputTail is how many lines of a failed step's output the error shows.
const outputTail = 40

// Build produces the package of m from source and returns its store path. It
// returns an existing store path untouched, and it removes a half-built one on
// any failure.
//
// {{prefix}} is the final store path, because build systems write it into the
// files they install. The path counts as realized once its meta file exists.
func (s *Store) Build(
	ctx context.Context,
	m *manifest.Manifest,
	p platform.Platform,
	opts BuildOptions,
) (Realized, error) {
	build := m.Build
	deps, log := opts.Deps, opts.Log

	// The deps are part of the hash, so a package rebuilt against another dep
	// version gets another store path.
	depPaths := []string{"build"}
	for _, dep := range deps {
		depPaths = append(depPaths, dep.Prefix)
	}

	prefix := s.pathFor(m, p, depPaths...)

	if exists(filepath.Join(prefix, metaFile)) {
		return Realized{Path: prefix}, nil
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

	for _, dep := range deps {
		vars["dep."+dep.Name+".prefix"] = dep.Prefix
	}

	env := append(linkEnv(deps), []string{
		"PATH=" + joinPaths(append(append(depDirs(deps, "bin"), toolDirs...), "/usr/bin", "/bin")),
		"HOME=" + filepath.Join(work, "home"),
		"TMPDIR=" + filepath.Join(work, "tmp"),
		"OKU_PREFIX=" + prefix, "OKU_SRC=" + src, "OKU_JOBS=" + vars["jobs"],
	}...)

	home, _ := os.UserHomeDir()
	box := sandbox.Spec{
		Home:     home,
		Readable: append([]string{s.dir}, toolDirs...),
		Writable: []string{work, prefix},
	}

	if rustup := rustupHome(build.Needs, home); rustup != "" {
		env = append(env, "RUSTUP_HOME="+rustup)
		box.Readable = append(box.Readable, rustup)

		if toolchain := os.Getenv("RUSTUP_TOOLCHAIN"); toolchain != "" {
			env = append(env, "RUSTUP_TOOLCHAIN="+toolchain)
		}
	}

	result := Realized{Path: prefix}

	var vendored []string

	for i, step := range build.Steps {
		if !step.When.Matches(p) {
			continue
		}

		var err error

		switch {
		case step.Run != nil:
			result.Impure = result.Impure || step.Network
			result.Unsandboxed, err = runCommand(ctx, step, src, vars, env, box, log)
		case step.Vendor != nil:
			var digest string

			digest, result.Unsandboxed, err = runVendor(ctx, *step.Vendor, src, env, box, log)
			vendored = append(vendored, digest)
		default:
			err = s.runStep(ctx, step, src, prefix, vars)
		}

		if err != nil {
			os.RemoveAll(prefix)

			return Realized{}, fmt.Errorf(
				"build.step[%d] (%s) failed: %w", i, strings.Join(step.Kinds(), ","), err,
			)
		}
	}

	if len(vendored) > 0 {
		sum := sha256.Sum256([]byte(strings.Join(vendored, "\n")))
		result.VendorSHA256 = hex.EncodeToString(sum[:])
	}

	// The check comes after the build on purpose. The vendored files decide what
	// was compiled, so a mismatch has to stop the package from being kept.
	if opts.PinnedVendor != "" && opts.PinnedVendor != result.VendorSHA256 {
		os.RemoveAll(prefix)

		return Realized{}, fmt.Errorf(
			"%w: oku.lock pinned %s, this build downloaded %s",
			ErrVendorChanged, opts.PinnedVendor, result.VendorSHA256,
		)
	}

	if entries, _ := os.ReadDir(prefix); len(entries) == 0 {
		os.RemoveAll(prefix)

		return Realized{}, errors.New("the build installed nothing, add an install step")
	}

	meta, err := toml.Marshal(Meta{
		Name: m.Package.Name, Version: m.Version.Value, Platform: p.String(), Impure: result.Impure,
	})
	if err == nil {
		err = os.WriteFile(filepath.Join(prefix, metaFile), meta, 0o644)
	}

	if err != nil {
		os.RemoveAll(prefix)

		return Realized{}, fmt.Errorf("write %s: %w", metaFile, err)
	}

	return result, nil
}

// rustupHome returns the rustup directory when the build needs cargo or rustc
// and rustup manages them. The cargo on PATH is then a rustup proxy that finds
// its toolchain through RUSTUP_HOME, which defaults to a directory in the home
// that the sandbox hides. The toolchain stays read-only, and CARGO_HOME still
// points into the build's temporary HOME.
func rustupHome(needs []string, home string) string {
	if !slices.Contains(needs, "cargo") && !slices.Contains(needs, "rustc") {
		return ""
	}

	dir := os.Getenv("RUSTUP_HOME")
	if dir == "" && home != "" {
		dir = filepath.Join(home, ".rustup")
	}

	if info, err := os.Stat(filepath.Join(dir, "toolchains")); err != nil || !info.IsDir() {
		return ""
	}

	return dir
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

// runStep runs a step that moves files. Run steps go through runCommand.
func (s *Store) runStep(
	ctx context.Context,
	step manifest.Step,
	src, prefix string,
	vars map[string]string,
) error {
	switch {
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

// runCommand runs a run step in the sandbox. The string says why the step ran
// without the sandbox, and is empty when it was sandboxed.
func runCommand(
	ctx context.Context,
	step manifest.Step,
	src string,
	vars map[string]string,
	env []string,
	box sandbox.Spec,
	log io.Writer,
) (string, error) {
	script, err := manifest.Expand(*step.Run, vars)
	if err != nil {
		return "", err
	}

	shell := step.Shell
	if shell == "" {
		if runtime.GOOS == "windows" {
			return "", errors.New("a run step needs shell on Windows")
		}

		shell = "sh"
	}

	args := map[string][]string{
		"sh": {"-e", "-c"}, "bash": {"-e", "-c"}, "pwsh": {"-NoProfile", "-Command"}, "cmd": {"/C"},
	}[shell]
	if args == nil {
		return "", fmt.Errorf("shell %q must be sh, bash, pwsh or cmd", shell)
	}

	box.Argv = append(append([]string{shell}, args...), script)
	box.Dir = src
	box.Network = step.Network
	box.Env = slices.Clone(env)

	for key, value := range step.Env {
		expanded, err := manifest.Expand(value, vars)
		if err != nil {
			return "", err
		}

		box.Env = append(box.Env, key+"="+expanded)
	}

	cmd, why := sandbox.Command(ctx, box)

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

		return why, fmt.Errorf("%w\n%s", err, strings.Join(lines, "\n"))
	}

	return why, nil
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

// BuildOptions are the inputs of a build besides the manifest.
type BuildOptions struct {
	// Deps are the realized build deps.
	Deps []Dep
	// Log receives the output of commands as they run, and may be nil.
	Log io.Writer
	// PinnedVendor is the vendor digest oku.lock recorded, or empty.
	PinnedVendor string
}

// ErrVendorChanged reports vendor steps that downloaded something other than
// what oku.lock pinned.
var ErrVendorChanged = errors.New("the vendored packages changed")

// runVendor downloads a language's packages into the source directory, with the
// network on, and returns the digest of what it downloaded.
func runVendor(
	ctx context.Context,
	kind, src string,
	env []string,
	box sandbox.Spec,
	log io.Writer,
) (digest, unsandboxed string, err error) {
	vendor, ok := vendorKinds[kind]
	if !ok {
		return "", "", fmt.Errorf(
			"vendor %q must be one of %s",
			kind,
			strings.Join(manifest.VendorKinds, ", "),
		)
	}

	tool, err := findTool(vendor.tools, env)
	if err != nil {
		return "", "", fmt.Errorf("vendor %q %w", kind, err)
	}

	script := "tool=" + strconv.Quote(tool) + "\n" + vendor.script
	step := manifest.Step{Run: &script, Shell: "sh", Network: true}

	unsandboxed, err = runCommand(
		ctx,
		step,
		src,
		nil,
		append(slices.Clone(env), vendor.env...),
		box,
		log,
	)
	if err != nil {
		return "", unsandboxed, err
	}

	digest, err = hashTree(filepath.Join(src, filepath.FromSlash(vendor.output)))

	return digest, unsandboxed, err
}

// Dep is a realized package that a build uses.
type Dep struct {
	Name   string
	Prefix string
}

// linkEnv returns the variables that let compilers, linkers, pkg-config and
// cmake find the deps without flags in the manifest. LD_RUN_PATH makes the GNU
// linker record the deps' lib directories in what it links, so the result finds
// its shared libraries at runtime. On macOS a library records its own absolute
// install name, which does the same.
func linkEnv(deps []Dep) []string {
	if len(deps) == 0 {
		return nil
	}

	lib, include := depDirs(deps, "lib"), depDirs(deps, "include")
	pkgconfig := append(depDirs(deps, "lib/pkgconfig"), depDirs(deps, "share/pkgconfig")...)

	return []string{
		"PKG_CONFIG_PATH=" + joinPaths(pkgconfig),
		"CPATH=" + joinPaths(include),
		"LIBRARY_PATH=" + joinPaths(lib),
		"LD_RUN_PATH=" + joinPaths(lib),
		"CMAKE_PREFIX_PATH=" + joinPaths(depDirs(deps, "")),
	}
}

func depDirs(deps []Dep, sub string) []string {
	dirs := make([]string, len(deps))
	for i, dep := range deps {
		dirs[i] = filepath.Join(dep.Prefix, filepath.FromSlash(sub))
	}

	return dirs
}

func joinPaths(dirs []string) string {
	return strings.Join(dirs, string(os.PathListSeparator))
}
