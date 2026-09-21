package store

import (
	"context"
	"crypto/sha256"
	"crypto/sha512"
	"encoding/base64"
	"encoding/hex"
	"fmt"
	"io"
	"net/http"
	"os"
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

	dest := filepath.Join(dir, got)
	if err := os.Rename(tmp.Name(), dest); err != nil {
		return "", "", fmt.Errorf("download %s: %w", url, err)
	}

	// The index only avoids a later download, so fetch ignores a failed write.
	if os.MkdirAll(filepath.Dir(recent), 0o755) == nil {
		_ = os.WriteFile(recent, []byte(got+"\n"), 0o644)
	}

	return dest, got, nil
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

	if resp.StatusCode != http.StatusOK {
		resp.Body.Close()

		return nil, fmt.Errorf("download %s: server returned %s", url, resp.Status)
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

// publishedSHA256 reads the digest of fileName from a checksum file at url. The
// file holds either one digest or "digest  name" lines such as sha256sum writes.
func (s *Store) publishedSHA256(ctx context.Context, url, fileName string) (string, error) {
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
