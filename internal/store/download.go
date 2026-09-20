package store

import (
	"context"
	"crypto/sha256"
	"encoding/hex"
	"fmt"
	"io"
	"net/http"
	"os"
	"path/filepath"
	"regexp"
	"strings"
)

// fetch downloads url into the cache and returns the file's path and digest. It
// names the file after its digest and reuses an earlier download that still
// verifies. With a wantSHA it deletes a download whose digest differs. With an
// empty wantSHA it accepts the download, and the caller pins the digest.
func (s *Store) fetch(ctx context.Context, url, wantSHA string) (string, string, error) {
	dir := filepath.Join(s.cache, "downloads")
	if err := os.MkdirAll(dir, 0o755); err != nil {
		return "", "", fmt.Errorf("create download cache: %w", err)
	}

	if wantSHA != "" {
		dest := filepath.Join(dir, wantSHA)
		if got, err := fileSHA256(dest); err == nil && got == wantSHA {
			return dest, wantSHA, nil
		}
	}

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

	_, err = io.Copy(io.MultiWriter(tmp, hash), resp.Body)
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

	return dest, got, nil
}

func (s *Store) get(ctx context.Context, url string) (*http.Response, error) {
	req, err := http.NewRequestWithContext(ctx, http.MethodGet, url, nil)
	if err != nil {
		return nil, fmt.Errorf("download %s: %w", url, err)
	}

	req.Header.Set("User-Agent", "oku")

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

var hexDigestRe = regexp.MustCompile(`\b[0-9a-fA-F]{64}\b`)

// publishedSHA256 reads the digest of fileName from a checksum file at url. The
// file holds either one digest or "digest  name" lines such as sha256sum writes.
func (s *Store) publishedSHA256(ctx context.Context, url, fileName string) (string, error) {
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
