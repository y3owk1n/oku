package store

import (
	"context"
	"crypto/sha256"
	"crypto/sha512"
	"encoding/base64"
	"encoding/hex"
	"encoding/json"
	"fmt"
	"io"
	"net/http"
	"os"
	"path"
	"path/filepath"
	"regexp"
	"strings"
	"time"

	"github.com/y3owk1n/oku/internal/status"
)

// recentDownload is how long fetch trusts that a url still serves the bytes it
// served last time, when the caller has no digest to ask for.
const recentDownload = 24 * time.Hour

// fetch downloads url into the cache and returns the file's path and digest. It
// names the file after its digest and reuses an earlier download that still
// verifies. With a wantSHA it deletes a download whose digest differs. With an
// empty wantSHA it accepts the download, and the caller pins the digest.
func (s *Store) fetch(ctx context.Context, url, wantSHA string) (string, string, error) {
	return s.download(ctx, url, wantSHA, true)
}

// download does the work of fetch. With reuseRecent false it ignores the digest
// that an earlier download of url gave, which a url whose bytes change needs.
func (s *Store) download(
	ctx context.Context,
	url, wantSHA string,
	reuseRecent bool,
) (string, string, error) {
	dir := filepath.Join(s.cache, "downloads")
	if err := os.MkdirAll(dir, 0o755); err != nil {
		return "", "", fmt.Errorf("create download cache: %w", err)
	}

	// A run that stops before it writes the lock records no digest, so fetch
	// uses the digest that a recent download of the same url gave.
	recent := filepath.Join(dir, "by-url", fmt.Sprintf("%x", sha256.Sum256([]byte(url))))

	cached := wantSHA
	if info, err := os.Stat(recent); reuseRecent && cached == "" && err == nil &&
		time.Since(info.ModTime()) < recentDownload {
		data, _ := os.ReadFile(recent)
		cached = strings.TrimSpace(string(data))
	}

	if cached != "" {
		dest := filepath.Join(dir, cached)
		if got, err := fileSHA256(dest); err == nil && got == cached {
			return dest, cached, nil
		}
	}

	defer status.Start(ctx, "downloading %s", url)()

	resp, err := s.get(ctx, url)
	if err != nil {
		return "", "", err
	}
	defer resp.Body.Close()

	tmp, err := os.CreateTemp(dir, ".partial-*")
	if err != nil {
		return "", "", fmt.Errorf("download %s: %w", url, err)
	}
	defer os.Remove(tmp.Name())

	hash := sha256.New()

	_, err = io.Copy(io.MultiWriter(tmp, hash), status.Reader(ctx, resp.Body, resp.ContentLength))
	if closeErr := tmp.Close(); err == nil {
		err = closeErr
	}

	if err != nil {
		return "", "", fmt.Errorf("download %s: %w", url, err)
	}

	got := hex.EncodeToString(hash.Sum(nil))
	if wantSHA != "" && got != wantSHA {
		return "", "", fmt.Errorf(
			"checksum mismatch for %s: expected %s, download is %s",
			url, wantSHA, got,
		)
	}

	// Two packages of one sync may download the same file at once. Windows
	// refuses to replace the file while the other package unpacks it. The
	// file's name is its digest, so it holds the same bytes and oku uses it.
	dest := filepath.Join(dir, got)
	if err := os.Rename(tmp.Name(), dest); err != nil {
		if have, shaErr := fileSHA256(dest); shaErr != nil || have != got {
			return "", "", fmt.Errorf("download %s: %w", url, err)
		}
	}

	// The index only avoids a later download, so fetch ignores a failed write.
	if os.MkdirAll(filepath.Dir(recent), 0o755) == nil {
		_ = os.WriteFile(recent, []byte(got+"\n"), 0o644)
	}

	return dest, got, nil
}

// Reachable reports an error when url serves no download, the error that a
// download of it would give. It reads none of the body.
func (s *Store) Reachable(ctx context.Context, url string) error {
	resp, err := s.get(ctx, url)
	if err != nil {
		return err
	}

	return resp.Body.Close()
}

func (s *Store) get(ctx context.Context, url string) (*http.Response, error) {
	req, err := http.NewRequestWithContext(ctx, http.MethodGet, url, nil)
	if err != nil {
		return nil, fmt.Errorf("download %s: %w", url, err)
	}

	req.Header.Set("User-Agent", "oku")

	// Go drops this header when a redirect leaves the host.
	if header := s.auth.For(url); header != "" {
		req.Header.Set("Authorization", header)
	}

	resp, err := s.http.Do(req)
	if err != nil {
		return nil, fmt.Errorf("download %s: %w", url, err)
	}

	if resp.StatusCode == http.StatusNotFound && s.Private != nil {
		if private, err := s.privateGet(ctx, url); private != nil || err != nil {
			resp.Body.Close()

			return private, err
		}
	}

	if resp.StatusCode != http.StatusOK {
		resp.Body.Close()

		return nil, fmt.Errorf("download %s: server returned %s", url, resp.Status)
	}

	return resp, nil
}

// privateGet downloads url from the address Private gives, or returns nil when
// Private knows none. The API answers with a redirect to a signed address.
func (s *Store) privateGet(ctx context.Context, url string) (*http.Response, error) {
	api, header, err := s.Private(ctx, url)
	if err != nil {
		return nil, fmt.Errorf("download %s: %w", url, err)
	}

	if api == "" {
		return nil, nil
	}

	req, err := http.NewRequestWithContext(ctx, http.MethodGet, api, nil)
	if err != nil {
		return nil, fmt.Errorf("download %s: %w", url, err)
	}

	req.Header.Set("User-Agent", "oku")
	req.Header.Set("Accept", "application/octet-stream")
	req.Header.Set("Authorization", header)

	// The address the API redirects to is signed, so the token never follows,
	// whichever host it names.
	client := *s.http
	client.CheckRedirect = func(next *http.Request, _ []*http.Request) error {
		next.Header.Del("Authorization")

		return nil
	}

	resp, err := client.Do(req)
	if err != nil {
		return nil, fmt.Errorf("download %s: %w", url, err)
	}

	if resp.StatusCode != http.StatusOK {
		resp.Body.Close()

		return nil, fmt.Errorf("download %s through the API: server returned %s", url, resp.Status)
	}

	return resp, nil
}

// verifyIntegrity checks the file at path against want, which is "sha512-" and
// the digest in base64.
func verifyIntegrity(path, want string) error {
	got, err := fileIntegrity(path)
	if err != nil {
		return err
	}

	if got != want {
		return fmt.Errorf("integrity mismatch: expected %s, download is %s", want, got)
	}

	return nil
}

// fileIntegrity returns the sha512 of the file at path the way npm writes it.
func fileIntegrity(path string) (string, error) {
	f, err := os.Open(path)
	if err != nil {
		return "", err
	}
	defer f.Close()

	hash := sha512.New()
	if _, err := io.Copy(hash, f); err != nil {
		return "", err
	}

	return "sha512-" + base64.StdEncoding.EncodeToString(hash.Sum(nil)), nil
}

var hexDigestRe = regexp.MustCompile(`\b[0-9a-fA-F]{64}\b`)

// PublishedSHA256 reads the digest of fileName from a checksum file at url. The
// file holds one digest, "digest  name" lines such as sha256sum writes, or a JSON
// document that maps file names to digests.
func (s *Store) PublishedSHA256(ctx context.Context, url, fileName string) (string, error) {
	defer status.Start(ctx, "reading the checksums at %s", url)()

	resp, err := s.get(ctx, url)
	if err != nil {
		return "", err
	}
	defer resp.Body.Close()

	data, err := io.ReadAll(io.LimitReader(resp.Body, 1<<20))
	if err != nil {
		return "", fmt.Errorf("download %s: %w", url, err)
	}

	if digest := jsonSHA256(data, fileName); digest != "" {
		return digest, nil
	}

	lines := strings.Split(string(data), "\n")
	for _, line := range lines {
		if strings.Contains(line, fileName) {
			if digest := hexDigestRe.FindString(line); digest != "" {
				return strings.ToLower(digest), nil
			}
		}
	}

	if digests := hexDigestRe.FindAllString(string(data), -1); len(digests) == 1 {
		return strings.ToLower(digests[0]), nil
	}

	return "", fmt.Errorf("%s holds no sha256 for %s", url, fileName)
}

// jsonSHA256 reads the digest of fileName from a JSON checksum manifest, or
// returns "". The document is an object whose keys are file names and whose
// values are digests, or an array of objects with "name" and "sha256" fields.
// A key or a name may hold a path, and its base name is what counts.
func jsonSHA256(data []byte, fileName string) string {
	var digest string

	switch trimmed := strings.TrimSpace(string(data)); {
	case strings.HasPrefix(trimmed, "{"):
		var byName map[string]string
		if json.Unmarshal(data, &byName) != nil {
			return ""
		}

		for name, sum := range byName {
			if path.Base(name) == fileName {
				digest = sum
			}
		}
	case strings.HasPrefix(trimmed, "["):
		var entries []struct {
			Name   string `json:"name"`
			SHA256 string `json:"sha256"`
		}
		if json.Unmarshal(data, &entries) != nil {
			return ""
		}

		for _, entry := range entries {
			if path.Base(entry.Name) == fileName {
				digest = entry.SHA256
			}
		}
	}

	return strings.ToLower(hexDigestRe.FindString(digest))
}

func fileSHA256(path string) (string, error) {
	f, err := os.Open(path)
	if err != nil {
		return "", err
	}
	defer f.Close()

	hash := sha256.New()
	if _, err := io.Copy(hash, f); err != nil {
		return "", err
	}

	return hex.EncodeToString(hash.Sum(nil)), nil
}
