// Package limit reads an answer from an outside source up to a size, and
// refuses one that holds more instead of cutting it short.
package limit

import (
	"errors"
	"fmt"
	"io"
)

// ErrTooLarge reports an answer over the size its reader takes.
var ErrTooLarge = errors.New("response is larger than the limit")

// Read reads all of r, and fails with ErrTooLarge when r holds more than n
// bytes.
func Read(r io.Reader, n int64) ([]byte, error) {
	data, err := io.ReadAll(io.LimitReader(r, n+1))
	if err != nil {
		return nil, err
	}

	if int64(len(data)) > n {
		return nil, fmt.Errorf("%w of %d bytes", ErrTooLarge, n)
	}

	return data, nil
}
