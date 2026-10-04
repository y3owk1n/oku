package store

import (
	"bytes"
	"cmp"
	"encoding/binary"
	"errors"
	"fmt"
	"io"
	"slices"
)

// rpmPayloadFormat is the header tag that names the archive format of an RPM
// package's payload.
const rpmPayloadFormat = 1124

// rpmPayload reads the lead and the two headers of the RPM package in r, and
// leaves r at the payload. It returns the payload's archive format, which is
// cpio when the header names none.
func rpmPayload(r io.Reader) (string, error) {
	var lead [96]byte
	if _, err := io.ReadFull(r, lead[:]); err != nil {
		return "", fmt.Errorf("read the package: %w", err)
	}

	if !bytes.Equal(lead[:4], []byte{0xED, 0xAB, 0xEE, 0xDB}) {
		return "", errors.New("it is not an RPM package")
	}

	// The signature header comes first, padded to a multiple of 8 bytes.
	if _, err := rpmHeader(r, true); err != nil {
		return "", fmt.Errorf("read the signature header: %w", err)
	}

	tags, err := rpmHeader(r, false)
	if err != nil {
		return "", fmt.Errorf("read the header: %w", err)
	}

	return cmp.Or(tags[rpmPayloadFormat], "cpio"), nil
}

// rpmHeader reads one header from r and returns its string tags by number.
func rpmHeader(r io.Reader, padded bool) (map[uint32]string, error) {
	var intro [16]byte
	if _, err := io.ReadFull(r, intro[:]); err != nil {
		return nil, err
	}

	if !bytes.Equal(intro[:3], []byte{0x8E, 0xAD, 0xE8}) {
		return nil, errors.New("it does not start like an RPM header")
	}

	count, size := binary.BigEndian.Uint32(intro[8:12]), binary.BigEndian.Uint32(intro[12:16])
	if count > 1<<16 || size > 32<<20 {
		return nil, fmt.Errorf("it is too large, with %d entries in %d bytes", count, size)
	}

	index := make([]byte, int(count)*16)
	if _, err := io.ReadFull(r, index); err != nil {
		return nil, err
	}

	data := make([]byte, size)
	if _, err := io.ReadFull(r, data); err != nil {
		return nil, err
	}

	const stringType = 6

	tags := map[uint32]string{}

	for entry := range slices.Chunk(index, 16) {
		tag, kind, offset := binary.BigEndian.Uint32(entry), binary.BigEndian.Uint32(entry[4:]),
			binary.BigEndian.Uint32(entry[8:])
		if kind != stringType {
			continue
		}

		if offset >= size {
			return nil, fmt.Errorf("tag %d points past the header", tag)
		}

		end := bytes.IndexByte(data[offset:], 0)
		if end < 0 {
			return nil, fmt.Errorf("tag %d has no end", tag)
		}

		tags[tag] = string(data[offset : offset+uint32(end)])
	}

	if pad := (8 - size%8) % 8; padded && pad != 0 {
		if _, err := io.CopyN(io.Discard, r, int64(pad)); err != nil {
			return nil, err
		}
	}

	return tags, nil
}
