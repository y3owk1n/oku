package sandbox

import (
	"context"
	"errors"
	"fmt"
	"os/exec"
	"strings"
)

const sandboxExec = "/usr/bin/sandbox-exec"

func unavailable() string {
	if _, err := exec.LookPath(sandboxExec); err != nil {
		return sandboxExec + " is missing"
	}

	return ""
}

func command(ctx context.Context, spec Spec) (*exec.Cmd, string) {
	if why := unavailable(); why != "" {
		return nil, why
	}

	args := append([]string{"-p", profile(spec)}, spec.Argv...)

	return exec.CommandContext(ctx, sandboxExec, args...), ""
}

// profile writes the sandbox rules. The read rule is one deny that leaves out
// the readable paths, because newer macOS does not let a later allow override
// it. Reads of file names and sizes under Home stay allowed, because resolving
// a path to the store passes through Home. File contents and directory listings
// do not.
func profile(spec Spec) string {
	var b strings.Builder

	b.WriteString("(version 1)\n(allow default)\n")

	if !spec.Network {
		b.WriteString("(deny network*)\n")
	} else {
		// With the network the build still must not reach the user's agents, such as
		// ssh-agent under /private/var/run or Docker's socket in home. The rule
		// allows mDNSResponder, for DNS, and the sockets in the build's own
		// directories.
		b.WriteString(`(deny network-outbound (require-all (remote unix-socket)` +
			` (require-not (remote unix-socket (path-literal "/private/var/run/mDNSResponder")))`)

		for _, path := range spec.Writable {
			fmt.Fprintf(&b, " (require-not (remote unix-socket (subpath %q)))", path)
		}

		b.WriteString("))\n")
	}

	// A build signals its own processes only, not the user's.
	b.WriteString("(deny signal (require-not (target same-sandbox)))\n")

	// A build that could ask LaunchServices, another app or launchd to start a
	// program would run that program outside the sandbox. A program that talks to
	// launchd over XPC itself still can, so the sandbox limits a malicious build
	// and does not contain it.
	b.WriteString(`(deny appleevent-send)
(deny mach-lookup (global-name-prefix "com.apple.coreservices.") (global-name-prefix "com.apple.lsd."))
(deny process-exec (literal "/bin/launchctl"))
`)

	// cfprefsd reads the user's preferences under ~/Library/Preferences for the
	// build, and securityd answers for the keychain, so the rule on Home below
	// would not stop either.
	b.WriteString(`(deny mach-lookup (global-name "com.apple.cfprefsd.agent") (global-name "com.apple.cfprefsd.daemon")` +
		` (global-name "com.apple.SecurityServer") (global-name "com.apple.securityd"))
`)

	// The build runs in a session without a terminal, and this also keeps it from
	// reading what the user types into one.
	b.WriteString(`(deny file-read-data (literal "/dev/tty") (regex #"^/dev/ttys[0-9]+$"))
`)

	keep := append(append([]string{}, spec.Readable...), spec.Writable...)

	if spec.Home != "" {
		denyRead(&b, spec.Home, keep)
	}

	// The user's temporary and cache directories are under /private/var/folders.
	denyRead(&b, "/private/var/folders", keep)

	b.WriteString("(deny file-write*)\n")

	for _, path := range spec.Writable {
		fmt.Fprintf(&b, "(allow file-write* (subpath %q))\n", path)
	}

	// Shells and compilers write to these devices.
	b.WriteString(
		`(allow file-write* (literal "/dev/null") (literal "/dev/dtracehelper") (subpath "/dev/fd"))` + "\n",
	)

	return b.String()
}

// denyRead denies reading file contents and listings under dir, except under
// the paths of keep.
func denyRead(b *strings.Builder, dir string, keep []string) {
	fmt.Fprintf(b, "(deny file-read-data (require-all (subpath %q)", dir)

	for _, path := range keep {
		fmt.Fprintf(b, " (require-not (subpath %q))", path)
	}

	b.WriteString("))\n")
}

// Init only exists for the Linux sandbox.
func Init() error {
	return errors.New("sandbox-init is only used on Linux")
}
