package loopback

import (
	"syscall"
	"testing"
)

// ReservePort binds a socket to 127.0.0.1:0 with SO_REUSEADDR and keeps it
// bound, but not listening, until the test ends.
//
// On Linux that is a reservation the kernel honors: neither bind(0) nor
// connect() hands out a port such a socket holds, in this process or any
// other, while an explicit bind with SO_REUSEADDR followed by listen — what
// net.Listen does, so what the code under test does — still succeeds, since at
// most one of the sockets sharing the port listens. Nothing is released before
// the code under test binds, so there is no window to lose the port in, and two
// reservations held at once can never return the same port.
func ReservePort(t testing.TB) int {
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
