package cli

import (
	"crypto/sha256"
	"encoding/hex"
	"errors"
	"fmt"
	"path"
	"slices"
	"strconv"
	"strings"

	"github.com/spf13/cobra"

	"github.com/y3owk1n/oku/internal/list"
	"github.com/y3owk1n/oku/internal/lock"
	"github.com/y3owk1n/oku/internal/manifest"
	"github.com/y3owk1n/oku/internal/ref"
)

// adopt sets this machine up from the list at arg. It writes a global oku.toml
// that includes the list, and an oku.lock that starts from the lock beside it,
// so the following sync installs what that lock pinned.
func adopt(cmd *cobra.Command, opts Options, e env, arg string) error {
	own, err := list.Read(e.listPath())
	if err != nil {
		return err
	}

	r, err := ref.Parse(arg)
	if err != nil {
		return err
	}

	if r.Version != "" {
		return fmt.Errorf("%s: a list ref takes no @version", arg)
	}

	if len(own.Include) > 0 || len(own.Packages) > 0 {
		return fmt.Errorf(
			"%s already has packages or includes, so oku will not replace it\n"+
				"add %q to its include array and run `oku sync`",
			e.listPath(), r.String(),
		)
	}

	fetcher := e.fetcher(opts)

	fetched, err := fetcher.Fetch(cmd.Context(), r, "", ref.List)
	if err != nil {
		return err
	}

	if _, err := list.Parse(fetched.Data, r.String()); err != nil {
		return err
	}

	out := cmd.OutOrStdout()
	adopted := &lock.Lock{}
	lockName := strings.TrimSuffix(
		path.Base(strings.ReplaceAll(fetched.Path, `\`, "/")),
		".toml",
	) + ".lock"

	beside, err := fetcher.FetchBeside(cmd.Context(), r, fetched, lockName)

	switch {
	case errors.Is(err, ref.ErrNotFound):
		fmt.Fprintf(out, "%s has no %s beside it, so oku resolves versions fresh\n", r, lockName)
	case err != nil:
		return err
	default:
		if adopted, err = lock.Parse(beside.Data, lockName); err != nil {
			return err
		}

		if r.Kind != ref.File {
			if err := besideList(adopted, r, fetched); err != nil {
				return fmt.Errorf("%s: %w", lockName, err)
			}
		}

		fmt.Fprintf(out, "adopted %s with %d locked packages\n", r, len(adopted.Packages))
	}

	sum := sha256.Sum256(fetched.Data)
	adopted.Includes = append([]lock.Include{{
		Ref:    r.String(),
		Commit: fetched.Commit,
		SHA256: hex.EncodeToString(sum[:]),
	}}, adopted.Includes...)

	content := fmt.Sprintf("# Set up with `oku sync %s`.\ninclude = [%q]\n", r, r.String())
	if err := list.WriteFile(e.listPath(), []byte(content)); err != nil {
		return err
	}

	return adopted.Write(e.lockPath())
}

// besideList rewrites the relative refs of a published lock into refs of the
// list's repo at the list's commit, or into URLs beside the list. The machine
// that wrote the lock read them beside the list, and this one must read the
// same files. An inferred manifest names its node that way too, so its text
// changes with its hash.
func besideList(l *lock.Lock, r ref.Ref, got ref.Fetched) error {
	moved := func(old string) (string, string, error) {
		if !ref.IsRelative(old) {
			return old, "", nil
		}

		at, err := ref.Beside(r, got, old)
		if err != nil {
			return "", "", err
		}

		return at.String(), got.Commit, nil
	}

	var fix func(p *lock.Package) error

	fix = func(p *lock.Package) error {
		to, commit, err := moved(p.Ref)
		if err != nil {
			return err
		}

		if to != p.Ref {
			p.Ref, p.Commit = to, commit
		}

		if p.Inferred && p.Manifest != "" {
			m, err := manifest.Parse([]byte(p.Manifest), p.Name)
			if err != nil {
				return err
			}

			deps := slices.Clone(m.Runtime.Deps)
			if m.Build != nil {
				deps = append(deps, m.Build.Deps...)
			}

			for _, dep := range deps {
				to, _, err := moved(dep.Ref)
				if err != nil {
					return err
				}

				p.Manifest = strings.ReplaceAll(p.Manifest, strconv.Quote(dep.Ref), strconv.Quote(to))
			}

			sum := sha256.Sum256([]byte(p.Manifest))
			p.ManifestSHA256 = hex.EncodeToString(sum[:])
		}

		for i := range p.Deps {
			if err := fix(&p.Deps[i]); err != nil {
				return err
			}
		}

		return nil
	}

	for i := range l.Packages {
		if err := fix(&l.Packages[i]); err != nil {
			return err
		}
	}

	for i, include := range l.Includes {
		to, commit, err := moved(include.Ref)
		if err != nil {
			return err
		}

		if to != include.Ref {
			l.Includes[i] = lock.Include{Ref: to, Commit: commit}
		}
	}

	return nil
}
