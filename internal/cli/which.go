package cli

import (
	"errors"
	"fmt"
	"os/exec"
	"path/filepath"
	"runtime"
	"strings"

	"github.com/spf13/cobra"

	"github.com/y3owk1n/oku/internal/profile"
	"github.com/y3owk1n/oku/internal/ui"
)

func newWhichCmd(opts Options) *cobra.Command {
	return &cobra.Command{
		Use:   "which <program>",
		Short: "Say which package provides a program, and whether PATH runs it",
		Args:  exactArgs(1),
		RunE: func(cmd *cobra.Command, args []string) error {
			e, err := scopedEnv(cmd, opts)
			if err != nil {
				return err
			}

			prof := e.profile()

			pkgs, err := prof.Packages()
			if err != nil {
				return err
			}

			answer, err := which(prof, pkgs, args[0])
			if err != nil {
				return err
			}

			if wantJSON(cmd) {
				return printJSON(cmd, answer)
			}

			out := cmd.OutOrStdout()
			s := ui.For(out)

			if err := s.KV(out,
				[2]string{"program", s.Bold(answer.Program)},
				[2]string{"package", answer.Package + " " + answer.Version},
				[2]string{"path", s.Home(answer.Path)},
			); err != nil {
				return err
			}

			if answer.ShadowedBy != "" {
				warn(
					cmd.ErrOrStderr(),
					"PATH runs %s instead, because its directory comes first. `oku doctor` says how to fix that",
					answer.ShadowedBy,
				)
			}

			return nil
		},
	}
}

// whichAnswer is the JSON form of "oku which".
type whichAnswer struct {
	Program string `json:"program"`
	Package string `json:"package"`
	Version string `json:"version"`
	// Path is the file in the store that the profile's link points at.
	Path string `json:"path"`
	// ShadowedBy is the program PATH runs in place of oku's, or empty.
	ShadowedBy string `json:"shadowed_by,omitempty"`
}

func which(prof *profile.Profile, pkgs []profile.Package, program string) (whichAnswer, error) {
	link := filepath.Join(prof.BinDir(), program)
	if runtime.GOOS == "windows" && filepath.Ext(program) == "" {
		link += ".exe"
	}

	target, err := filepath.EvalSymlinks(link)
	if err != nil {
		if found, err := exec.LookPath(program); err == nil {
			return whichAnswer{}, fmt.Errorf(
				"%s is not from oku, PATH runs %s\n`oku search %s` looks for a package that provides it",
				program, found, program,
			)
		}

		return whichAnswer{}, fmt.Errorf(
			"no program named %s is installed\n`oku search %s` looks for a package that provides it",
			program, program,
		)
	}

	answer := whichAnswer{Program: program, Path: target}

	for _, pkg := range pkgs {
		// The store may sit behind a symlinked directory, as /var does on macOS.
		root, err := filepath.EvalSymlinks(pkg.StorePath)
		if err != nil {
			root = pkg.StorePath
		}

		if strings.HasPrefix(target, root+string(filepath.Separator)) {
			answer.Package, answer.Version = pkg.Name, pkg.Version

			break
		}
	}

	if answer.Package == "" {
		return whichAnswer{}, errors.New(program + " is in the profile, but no installed package holds it")
	}

	// A shim on Windows is a copy, so the comparison is by path.
	if found, err := exec.LookPath(program); err == nil {
		resolved, _ := filepath.EvalSymlinks(found)
		if resolved != target && found != link {
			answer.ShadowedBy = found
		}
	}

	return answer, nil
}
