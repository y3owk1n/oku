package ref

import (
	"context"
	"crypto/sha256"
	"encoding/hex"
	"errors"
	"fmt"
	"io"
	"io/fs"
	"net/http"
	"os"
	"os/exec"
	"path"
	"path/filepath"
	"strings"
	"sync"

	"github.com/y3owk1n/oku/internal/forge"
	"github.com/y3owk1n/oku/internal/status"
)

// maxManifest is the most bytes Fetch reads from a server.
const maxManifest = 1 << 20

// Fetcher reads manifests.
type Fetcher struct {
	HTTP *http.Client
	// Hosts opens the forge of a GitHub ref.
	Hosts forge.Hosts
	// GitCache holds the clones that Git refs read from.
	GitCache string
}

// Fetched is a file plus where it came from.
type Fetched struct {
	Data []byte
	// Commit is empty for File and HTTP refs.
	Commit string
	// Path is the file Fetch read. It is a local path, a URL, or a path inside the
	// repository.
	Path string
}

// ErrNotFound reports that the file a ref or path names does not exist.
var ErrNotFound = errors.New("not found")

// NewFetcher returns a Fetcher that clones under cacheDir and keeps the answers
// of forge APIs there.
func NewFetcher(cacheDir string) *Fetcher {
	return &Fetcher{
		HTTP:     http.DefaultClient,
		Hosts:    forge.Hosts{HTTP: forge.Revalidating(filepath.Join(cacheDir, "api"))},
		GitCache: filepath.Join(cacheDir, "git"),
	}
}

// file reads one manifest file from a forge.
func file(ctx context.Context, host forge.Forge, repo, commit, path string) ([]byte, error) {
	data, err := host.File(ctx, repo, commit, path)
	if errors.Is(err, forge.ErrNotFound) {
		return nil, ErrNotFound
	}

	if err == nil && len(data) > maxManifest {
		return nil, fmt.Errorf("response is larger than %d bytes", maxManifest)
	}

	return data, err
}

// Fetch reads the file r points at as a t. A non-empty commit pins GitHub and
// Git refs to it. An empty commit means the default branch's newest commit.
func (f *Fetcher) Fetch(ctx context.Context, r Ref, commit string, t Target) (Fetched, error) {
	if r.Kind != File {
		defer status.Start(ctx, "reading %s", r)()
	}

	switch r.Kind {
	case File:
		data, err := os.ReadFile(r.Location)
		if errors.Is(err, fs.ErrNotExist) {
			return Fetched{}, fmt.Errorf("read %s: %w", r.Location, ErrNotFound)
		}

		if err != nil {
			return Fetched{}, fmt.Errorf("read %s: %w", r.Location, err)
		}

		return Fetched{Data: data, Path: r.Location}, nil
	case HTTP:
		data, err := f.get(ctx, r.Location)
		if err != nil {
			return Fetched{}, fmt.Errorf("fetch %s: %w", r.Location, err)
		}

		return Fetched{Data: data, Path: r.Location}, nil
	case Forge:
		return f.fetchForge(ctx, r, commit, t)
	case NPM:
		return Fetched{}, fmt.Errorf(
			"%s: an npm package holds no %s: %w",
			r,
			t.Default,
			ErrNotFound,
		)
	default:
		return f.fetchGit(ctx, r, commit, t)
	}
}

func (f *Fetcher) fetchForge(
	ctx context.Context,
	r Ref,
	commit string,
	t Target,
) (Fetched, error) {
	host, repo, err := f.Hosts.Open(r.Scheme, r.Location)
	if err != nil {
		return Fetched{}, err
	}

	if commit == "" {
		if commit, err = host.Head(ctx, repo); err != nil {
			return Fetched{}, fmt.Errorf("resolve %s: %w", r, notFound(err))
		}
	}

	for _, path := range t.paths(r.Fragment) {
		data, err := file(ctx, host, repo, commit, path)
		if errors.Is(err, ErrNotFound) {
			continue
		}

		if err != nil {
			return Fetched{}, fmt.Errorf("fetch %s: %w", r, err)
		}

		return Fetched{Data: data, Commit: commit, Path: path}, nil
	}

	return Fetched{Commit: commit}, fmt.Errorf(
		"%s: no %s at commit %s: %w",
		r, strings.Join(t.paths(r.Fragment), " or "), commit, ErrNotFound,
	)
}

// paths lists where a Forge ref's file may be, in lookup order.
func (t Target) paths(name string) []string {
	switch {
	case name == "":
		return []string{t.Default}
	case isPath(name):
		return []string{name}
	}

	return []string{name + ".toml", t.Dir + "/" + name + ".toml"}
}

// notFound turns a forge's missing file or repo into ErrNotFound.
func notFound(err error) error {
	if errors.Is(err, forge.ErrNotFound) {
		return ErrNotFound
	}

	return err
}

func (f *Fetcher) get(ctx context.Context, url string) ([]byte, error) {
	req, err := http.NewRequestWithContext(ctx, http.MethodGet, url, nil)
	if err != nil {
		return nil, err
	}

	req.Header.Set("User-Agent", "oku")

	resp, err := f.HTTP.Do(req)
	if err != nil {
		return nil, err
	}
	defer resp.Body.Close()

	switch {
	case resp.StatusCode == http.StatusNotFound:
		return nil, ErrNotFound
	case resp.StatusCode != http.StatusOK:
		return nil, fmt.Errorf("server returned %s", resp.Status)
	}

	data, err := io.ReadAll(io.LimitReader(resp.Body, maxManifest+1))
	if err != nil {
		return nil, err
	}

	if len(data) > maxManifest {
		return nil, fmt.Errorf("response is larger than %d bytes", maxManifest)
	}

	return data, nil
}

// clones holds a lock for each clone in the cache, by the location of its ref.
var clones sync.Map

// fetchGit reads the file from a shallow clone kept in the cache. It needs
// the git binary, because git hosts share no HTTP API.
func (f *Fetcher) fetchGit(
	ctx context.Context,
	r Ref,
	commit string,
	t Target,
) (Fetched, error) {
	// Two refs into one repository share a clone, and each checks out its commit.
	mu, _ := clones.LoadOrStore(r.Location, &sync.Mutex{})
	mu.(*sync.Mutex).Lock()
	defer mu.(*sync.Mutex).Unlock()

	dir, head, err := f.checkout(ctx, r, commit)
	if err != nil {
		return Fetched{}, err
	}

	// A fragment is a path. fetchGit looks up a bare name, such as "ripgrep", the
	// way fetchForge does.
	paths := []string{r.Fragment}
	if !strings.ContainsAny(r.Fragment, "/.") {
		paths = t.paths(r.Fragment)
	}

	// os.Root refuses to follow a symlink in the repository that points outside it.
	root, err := os.OpenRoot(dir)
	if err != nil {
		return Fetched{}, err
	}
	defer root.Close()

	for _, path := range paths {
		data, err := root.ReadFile(path)
		if errors.Is(err, fs.ErrNotExist) {
			continue
		}

		if err != nil {
			return Fetched{}, fmt.Errorf("%s: read %s: %w", r, path, err)
		}

		return Fetched{Data: data, Commit: head, Path: path}, nil
	}

	return Fetched{}, fmt.Errorf("%s: no %s: %w", r, strings.Join(paths, " or "), ErrNotFound)
}

func git(ctx context.Context, dir string, args ...string) error {
	_, err := gitOutput(ctx, dir, args...)

	return err
}

func gitOutput(ctx context.Context, dir string, args ...string) (string, error) {
	cmd := exec.CommandContext(ctx, "git", append([]string{"-C", dir}, args...)...)
	// A credential prompt would hang a non-interactive install.
	cmd.Env = append(os.Environ(), "GIT_TERMINAL_PROMPT=0")

	out, err := cmd.CombinedOutput()
	if err != nil {
		return "", fmt.Errorf("git %s: %w: %s", args[0], err, strings.TrimSpace(string(out)))
	}

	return strings.TrimSpace(string(out)), nil
}

// FetchBeside reads the file next to an earlier result, at the same commit. name replaces the last element of got.Path.
func (f *Fetcher) FetchBeside(
	ctx context.Context,
	r Ref,
	got Fetched,
	name string,
) (Fetched, error) {
	at := got.Path[:strings.LastIndexAny(got.Path, `/\\`)+1] + name

	switch r.Kind {
	case File, HTTP:
		r.Location = at
	case Forge:
		host, repo, err := f.Hosts.Open(r.Scheme, r.Location)
		if err != nil {
			return Fetched{}, err
		}

		data, err := file(ctx, host, repo, got.Commit, at)
		if err != nil {
			return Fetched{}, fmt.Errorf("fetch %s: %w", at, err)
		}

		return Fetched{Data: data, Commit: got.Commit, Path: at}, nil
	default:
		r.Fragment = at
	}

	return f.Fetch(ctx, r, got.Commit, Target{Default: at})
}

// Archive returns the files of the repo of r at commit as a tar.gz, and how
// many directories are above them in it.
func (f *Fetcher) Archive(ctx context.Context, r Ref, commit string) ([]byte, int, error) {
	defer status.Start(ctx, "downloading the files of %s", r.Location)()

	switch r.Kind {
	case Forge:
		host, repo, err := f.Hosts.Open(r.Scheme, r.Location)
		if err != nil {
			return nil, 0, err
		}

		data, err := host.Archive(ctx, repo, commit)
		if err != nil {
			return nil, 0, fmt.Errorf("download the files of %s: %w", r, notFound(err))
		}

		// A forge puts the files under one directory named after the repo.
		return data, 1, nil
	case Git:
		mu, _ := clones.LoadOrStore(r.Location, &sync.Mutex{})
		mu.(*sync.Mutex).Lock()
		defer mu.(*sync.Mutex).Unlock()

		dir, _, err := f.checkout(ctx, r, commit)
		if err != nil {
			return nil, 0, err
		}

		cmd := exec.CommandContext(ctx, "git", "-C", dir, "archive", "--format=tar.gz", "HEAD")

		data, err := cmd.Output()
		if err != nil {
			return nil, 0, fmt.Errorf("%s: git archive: %w", r, err)
		}

		return data, 0, nil
	default:
		return nil, 0, fmt.Errorf("%s: only a repo has files oku can download", r)
	}
}

// checkout fetches one commit of a git ref into the cache and returns the
// directory and the commit.
func (f *Fetcher) checkout(ctx context.Context, r Ref, commit string) (string, string, error) {
	if _, err := exec.LookPath("git"); err != nil {
		return "", "", fmt.Errorf("%s: git+ refs need git on PATH", r)
	}

	sum := sha256.Sum256([]byte(r.Location))
	dir := filepath.Join(f.GitCache, hex.EncodeToString(sum[:])[:16])

	if _, err := os.Stat(filepath.Join(dir, ".git")); errors.Is(err, fs.ErrNotExist) {
		if err := os.MkdirAll(dir, 0o755); err != nil {
			return "", "", err
		}

		if err := git(ctx, dir, "init", "--quiet"); err != nil {
			return "", "", err
		}
	}

	target := commit
	if target == "" {
		target = "HEAD"
	}

	if err := git(
		ctx,
		dir,
		"fetch",
		"--quiet",
		"--depth",
		"1",
		"--",
		r.Location,
		target,
	); err != nil {
		return "", "", fmt.Errorf("fetch %s: %w", r, err)
	}

	if err := git(ctx, dir, "checkout", "--quiet", "--force", "FETCH_HEAD"); err != nil {
		return "", "", fmt.Errorf("fetch %s: %w", r, err)
	}

	head, err := gitOutput(ctx, dir, "rev-parse", "HEAD")
	if err != nil {
		return "", "", fmt.Errorf("fetch %s: %w", r, err)
	}

	return dir, head, nil
}

// ListManifests returns the manifest files of the collection at r, keyed by
// package name. A collection keeps them as "<name>.toml" at its root or under
// "packages/". A URL cannot be listed.
func (f *Fetcher) ListManifests(ctx context.Context, r Ref) (map[string][]byte, error) {
	switch r.Kind {
	case File:
		return readCollection(r.Location)
	case Git:
		dir, _, err := f.checkout(ctx, r, "")
		if err != nil {
			return nil, err
		}

		return readCollection(dir)
	case Forge:
		return f.listForge(ctx, r)
	default:
		return nil, fmt.Errorf("%s: a URL cannot be listed, so it cannot be searched", r)
	}
}

func readCollection(dir string) (map[string][]byte, error) {
	found := map[string][]byte{}

	for _, sub := range []string{Manifest.Dir, ""} {
		paths, err := filepath.Glob(filepath.Join(dir, sub, "*.toml"))
		if err != nil {
			return nil, err
		}

		for _, path := range paths {
			data, err := os.ReadFile(path)
			if err != nil {
				return nil, err
			}

			found[strings.TrimSuffix(filepath.Base(path), ".toml")] = data
		}
	}

	return found, nil
}

func (f *Fetcher) listForge(ctx context.Context, r Ref) (map[string][]byte, error) {
	host, repo, err := f.Hosts.Open(r.Scheme, r.Location)
	if err != nil {
		return nil, err
	}

	commit, err := host.Head(ctx, repo)
	if err != nil {
		return nil, fmt.Errorf("resolve %s: %w", r, notFound(err))
	}

	paths, err := host.Files(ctx, repo, commit)
	if err != nil {
		return nil, fmt.Errorf("list %s: %w", r, notFound(err))
	}

	found := map[string][]byte{}

	for _, at := range paths {
		dir, name := path.Split(at)
		if !strings.HasSuffix(name, ".toml") || dir != "" && dir != Manifest.Dir+"/" {
			continue
		}

		data, err := file(ctx, host, repo, commit, at)
		if err != nil {
			return nil, fmt.Errorf("fetch %s: %w", at, err)
		}

		name = strings.TrimSuffix(name, ".toml")
		if _, taken := found[name]; !taken || dir == "" {
			found[name] = data
		}
	}

	return found, nil
}
