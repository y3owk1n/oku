package store

import (
	"bytes"
	"cmp"
	"context"
	"crypto/sha256"
	"encoding/hex"
	"errors"
	"fmt"
	"io"
	"io/fs"
	"maps"
	"os"
	"os/exec"
	"path"
	"path/filepath"
	"runtime"
	"slices"
	"strconv"
	"strings"
	"time"

	"github.com/pelletier/go-toml/v2"

	"github.com/y3owk1n/oku/internal/clone"
	"github.com/y3owk1n/oku/internal/expose"
	"github.com/y3owk1n/oku/internal/goproxy"
	"github.com/y3owk1n/oku/internal/manifest"
	"github.com/y3owk1n/oku/internal/npm"
	"github.com/y3owk1n/oku/internal/platform"
	"github.com/y3owk1n/oku/internal/pypi"
	"github.com/y3owk1n/oku/internal/sandbox"
	"github.com/y3owk1n/oku/internal/shim"
	"github.com/y3owk1n/oku/internal/status"
	"github.com/y3owk1n/oku/internal/tempdir"
)

// outputTail is how many lines of a failed step's output the error shows.
const outputTail = 40

// BuildPath names the store path that building m against deps produces.
//
// The deps are part of the hash, so a package rebuilt against another dep
// version gets another store path. A dep counts by its store name, which is the
// same under every store root. A build that is not relocatable can contain its
// own path, so its hash covers the store root too, and a cache never offers it
// to a machine with another root.
func (s *Store) BuildPath(m *manifest.Manifest, p platform.Platform, deps []Dep) string {
	extra := []string{"build"}
	for _, dep := range deps {
		extra = append(extra, filepath.Base(dep.Prefix))
	}

	if !m.Package.Relocatable {
		extra = append(extra, "root", s.dir)
	}

	// A wrapper that puts its runtime deps on PATH differs from the ones older
	// oku wrote, so it gets a store path of its own.
	if len(m.Runtime.Deps) > 0 && slices.ContainsFunc(m.Build.Steps, func(step manifest.Step) bool {
		return step.Install != nil && len(step.Install.Wrap) > 0
	}) {
		extra = append(extra, "path")
	}

	return s.pathFor(m, p, extra...)
}

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
) (_ Realized, failed error) {
	build := m.Build
	deps, log := opts.Deps, opts.Log
	if log != nil {
		log = status.Writer(ctx, log)
	}

	prefix := s.BuildPath(m, p, deps)

	// A rebuild that a crash interrupted left the old build aside.
	if old := prefix + ".old"; !exists(filepath.Join(prefix, metaFile)) &&
		exists(filepath.Join(old, metaFile)) {
		removeTree(prefix)

		if err := os.Rename(old, prefix); err != nil {
			return Realized{}, err
		}
	}

	// A build that is in the store reports what it pinned when it ran, so the
	// lock keeps those pins. A path from before oku recorded them reports none.
	if meta, err := ReadMeta(prefix); err == nil && !opts.VendorOnly && !opts.Rebuild {
		if opts.PinnedVendor != "" && meta.VendorSHA256 != "" &&
			opts.PinnedVendor != meta.VendorSHA256 {
			return Realized{}, fmt.Errorf(
				"%w: oku.lock pinned %s, the build in the store downloaded %s",
				ErrVendorChanged, opts.PinnedVendor, meta.VendorSHA256,
			)
		}

		return Realized{
			Path: prefix, Impure: meta.Impure, VendorSHA256: meta.VendorSHA256,
			SourceURL: meta.URL, SHA256: meta.SHA256,
		}, nil
	}

	tools, err := findNeeds(build.Needs)
	if err != nil {
		return Realized{}, err
	}

	work, err := tempdir.Dir("build")
	if err != nil {
		return Realized{}, err
	}
	defer removeTree(work)

	// The temporary directory of macOS is behind a symlink. npm writes the keys of
	// its lockfile relative to the real path, so a prefix behind a symlink would
	// put the name of this directory into the vendored files. On Windows this
	// would only turn a short path such as RUNNER~1 into the long one.
	if runtime.GOOS != "windows" {
		if work, err = filepath.EvalSymlinks(work); err != nil {
			return Realized{}, err
		}
	}

	src := filepath.Join(work, "src")

	// The packages of another platform never enter the store.
	if opts.VendorOnly {
		prefix = filepath.Join(work, "prefix")
	}

	for _, dir := range []string{src, filepath.Join(work, "home"), filepath.Join(work, "tmp")} {
		if err := os.Mkdir(dir, 0o755); err != nil {
			return Realized{}, err
		}
	}

	toolDirs, toolBin, err := linkNeeds(filepath.Join(work, "needs"), tools)
	if err != nil {
		return Realized{}, err
	}

	vars := map[string]string{
		"version": m.Version.Value, "tag": m.Tag,
		"os": p.OS, "arch": p.Arch, "libc": p.Libc,
		"prefix": prefix, "src": src, "jobs": strconv.Itoa(runtime.NumCPU()),
	}

	// A branch moves with every push, so its source is the commit of the version.
	commit := ""
	if m.Version.From == manifest.FromGitBranch {
		commit = m.TagCommit
	}

	source, err := s.fetchSource(ctx, build.Source, src, vars, opts.PinnedSource, commit)
	if err != nil {
		return Realized{}, fmt.Errorf("fetch the source: %w", err)
	}

	if err := checkTagCommit(ctx, m, src); err != nil {
		return Realized{}, err
	}

	// Every generation that holds this package points at prefix, so a build that
	// fails must leave the old one where it was.
	if opts.Rebuild && exists(filepath.Join(prefix, metaFile)) {
		old := prefix + ".old"
		if err := os.RemoveAll(old); err != nil {
			return Realized{}, err
		}

		if err := os.Rename(prefix, old); err != nil {
			return Realized{}, err
		}

		defer func() {
			if failed != nil {
				removeTree(prefix)

				failed = errors.Join(failed, os.Rename(old, prefix))

				return
			}

			removeTree(old)
		}()
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

	systemDirs, hostVars := hostEnv(filepath.Join(work, "home"), filepath.Join(work, "tmp"))

	systemPC := writeSystemPkgConfig(filepath.Join(work, "pkgconfig"))

	env := append(append(linkEnv(deps, systemPC), hostVars...), []string{
		"PATH=" + joinPaths(append(append(append(depDirs(deps, "bin"), dllDirs(deps)...), toolBin...), systemDirs...)),
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

	if root := goRoot(build.Needs); root != "" {
		env = append(env, "GOROOT="+root)
		box.Readable = append(box.Readable, root)
	}

	result := Realized{
		Path: prefix, SHA256: source.sha256, SourceURL: source.url, FirstUse: source.firstUse,
	}

	var vendored []string

	for i, step := range build.Steps {
		if !step.When.Matches(p) || opts.VendorOnly && step.Vendor == nil {
			continue
		}

		var err error

		kinds := strings.Join(step.Kinds(), ",")
		done := status.Start(ctx, "building, step %d of %d (%s)", i+1, len(build.Steps), kinds)

		switch {
		case step.Run != nil:
			result.Impure = result.Impure || step.Impure()
			result.Unsandboxed, err = runCommand(ctx, step, src, vars, env, box, log)
		case step.Vendor != nil:
			var digest string

			vendorEnv := env

			if step.Package != "" {
				result.Impure = result.Impure || step.Impure()

				// A pin for another platform hashes the install alone, so the
				// scripts, which run the host's code, stay out of it.
				scripts := step.Scripts
				if opts.VendorOnly {
					scripts = nil
				}

				switch *step.Vendor {
				case "cargo":
					// The source is the crate, so the step needs nothing more.
				case "pip":
					vendorEnv, err = s.pipPackageEnv(ctx, env, step.Package, m.Version.Value, opts.PyPIIndex)
					vendorEnv = append(vendorEnv, pipTarget(p)...)
				case "go":
					// A manifest that follows the module's versions names the module,
					// which may hold the package deeper down.
					module := step.Package
					if m.Version.From == manifest.FromGo {
						module = m.Version.Repo
					}

					// The go command reads its default proxy from the go.env of its
					// GOROOT, and a toolchain may not have one.
					vendorEnv = append(
						slices.Clone(env),
						"OKU_GO_MODULE="+module, "OKU_GO_PACKAGE="+step.Package,
						"OKU_GO_VERSION="+m.Version.Value,
						"GOPROXY="+cmp.Or(opts.GoProxy, goproxy.Proxy+",direct"),
						"GOSUMDB=sum.golang.org",
					)
				default:
					vendorEnv, err = s.npmPackageEnv(
						ctx,
						env,
						step.Package,
						m.Version.Value,
						opts.NPMRegistry,
						scripts,
					)
				}
			}

			if opts.VendorOnly {
				vendorEnv = append(slices.Clone(vendorEnv), npmTarget(p)...)
			}

			if err == nil {
				digest, result.Unsandboxed, err = runVendor(
					ctx, *step.Vendor, step.Package != "", src, prefix, vendorEnv, box, log,
				)
			}

			// npm installed the tree, so oku can tell which of its packages have
			// install scripts, and whether scripts names packages it holds. The
			// tree of another platform may lack a package that scripts names.
			if err == nil && *step.Vendor == "npm" && step.Package != "" {
				all, scripted := npmTree(filepath.Join(prefix, "lib", "node_modules"))

				for _, name := range step.Scripts {
					if !opts.VendorOnly && !slices.Contains(all, name) {
						err = fmt.Errorf("scripts names %s, which is not a package of the tree of %s", name, step.Package)
					}
				}

				for _, name := range scripted {
					if !slices.Contains(step.Scripts, name) {
						result.UnnamedScripts = append(result.UnnamedScripts, name)
					}
				}
			}

			vendored = append(vendored, digest)
		default:
			err = s.runStep(ctx, step, src, prefix, vars, opts.RuntimeDeps)

			// The completions command runs the program the step just installed.
			if err == nil && step.Generates() {
				result.Unsandboxed, err = generateCompletions(
					ctx, step.Install.Completions, src, prefix,
					withBinFirst(env, filepath.Join(prefix, "bin")), box, log,
				)
			}
		}

		done()

		if opts.Progress != nil {
			opts.Progress(i, len(build.Steps), kinds, err)
		}

		if err != nil {
			os.RemoveAll(prefix)

			return Realized{}, fmt.Errorf(
				"build.step[%d] (%s) failed: %w", i, kinds, err,
			)
		}
	}

	if len(vendored) > 0 {
		sum := sha256.Sum256([]byte(strings.Join(vendored, "\n")))
		result.VendorSHA256 = hex.EncodeToString(sum[:])
	}

	if opts.VendorOnly {
		return Realized{VendorSHA256: result.VendorSHA256, UnnamedScripts: result.UnnamedScripts}, nil
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

	apps, err := buildLaunchers(build.Steps, p, src, prefix)
	if err != nil {
		os.RemoveAll(prefix)

		return Realized{}, err
	}

	meta, err := toml.Marshal(Meta{
		Name: m.Package.Name, Version: m.Version.Value, Platform: p.String(),
		Impure: result.Impure, Launchers: apps, Services: m.ServicesFor(p),
		URL: result.SourceURL, SHA256: result.SHA256, VendorSHA256: result.VendorSHA256,
	})
	if err == nil {
		err = os.WriteFile(filepath.Join(prefix, metaFile), meta, 0o644)
	}

	if err != nil {
		os.RemoveAll(prefix)

		return Realized{}, fmt.Errorf("write %s: %w", metaFile, err)
	}

	result.MissingDeps = s.missingDeps(prefix, opts.RuntimeDeps)

	// A store path that shares nothing still works, and gc shares it later.
	_, _ = s.Share(prefix)

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

// goRoot returns the GOROOT of the go that needs names, when it is one. The go
// command reads its standard library there, and a go that oku installed is in
// the home directory, which the sandbox hides. The build may read it, and
// GOROOT tells the go command where it is, since it reaches the build as a link.
func goRoot(needs []string) string {
	if !slices.Contains(needs, "go") {
		return ""
	}

	tool, err := exec.LookPath("go")
	if err != nil {
		return ""
	}

	out, err := exec.Command(tool, "env", "GOROOT").Output()
	if err != nil {
		return ""
	}

	root := strings.TrimSpace(string(out))
	if info, err := os.Stat(filepath.Join(root, "src")); err != nil || !info.IsDir() {
		return ""
	}

	return root
}

// removeTree deletes dir even when it holds read-only directories. Go marks its
// module cache read-only, and os.RemoveAll cannot delete inside such a directory.
func removeTree(dir string) {
	_ = filepath.WalkDir(dir, func(path string, entry fs.DirEntry, err error) error {
		if err == nil && entry.IsDir() {
			_ = os.Chmod(path, 0o700)
		}

		return nil
	})

	os.RemoveAll(dir)
}

// needTool is a needed tool and where the user's PATH has it.
type needTool struct {
	name, path string
}

// findNeeds looks the needed tools up on the user's PATH, and fails on the first
// tool that is missing.
func findNeeds(needs []string) ([]needTool, error) {
	tools := make([]needTool, 0, len(needs))

	for _, tool := range needs {
		found, err := exec.LookPath(tool)
		if err != nil {
			return nil, fmt.Errorf("the build needs %q, which is not on PATH", tool)
		}

		tools = append(tools, needTool{name: tool, path: found})
	}

	return tools, nil
}

// linkNeeds fills dir with one entry per needed tool, so that a build sees the
// tools it named and nothing else from their directories. It returns the
// directories the tools really live in, which the sandbox keeps readable, and
// the directories to put on PATH.
//
// A link keeps the tool in its own directory, so a compiler driver still finds
// its assembler and linker beside itself. Windows has no symlinks for a plain
// user, so there a tool is a shim, as in a profile.
func linkNeeds(dir string, tools []needTool) (real, bin []string, err error) {
	if len(tools) == 0 {
		return nil, nil, nil
	}

	if err := os.Mkdir(dir, 0o755); err != nil {
		return nil, nil, err
	}

	for _, tool := range tools {
		if home := filepath.Dir(tool.path); !slices.Contains(real, home) {
			real = append(real, home)
		}

		if err := linkNeed(filepath.Join(dir, tool.name), tool.path); err != nil {
			return nil, nil, fmt.Errorf("link the build tool %s: %w", tool.name, err)
		}
	}

	return real, []string{dir}, nil
}

// checkTagCommit fails when the source was cloned from a moving tag that no
// longer points at the commit the version names.
func checkTagCommit(ctx context.Context, m *manifest.Manifest, src string) error {
	source := m.Build.Source
	if m.TagCommit == "" || source.Git == "" || !strings.Contains(source.Tag, "{{tag}}") {
		return nil
	}

	out, err := exec.CommandContext(ctx, "git", "-C", src, "rev-parse", "HEAD").Output()
	if err != nil {
		return fmt.Errorf("read the commit of the source: %w", err)
	}

	if got := strings.TrimSpace(string(out)); got != m.TagCommit {
		return fmt.Errorf(
			"upstream moved the tag %s to commit %s, and version %s is commit %s\n"+
				"run `oku update %s` to take the new build",
			m.Tag, got, m.Version.Value, m.TagCommit, m.Package.Name,
		)
	}

	return nil
}

// fetchedSource says which archive a build used.
type fetchedSource struct {
	url, sha256 string
	// firstUse reports that nothing stated the digest, so oku trusted the download.
	firstUse bool
}

// fetchSource puts the source of a build into src. For an archive it expects the
// first digest it finds in source.SHA256, the file at source.SHA256URL, and
// pinned. With none of them it trusts the download, like Realize does for an
// artifact.
func (s *Store) fetchSource(
	ctx context.Context,
	source manifest.Source,
	src string,
	vars map[string]string,
	pinned string,
	commit string,
) (fetchedSource, error) {
	switch {
	case source.Git != "" && commit != "":
		defer status.Start(ctx, "fetching commit %s of %s", commit[:7], source.Git)()

		for _, args := range [][]string{
			{"init", "--quiet", src},
			{"-C", src, "fetch", "--quiet", "--depth", "1", "--", source.Git, commit},
			{"-C", src, "checkout", "--quiet", "FETCH_HEAD"},
		} {
			cmd := exec.CommandContext(ctx, "git", args...)
			cmd.Env = append(os.Environ(), "GIT_TERMINAL_PROMPT=0")

			if out, err := cmd.CombinedOutput(); err != nil {
				return fetchedSource{}, fmt.Errorf(
					"git %s: %w: %s", strings.Join(args, " "), err, strings.TrimSpace(string(out)),
				)
			}
		}

		return fetchedSource{}, nil
	case source.Git != "":
		tag, err := manifest.Expand(source.Tag, vars)
		if err != nil {
			return fetchedSource{}, err
		}

		args := []string{"clone", "--quiet", "--depth", "1"}
		if tag != "" {
			args = append(args, "--branch", tag)
		}

		defer status.Start(ctx, "cloning %s", source.Git)()

		cmd := exec.CommandContext(ctx, "git", append(args, "--", source.Git, src)...)
		cmd.Env = append(os.Environ(), "GIT_TERMINAL_PROMPT=0")

		if out, err := cmd.CombinedOutput(); err != nil {
			return fetchedSource{}, fmt.Errorf(
				"git clone %s: %w: %s",
				source.Git,
				err,
				strings.TrimSpace(string(out)),
			)
		}

		return fetchedSource{}, nil
	case source.URL != "":
		url, err := manifest.Expand(source.URL, vars)
		if err != nil {
			return fetchedSource{}, err
		}

		want := source.SHA256

		if want == "" && source.SHA256URL != "" {
			checksums, err := manifest.Expand(source.SHA256URL, vars)
			if err != nil {
				return fetchedSource{}, err
			}

			if want, err = s.PublishedSHA256(ctx, checksums, path.Base(url)); err != nil {
				return fetchedSource{}, err
			}
		}

		if want != "" && pinned != "" && want != pinned {
			return fetchedSource{}, fmt.Errorf(
				"%w: upstream publishes sha256 %s, oku.lock pinned %s",
				ErrPinConflict,
				want,
				pinned,
			)
		}

		stated := want != ""
		if !stated {
			want = pinned
		}

		download, got, err := s.fetch(ctx, url, want)
		if err != nil {
			return fetchedSource{}, err
		}

		defer status.Start(ctx, "unpacking %s", path.Base(url))()

		return fetchedSource{url: url, sha256: got, firstUse: !stated && pinned == ""},
			extract(download, src, source.Strip)
	default:
		return fetchedSource{}, nil
	}
}

// runStep runs a step that moves files. Run steps go through runCommand.
func (s *Store) runStep(
	ctx context.Context,
	step manifest.Step,
	src, prefix string,
	vars map[string]string,
	runtimeDeps []Dep,
) error {
	switch {
	case step.Install != nil:
		if err := installFiles(*step.Install, src, prefix); err != nil {
			return err
		}

		// A build's files are at the top of the prefix, so {{pkg}} is the prefix.
		wrapVars := maps.Clone(vars)
		wrapVars["pkg"] = prefix

		// A build installs into its prefix, so the files are where the wrappers
		// name them.
		return writeWraps(
			filepath.Join(prefix, "bin"), step.Install.Wrap, wrapVars, vars["os"], depDirs(runtimeDeps, "bin"),
			func(path string) string { return path },
		)
	case step.Copy != nil:
		return copyInto(src, step.Copy.From, prefix, step.Copy.To, 0)
	case step.Patch != nil:
		return applyPatch(src, *step.Patch)
	case step.Fetch != nil:
		url, err := manifest.Expand(step.Fetch.URL, vars)
		if err != nil {
			return err
		}

		want := step.Fetch.SHA256
		if want == "" {
			checksums, err := manifest.Expand(step.Fetch.SHA256URL, vars)
			if err != nil {
				return err
			}

			if want, err = s.PublishedSHA256(ctx, checksums, path.Base(url)); err != nil {
				return err
			}
		}

		download, _, err := s.fetch(ctx, url, want)
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

	// Windows ships Windows PowerShell 5.1 and not PowerShell 7, so without pwsh
	// on PATH a pwsh step runs in powershell.exe.
	program := shell
	if _, err := exec.LookPath("pwsh"); shell == "pwsh" && runtime.GOOS == "windows" && err != nil {
		program = "powershell"
	}

	box.Argv = append(append([]string{program}, args...), script)
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

// buildLaunchers returns the launchers of the app entries that the install
// steps for p name. A launcher runs a program that an install step put in bin,
// and its icon is copied to share/icons.
func buildLaunchers(steps []manifest.Step, p platform.Platform, src, prefix string) ([]expose.Launcher, error) {
	var apps []expose.Launcher

	for _, step := range steps {
		if step.Install == nil || !step.When.Matches(p) {
			continue
		}

		got, err := launchers(step.Install.App, src, func(rel string) (string, error) {
			name := path.Base(rel)
			if _, err := os.Lstat(filepath.Join(prefix, "bin", name)); err != nil {
				return "", fmt.Errorf("it runs %s, which no install step puts in bin", name)
			}

			return "bin/" + name, nil
		}, func(rel string) (string, error) {
			dest := path.Join("share", "icons", path.Base(rel))

			return dest, copyInto(src, rel, prefix, dest, 0)
		})
		if err != nil {
			return nil, err
		}

		apps = append(apps, got...)
	}

	return apps, nil
}

// installFiles copies the named files from src into the package layout.
func installFiles(in manifest.Install, src, prefix string) error {
	fonts, err := expandGlobs(src, "font", in.Font)
	if err != nil {
		return err
	}

	mans, err := expandGlobs(src, "man", in.Man)
	if err != nil {
		return err
	}

	groups := []struct {
		files []string
		dir   func(file string) (string, error)
		mode  os.FileMode
	}{
		{in.Bin, fixedDir("bin"), 0o755},
		{in.Lib, fixedDir("lib"), 0},
		{in.Include, fixedDir("include"), 0},
		{in.Share, fixedDir("share"), 0},
		{fonts, fixedDir("fonts"), 0},
		{mans, func(file string) (string, error) {
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

	// A table with a path installs the file under the table's name.
	for _, w := range in.Wrap {
		if w.Path == "" {
			continue
		}

		if err := copyInto(src, w.Path, prefix, path.Join("bin", w.Name), 0o755); err != nil {
			return err
		}
	}

	for _, app := range in.App {
		if !app.Bundle() {
			continue
		}

		bundle := app.Path
		if !filepath.IsLocal(filepath.FromSlash(bundle)) {
			return fmt.Errorf("app %q points outside the source directory", bundle)
		}

		target := filepath.Join(prefix, "apps", path.Base(bundle))
		if err := os.MkdirAll(filepath.Dir(target), 0o755); err != nil {
			return err
		}

		if err := clone.Tree(
			filepath.Join(src, filepath.FromSlash(bundle)),
			target,
		); err != nil {
			return fmt.Errorf("app %q: %w", bundle, err)
		}
	}

	for name, command := range bundleCommands(in.App, in.Bin, in.Wrap, runtime.GOOS) {
		bundle := filepath.Join(prefix, "apps", path.Base(command[0]))
		if err := writeAppCommand(filepath.Join(prefix, "bin", name), bundle, command[1]); err != nil {
			return fmt.Errorf("bin %q: %w", name, err)
		}
	}

	completions, err := completionPaths(in.Completions, src)
	if err != nil {
		return err
	}

	for shell, file := range completions {
		if err := copyInto(src, file, prefix, completionDest(shell, file), 0); err != nil {
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
	// RuntimeDeps are the realized runtime deps. A built file that loads another
	// store package is reported in Realized.MissingDeps.
	RuntimeDeps []Dep
	// Log receives the output of commands as they run, and may be nil.
	Log io.Writer
	// PinnedVendor is the vendor digest oku.lock recorded, or empty.
	PinnedVendor string
	// PinnedSource is the digest of the source archive that oku.lock recorded for
	// this version and URL, or empty.
	PinnedSource string
	// NPMRegistry replaces the URL of the npm registry when set, which tests do.
	NPMRegistry string
	// PyPIIndex replaces the URL of the Python Package Index when set.
	PyPIIndex string
	// GoProxy replaces the URL of the Go module proxy when set.
	GoProxy string
	// Progress is called after each step that ran, with its position, the number
	// of steps, its kind and its error. It may be nil.
	Progress func(step, total int, kind string, err error)
	// Rebuild builds again when the store holds the build. Build moves the old
	// build aside first, and puts it back when the new one fails.
	Rebuild bool
	// VendorOnly runs the vendor steps alone, for a platform that may not be the
	// host, with no install scripts, and returns their digest and the packages of
	// an npm tree that have install scripts. It builds nothing and keeps nothing.
	// Only a build that CanCrossVendor accepts may ask for it.
	VendorOnly bool
}

// ErrVendorChanged reports vendor steps that downloaded something other than
// what oku.lock pinned.
var ErrVendorChanged = errors.New("the vendored packages changed")

// runVendor downloads a language's packages into the source directory, with the
// network on, and returns the digest of what it downloaded.
//
// With pkg, an npm step installs one package from the registry into the
// prefix, and that is what oku hashes.
func runVendor(
	ctx context.Context,
	kind string,
	pkg bool,
	src, prefix string,
	env []string,
	box sandbox.Spec,
	log io.Writer,
) (digest, unsandboxed string, err error) {
	if pkg {
		kind = map[string]string{
			"npm": npmPackageKind, "pip": pipPackageKind, "go": goPackageKind,
			"cargo": cargoPackageKind,
		}[kind]
	}

	vendor, ok := vendorKinds[kind]
	if !ok {
		return "", "", fmt.Errorf(
			"vendor %q must be one of %s",
			kind,
			strings.Join(manifest.VendorKinds, ", "),
		)
	}

	tools := vendor.tools
	if runtime.GOOS == "windows" && vendor.pwshTools != nil {
		tools = vendor.pwshTools
	}

	tool, err := findTool(tools, env)
	if err != nil {
		return "", "", fmt.Errorf("vendor %q %w", kind, err)
	}

	env = append(slices.Clone(env), vendor.env...)

	script, after, shell := vendor.script, vendor.after, "sh"
	if runtime.GOOS == "windows" && vendor.pwsh != "" {
		script, after, shell = vendor.pwsh, vendor.pwshAfter, "pwsh"
	}

	run := func(script string) error {
		if shell == "pwsh" {
			script = "$ErrorActionPreference = 'Stop'\n$tool = '" +
				strings.ReplaceAll(tool, "'", "''") + "'\n" + script
		} else {
			script = "tool=" + strconv.Quote(tool) + "\n" + script
		}

		step := manifest.Step{Run: &script, Shell: shell, Network: true}

		var err error
		unsandboxed, err = runCommand(ctx, step, src, nil, env, box, log)

		return err
	}

	if err = run(script); err != nil {
		return "", unsandboxed, err
	}

	output := filepath.Join(src, filepath.FromSlash(vendor.output))
	if vendor.inPrefix {
		output = filepath.Join(prefix, filepath.FromSlash(vendor.output))
	}

	if digest, err = hashTree(output); err != nil || after == "" {
		return digest, unsandboxed, err
	}

	return digest, unsandboxed, run(after)
}

// npmPackageEnv returns env with what an npm step needs to install one package:
// its name, its version, the time that version was published, and the packages
// in scripts whose install scripts then run. npm resolves dependencies as of
// that time, so a later install gets the same packages. The build checks the
// names in scripts against the tree npm installed.
func (s *Store) npmPackageEnv(
	ctx context.Context,
	env []string,
	name, version, registry string,
	scripts []string,
) ([]string, error) {
	published, err := npm.Published(ctx, s.http, registry, name, version)
	if err != nil {
		return nil, fmt.Errorf("read when %s %s was published: %w", name, version, err)
	}

	env = append(
		slices.Clone(env),
		"OKU_NPM_PACKAGE="+name+"@"+version,
		"OKU_NPM_BEFORE="+published.At.UTC().Format(time.RFC3339Nano),
		"OKU_NPM_SCRIPTS="+strings.Join(scripts, " "),
	)

	if registry != "" {
		env = append(env, "npm_config_registry="+registry)
	}

	return env, nil
}

// pipPackageEnv returns env with what a pip step needs to install one package:
// its name, its version and the time that version was uploaded. uv resolves
// dependencies as of that time, so a later install gets the same packages.
func (s *Store) pipPackageEnv(
	ctx context.Context,
	env []string,
	name, version, index string,
) ([]string, error) {
	pkg, err := pypi.Read(ctx, s.http, index, name)
	if err != nil {
		return nil, fmt.Errorf("read when %s %s was uploaded: %w", name, version, err)
	}

	uploaded, ok := pkg.Versions[version]
	if !ok {
		return nil, fmt.Errorf("the Python package %s has no version %s", name, version)
	}

	// uv takes what was uploaded before the time, and the version's own files
	// were uploaded up to it.
	env = append(
		slices.Clone(env),
		"OKU_PIP_PACKAGE="+name,
		"OKU_PIP_VERSION="+version,
		"OKU_PIP_BEFORE="+uploaded.Uploaded.Add(time.Second).UTC().Format(time.RFC3339),
	)

	if index != "" {
		env = append(env, "UV_DEFAULT_INDEX="+strings.TrimRight(index, "/")+"/simple")
	}

	return env, nil
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
//
// systemPC is a directory of pkg-config files for the libraries of the OS, or
// empty. It comes last, so a dep wins over the OS.
func linkEnv(deps []Dep, systemPC string) []string {
	if len(deps) == 0 {
		if systemPC == "" {
			return nil
		}

		return []string{"PKG_CONFIG_PATH=" + systemPC}
	}

	lib, include := depDirs(deps, "lib"), depDirs(deps, "include")
	pkgconfig := append(depDirs(deps, "lib/pkgconfig"), depDirs(deps, "share/pkgconfig")...)

	if systemPC != "" {
		pkgconfig = append(pkgconfig, systemPC)
	}

	return []string{
		"PKG_CONFIG_PATH=" + joinPaths(pkgconfig),
		"CPATH=" + joinPaths(include),
		"LIBRARY_PATH=" + joinPaths(lib),
		"LD_RUN_PATH=" + joinPaths(lib),
		"CMAKE_PREFIX_PATH=" + joinPaths(depDirs(deps, "")),
	}
}

// dllDirs returns the directories that hold the DLLs of the deps on Windows,
// and nothing elsewhere. Windows looks for a program's DLLs beside the file it
// started and then on PATH. A build step runs the link in a dep's bin, which is
// a copy of the program when the user may not create symlinks. So PATH takes the
// download and the directory of each real file in it.
func dllDirs(deps []Dep) []string {
	if runtime.GOOS != "windows" {
		return nil
	}

	dirs := depDirs(deps, "pkg")

	for _, dep := range deps {
		specs, _ := filepath.Glob(filepath.Join(dep.Prefix, "bin", "*"+shim.Ext))

		for _, file := range specs {
			spec, err := shim.Read(file)
			if err != nil {
				continue
			}

			if dir := filepath.Dir(spec.Target); !slices.Contains(dirs, dir) {
				dirs = append(dirs, dir)
			}
		}
	}

	return dirs
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
