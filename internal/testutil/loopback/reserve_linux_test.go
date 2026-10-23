package loopback

import (
	"errors"
	"net"
	"strconv"
	"syscall"
	"testing"
)

// TestAReservedPortStaysOccupiedYetCanBeListenedOn is DEVDESK-35's
// regression test. A reserved port must still occupy the kernel's bind table —
// that table is what bind(0) and connect() consult when they pick a port, so a
// port absent from it can be given to anyone before the code under test binds
// it. A bind without SO_REUSEADDR is the observable: it fails on an occupied
// port and succeeds on a released one. And net.Listen must still take it.
func TestAReservedPortStaysOccupiedYetCanBeListenedOn(t *testing.T) {
	port := ReservePort(t)

	fd, err := syscall.Socket(syscall.AF_INET, syscall.SOCK_STREAM|syscall.SOCK_CLOEXEC, 0)
	if err != nil {
		t.Fatal(err)
	}
	defer func() { _ = syscall.Close(fd) }()
	err = syscall.Bind(fd, &syscall.SockaddrInet4{Port: port, Addr: [4]byte{127, 0, 0, 1}})
	if !errors.Is(err, syscall.EADDRINUSE) {
		t.Fatalf("binding the reserved port %d answered %v, want EADDRINUSE: the port is up for grabs", port, err)
	}

	ln, err := net.Listen("tcp", "127.0.0.1:"+strconv.Itoa(port))
	if err != nil {
		t.Fatalf("net.Listen refused the reserved port: %v", err)
	}
	_ = ln.Close()
}

// TestTwoReservationsHeldAtOnceNeverShareAPort covers the
// `oldPort, newPort := freePort(t), freePort(t)` pattern: a released port can
// come back from the very next bind(0), a held one cannot.
func TestTwoReservationsHeldAtOnceNeverShareAPort(t *testing.T) {
	seen := map[int]bool{}
	for i := 0; i < 500; i++ {
		p := ReservePort(t)
		if seen[p] {
			t.Fatalf("port %d was handed out twice while both reservations are held", p)
		}
		seen[p] = true
	}
}
