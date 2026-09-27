package store

import (
	"errors"
	"fmt"
	"io"
	"io/fs"
	"net/http"
	"net/url"
	"os"
	"runtime"
	"strings"
)

// fileTransport serves file:// URLs from this machine's disk. Go's own file
// transport joins the URL path under one root, which cannot name a Windows
// drive, so file:///C:/x.zip would never be found.
type fileTransport struct{}

func (fileTransport) RoundTrip(req *http.Request) (*http.Response, error) {
	path, err := localPath(req.URL, runtime.GOOS)
	if err != nil {
		return nil, err
	}

	respond := func(code int, body io.ReadCloser, size int64) *http.Response {
		return &http.Response{
			Status: fmt.Sprintf("%d %s", code, http.StatusText(code)), StatusCode: code,
			Proto: "HTTP/1.0", ProtoMajor: 1, Header: http.Header{},
			Body: body, ContentLength: size, Request: req,
		}
	}

	f, err := os.Open(path)
	if errors.Is(err, fs.ErrNotExist) {
		return respond(http.StatusNotFound, http.NoBody, 0), nil
	}

	if err != nil {
		return nil, err
	}

	info, err := f.Stat()
	if err != nil || info.IsDir() {
		f.Close()

		return respond(http.StatusNotFound, http.NoBody, 0), nil
	}

	return respond(http.StatusOK, f, info.Size()), nil
}

// localPath turns a file:// URL into a path on goos. On Windows file:///C:/x
// is C:\x, and file://server/share/x is the share \\server\share\x. Elsewhere
// a URL can only name a file of this machine.
func localPath(u *url.URL, goos string) (string, error) {
	p, local := u.Path, u.Host == "" || strings.EqualFold(u.Host, "localhost")

	if goos != "windows" {
		if !local {
			return "", fmt.Errorf("%s names a file of another machine", u)
		}

		return p, nil
	}

	if !local {
		return `\\` + u.Host + strings.ReplaceAll(p, "/", `\`), nil
	}

	if len(p) >= 3 && p[0] == '/' && p[2] == ':' {
		p = p[1:]
	}

	return strings.ReplaceAll(p, "/", `\`), nil
}
