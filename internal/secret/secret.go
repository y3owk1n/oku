// Package secret decrypts the secrets of a list. It reads an age file itself and
// runs sops for a sops file. It never writes an encrypted file and never writes
// or creates a key.
package secret

import (
	"bytes"
	"context"
	"errors"
	"fmt"
	"io"
	"os"
	"os/exec"
	"path/filepath"
	"strings"

	"filippo.io/age"
	"filippo.io/age/armor"

	"github.com/y3owk1n/oku/internal/status"
)

// ageIntro starts a binary age file.
const ageIntro = "age-encryption.org/v1\n"

// IsAge reports whether data is an age file, binary or armored. Anything else
// that a list names as a secret is a sops file.
func IsAge(data []byte) bool {
	return bytes.HasPrefix(data, []byte(ageIntro)) ||
		bytes.HasPrefix(bytes.TrimLeft(data, " \t\r\n"), []byte(armor.Header))
}

// IdentityFile returns the file that holds the age identities: the one in
// SOPS_AGE_KEY_FILE, else sops/age/keys.txt under configHome. sops itself looks
// elsewhere on macOS and on Windows, so oku passes this path on to it.
func IdentityFile(configHome string) string {
	if path := os.Getenv("SOPS_AGE_KEY_FILE"); path != "" {
		return path
	}

	return filepath.Join(configHome, "sops", "age", "keys.txt")
}

// Source is one encrypted value.
type Source struct {
	// Name is how errors name the file, which is its path in the user's list.
	Name string
	// Data is the encrypted file.
	Data []byte
	// Key is the path of one value in a sops file, with "/" between its parts.
	Key string
}

// Decrypter holds what decryption needs on this machine.
type Decrypter struct {
	// Identities is the file from IdentityFile.
	Identities string
	// Sops is the sops program, or empty when there is none.
	Sops string
}

// Decrypt returns the value of s. No error holds decrypted bytes.
func (d Decrypter) Decrypt(ctx context.Context, s Source) ([]byte, error) {
	if IsAge(s.Data) {
		if s.Key != "" {
			return nil, fmt.Errorf(
				"%s is an age file, which holds one value, so key does not apply",
				s.Name,
			)
		}

		return d.age(s)
	}

	return d.sops(ctx, s)
}

func (d Decrypter) age(s Source) ([]byte, error) {
	keys, err := os.Open(d.Identities)
	if err != nil {
		return nil, fmt.Errorf("decrypt %s: the age identities are missing: %w", s.Name, err)
	}
	defer keys.Close()

	identities, err := age.ParseIdentities(keys)
	if err != nil {
		return nil, fmt.Errorf("decrypt %s: read %s: %w", s.Name, d.Identities, err)
	}

	var in io.Reader = bytes.NewReader(s.Data)
	if !bytes.HasPrefix(s.Data, []byte(ageIntro)) {
		in = armor.NewReader(bytes.NewReader(bytes.TrimLeft(s.Data, " \t\r\n")))
	}

	out, err := age.Decrypt(in, identities...)
	if err != nil {
		return nil, fmt.Errorf("decrypt %s with %s: %w", s.Name, d.Identities, err)
	}

	value, err := io.ReadAll(out)
	if err != nil {
		return nil, fmt.Errorf("decrypt %s: %w", s.Name, err)
	}

	return value, nil
}

// sops runs "sops decrypt" on a copy of the encrypted file. The copy keeps the
// name of the original, because sops picks the format by the extension.
func (d Decrypter) sops(ctx context.Context, s Source) ([]byte, error) {
	if d.Sops == "" {
		return nil, fmt.Errorf(
			"decrypt %s: sops is not installed, add it to the list or put it on PATH", s.Name,
		)
	}

	dir, err := os.MkdirTemp("", "oku-sops-*")
	if err != nil {
		return nil, err
	}
	defer os.RemoveAll(dir)

	file := filepath.Join(dir, filepath.Base(s.Name))
	if err := os.WriteFile(file, s.Data, 0o600); err != nil {
		return nil, err
	}

	args := []string{"decrypt"}

	if s.Key != "" {
		var path strings.Builder
		for _, part := range strings.Split(s.Key, "/") {
			path.WriteString(`["` + part + `"]`)
		}

		args = append(args, "--extract", path.String())
	}

	cmd := exec.CommandContext(ctx, d.Sops, append(args, file)...)
	cmd.Env = append(os.Environ(), "SOPS_AGE_KEY_FILE="+d.Identities)

	var stderr bytes.Buffer

	cmd.Stderr = &stderr

	done := status.Start(ctx, "decrypting %s", describe(s))
	value, err := cmd.Output()

	done()

	if err != nil {
		// sops explains a failure on stderr, and prints a value only on stdout.
		reason := strings.ReplaceAll(strings.TrimSpace(stderr.String()), file, s.Name)

		return nil, fmt.Errorf("decrypt %s: %w", describe(s), errors.Join(err, errors.New(reason)))
	}

	return value, nil
}

func describe(s Source) string {
	if s.Key == "" {
		return s.Name
	}

	return s.Name + ", key " + s.Key
}
