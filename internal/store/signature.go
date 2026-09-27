package store

import (
	"context"
	"errors"
	"fmt"
	"io"
	neturl "net/url"
	"os"
	"path"
	"strings"

	"aead.dev/minisign"
)

// ErrSignature reports an artifact that its manifest's signing key did not sign.
var ErrSignature = errors.New("signature check failed")

// verifySignature checks the file at download against the minisign signature at
// url + ".minisig". The key signs every release, so the signed trusted comment
// must also name this file, as minisign's "file:<name>" does, or the version, or
// an older signed file could pass for this one.
func (s *Store) verifySignature(ctx context.Context, keyText, url, version, download string) error {
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

	var parsed minisign.Signature
	if err := parsed.UnmarshalText(signature); err != nil {
		return err
	}

	name := path.Base(url)
	if u, err := neturl.Parse(url); err == nil {
		name = path.Base(u.Path)
	}

	if !strings.Contains(parsed.TrustedComment, "file:"+name) && !namesVersion(parsed.TrustedComment, version) {
		return fmt.Errorf(
			"%w: the signed comment %q names neither file:%s nor the version %s",
			ErrSignature, parsed.TrustedComment, name, version,
		)
	}

	return nil
}

// namesVersion reports whether comment holds version as a word of its own, so
// that 1.2 does not match inside 1.2.3.
func namesVersion(comment, version string) bool {
	if version == "" {
		return false
	}

	part := func(b byte) bool { return b == '.' || b >= '0' && b <= '9' }

	for rest, at := comment, 0; ; {
		i := strings.Index(rest, version)
		if i < 0 {
			return false
		}

		start, end := at+i, at+i+len(version)
		if (start == 0 || !part(comment[start-1])) && (end == len(comment) || !part(comment[end])) {
			return true
		}

		rest, at = rest[i+1:], at+i+1
	}
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
// file at signaturePath, made by the public key keyText. The signed comment of
// the signature must be trusted, so a file that the key signed for another
// release fails.
func VerifyDetached(keyText, path, signaturePath, trusted string) error {
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

	var parsed minisign.Signature
	if err := parsed.UnmarshalText(signature); err != nil {
		return err
	}

	if parsed.TrustedComment != trusted {
		return fmt.Errorf("the signature is for %q, not %q", parsed.TrustedComment, trusted)
	}

	return nil
}
