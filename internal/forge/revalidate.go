package forge

import (
	"bytes"
	"context"
	"crypto/sha256"
	"encoding/hex"
	"encoding/json"
	"io"
	"net/http"
	"os"
	"path/filepath"
	"sync"
)

// Revalidating returns a client that keeps each GET answer with an ETag under
// dir, and asks the host again with If-None-Match. The host answers 304 when
// nothing changed. GitHub does not count a 304 against the rate limit of a
// request with a token. The host checks every request, so the answer is never
// stale.
func Revalidating(dir string) *http.Client {
	return &http.Client{Transport: revalidator{dir: dir}}
}

type revalidator struct{ dir string }

// next is the transport of http.DefaultClient, so a test that replaces it
// reaches the forges through this cache too.
func next() http.RoundTripper {
	if t := http.DefaultClient.Transport; t != nil {
		return t
	}

	return http.DefaultTransport
}

// kept is one answer on disk. Link holds the next page of a list, and Type the
// media type, which a reader may check.
type kept struct {
	ETag string `json:"etag"`
	Link string `json:"link,omitempty"`
	Type string `json:"type,omitempty"`
	Body []byte `json:"body"`
}

func (r revalidator) RoundTrip(req *http.Request) (*http.Response, error) {
	if req.Method != http.MethodGet {
		return next().RoundTrip(req)
	}

	if a, ok := req.Context().Value(answersKey{}).(*answers); ok {
		return a.get(req, r.revalidate)
	}

	return r.revalidate(req)
}

func (r revalidator) revalidate(req *http.Request) (*http.Response, error) {
	// The same URL gives a sha, raw bytes or JSON depending on Accept.
	sum := sha256.Sum256([]byte(req.URL.String() + "\n" + req.Header.Get("Accept")))
	path := filepath.Join(r.dir, hex.EncodeToString(sum[:]))

	var old kept

	// oku asks again in full for an answer kept without its media type, as an
	// older oku kept it.
	if data, err := os.ReadFile(path); err == nil && json.Unmarshal(data, &old) == nil && old.Type != "" {
		req = req.Clone(req.Context())
		req.Header.Set("If-None-Match", old.ETag)
	}

	resp, err := next().RoundTrip(req)
	if err != nil {
		return nil, err
	}

	switch {
	case resp.StatusCode == http.StatusNotModified && old.ETag != "":
		resp.Body.Close()

		resp.StatusCode, resp.Status = http.StatusOK, "200 OK"
		resp.Header.Set("Link", old.Link)
		resp.Header.Set("Content-Type", old.Type)
		resp.Body = io.NopCloser(bytes.NewReader(old.Body))
		resp.ContentLength = int64(len(old.Body))
	case resp.StatusCode == http.StatusOK && resp.Header.Get("ETag") != "":
		// Every reader of this client refuses an answer over maxAnswer, so more is
		// not worth reading.
		body, err := io.ReadAll(io.LimitReader(resp.Body, maxAnswer+1))
		resp.Body.Close()

		if err != nil {
			return nil, err
		}

		resp.Body = io.NopCloser(bytes.NewReader(body))

		if len(body) <= maxAnswer {
			keep(path, kept{
				ETag: resp.Header.Get("ETag"), Link: resp.Header.Get("Link"),
				Type: resp.Header.Get("Content-Type"), Body: body,
			})
		}
	}

	return resp, nil
}

// keep writes an answer in one rename. The cache only saves requests, so a
// failed write costs one request later and is not an error.
func keep(path string, answer kept) {
	data, err := json.Marshal(answer)
	if err != nil || os.MkdirAll(filepath.Dir(path), 0o755) != nil {
		return
	}

	tmp, err := os.CreateTemp(filepath.Dir(path), ".tmp-")
	if err != nil {
		return
	}
	defer os.Remove(tmp.Name())

	_, err = tmp.Write(data)
	if closeErr := tmp.Close(); err == nil && closeErr == nil {
		_ = os.Rename(tmp.Name(), path)
	}
}

type answersKey struct{}

// maxAnswer is the most of an answer that the client reads and keeps. It is
// the largest limit of any reader of this client, which the npm and PyPI
// readers have.
const maxAnswer = 64 << 20

// answers holds the GET answers of one command, by URL and Accept.
type answers struct {
	mu    sync.Mutex
	byKey map[string]*answer
}

type answer struct {
	once   sync.Once
	status string
	code   int
	header http.Header
	body   []byte
	err    error
}

// WithAnswers returns a context in which a revalidating client asks the host
// once for each URL, and gives a repeat the same answer. Inference and the
// version lookup of one package read the same registry document, as do the
// packages that share a repo.
func WithAnswers(ctx context.Context) context.Context {
	return context.WithValue(ctx, answersKey{}, &answers{byKey: map[string]*answer{}})
}

func (a *answers) get(
	req *http.Request,
	ask func(*http.Request) (*http.Response, error),
) (*http.Response, error) {
	key := req.URL.String() + "\n" + req.Header.Get("Accept")

	a.mu.Lock()

	x := a.byKey[key]
	if x == nil {
		x = &answer{}
		a.byKey[key] = x
	}

	a.mu.Unlock()

	x.once.Do(func() {
		resp, err := ask(req)
		if err != nil {
			x.err = err

			return
		}
		defer resp.Body.Close()

		x.status, x.code, x.header = resp.Status, resp.StatusCode, resp.Header
		x.body, x.err = io.ReadAll(io.LimitReader(resp.Body, maxAnswer+1))
	})

	if x.err != nil {
		return nil, x.err
	}

	return &http.Response{
		Status:        x.status,
		StatusCode:    x.code,
		Header:        x.header.Clone(),
		Body:          io.NopCloser(bytes.NewReader(x.body)),
		ContentLength: int64(len(x.body)),
		Request:       req,
	}, nil
}
