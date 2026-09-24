// Package goproxy reads the versions of Go modules from a module proxy.
package goproxy

import (
	"context"
	"encoding/json"
	"errors"
	"fmt"
	"io"
	"net/http"
	"path"
	"regexp"
	"strings"
	"unicode"

	"github.com/y3owk1n/oku/internal/shape"
)

// Proxy is the module proxy that the go command uses by default.
const Proxy = "https://proxy.golang.org"

// maxBody is the most bytes read from one answer of the proxy.
const maxBody = 8 << 20

// ErrNotFound reports that no module holds a package path.
var ErrNotFound = errors.New("no module holds this package")

// pathRe is a package path as the go command accepts it, such as
// "golang.org/x/tools/gopls". The first element has a dot.
var pathRe = regexp.MustCompile(`^[a-z0-9.-]+\.[a-z]{2,}(/[A-Za-z0-9._~+-]+)+$`)

// ValidPath reports whether p is a package path the proxy can serve.
func ValidPath(p string) bool {
	return pathRe.MatchString(p) && !strings.Contains(p, "..")
}

// Escape writes a module path the way the proxy wants it, with each capital
// letter as "!" and its lower case.
func Escape(p string) string {
	var b strings.Builder

	for _, r := range p {
		if unicode.IsUpper(r) {
			b.WriteByte('!')
			r = unicode.ToLower(r)
		}

		b.WriteRune(r)
	}

	return b.String()
}

// Module returns the module that holds the package at pkg, which is the
// longest prefix of it that the proxy knows, as "go install" finds it.
func Module(ctx context.Context, client *http.Client, proxy, pkg string) (string, error) {
	for at := pkg; strings.Contains(at, "/"); at = path.Dir(at) {
		_, err := get(ctx, client, proxy, at+"/@v/list")
		if err == nil {
			return at, nil
		}

		if !errors.Is(err, ErrNotFound) {
			return "", err
		}
	}

	return "", fmt.Errorf("%s: %w", pkg, ErrNotFound)
}

// Versions returns the tagged versions of module, without the leading "v". A
// module with no tags has one version, the pseudo-version of its newest commit.
func Versions(ctx context.Context, client *http.Client, proxy, module string) ([]string, error) {
	body, err := get(ctx, client, proxy, module+"/@v/list")
	if err != nil {
		return nil, err
	}

	var versions []string

	for _, line := range strings.Fields(string(body)) {
		versions = append(versions, strings.TrimPrefix(line, "v"))
	}

	if len(versions) > 0 {
		return versions, nil
	}

	body, err = get(ctx, client, proxy, module+"/@latest")
	if err != nil {
		return nil, err
	}

	var latest struct{ Version string }
	if err := json.Unmarshal(body, &latest); err != nil {
		return nil, fmt.Errorf("read the proxy's answer for %s: %w", module, err)
	}

	if err := shape.Check("the Go proxy's answer for "+module,
		shape.Field{Name: "the version", Has: latest.Version != ""}); err != nil {
		return nil, err
	}

	return []string{strings.TrimPrefix(latest.Version, "v")}, nil
}

func get(ctx context.Context, client *http.Client, proxy, at string) ([]byte, error) {
	if proxy == "" {
		proxy = Proxy
	}

	url := strings.TrimRight(proxy, "/") + "/" + Escape(at)

	req, err := http.NewRequestWithContext(ctx, http.MethodGet, url, nil)
	if err != nil {
		return nil, err
	}

	req.Header.Set("User-Agent", "oku")

	resp, err := client.Do(req)
	if err != nil {
		return nil, err
	}
	defer resp.Body.Close()

	switch {
	// The proxy answers 410 for a path that is no module.
	case resp.StatusCode == http.StatusNotFound || resp.StatusCode == http.StatusGone:
		return nil, ErrNotFound
	case resp.StatusCode != http.StatusOK:
		return nil, fmt.Errorf("the module proxy returned %s", resp.Status)
	}

	body, err := io.ReadAll(io.LimitReader(resp.Body, maxBody+1))
	if err != nil {
		return nil, err
	}

	if len(body) > maxBody {
		return nil, fmt.Errorf("the module proxy answered with more than %d bytes", maxBody)
	}

	return body, nil
}
