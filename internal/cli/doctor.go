package cli

import (
	"errors"
	"fmt"
	"os"
	"path/filepath"
	"slices"
	"strings"

	"github.com/spf13/cobra"

	"github.com/y3owk1n/oku/internal/profile"
	"github.com/y3owk1n/oku/internal/sandbox"
	"github.com/y3owk1n/oku/internal/secret"
	"github.com/y3owk1n/oku/internal/store"
	"github.com/y3owk1n/oku/internal/ui"
)

func newDoctorCmd(opts Options) *cobra.Command {
	return &cobra.Command{
		Use:   "doctor",
		Short: "Check this machine's oku setup and say what to fix",
		Long: `Check this machine's oku setup and say what to fix.

doctor reads local files only. It prints one line per check, and exits with
code 1 when a check found a problem.`,
		Args: cobra.NoArgs,
		RunE: func(cmd *cobra.Command, _ []string) error {
			return runDoctor(cmd, opts)
		},
	}
}

// check is one line of the report. Status is "ok", "note" or "problem".
type check struct {
	Status  string `json:"status"`
	Message string `json:"message"`
}

// report collects the checks and counts the problems.
type report struct {
	checks   []check
	problems int
}

func (r *report) add(status, format string, args ...any) {
	r.checks = append(r.checks, check{status, fmt.Sprintf(format, args...)})
}

func (r *report) ok(format string, args ...any)   { r.add("ok", format, args...) }
func (r *report) note(format string, args ...any) { r.add("note", format, args...) }

func (r *report) problem(format string, args ...any) {
	r.problems++

	r.add("problem", format, args...)
}

func runDoctor(cmd *cobra.Command, opts Options) error {
	e, err := loadEnv()
	if err != nil {
		return err
	}

	r := &report{}

	checkStore(r, e)
	checkSandbox(r)
	checkHook(r, opts)
	checkPath(r, e)
	checkPending(r, e)
	checkSecrets(r, e)

	if err := checkProfiles(r, e); err != nil {
		return err
	}

	if wantJSON(cmd) {
		if err := printJSON(cmd, struct {
			Problems int     `json:"problems"`
			Checks   []check `json:"checks"`
		}{r.problems, r.checks}); err != nil {
			return err
		}
	} else {
		out := cmd.OutOrStdout()
		s := ui.For(out)

		for _, c := range r.checks {
			if !s.On() {
				fmt.Fprintf(out, "%-8s %s\n", c.Status, c.Message)

				continue
			}

			glyph := s.Check()

			switch c.Status {
			case "note":
				glyph = s.Note()
			case "problem":
				glyph = s.Cross()
			}

			fmt.Fprintln(out, s.Wrap(glyph+" "+s.Homes(c.Message), 2))
		}
	}

	if r.problems > 0 {
		return fmt.Errorf("doctor found %s", count(r.problems, "problem"))
	}

	if !wantJSON(cmd) {
		fmt.Fprintln(cmd.OutOrStdout(), "no problems found")
	}

	return nil
}

// checkSecrets reports a machine that cannot decrypt the secrets of the active
// generation, which the next sync would fail on. It decrypts nothing.
func checkSecrets(r *report, e env) {
	prof := e.globalProfile()

	files, err := prof.FilesWithContent(prof.Current())
	if err != nil {
		r.problem("%v", err)

		return
	}

	d := e.decrypter(prof.Current())
	count, needsSops := 0, false

	for _, f := range files {
		for _, ref := range f.Secrets {
			count++

			needsSops = needsSops || !secret.IsAge(ref.Data)
		}
	}

	switch {
	case count == 0:
	case !fileExists(d.Identities):
		r.problem(
			"the list has secrets, and the age identities are not at %s\n"+
				"copy your key file there, or set SOPS_AGE_KEY_FILE", d.Identities,
		)
	case needsSops && d.Sops == "":
		r.problem(
			"the list has a secret in a sops file, and sops is neither in the list nor on PATH",
		)
	default:
		r.ok("the age identities for %d secrets are at %s", count, d.Identities)
	}
}

// checkPending reports a change that stopped halfway and that oku has not put
// back yet.
func checkPending(r *report, e env) {
	p, err := e.readPending()

	switch {
	case err != nil:
		r.problem("%v", err)
	case p != nil:
		r.problem(
			"a change from generation %d to %d did not finish, and %s records how to undo it\n"+
				"run `oku sync`. It puts generation %d back first, and says why when it cannot",
			p.From, p.To, filepath.Join(e.data, pendingFile), p.From,
		)
	}
}

func checkStore(r *report, e env) {
	storeDir := filepath.Join(e.root, "store")

	if e.root == e.data {
		r.ok("the store is %s, in your data directory", storeDir)
	} else {
		r.ok("the store is %s, the shared root from \"oku setup --system\"", storeDir)
	}

	// A store that does not exist yet is a fresh install, not a problem.
	if _, err := os.Stat(e.root); err != nil {
		return
	}

	probe, err := os.CreateTemp(e.root, ".doctor-*")
	if err != nil {
		r.problem("oku cannot write to %s: %v", e.root, err)

		return
	}

	probe.Close()
	os.Remove(probe.Name())
}

func checkSandbox(r *report) {
	if ok, why := sandbox.Available(); ok {
		r.ok("builds from source run in a sandbox")
	} else {
		r.note("builds from source run without a sandbox, because %s", why)
	}
}

func checkHook(r *report, opts Options) {
	if lines := hookLines(); len(lines) > 0 {
		r.ok("the shell hook is loaded from %s", strings.Join(lines, ", "))

		return
	}

	hint := setupHint(opts)
	if hint == "" {
		hint = "see \"oku hook --help\""
	}

	r.note("no shell hook line found in a startup file. The line puts oku's programs on PATH "+
		"and activates projects. To set it up, %s", hint)
}

// checkPath looks for the global bin on PATH, and for programs earlier on PATH
// that have the name of an oku program and so run in its place.
func checkPath(r *report, e env) {
	bin := e.globalProfile().BinDir()
	dirs := filepath.SplitList(os.Getenv("PATH"))

	at := slices.Index(dirs, bin)
	if at < 0 {
		r.problem(
			"%s is not on PATH, so installed programs do not run by name. The hook line puts it there",
			bin,
		)

		return
	}

	r.ok("%s is on PATH", bin)

	entries, err := os.ReadDir(bin)
	if err != nil {
		return
	}

	// One line names every program that a directory earlier on PATH also has,
	// because the fix is one change to PATH.
	type shadow struct{ name, dir string }

	var shadowed []shadow

	for _, entry := range entries {
		if strings.HasSuffix(entry.Name(), ".shim") {
			continue
		}

		for _, dir := range dirs[:at] {
			if info, err := os.Stat(filepath.Join(dir, entry.Name())); err == nil && !info.IsDir() {
				shadowed = append(shadowed, shadow{entry.Name(), dir})

				break
			}
		}
	}

	switch len(shadowed) {
	case 0:
	case 1:
		r.problem(
			"%s runs in place of oku's %s, because %s is earlier on PATH. "+
				"Put %s before it, or remove the other copy",
			filepath.Join(
				shadowed[0].dir,
				shadowed[0].name,
			),
			shadowed[0].name,
			shadowed[0].dir,
			bin,
		)
	default:
		names := make([]string, len(shadowed))
		for i, s := range shadowed {
			names[i] = s.name + " (" + s.dir + ")"
		}

		r.problem(
			"%d programs earlier on PATH run in place of oku's: %s. "+
				"Put %s before those directories, or remove the other copies",
			len(shadowed), strings.Join(names, ", "), bin,
		)
	}
}

// checkProfiles looks for packages whose store path is gone and for links in a
// profile's bin that point at a file that does not exist.
func checkProfiles(r *report, e env) error {
	profiles, err := profile.All(e.data)
	if err != nil {
		return err
	}

	broken := 0

	for _, prof := range profiles {
		pkgs, err := prof.Packages()
		if err != nil {
			return err
		}

		for _, pkg := range pkgs {
			if _, err := store.ReadMeta(pkg.StorePath); err != nil {
				broken++

				r.problem(
					"profile %s has %s, but %s is missing. Run \"oku sync\" to install it again",
					prof.Name(), pkg.Name, pkg.StorePath,
				)
			}
		}

		entries, err := os.ReadDir(prof.BinDir())
		if err != nil && !errors.Is(err, os.ErrNotExist) {
			return err
		}

		for _, entry := range entries {
			if _, err := os.Stat(filepath.Join(prof.BinDir(), entry.Name())); err != nil {
				broken++

				r.problem(
					"profile %s: %s points at a file that does not exist",
					prof.Name(),
					entry.Name(),
				)
			}
		}
	}

	if broken == 0 {
		noun := "profiles"
		if len(profiles) == 1 {
			noun = "profile"
		}

		r.ok("every link in %d %s points at a file in the store", len(profiles), noun)
	}

	return nil
}
