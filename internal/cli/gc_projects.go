package cli

import (
	"fmt"
	"io"
	"os"
	"path/filepath"
	"slices"
	"strings"

	"github.com/y3owk1n/oku/internal/list"
	"github.com/y3owk1n/oku/internal/profile"
	"github.com/y3owk1n/oku/internal/trust"
	"github.com/y3owk1n/oku/internal/ui"
)

// projectState is what gc found out about the project of one profile.
type projectState struct {
	prof *profile.Profile
	// dir is the project's directory, or empty when oku does not know it.
	dir string
	// gone says why the project is gone, or is empty while it is there.
	gone string
	// away reports a project on a volume that is not mounted.
	away bool
}

// projectStates looks up the project of each project profile. A profile from
// before profiles recorded their project finds it among the projects that the
// shell hook may apply.
func (e env) projectStates(profiles []*profile.Profile) ([]projectState, error) {
	allowed, err := trust.ReadAllowed(e.data)
	if err != nil {
		return nil, err
	}

	known := map[string]string{}
	for _, item := range allowed.Items {
		known[projectProfile(item.Dir)] = item.Dir
	}

	var states []projectState

	for _, prof := range profiles {
		if !strings.HasPrefix(prof.Name(), "project-") {
			continue
		}

		s := projectState{prof: prof, dir: prof.Project()}
		if s.dir == "" {
			s.dir = known[prof.Name()]
		}

		switch {
		case s.dir == "":
		case unmounted(s.dir):
			s.away = true
		case !exists(s.dir):
			s.gone = "its folder is gone"
		case !exists(filepath.Join(s.dir, list.FileName)):
			s.gone = "its " + list.FileName + " is gone"
		}

		states = append(states, s)
	}

	return states, nil
}

// removeGoneProjects deletes the profile of each project that is gone, and its
// entry among the projects the shell hook may apply, and says so on out. It
// keeps a project on a volume that is not mounted, and one whose folder it does
// not know, and says that too. It returns the projects that are gone.
func (e env) removeGoneProjects(
	out io.Writer,
	profiles []*profile.Profile,
	verb string,
	mark func(string) string,
	dryRun bool,
) ([]projectState, error) {
	states, err := e.projectStates(profiles)
	if err != nil {
		return nil, err
	}

	s := ui.For(out)
	unknown := 0

	var gone []projectState

	for _, state := range states {
		switch {
		case state.gone != "":
			gens, err := state.prof.Generations()
			if err != nil {
				return nil, err
			}

			if !dryRun {
				if err := state.prof.Delete(); err != nil {
					return nil, err
				}

				allowed, err := trust.ReadAllowed(e.data)
				if err != nil {
					return nil, err
				}

				if _, ok := allowed.Get(state.dir); ok {
					if err := allowed.Set(state.dir, nil); err != nil {
						return nil, err
					}
				}
			}

			gone = append(gone, state)
			fmt.Fprintln(out, mark(fmt.Sprintf(
				"%s project %s, %s (%s)", verb, s.Home(state.dir), state.gone, count(len(gens), "generation"),
			)))
		case state.away:
			fmt.Fprintf(out, "kept project %s, its volume is not mounted\n", s.Home(state.dir))
		case state.dir == "":
			unknown++
		}
	}

	if unknown > 0 {
		fmt.Fprintf(out, "kept %s whose folder oku does not know\n", count(unknown, "project profile"))
	}

	return gone, nil
}

// unmounted reports whether dir is on a volume that is not mounted, such as an
// external drive, so that its project is away and not gone. The nearest part of
// dir that exists is then a place where volumes mount, or a Windows drive is
// missing.
func unmounted(dir string) bool {
	if volume := filepath.VolumeName(dir); volume != "" {
		return !exists(volume + string(filepath.Separator))
	}

	at := filepath.Clean(dir)
	for !exists(at) && filepath.Dir(at) != at {
		at = filepath.Dir(at)
	}

	mounts := []string{"/Volumes", "/media", "/run/media", "/mnt"}

	// Linux mounts a user's drives under /media/<user> and /run/media/<user>.
	parent := filepath.Dir(at)

	return slices.Contains(mounts, at) ||
		(parent == "/media" || parent == "/run/media") && dir != at
}

func exists(path string) bool {
	_, err := os.Stat(path)

	return err == nil
}
