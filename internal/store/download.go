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
)

// fetch downloads url into the cache and returns the file's path. It names the
// file after wantSHA and reuses an earlier download that still verifies. It
// deletes a download whose digest differs from wantSHA.
func (s *Store) fetch(ctx context.Context, url, wantSHA string) (string, error) {
	dir := filepath.Join(s.cache, "downloads")
	if err := os.MkdirAll(dir, 0o755); err != nil {
		return "", fmt.Errorf("create download cache: %w", err)
	}

	dest := filepath.Join(dir, wantSHA)
	if got, err := fileSHA256(dest); err == nil && got == wantSHA {
		return dest, nil
	}

	req, err := http.NewRequestWithContext(ctx, http.MethodGet, url, nil)
	if err != nil {
		return "", fmt.Errorf("download %s: %w", url, err)
	}

	req.Header.Set("User-Agent", "oku")

	resp, err := s.http.Do(req)
	if err != nil {
		return "", fmt.Errorf("download %s: %w", url, err)
	}
	defer resp.Body.Close()

	if resp.StatusCode != http.StatusOK {
		return "", fmt.Errorf("download %s: server returned %s", url, resp.Status)
	}

	tmp, err := os.CreateTemp(dir, ".partial-*")
	if err != nil {
		return "", fmt.Errorf("download %s: %w", url, err)
	}
	defer os.Remove(tmp.Name())

	hash := sha256.New()

	_, err = io.Copy(io.MultiWriter(tmp, hash), resp.Body)
	if closeErr := tmp.Close(); err == nil {
		err = closeErr
	}

	if err != nil {
		return "", fmt.Errorf("download %s: %w", url, err)
	}

	if got := hex.EncodeToString(hash.Sum(nil)); got != wantSHA {
		return "", fmt.Errorf(
			"checksum mismatch for %s: manifest says %s, download is %s",
			url, wantSHA, got,
		)
	}

	if err := os.Rename(tmp.Name(), dest); err != nil {
		return "", fmt.Errorf("download %s: %w", url, err)
	}

	return dest, nil
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
