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

// missingLine names a requirement the machine lacks, and how to get it.
func missingLine(m host.Missing) string {
	switch {
	case m.Unchecked:
		return fmt.Sprintf(
			"%s names a package only for %s, so oku cannot check it on this machine",
			m.Name, strings.Join(slices.Sorted(maps.Keys(m.Packages)), ", "),
		)
	case m.Fix == "":
		return m.Name + " is missing"
	case m.Command:
		return fmt.Sprintf("%s is missing\nget it with `%s`", m.Name, m.Fix)
	default:
		return m.Name + " is missing\n" + m.Fix
	}
}

// tellMissing warns about each requirement of reqs that the machine lacks.
// oku never installs one, so a sync goes on without it.
func tellMissing(w io.Writer, opts Options, reqs []host.Requirement) error {
	missing, err := hostSystem(opts).Check(reqs)
	if err != nil {
		return err
	}

	for _, m := range missing {
		warn(w, "%s", missingLine(m))
	}

	return nil
}
