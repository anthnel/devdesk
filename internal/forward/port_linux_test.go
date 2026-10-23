package forward

import (
	"errors"
	"net"
	"strconv"
	"syscall"
	"testing"
)

// reservePort binds a socket to 127.0.0.1:0 with SO_REUSEADDR and keeps it
// bound, but not listening, until the test ends.
//
// On Linux that is a reservation the kernel honors: neither bind(0) nor
// connect() hands out a port such a socket holds, in this process or any
// other, while an explicit bind with SO_REUSEADDR followed by listen — what
// net.Listen does, so what the code under test does — still succeeds, since at
// most one of the sockets sharing the port listens. Nothing has to be released
// before the code under test binds, so there is no window to lose the port in.
func reservePort(t *testing.T) int {
	t.Helper()
	fd, err := syscall.Socket(syscall.AF_INET, syscall.SOCK_STREAM|syscall.SOCK_CLOEXEC, 0)
	if err != nil {
		t.Fatalf("reserving a port: %v", err)
	}
	t.Cleanup(func() { _ = syscall.Close(fd) })
	if err := syscall.SetsockoptInt(fd, syscall.SOL_SOCKET, syscall.SO_REUSEADDR, 1); err != nil {
		t.Fatalf("reserving a port: %v", err)
	}
	if err := syscall.Bind(fd, &syscall.SockaddrInet4{Addr: [4]byte{127, 0, 0, 1}}); err != nil {
		t.Fatalf("reserving a port: %v", err)
	}
	sa, err := syscall.Getsockname(fd)
	if err != nil {
		t.Fatalf("reading the reserved port: %v", err)
	}
	return sa.(*syscall.SockaddrInet4).Port
}

// TestAReservedPortStaysHeldYetTheProxyCanBindIt is DEVDESK-35's regression
// test. A port freePort hands out must still occupy the kernel's bind table —
// that table is what bind(0) and connect() consult when they pick a port, so a
// port absent from it can be given to anyone before the proxy binds it. A bind
// without SO_REUSEADDR is the observable: it fails on an occupied port and
// succeeds on a released one. And the occupation must not stop the proxy.
func TestAReservedPortStaysHeldYetTheProxyCanBindIt(t *testing.T) {
	r, port := routedRegistry(t)

	fd, err := syscall.Socket(syscall.AF_INET, syscall.SOCK_STREAM|syscall.SOCK_CLOEXEC, 0)
	if err != nil {
		t.Fatal(err)
	}
	defer func() { _ = syscall.Close(fd) }()
	err = syscall.Bind(fd, &syscall.SockaddrInet4{Port: port, Addr: [4]byte{127, 0, 0, 1}})
	if !errors.Is(err, syscall.EADDRINUSE) {
		t.Fatalf("binding the reserved port %d answered %v, want EADDRINUSE: the port is up for grabs before the proxy binds it", port, err)
	}

	if _, err := r.OpenRoute("api.localhost", backend(t, "b")); err != nil {
		t.Fatalf("the proxy could not bind its reserved port: %v", err)
	}
	if ln, err := net.Listen("tcp", "127.0.0.1:"+strconv.Itoa(port)); err == nil {
		_ = ln.Close()
		t.Fatal("the proxy is not listening on its port")
	}
}
