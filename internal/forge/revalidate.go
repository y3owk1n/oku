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
	"sync"
	"time"

	"github.com/klauspost/compress/zstd"
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

// encoder and decoder pack and unpack the body of a kept answer. A release list
// of 6.6 MB keeps as 0.4 MB, and both calls are safe from several goroutines.
var (
	encoder, _ = zstd.NewWriter(nil, zstd.WithEncoderLevel(zstd.SpeedFastest))
	decoder, _ = zstd.NewReader(nil, zstd.WithDecoderMaxMemory(maxAnswer))
)

// next is the transport of http.DefaultClient, so a test that replaces it
// reaches the forges through this cache too.
func next() http.RoundTripper {
	if t := http.DefaultClient.Transport; t != nil {
		return t
	}

	return http.DefaultTransport
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
	// Body is the answer itself. It does not go into the JSON, since a body of
	// 30 MB costs a third more as base64 and another read of all of it to decode.
	Body []byte `json:"-"`
}

// read loads the answer at path: one line of JSON, then the body as zstd.
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

	// oku asks again in full for an answer it cannot read, such as one an older
	// oku kept in another format.
	old, have := read(path)
	if have {
		req = req.Clone(req.Context())
		req.Header.Set("If-None-Match", old.ETag)
	}

	resp, err := next().RoundTrip(req)
	if err != nil {
		return nil, err
	}

	switch {
	case resp.StatusCode == http.StatusNotModified && have:
		resp.Body.Close()

		// The age of the file says when oku last used the answer, which is what gc
		// goes by.
		now := time.Now()
		_ = os.Chtimes(path, now, now)

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
				Type: resp.Header.Get("Content-Type"), Bytes: len(body), Body: body,
			})
		}
	}

	return resp, nil
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

// AnswerRetention is how long an answer that no command has used stays in the
// cache. An answer costs one request to fetch again, and a machine that has not
// looked a package up in a month is unlikely to want its old answer.
const AnswerRetention = 30 * 24 * time.Hour

// StaleAnswers returns the answers under dir that no command has read for
// AnswerRetention, with their sizes. Reading an answer sets the time on its
// file, so the age is the time since oku last used it.
func StaleAnswers(dir string, now time.Time) (map[string]int64, error) {
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

		if now.Sub(info.ModTime()) >= AnswerRetention {
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
