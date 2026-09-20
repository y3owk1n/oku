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
	"path/filepath"
	"strings"
)

// maxManifest is the most bytes Fetch reads from a server.
const maxManifest = 1 << 20

// Fetcher reads manifests. The GitHub URLs are fields so tests can point them at
// a local server.
type Fetcher struct {
	HTTP      *http.Client
	GitHubAPI string
	GitHubRaw string
	// Fetch sends Token to the GitHub API when set, which raises the rate limit.
	Token string
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

// NewFetcher returns a Fetcher for github.com that clones under cacheDir.
func NewFetcher(cacheDir string) *Fetcher {
	return &Fetcher{
		HTTP:      http.DefaultClient,
		GitHubAPI: "https://api.github.com",
		GitHubRaw: "https://raw.githubusercontent.com",
		Token:     os.Getenv("GITHUB_TOKEN"),
		GitCache:  filepath.Join(cacheDir, "git"),
	}
}

// Fetch reads the file r points at as a t. A non-empty commit pins GitHub and
// Git refs to it. An empty commit means the default branch's newest commit.
func (f *Fetcher) Fetch(ctx context.Context, r Ref, commit string, t Target) (Fetched, error) {
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
		data, err := f.get(ctx, r.Location, nil)
		if err != nil {
			return Fetched{}, fmt.Errorf("fetch %s: %w", r.Location, err)
		}

		return Fetched{Data: data, Path: r.Location}, nil
	case GitHub:
		return f.fetchGitHub(ctx, r, commit, t)
	default:
		return f.fetchGit(ctx, r, commit, t)
	}
}

func (f *Fetcher) fetchGitHub(
	ctx context.Context,
	r Ref,
	commit string,
	t Target,
) (Fetched, error) {
	if commit == "" {
		sha, err := f.get(
			ctx,
			f.GitHubAPI+"/repos/"+r.Location+"/commits/HEAD",
			map[string]string{"Accept": "application/vnd.github.sha"},
		)
		if err != nil {
			return Fetched{}, fmt.Errorf("resolve github:%s: %w", r.Location, err)
		}

		commit = strings.TrimSpace(string(sha))
	}

	for _, path := range t.paths(r.Fragment) {
		data, err := f.get(ctx, f.GitHubRaw+"/"+r.Location+"/"+commit+"/"+path, nil)
		if errors.Is(err, ErrNotFound) {
			continue
		}

		if err != nil {
			return Fetched{}, fmt.Errorf("fetch %s: %w", r, err)
		}

		return Fetched{Data: data, Commit: commit, Path: path}, nil
	}

	return Fetched{}, fmt.Errorf(
		"%s: no %s at commit %s",
		r, strings.Join(t.paths(r.Fragment), " or "), commit,
	)
}

// paths lists where a GitHub ref's file may be, in lookup order.
func (t Target) paths(name string) []string {
	if name == "" {
		return []string{t.Default}
	}

	return []string{name + ".toml", t.Dir + "/" + name + ".toml"}
}

func (f *Fetcher) get(ctx context.Context, url string, headers map[string]string) ([]byte, error) {
	req, err := http.NewRequestWithContext(ctx, http.MethodGet, url, nil)
	if err != nil {
		return nil, err
	}

	req.Header.Set("User-Agent", "oku")

	for k, v := range headers {
		req.Header.Set(k, v)
	}

	if f.Token != "" && strings.HasPrefix(url, f.GitHubAPI) {
		req.Header.Set("Authorization", "Bearer "+f.Token)
	}

	resp, err := f.HTTP.Do(req)
	if err != nil {
		return nil, err
	}
	defer resp.Body.Close()

	switch {
	case resp.StatusCode == http.StatusNotFound:
		return nil, ErrNotFound
	case resp.StatusCode == http.StatusForbidden && resp.Header.Get("X-RateLimit-Remaining") == "0":
		return nil, errors.New("GitHub rate limit reached, set GITHUB_TOKEN to raise it")
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

// fetchGit reads the file from a shallow clone kept in the cache. It needs
// the git binary, because git hosts share no HTTP API.
func (f *Fetcher) fetchGit(
	ctx context.Context,
	r Ref,
	commit string,
	t Target,
) (Fetched, error) {
	if _, err := exec.LookPath("git"); err != nil {
		return Fetched{}, fmt.Errorf("%s: git+ refs need git on PATH", r)
	}

	sum := sha256.Sum256([]byte(r.Location))
	dir := filepath.Join(f.GitCache, hex.EncodeToString(sum[:])[:16])

	if _, err := os.Stat(filepath.Join(dir, ".git")); errors.Is(err, fs.ErrNotExist) {
		if err := os.MkdirAll(dir, 0o755); err != nil {
			return Fetched{}, err
		}

		if err := git(ctx, dir, "init", "--quiet"); err != nil {
			return Fetched{}, err
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
		return Fetched{}, fmt.Errorf("fetch %s: %w", r, err)
	}

	if err := git(ctx, dir, "checkout", "--quiet", "--force", "FETCH_HEAD"); err != nil {
		return Fetched{}, fmt.Errorf("fetch %s: %w", r, err)
	}

	head, err := gitOutput(ctx, dir, "rev-parse", "HEAD")
	if err != nil {
		return Fetched{}, fmt.Errorf("fetch %s: %w", r, err)
	}

	path := r.Fragment
	if path == "" {
		path = t.Default
	}

	// os.Root refuses to follow a symlink in the repository that points outside it.
	root, err := os.OpenRoot(dir)
	if err != nil {
		return Fetched{}, err
	}
	defer root.Close()

	data, err := root.ReadFile(path)
	if errors.Is(err, fs.ErrNotExist) {
		return Fetched{}, fmt.Errorf("%s: read %s: %w", r, path, ErrNotFound)
	}

	if err != nil {
		return Fetched{}, fmt.Errorf("%s: read %s: %w", r, path, err)
	}

	return Fetched{Data: data, Commit: head, Path: path}, nil
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
	case GitHub:
		data, err := f.get(ctx, f.GitHubRaw+"/"+r.Location+"/"+got.Commit+"/"+at, nil)
		if err != nil {
			return Fetched{}, fmt.Errorf("fetch %s: %w", at, err)
		}

		return Fetched{Data: data, Commit: got.Commit, Path: at}, nil
	default:
		r.Fragment = at
	}

	return f.Fetch(ctx, r, got.Commit, Target{Default: at})
}
