package xz_test

import (
	"bytes"
	"io"
	"math/rand/v2"
	"testing"

	ulikunitz "github.com/ulikunitz/xz"

	"github.com/y3owk1n/oku/internal/xz"
)

func TestReaderDecodesWhatAnEncoderWrote(t *testing.T) {
	// Runs of one byte and short patterns make matches that overlap what they
	// write, random bytes make literals, and 4 MiB in a 64 KiB dictionary make
	// matches that wrap around the end of the buffer.
	rng := rand.New(rand.NewPCG(1, 2))

	var want bytes.Buffer

	for want.Len() < 4<<20 {
		switch rng.IntN(4) {
		case 0:
			want.Write(bytes.Repeat([]byte{byte(rng.IntN(256))}, 1+rng.IntN(300)))
		case 1:
			want.Write(bytes.Repeat([]byte("abc"), 1+rng.IntN(100)))
		case 2:
			for range 1 + rng.IntN(500) {
				want.WriteByte(byte(rng.IntN(256)))
			}
		default:
			// An earlier stretch again, from up to 60 KiB back.
			from := max(0, want.Len()-rng.IntN(60<<10))
			want.Write(bytes.Clone(want.Bytes()[from:min(want.Len(), from+1+rng.IntN(2000))]))
		}
	}

	var packed bytes.Buffer

	w, err := ulikunitz.WriterConfig{DictCap: 64 << 10}.NewWriter(&packed)
	if err != nil {
		t.Fatal(err)
	}

	if _, err := w.Write(want.Bytes()); err != nil {
		t.Fatal(err)
	}

	if err := w.Close(); err != nil {
		t.Fatal(err)
	}

	r, err := xz.NewReader(&packed, 0)
	if err != nil {
		t.Fatal(err)
	}

	got, err := io.ReadAll(r)
	if err != nil {
		t.Fatal(err)
	}

	if !bytes.Equal(got, want.Bytes()) {
		t.Fatalf("decoded %d bytes that differ from the %d written", len(got), want.Len())
	}
}
