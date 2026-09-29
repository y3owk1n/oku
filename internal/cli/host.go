package cli

import (
	"fmt"
	"io"
	"maps"
	"slices"
	"strings"

	"github.com/y3owk1n/oku/internal/host"
	"github.com/y3owk1n/oku/internal/platform"
)

// hostSystem returns the package manager of this machine and how to query it,
// or the stand-in of opts.
func hostSystem(opts Options) host.System {
	if opts.Host != nil {
		return *opts.Host
	}

	return host.Detect()
}

// hostHere keeps the requirements whose when matches this machine.
func hostHere(reqs []host.Requirement) []host.Requirement {
	return slices.DeleteFunc(slices.Clone(reqs), func(r host.Requirement) bool {
		return !r.When.Matches(platform.Host())
	})
}

// missingLine names a requirement the machine lacks, the packages that need
// it, and how to get it.
func missingLine(m host.Missing, by []string) string {
	if m.Unchecked {
		return fmt.Sprintf(
			"%s names a package only for %s, so oku cannot check it on this machine",
			m.Name, strings.Join(slices.Sorted(maps.Keys(m.Packages)), ", "),
		)
	}

	line := m.Name + " is missing"

	// With many packages the line names the first ones and counts the rest.
	const named = 3

	switch {
	case len(by) == 0:
	case len(by) == 1:
		line = fmt.Sprintf("%s, which %s needs, is missing", m.Name, by[0])
	case len(by) <= named:
		line = fmt.Sprintf("%s, which %s and %s need, is missing",
			m.Name, strings.Join(by[:len(by)-1], ", "), by[len(by)-1])
	default:
		line = fmt.Sprintf("%s, which %s and %s need, is missing",
			m.Name, strings.Join(by[:named], ", "), count(len(by)-named, "other package"))
	}

	switch {
	case m.Fix == "":
		return line
	case m.Command:
		return line + "\nget it with `" + m.Fix + "`"
	default:
		return line + "\n" + m.Fix
	}
}

// hostOf returns the [host] requirements of the package name and its deps.
// Each one is named after the package and listed once.
func hostOf(name string, got installed) []host.Requirement {
	var reqs []host.Requirement

	for _, r := range got.host {
		r.Package = name
		if !slices.ContainsFunc(reqs, func(have host.Requirement) bool { return sameChecks(have, r) }) {
			reqs = append(reqs, r)
		}
	}

	return reqs
}

// jobsHost returns the [host] requirements of the packages that jobs installed.
func jobsHost(jobs []*job) []host.Requirement {
	var reqs []host.Requirement

	for _, j := range jobs {
		if j.err == nil && !j.got.lockOnly {
			reqs = append(reqs, hostOf(j.name, j.got)...)
		}
	}

	return reqs
}

// missingHost checks reqs once for each set of checks that several packages
// share. It returns a line for each requirement the machine lacks, and one for
// each that oku cannot check here.
func missingHost(opts Options, reqs []host.Requirement) (lacking, unchecked []string, err error) {
	type group struct {
		req host.Requirement
		by  []string
	}

	var groups []group

	for _, r := range reqs {
		i := slices.IndexFunc(groups, func(g group) bool { return sameChecks(g.req, r) })
		if i < 0 {
			groups = append(groups, group{req: r})
			i = len(groups) - 1
		}

		if r.Package != "" && !slices.Contains(groups[i].by, r.Package) {
			groups[i].by = append(groups[i].by, r.Package)
		}
	}

	system := hostSystem(opts)

	for _, g := range groups {
		missing, err := system.Check([]host.Requirement{g.req})
		if err != nil {
			return nil, nil, err
		}

		slices.Sort(g.by)

		for _, m := range missing {
			if m.Unchecked {
				unchecked = append(unchecked, missingLine(m, g.by))
			} else {
				lacking = append(lacking, missingLine(m, g.by))
			}
		}
	}

	return lacking, unchecked, nil
}

// sameChecks reports whether a and b check the same things, whoever names them.
func sameChecks(a, b host.Requirement) bool {
	return a.Name == b.Name && a.Command == b.Command && a.Path == b.Path &&
		a.Install == b.Install && maps.Equal(a.Packages, b.Packages)
}

// tellMissing warns about each requirement of reqs that the machine lacks.
// oku never installs one, so a sync goes on without it.
func tellMissing(w io.Writer, opts Options, reqs []host.Requirement) error {
	lacking, unchecked, err := missingHost(opts, reqs)
	if err != nil {
		return err
	}

	for _, line := range append(lacking, unchecked...) {
		warn(w, "%s", line)
	}

	return nil
}
