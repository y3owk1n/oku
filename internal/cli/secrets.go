package cli

import (
	"bytes"
	"context"
	"crypto/sha256"
	"encoding/hex"
	"errors"
	"fmt"
	"io/fs"
	"os"
	"os/exec"
	"path/filepath"
	"runtime"

	"github.com/y3owk1n/oku/internal/expose"
	"github.com/y3owk1n/oku/internal/list"
	"github.com/y3owk1n/oku/internal/profile"
	"github.com/y3owk1n/oku/internal/secret"
)

// secretPrefix starts the name of a secret in a text or a template, as in
// {{secret.github_token}}.
const secretPrefix = "secret."

// inline is the name of the secret of a "secret" entry, which has no name in
// [secrets]. No name of the user's can be empty.
const inline = ""

// listedSecret is one entry of [secrets] of the merged list.
type listedSecret struct {
	secret list.Secret
	// dir is the directory of the list that declares the entry, where a relative
	// file starts.
	dir string
}

// placeholder marks the place in a generation's content where the value of a
// secret goes. The zero bytes keep it apart from anything a config file holds.
func placeholder(name string) string {
	return "\x00oku-secret:" + name + "\x00"
}

// secretRef reads the encrypted file of s and describes it for a generation.
func secretRef(name string, s listedSecret) (profile.SecretRef, error) {
	path := s.secret.File
	if !filepath.IsAbs(path) {
		path = filepath.Join(s.dir, filepath.FromSlash(path))
	}

	data, err := os.ReadFile(path)
	if err != nil {
		return profile.SecretRef{}, fmt.Errorf("the encrypted file of a secret: %w", err)
	}

	sum := sha256.Sum256(data)

	return profile.SecretRef{
		Name: name, Source: s.secret.File, Key: s.secret.Key, Data: data,
		// The name keeps the extension, by which sops picks the format.
		Cipher: hex.EncodeToString(sum[:])[:16] + "-" + filepath.Base(path),
	}, nil
}

// sealedHash identifies a file that uses secrets by the content the generation
// holds and the encrypted files behind it, so a changed secret writes it again.
func sealedHash(text string, refs []profile.SecretRef) string {
	sum := sha256.New()
	sum.Write([]byte(text))

	for _, ref := range refs {
		fmt.Fprintf(sum, "\x00%s\x00%s\x00%s", ref.Name, ref.Key, ref.Cipher)
	}

	return hex.EncodeToString(sum.Sum(nil))
}

// decrypter returns what decrypts the secrets of generation n. sops from the
// packages of that generation comes before one on PATH, so the sync that
// installs sops can also use it.
func (e env) decrypter(n int) secret.Decrypter {
	d := secret.Decrypter{Identities: secret.IdentityFile(filepath.Dir(e.config))}

	name := "sops"
	if runtime.GOOS == "windows" {
		name += ".exe"
	}

	if own := filepath.Join(e.globalProfile().BinDirOf(n), name); n > 0 && fileExists(own) {
		d.Sops = own
	} else if found, err := exec.LookPath("sops"); err == nil {
		d.Sops = found
	}

	return d
}

func fileExists(path string) bool {
	_, err := os.Stat(path)

	return err == nil
}

// sealed is the final content of one file that uses secrets.
type sealed struct {
	data []byte
	mode fs.FileMode
}

// unseal decrypts the secrets of the files of generation n and returns the final
// content by target. It writes nothing.
func (e env) unseal(ctx context.Context, n int, files []profile.File) (map[string]sealed, error) {
	out := map[string]sealed{}
	d := e.decrypter(n)

	for _, f := range files {
		if len(f.Secrets) == 0 {
			continue
		}

		data := f.Text

		for _, ref := range f.Secrets {
			value, err := d.Decrypt(
				ctx,
				secret.Source{Name: ref.Source, Data: ref.Data, Key: ref.Key},
			)
			if err != nil {
				return nil, err
			}

			data = bytes.ReplaceAll(data, []byte(placeholder(ref.Name)), value)
		}

		out[f.Target] = sealed{data: data, mode: f.Mode}
	}

	return out, nil
}

// secretPath is where the decrypted content of f is on this machine.
func (e env) secretPath(f profile.File) string {
	return filepath.Join(e.data, "secrets", f.Content)
}

// secretHandler writes a decrypted file where only the user can read it, and
// makes the target point at it. Windows gets a copy with the same protection,
// because a normal user cannot create a symlink there.
func secretHandler(contents map[string]sealed) expose.Handler {
	return expose.Handler{
		Place: func(item expose.Item) error {
			content, ok := contents[item.Target]
			if !ok {
				return errors.New("the secret was not decrypted for this change")
			}

			if err := writeSecret(item.Source, content, true); err != nil {
				return err
			}

			if err := os.MkdirAll(filepath.Dir(item.Target), 0o755); err != nil {
				return err
			}

			if runtime.GOOS == "windows" {
				return writeSecret(item.Target, content, false)
			}

			return os.Symlink(item.Source, item.Target)
		},
		Remove: func(item expose.Item) error {
			if err := removeSecretTarget(item); err != nil {
				return err
			}

			if err := os.Remove(item.Source); err != nil && !errors.Is(err, fs.ErrNotExist) {
				return err
			}

			return nil
		},
	}
}

// writeSecret writes content to path, which only the user can read afterwards.
// With ownDir the directory is oku's and gets the same protection. The target of
// a copy on Windows is in a directory of the user's, which keeps its own.
func writeSecret(path string, content sealed, ownDir bool) error {
	dir := filepath.Dir(path)

	if ownDir {
		if err := os.MkdirAll(dir, 0o700); err != nil {
			return err
		}

		if err := secret.Restrict(dir, 0o700); err != nil {
			return err
		}
	}

	// An earlier copy may be read-only.
	if err := os.Remove(path); err != nil && !errors.Is(err, fs.ErrNotExist) {
		return err
	}

	if err := os.WriteFile(path, content.data, content.mode); err != nil {
		return err
	}

	return secret.Restrict(path, content.mode)
}

// removeSecretTarget deletes the link or the copy that oku made. A target that
// the user replaced stays.
func removeSecretTarget(item expose.Item) error {
	info, err := os.Lstat(item.Target)
	if err != nil {
		return nil
	}

	if runtime.GOOS != "windows" {
		if info.Mode()&fs.ModeSymlink == 0 {
			return nil
		}

		return os.Remove(item.Target)
	}

	ours, _ := os.ReadFile(item.Source)
	theirs, err := os.ReadFile(item.Target)

	if err != nil || !bytes.Equal(ours, theirs) {
		return nil
	}

	return os.Remove(item.Target)
}
