// Package netpolicy decides which hosts and addresses oku connects to, from
// the [network] table of config.toml.
package netpolicy

import (
	"context"
	"errors"
	"fmt"
	"net"
	"net/http"
	"net/netip"
	"net/url"
	"strings"
	"syscall"
	"time"

	"github.com/y3owk1n/oku/internal/status"
)

// Policy is where oku may connect. The zero Policy refuses private addresses
// and allows every public host.
type Policy struct {
	// AllowPrivate lets oku reach loopback, LAN, link-local and other private
	// addresses through any host name.
	AllowPrivate bool
	// Allow lists the hosts oku may reach, as "example.com", "*.example.com" for
	// its subdomains, or an IP address. Empty allows every host.
	Allow []string
	// Private lists the hosts, in the form of Allow, that may resolve to a
	// private address.
	Private []string
}

// ValidPattern reports why an entry of Allow or Private is not a host, a "*."
// pattern or an IP address, or nil.
func ValidPattern(pattern string) error {
	host := strings.TrimPrefix(pattern, "*.")
	if _, err := netip.ParseAddr(host); err == nil && host == pattern {
		return nil
	}

	if host == "" || strings.ContainsAny(host, "/:*@ ") || strings.Contains(host, "..") {
		return fmt.Errorf("%q is not a host, a *. pattern or an IP address", pattern)
	}

	return nil
}

// Refused reports a connection the policy does not allow.
type Refused struct {
	Host string
	// Addr is the address the host resolved to, when that address is the reason.
	Addr netip.Addr
	// Chain is the redirects that led to Host, first URL first.
	Chain []string
}

func (r *Refused) Error() string {
	var why string

	switch {
	case Local(r.Host):
		why = fmt.Sprintf(
			"%s is this machine, and oku follows no redirect there from another host. "+
				"deny_private = false under [network] in config.toml allows it",
			r.Host,
		)
	case r.Addr.IsValid():
		why = fmt.Sprintf(
			"%s resolves to %s, a private address, and oku does not connect there. "+
				"Add %q to [network] private in config.toml to reach it",
			r.Host, r.Addr, r.Host,
		)
	default:
		why = fmt.Sprintf(
			"%s is not in [network] allow in config.toml, so oku does not connect there",
			r.Host,
		)
	}

	if len(r.Chain) > 0 {
		why += ". Redirected from " + strings.Join(r.Chain, " -> ")
	}

	return why
}

// allowed reports whether Allow lists host. An empty Allow lists every host.
func (p Policy) allowed(host string) bool {
	return len(p.Allow) == 0 || matches(p.Allow, host)
}

// privateOK reports whether host may resolve to a private address.
func (p Policy) privateOK(host string) bool {
	return p.AllowPrivate || matches(p.Private, host)
}

// matches reports whether one of patterns names host.
func matches(patterns []string, host string) bool {
	host = strings.ToLower(strings.TrimSuffix(host, "."))

	for _, pattern := range patterns {
		pattern = strings.ToLower(pattern)
		if suffix, ok := strings.CutPrefix(pattern, "*."); ok {
			if strings.HasSuffix(host, "."+suffix) {
				return true
			}
		} else if host == pattern {
			return true
		}
	}

	return false
}

// Local reports whether host names this machine: localhost or a loopback
// address.
func Local(host string) bool {
	if strings.EqualFold(host, "localhost") {
		return true
	}

	addr, err := netip.ParseAddr(host)

	return err == nil && addr.Unmap().IsLoopback()
}

var cgnat = netip.MustParsePrefix("100.64.0.0/10")

// private reports whether addr is not a public unicast address.
func private(addr netip.Addr) bool {
	addr = addr.Unmap()

	return addr.IsLoopback() || addr.IsPrivate() || addr.IsLinkLocalUnicast() ||
		addr.IsLinkLocalMulticast() || addr.IsInterfaceLocalMulticast() ||
		addr.IsMulticast() || addr.IsUnspecified() || cgnat.Contains(addr)
}

// checkHost refuses a host that Allow does not list. A URL of this machine
// passes, since only the user or a manifest on this machine may name one.
func (p Policy) checkHost(host string) error {
	if Local(host) || p.allowed(host) {
		return nil
	}

	return &Refused{Host: host}
}

// checkAddr refuses addr for host when it is private. A host that Private
// names, and "localhost", may resolve to anything. A loopback address that
// the URL names itself passes too.
func (p Policy) checkAddr(host string, addr netip.Addr) error {
	if !private(addr) || p.privateOK(host) || strings.EqualFold(host, "localhost") {
		return nil
	}

	if named, err := netip.ParseAddr(host); err == nil && named.Unmap().IsLoopback() {
		return nil
	}

	return &Refused{Host: host, Addr: addr}
}

// resolves refuses a host whose every address checkAddr refuses.
func (p Policy) resolves(ctx context.Context, host string) error {
	if addr, err := netip.ParseAddr(host); err == nil {
		return p.checkAddr(host, addr)
	}

	found, err := net.DefaultResolver.LookupNetIP(ctx, "ip", host)
	if err != nil {
		return err
	}

	for _, addr := range found {
		if p.checkAddr(host, addr) == nil {
			return nil
		}
	}

	return &Refused{Host: host, Addr: found[0]}
}

// CheckURL refuses a URL that the policy would not connect to, for a program
// such as git that makes its own connections. A path or a file URL passes.
func (p Policy) CheckURL(ctx context.Context, rawURL string) error {
	host := gitHost(rawURL)
	if host == "" {
		return nil
	}

	if err := p.checkHost(host); err != nil {
		return err
	}

	return p.resolves(ctx, host)
}

// gitHost is the host of a URL that git accepts, or "" for a local one. Git
// takes "user@host:path" for ssh as well as URLs.
func gitHost(rawURL string) string {
	// A one-letter scheme is a Windows drive.
	if u, err := url.Parse(rawURL); err == nil && len(u.Scheme) > 1 {
		if u.Scheme == "file" {
			return ""
		}

		return u.Hostname()
	}

	before, _, ok := strings.Cut(rawURL, ":")
	if !ok || strings.Contains(before, "/") || len(before) < 2 {
		return ""
	}

	_, host, found := strings.Cut(before, "@")
	if !found {
		host = before
	}

	return strings.Trim(host, "[]")
}

type proxyKey struct{}

// Transport sets t to connect only where p allows, and returns it wrapped so
// that each request and each redirect checks its host first.
//
// The dialer is Go's own. It dials a host's IPv4 addresses 0.3 s after its
// IPv6 ones, and splits the timeout between the addresses, so one address that
// drops connection attempts does not use all of it. It checks each address as
// it dials it.
func (p Policy) Transport(t *http.Transport) http.RoundTripper {
	t.DialContext = func(ctx context.Context, network, address string) (net.Conn, error) {
		dialer := &net.Dialer{Timeout: 30 * time.Second, KeepAlive: 30 * time.Second}

		// The proxy is the user's own, so oku dials it and checks only host names.
		if address == ctx.Value(proxyKey{}) {
			return dialer.DialContext(ctx, network, address)
		}

		host, _, err := net.SplitHostPort(address)
		if err != nil {
			return nil, err
		}

		dialer.ControlContext = func(_ context.Context, _, address string, _ syscall.RawConn) error {
			addr, err := netip.ParseAddrPort(address)
			if err != nil {
				return err
			}

			return p.checkAddr(host, addr.Addr())
		}

		conn, err := dialer.DialContext(ctx, network, address)

		// The dial error around a refusal repeats the address, so return the
		// refusal alone.
		if refused, ok := errors.AsType[*Refused](err); ok {
			return nil, refused
		}

		return conn, err
	}

	return guard{policy: p, next: t}
}

type guard struct {
	policy Policy
	next   *http.Transport
}

func (g guard) RoundTrip(req *http.Request) (*http.Response, error) {
	if req.URL.Scheme == "file" {
		return g.next.RoundTrip(req)
	}

	host := req.URL.Hostname()

	var chain []string
	for prev := req.Response; prev != nil && prev.Request != nil; prev = prev.Request.Response {
		chain = append([]string{prev.Request.URL.String()}, chain...)
	}

	// A redirect to this machine is not a URL the user named.
	var refused *Refused

	switch {
	case !Local(host) && !g.policy.allowed(host):
		refused = &Refused{Host: host}
	case Local(host) && len(chain) > 0 && !g.policy.AllowPrivate:
		if first, err := url.Parse(chain[0]); err == nil && !Local(first.Hostname()) {
			refused = &Refused{Host: host}
		}
	}

	if refused != nil {
		refused.Chain = chain

		return nil, refused
	}

	if g.next.Proxy != nil {
		if proxy, err := g.next.Proxy(req); err == nil && proxy != nil {
			req = req.WithContext(context.WithValue(req.Context(), proxyKey{}, proxyAddress(proxy)))
		}
	}

	// Most requests run inside a Start wait that names them. Idle names the host
	// for the rest once they take a second.
	defer status.Idle(req.Context(), "waiting for %s", host)()

	return g.next.RoundTrip(req)
}

// proxyAddress is the host:port that the transport dials for proxy.
func proxyAddress(proxy *url.URL) string {
	if proxy.Port() != "" {
		return proxy.Host
	}

	port := "80"

	switch proxy.Scheme {
	case "https":
		port = "443"
	case "socks5", "socks5h":
		port = "1080"
	}

	return net.JoinHostPort(proxy.Hostname(), port)
}

// Client returns a client whose connections follow p, over a copy of Go's
// default transport. checkRedirect is the client's redirect policy.
func (p Policy) Client(
	checkRedirect func(*http.Request, []*http.Request) error,
) *http.Client {
	return &http.Client{
		Transport:     p.Transport(http.DefaultTransport.(*http.Transport).Clone()),
		CheckRedirect: checkRedirect,
	}
}
