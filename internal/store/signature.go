package store

import (
	"context"
	"errors"
	"fmt"
	"io"
	"os"

	"aead.dev/minisign"
)

// ErrSignature reports an artifact that its manifest's signing key did not sign.
var ErrSignature = errors.New("signature check failed")

// verifySignature checks the file at download against the minisign signature at
// url + ".minisig".
func (s *Store) verifySignature(ctx context.Context, keyText, url, download string) error {
	var key minisign.PublicKey
	if err := key.UnmarshalText([]byte(keyText)); err != nil {
		return fmt.Errorf("signing_key %s: %w", keyText, err)
	}

	resp, err := s.get(ctx, url+signatureSuffix)
	if err != nil {
		return fmt.Errorf("%w: the manifest has a signing_key, and %w", ErrSignature, err)
	}

	signature, err := io.ReadAll(io.LimitReader(resp.Body, 1<<16))
	resp.Body.Close()

	if err != nil {
		return fmt.Errorf("download %s: %w", url+signatureSuffix, err)
	}

	signed, err := verifyFile(key, download, signature)
	if err != nil {
		return err
	}

	if !signed {
		return fmt.Errorf("%w: %s is not signed by %s", ErrSignature, url, keyText)
	}

	return nil
}

// verifyFile accepts both kinds of minisign signature. The current one signs a
// hash of the file, so oku streams the file. The legacy one, from "minisign -l"
// and old versions, signs the file itself, so oku has to read it into memory.
func verifyFile(key minisign.PublicKey, path string, signature []byte) (bool, error) {
	var parsed minisign.Signature
	if err := parsed.UnmarshalText(signature); err != nil {
		return false, nil //nolint:nilerr
	}

	if parsed.Algorithm == minisign.EdDSA {
		data, err := os.ReadFile(path)
		if err != nil {
			return false, err
		}

		return minisign.Verify(key, data, signature), nil
	}

	f, err := os.Open(path)
	if err != nil {
		return false, err
	}
	defer f.Close()

	verifier := minisign.NewReader(f)
	if _, err := io.Copy(io.Discard, verifier); err != nil {
		return false, err
	}

	return verifier.Verify(key, signature), nil
}

// Download saves url in the download cache and returns the file's path. It
// always downloads, because the assets of a moving tag such as nightly keep
// their url when their bytes change.
func (s *Store) Download(ctx context.Context, url string) (string, error) {
	path, _, err := s.download(ctx, url, "", false)

	return path, err
}

// VerifyDetached checks the file at path against the minisign signature in the
// file at signaturePath, made by the public key keyText.
func VerifyDetached(keyText, path, signaturePath string) error {
	var key minisign.PublicKey
	if err := key.UnmarshalText([]byte(keyText)); err != nil {
		return fmt.Errorf("public key %s: %w", keyText, err)
	}

	signature, err := os.ReadFile(signaturePath)
	if err != nil {
		return err
	}

	signed, err := verifyFile(key, path, signature)
	if err != nil {
		return err
	}

	if !signed {
		return fmt.Errorf("%w: %s is not signed by %s", ErrSignature, path, keyText)
	}

	return nil
}
