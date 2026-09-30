package ref

import (
	"net/url"
	"strings"

	"github.com/y3owk1n/oku/internal/forge"
)

// Origin is who wrote what r points at, for source trust: the owner or top
// group on a forge, the host of a URL or a git repo, or the package of a
// registry, with an npm package's scope in place of its name. It is "" for a
// ref on this machine, which needs no trust.
func (r Ref) Origin() string {
	switch r.Kind {
	case Forge:
		host, repo := forge.Split(r.Location)
		owner, _, _ := strings.Cut(repo, "/")

		if host != "" {
			return r.Scheme + ":" + host + "/" + owner
		}

		return r.Scheme + ":" + owner
	case Git, HTTP:
		u, err := url.Parse(r.Location)
		if err != nil || u.Scheme == "file" || local(u.Hostname()) {
			return ""
		}

		return u.Hostname()
	case File:
		return ""
	case NPM:
		if scope, _, ok := strings.Cut(r.Location, "/"); ok {
			return "npm:" + scope
		}
	}

	return r.String()
}

// Repo is the one forge repo r points at, a narrower origin a user may trust
// in place of its owner, or "" for any other kind.
func (r Ref) Repo() string {
	if r.Kind != Forge {
		return ""
	}

	return r.Scheme + ":" + r.Location
}

// local reports whether host names this machine, whose servers are the user's
// own.
func local(host string) bool {
	return host == "localhost" || host == "127.0.0.1" || host == "::1"
}
