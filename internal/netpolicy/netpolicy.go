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
	case local(r.Host):
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

// local reports whether host names this machine: "localhost" or a loopback
// address.
func local(host string) bool {
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
// always passes, since the user named it.
func (p Policy) checkHost(host string) error {
	if local(host) || p.allowed(host) {
		return nil
	}

	return &Refused{Host: host}
}

// addrs resolves host to the addresses oku may connect to. A host that Private
// names, and "localhost", may resolve to anything. A loopback address that
// the URL names itself passes too.
func (p Policy) addrs(ctx context.Context, host string) ([]netip.Addr, error) {
	if addr, err := netip.ParseAddr(host); err == nil {
		if private(addr) && !addr.Unmap().IsLoopback() && !p.privateOK(host) {
			return nil, &Refused{Host: host, Addr: addr}
		}

		return []netip.Addr{addr}, nil
	}

	found, err := net.DefaultResolver.LookupNetIP(ctx, "ip", host)
	if err != nil {
		return nil, err
	}

	if p.privateOK(host) || strings.EqualFold(host, "localhost") {
		return found, nil
	}

	var public []netip.Addr

	for _, addr := range found {
		if !private(addr) {
			public = append(public, addr)
		}
	}

	if len(public) == 0 {
		return nil, &Refused{Host: host, Addr: found[0]}
	}

	return public, nil
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

	_, err := p.addrs(ctx, host)

	return err
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
func (p Policy) Transport(t *http.Transport) http.RoundTripper {
	dialer := &net.Dialer{}
	t.DialContext = func(ctx context.Context, network, address string) (net.Conn, error) {
		// The proxy is the user's own, so oku dials it and checks only host names.
		if address == ctx.Value(proxyKey{}) {
			return dialer.DialContext(ctx, network, address)
		}

		host, port, err := net.SplitHostPort(address)
		if err != nil {
			return nil, err
		}

		addrs, err := p.addrs(ctx, host)
		if err != nil {
			return nil, err
		}

		var errs []error

		for _, addr := range addrs {
			conn, err := dialer.DialContext(ctx, network, net.JoinHostPort(addr.String(), port))
			if err == nil {
				return conn, nil
			}

			errs = append(errs, err)
		}

		return nil, errors.Join(errs...)
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
	case !local(host) && !g.policy.allowed(host):
		refused = &Refused{Host: host}
	case local(host) && len(chain) > 0 && !g.policy.AllowPrivate:
		if first, err := url.Parse(chain[0]); err == nil && !local(first.Hostname()) {
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
