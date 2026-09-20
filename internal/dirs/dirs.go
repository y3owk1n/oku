// Package dirs locates oku's config, data and cache directories.
package dirs

import (
	"fmt"
	"os"
	"path/filepath"
	"runtime"
)

// Config returns the directory holding oku.toml and oku.lock.
func Config() (string, error) {
	return resolve("XDG_CONFIG_HOME", "APPDATA", ".config", "")
}

// Data returns the directory holding the store and profiles.
func Data() (string, error) {
	return resolve("XDG_DATA_HOME", "LOCALAPPDATA", filepath.Join(".local", "share"), "")
}

// Cache returns the directory holding downloads.
func Cache() (string, error) {
	return resolve("XDG_CACHE_HOME", "LOCALAPPDATA", ".cache", "cache")
}

// resolve prefers the XDG variable on every OS. Without it, Windows uses winEnv
// and everything else uses homeRel under the home directory. winSub separates
// directories that share one winEnv.
func resolve(xdgEnv, winEnv, homeRel, winSub string) (string, error) {
	if base := os.Getenv(xdgEnv); base != "" {
		return filepath.Join(base, "oku"), nil
	}

	if runtime.GOOS == "windows" {
		if base := os.Getenv(winEnv); base != "" {
			return filepath.Join(base, "oku", winSub), nil
		}
	}

	home, err := os.UserHomeDir()
	if err != nil {
		return "", fmt.Errorf("locate home directory: %w", err)
	}

	return filepath.Join(home, homeRel, "oku"), nil
}
