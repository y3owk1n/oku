//go:build linux

package netpolicy_test

import (
	"context"
	"net"
	"net/http"
	"net/http/httptest"
	"net/netip"
	"slices"
	"strconv"
	"syscall"
	"testing"
	"time"

	"github.com/y3owk1n/oku/internal/netpolicy"
)

func TestB558AnAddressThatDropsConnectionsFallsBackToTheOthers(t *testing.T) {
	found, err := net.DefaultResolver.LookupNetIP(context.Background(), "ip", "localhost")
	if err != nil || !slices.ContainsFunc(found, func(a netip.Addr) bool { return a.Is6() }) {
		t.Skipf("localhost has no IPv6 address here: %v %v", found, err)
	}

	server := httptest.NewServer(http.HandlerFunc(func(w http.ResponseWriter, _ *http.Request) {
		_, _ = w.Write([]byte("ok"))
	}))
	defer server.Close()

	_, port, err := net.SplitHostPort(server.Listener.Addr().String())
	if err != nil {
		t.Fatal(err)
	}

	dropSYNs(t, port)

	ctx, cancel := context.WithTimeout(context.Background(), 20*time.Second)
	defer cancel()

	req, err := http.NewRequestWithContext(ctx, http.MethodGet, "http://localhost:"+port+"/", nil)
	if err != nil {
		t.Fatal(err)
	}

	started := time.Now()

	resp, err := netpolicy.Policy{}.Client(nil).Do(req)
	if err != nil {
		t.Fatalf("want the request served over IPv4, got %v", err)
	}

	resp.Body.Close()

	if took := time.Since(started); took > 5*time.Second {
		t.Fatalf("the IPv4 address should answer while IPv6 hangs, took %s", took)
	}
}

// dropSYNs listens on [::1]:port and fills the backlog, so the kernel drops
// each new connection attempt there, as a broken route does.
func dropSYNs(t *testing.T, port string) {
	t.Helper()

	n, err := strconv.Atoi(port)
	if err != nil {
		t.Fatal(err)
	}

	fd, err := syscall.Socket(syscall.AF_INET6, syscall.SOCK_STREAM, 0)
	if err != nil {
		t.Skipf("this machine has no IPv6 socket: %v", err)
	}

	t.Cleanup(func() { _ = syscall.Close(fd) })

	if err := syscall.Bind(fd, &syscall.SockaddrInet6{Port: n, Addr: [16]byte{15: 1}}); err != nil {
		t.Skipf("[::1]:%s is taken: %v", port, err)
	}

	if err := syscall.Listen(fd, 0); err != nil {
		t.Fatal(err)
	}

	for range 8 {
		conn, err := net.DialTimeout("tcp", net.JoinHostPort("::1", port), 300*time.Millisecond)
		if err != nil {
			return
		}

		t.Cleanup(func() { _ = conn.Close() })
	}

	t.Skip("this kernel does not drop connections to a full backlog")
}
