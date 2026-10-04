package store

import (
	"context"
	"errors"
	"fmt"
	"io"
	neturl "net/url"
	"os"
	"path"
	"regexp"
	"strconv"
	"strings"
	"time"

	"aead.dev/minisign"
)

// ErrSignature reports an artifact that its manifest's signing key did not sign.
var ErrSignature = errors.New("signature check failed")

// verifySignature checks the file at download against the minisign signature at
// url + ".minisig", and returns the unix time in its signed comment, or zero.
// The key signs every release, so the signed comment must also name this file,
// as minisign's "file:<name>" does, or the version, or an older signed file
// could pass for this one. A file name without the version leaves that open,
// so such a signature must not be older than s.signedAfter.
func (s *Store) verifySignature(ctx context.Context, keyText, url, version, download string) (int64, error) {
	var key minisign.PublicKey
	if err := key.UnmarshalText([]byte(keyText)); err != nil {
		return 0, fmt.Errorf("signing_key %s: %w", keyText, err)
	}

	resp, err := s.get(ctx, url+signatureSuffix)
	if err != nil {
		return 0, fmt.Errorf("%w: the manifest has a signing_key, and %w", ErrSignature, err)
	}

	signature, err := io.ReadAll(io.LimitReader(resp.Body, 1<<16))
	resp.Body.Close()

	if err != nil {
		return 0, fmt.Errorf("download %s: %w", url+signatureSuffix, err)
	}

	signed, err := verifyFile(key, download, signature)
	if err != nil {
		return 0, err
	}

	if !signed {
		return 0, fmt.Errorf("%w: %s is not signed by %s", ErrSignature, url, keyText)
	}

	var parsed minisign.Signature
	if err := parsed.UnmarshalText(signature); err != nil {
		return 0, err
	}

	name := path.Base(url)
	if u, err := neturl.Parse(url); err == nil {
		name = path.Base(u.Path)
	}

	if !strings.Contains(parsed.TrustedComment, "file:"+name) && !namesVersion(parsed.TrustedComment, version) {
		return 0, fmt.Errorf(
			"%w: the signed comment %q names neither file:%s nor the version %s",
			ErrSignature, parsed.TrustedComment, name, version,
		)
	}

	signedAt := signatureTime(parsed.TrustedComment)

	if !namesVersion(parsed.TrustedComment, version) && !namesVersion(name, version) &&
		s.signedAfter > 0 && signedAt > 0 && signedAt < s.signedAfter {
		return 0, fmt.Errorf(
			"%w: %s was signed on %s, before the file that oku.lock holds, which was signed on %s, "+
				"and its signed comment does not name the version %s, so it could be the file of an older release\n"+
				"if the developer signed this release before the locked one, as for a fix to an older line, "+
				"run the command again with --accept-weaker-check",
			ErrSignature, name, signedDay(signedAt), signedDay(s.signedAfter), version,
		)
	}

	return signedAt, nil
}

var signatureTimeRe = regexp.MustCompile(`(?:^|\s)timestamp:([0-9]+)`)

// signatureTime returns the unix time that minisign writes into the signed
// comment as "timestamp:<seconds>", or zero.
func signatureTime(comment string) int64 {
	m := signatureTimeRe.FindStringSubmatch(comment)
	if m == nil {
		return 0
	}

	at, _ := strconv.ParseInt(m[1], 10, 64)

	return at
}

func signedDay(at int64) string {
	return time.Unix(at, 0).UTC().Format(time.DateOnly)
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
