//go:build !linux

package loopback

import (
	"net"
	"testing"
)

// ReservePort returns a port that was free a moment ago. Outside Linux there
// is no portable way to hold a port that a later SO_REUSEADDR bind may still
// take — on BSD and macOS an identical address needs SO_REUSEPORT, on Windows
// SO_REUSEADDR means something else — so the gap between the close and the
// caller's bind stays, and with it the rare "address already in use" of
// DEVDESK-35. Two calls in a row may even return the same port. CI runs on
// Linux.
func ReservePort(t testing.TB) int {
	t.Helper()
	ln, err := net.Listen("tcp", "127.0.0.1:0")
	if err != nil {
		t.Fatalf("reserving a port: %v", err)
	}
	port := ln.Addr().(*net.TCPAddr).Port
	if err := ln.Close(); err != nil {
		t.Fatalf("releasing the reserved port: %v", err)
	}
	return port
}
