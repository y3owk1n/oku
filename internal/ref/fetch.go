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

// errNotFound reports a 404 for one candidate path.
var errNotFound = errors.New("not found")

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

// Fetched is a manifest plus the commit it came from. Commit is empty for File
// and HTTP refs.
type Fetched struct {
	Data   []byte
	Commit string
}

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

// Fetch reads the manifest r points at. A non-empty commit pins GitHub and Git
// refs to it. An empty commit means the default branch's newest commit.
func (f *Fetcher) Fetch(ctx context.Context, r Ref, commit string) (Fetched, error) {
	switch r.Kind {
	case File:
		data, err := os.ReadFile(r.Location)
		if err != nil {
			return Fetched{}, fmt.Errorf("read manifest: %w", err)
		}

		return Fetched{Data: data}, nil
	case HTTP:
		data, err := f.get(ctx, r.Location, nil)
		if err != nil {
			return Fetched{}, fmt.Errorf("fetch %s: %w", r.Location, err)
		}

		return Fetched{Data: data}, nil
	case GitHub:
		return f.fetchGitHub(ctx, r, commit)
	default:
		return f.fetchGit(ctx, r, commit)
	}
}

func (f *Fetcher) fetchGitHub(ctx context.Context, r Ref, commit string) (Fetched, error) {
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

	for _, path := range manifestPaths(r.Fragment) {
		data, err := f.get(ctx, f.GitHubRaw+"/"+r.Location+"/"+commit+"/"+path, nil)
		if errors.Is(err, errNotFound) {
			continue
		}

		if err != nil {
			return Fetched{}, fmt.Errorf("fetch %s: %w", r, err)
		}

		return Fetched{Data: data, Commit: commit}, nil
	}

	return Fetched{}, fmt.Errorf(
		"%s: no %s at commit %s",
		r, strings.Join(manifestPaths(r.Fragment), " or "), commit,
	)
}

// manifestPaths lists where a GitHub ref's manifest may be, in lookup order.
func manifestPaths(name string) []string {
	if name == "" {
		return []string{DefaultManifest}
	}

	return []string{name + ".toml", "packages/" + name + ".toml"}
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
		return nil, errNotFound
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

// fetchGit reads the manifest from a shallow clone kept in the cache. It needs
// the git binary, because git hosts share no HTTP API.
func (f *Fetcher) fetchGit(ctx context.Context, r Ref, commit string) (Fetched, error) {
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
		path = DefaultManifest
	}

	// os.Root refuses to follow a symlink in the repository that points outside it.
	root, err := os.OpenRoot(dir)
	if err != nil {
		return Fetched{}, err
	}
	defer root.Close()

	data, err := root.ReadFile(path)
	if err != nil {
		return Fetched{}, fmt.Errorf("%s: read %s: %w", r, path, err)
	}

	return Fetched{Data: data, Commit: head}, nil
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
