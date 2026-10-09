package forge

import (
	"bytes"
	"context"
	"crypto/sha256"
	"encoding/hex"
	"encoding/json"
	"errors"
	"fmt"
	"io"
	"io/fs"
	"net/http"
	"os"
	"path/filepath"
	"strings"
	"sync"
	"time"

	"github.com/klauspost/compress/zstd"
)

// Revalidating returns a client that keeps each GET answer with an ETag under
// dir, and asks the host again with If-None-Match. The host answers 304 when
// nothing changed. GitHub does not count a 304 against the rate limit of a
// request with a token. The host checks every request, so the answer is never
// stale. A file at a full commit SHA cannot change, so the client gives the
// answer it kept, with or without an ETag, and asks nothing.
func Revalidating(dir string, next http.RoundTripper) *http.Client {
	return &http.Client{Transport: revalidator{dir: dir, next: next}, CheckRedirect: CheckRedirect}
}

type revalidator struct {
	dir  string
	next http.RoundTripper
}

// encoder and decoder pack and unpack the body of a kept answer. A release list
// of 6.6 MB packs to 0.4 MB, and both calls are safe to use from several
// goroutines.
var (
	encoder, _ = zstd.NewWriter(nil, zstd.WithEncoderLevel(zstd.SpeedFastest))
	decoder, _ = zstd.NewReader(nil, zstd.WithDecoderMaxMemory(maxAnswer))
)

// forward is the transport of http.DefaultClient when a test replaces it, so
// the test reaches the forges through this cache too, else r.next.
func (r revalidator) forward() http.RoundTripper {
	if t := http.DefaultClient.Transport; t != nil {
		return t
	}

	return r.next
}

// kept is one answer on disk. Link holds the next page of a list, and Type the
// media type, which a reader may check. Bytes is the length of the body before
// compression, and an answer without it is from an older oku that kept the body
// inside the JSON.
type kept struct {
	ETag  string `json:"etag"`
	Link  string `json:"link,omitempty"`
	Type  string `json:"type,omitempty"`
	Bytes int    `json:"bytes"`
	// Body is the answer itself. It stays out of the JSON, since base64 makes a
	// body of 30 MB a third larger and costs a full scan of it to decode.
	Body []byte `json:"-"`
}

// read loads the answer at path, one line of JSON and then the body as zstd.
func read(path string) (kept, bool) {
	data, err := os.ReadFile(path)
	if err != nil {
		return kept{}, false
	}

	header, packed, found := bytes.Cut(data, []byte{'\n'})
	if !found {
		return kept{}, false
	}

	var answer kept
	if json.Unmarshal(header, &answer) != nil || answer.Type == "" || answer.Bytes == 0 {
		return kept{}, false
	}

	body, err := decoder.DecodeAll(packed, make([]byte, 0, answer.Bytes))
	if err != nil || len(body) != answer.Bytes {
		return kept{}, false
	}

	answer.Body = body

	return answer, true
}

func (r revalidator) RoundTrip(req *http.Request) (*http.Response, error) {
	if req.Method != http.MethodGet {
		return r.forward().RoundTrip(req)
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

	// oku asks again in full for an answer it cannot read, such as one an older
	// oku kept in another format.
	old, have := read(path)

	// A file at a full commit SHA never changes, so oku asks the host nothing.
	if have && req.Context().Value(fixedKey{}) != nil {
		used(path)

		return &http.Response{
			Status:        "200 OK",
			StatusCode:    http.StatusOK,
			Header:        http.Header{"Content-Type": {old.Type}},
			Body:          io.NopCloser(bytes.NewReader(old.Body)),
			ContentLength: int64(len(old.Body)),
			Request:       req,
		}, nil
	}

	if have && old.ETag != "" {
		req = req.Clone(req.Context())
		req.Header.Set("If-None-Match", old.ETag)
	}

	resp, err := r.forward().RoundTrip(req)
	if err != nil {
		return nil, err
	}

	switch {
	case resp.StatusCode == http.StatusNotModified && have:
		resp.Body.Close()
		used(path)

		resp.StatusCode, resp.Status = http.StatusOK, "200 OK"
		resp.Header.Set("Link", old.Link)
		resp.Header.Set("Content-Type", old.Type)
		resp.Body = io.NopCloser(bytes.NewReader(old.Body))
		resp.ContentLength = int64(len(old.Body))
	case resp.StatusCode == http.StatusOK &&
		(resp.Header.Get("ETag") != "" || req.Context().Value(fixedKey{}) != nil):
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
				Type: resp.Header.Get("Content-Type"), Bytes: len(body), Body: body,
			})
		}
	}

	return resp, nil
}

// used sets the time on the answer at path. The age of the file says when oku
// last used the answer, which is what gc goes by.
func used(path string) {
	now := time.Now()
	_ = os.Chtimes(path, now, now)
}

type fixedKey struct{}

// fixed marks ctx, when commit is a full SHA, so that the client gives the
// answer it kept without asking the host. A branch, a tag or a short SHA can
// name another commit later, so fixed leaves ctx as it is for them.
func fixed(ctx context.Context, commit string) context.Context {
	if !FullSHA(commit) {
		return ctx
	}

	return context.WithValue(ctx, fixedKey{}, true)
}

// FullSHA reports whether commit is a whole SHA-1 or SHA-256 commit id, which
// always names the same files.
func FullSHA(commit string) bool {
	return (len(commit) == 40 || len(commit) == 64) && strings.Trim(commit, "0123456789abcdef") == ""
}

// keep writes an answer in one rename. The cache only saves requests, so a
// failed write costs one request later and is not an error.
func keep(path string, answer kept) {
	header, err := json.Marshal(answer)
	if err != nil || os.MkdirAll(filepath.Dir(path), 0o755) != nil {
		return
	}

	data := append(header, '\n')
	data = encoder.EncodeAll(answer.Body, data)

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

// AnswerRetention is how long an answer that no command has read stays in the
// cache. Fetching one again costs a single request, so an answer that no lookup
// has read for a month is not worth the disk.
const AnswerRetention = 30 * 24 * time.Hour

// StaleAnswers returns the answers under dir that no command has read for
// keepFor, with their sizes. Reading an answer sets the time on its file, so
// the age is the time since oku last used it.
func StaleAnswers(dir string, now time.Time, keepFor time.Duration) (map[string]int64, error) {
	entries, err := os.ReadDir(dir)
	if errors.Is(err, fs.ErrNotExist) {
		return nil, nil
	}

	if err != nil {
		return nil, fmt.Errorf("read %s: %w", dir, err)
	}

	stale := map[string]int64{}

	for _, entry := range entries {
		info, err := entry.Info()
		if err != nil || entry.IsDir() {
			continue
		}

		if now.Sub(info.ModTime()) >= keepFor {
			stale[filepath.Join(dir, entry.Name())] = info.Size()
		}
	}

	return stale, nil
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
