package forward

import (
	"errors"
	"io"
	"net"
	"strconv"
	"testing"
	"time"
)

// echoServer starts a loopback server that writes back whatever it reads, and
// returns its host:port. It is torn down with the test.
func echoServer(t *testing.T) string {
	t.Helper()
	ln, err := net.Listen("tcp", "127.0.0.1:0")
	if err != nil {
		t.Fatalf("starting the echo server: %v", err)
	}
	t.Cleanup(func() { _ = ln.Close() })

	go func() {
		for {
			conn, err := ln.Accept()
			if err != nil {
				return
			}
			go func() {
				defer func() { _ = conn.Close() }()
				_, _ = io.Copy(conn, conn)
			}()
		}
	}()
	return ln.Addr().String()
}

// freePort returns a loopback port nothing is listening on. The gap between
// the close and the caller's bind is a race in principle; in a test process
// binding one port at a time it does not happen.
func freePort(t *testing.T) int {
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

func TestAForwardCarriesBytesBothWays(t *testing.T) {
	target := echoServer(t)
	r := New()
	t.Cleanup(r.CloseAll)

	f, err := r.Open(freePort(t), target, "")
	if err != nil {
		t.Fatalf("Open: %v", err)
	}

	conn, err := net.DialTimeout("tcp", f.Addr(), time.Second)
	if err != nil {
		t.Fatalf("dialing the forward: %v", err)
	}
	defer func() { _ = conn.Close() }()

	const payload = "the bytes have to come back"
	if _, err := conn.Write([]byte(payload)); err != nil {
		t.Fatalf("writing through the forward: %v", err)
	}
	got := make([]byte, len(payload))
	if err := conn.SetReadDeadline(time.Now().Add(2 * time.Second)); err != nil {
		t.Fatalf("setting a read deadline: %v", err)
	}
	if _, err := io.ReadFull(conn, got); err != nil {
		t.Fatalf("reading through the forward: %v", err)
	}
	if string(got) != payload {
		t.Errorf("the echo came back as %q, want %q", got, payload)
	}
}

// The port check has to run before anything else, or a privileged port with an
// unreachable target would be reported as an unreachable target — the refusal
// the user can act on hidden behind the one they cannot.
func TestAPrivilegedPortIsRefusedBeforeTheTargetIsProbed(t *testing.T) {
	r := New()
	t.Cleanup(r.CloseAll)

	// Nothing is listening on this target, so a probe would fail too.
	_, err := r.Open(80, "127.0.0.1:1", "")
	if !errors.Is(err, ErrPrivilegedPort) {
		t.Fatalf("Open(80) = %v, want ErrPrivilegedPort", err)
	}
	if errors.Is(err, ErrTargetUnreachable) {
		t.Error("the target was probed although the port was already refused")
	}
}

func TestAPortAlreadyInUseIsRefused(t *testing.T) {
	target := echoServer(t)

	held, err := net.Listen("tcp", "127.0.0.1:0")
	if err != nil {
		t.Fatalf("holding a port: %v", err)
	}
	defer func() { _ = held.Close() }()
	port := held.Addr().(*net.TCPAddr).Port

	r := New()
	t.Cleanup(r.CloseAll)

	if _, err := r.Open(port, target, ""); !errors.Is(err, ErrPortInUse) {
		t.Fatalf("Open on a held port = %v, want ErrPortInUse", err)
	}
	if got := len(r.List()); got != 0 {
		t.Errorf("a refused Open left %d forwards behind, want 0", got)
	}
}

// A failed probe must not leave a listener: a row that accepts connections and
// can never deliver them is worse than no row.
func TestAnUnreachableTargetOpensNoListener(t *testing.T) {
	port := freePort(t)
	r := New()
	t.Cleanup(r.CloseAll)

	// Port 1 on loopback: reserved, and nothing listens there.
	_, err := r.Open(port, "127.0.0.1:1", "")
	if !errors.Is(err, ErrTargetUnreachable) {
		t.Fatalf("Open with a dead target = %v, want ErrTargetUnreachable", err)
	}
	if got := len(r.List()); got != 0 {
		t.Errorf("a refused Open left %d forwards behind, want 0", got)
	}

	ln, err := net.Listen("tcp", "127.0.0.1:"+strconv.Itoa(port))
	if err != nil {
		t.Fatalf("the local port was not released: %v", err)
	}
	_ = ln.Close()
}

func TestClosingAForwardReleasesItsPort(t *testing.T) {
	target := echoServer(t)
	r := New()
	t.Cleanup(r.CloseAll)

	port := freePort(t)
	f, err := r.Open(port, target, "")
	if err != nil {
		t.Fatalf("Open: %v", err)
	}
	if err := r.Close(f.ID); err != nil {
		t.Fatalf("Close: %v", err)
	}
	if got := len(r.List()); got != 0 {
		t.Fatalf("Close left %d forwards, want 0", got)
	}

	ln, err := net.Listen("tcp", "127.0.0.1:"+strconv.Itoa(port))
	if err != nil {
		t.Fatalf("the port was not released by Close: %v", err)
	}
	_ = ln.Close()
}

// Closing while a connection is live is the case that would deadlock if the
// registry held its lock across I/O.
func TestAForwardCanBeClosedWhileAConnectionIsOpen(t *testing.T) {
	target := echoServer(t)
	r := New()
	t.Cleanup(r.CloseAll)

	f, err := r.Open(freePort(t), target, "")
	if err != nil {
		t.Fatalf("Open: %v", err)
	}
	conn, err := net.DialTimeout("tcp", f.Addr(), time.Second)
	if err != nil {
		t.Fatalf("dialing the forward: %v", err)
	}
	defer func() { _ = conn.Close() }()

	done := make(chan error, 1)
	go func() { done <- r.Close(f.ID) }()
	select {
	case err := <-done:
		if err != nil {
			t.Fatalf("Close: %v", err)
		}
	case <-time.After(3 * time.Second):
		t.Fatal("Close blocked while a connection was open")
	}
}

func TestClosingAnUnknownForwardSaysSo(t *testing.T) {
	r := New()
	if err := r.Close("nope"); !errors.Is(err, ErrNoSuchForward) {
		t.Fatalf("Close of an unknown id = %v, want ErrNoSuchForward", err)
	}
}

func TestAForwardCountsTheConnectionsItCarried(t *testing.T) {
	target := echoServer(t)
	r := New()
	t.Cleanup(r.CloseAll)

	f, err := r.Open(freePort(t), target, "")
	if err != nil {
		t.Fatalf("Open: %v", err)
	}

	const rounds = 3
	for i := 0; i < rounds; i++ {
		conn, err := net.DialTimeout("tcp", f.Addr(), time.Second)
		if err != nil {
			t.Fatalf("dial %d: %v", i, err)
		}
		if _, err := conn.Write([]byte("x")); err != nil {
			t.Fatalf("write %d: %v", i, err)
		}
		_ = conn.Close()
	}

	// The count is bumped by the connection goroutine, so it lands shortly
	// after the dial returns rather than during it.
	deadline := time.Now().Add(3 * time.Second)
	var total int64
	for time.Now().Before(deadline) {
		list := r.List()
		if len(list) != 1 {
			t.Fatalf("List returned %d forwards, want 1", len(list))
		}
		total = list[0].Total
		if total >= rounds {
			break
		}
		time.Sleep(10 * time.Millisecond)
	}
	if total != rounds {
		t.Errorf("Total = %d after %d connections, want %d", total, rounds, rounds)
	}
}

func TestListIsOrderedOldestFirst(t *testing.T) {
	target := echoServer(t)
	r := New()
	t.Cleanup(r.CloseAll)

	var ids []string
	for i := 0; i < 3; i++ {
		f, err := r.Open(freePort(t), target, "")
		if err != nil {
			t.Fatalf("Open %d: %v", i, err)
		}
		ids = append(ids, f.ID)
		time.Sleep(time.Millisecond)
	}

	list := r.List()
	if len(list) != len(ids) {
		t.Fatalf("List returned %d forwards, want %d", len(list), len(ids))
	}
	for i, want := range ids {
		if list[i].ID != want {
			t.Errorf("List[%d].ID = %q, want %q", i, list[i].ID, want)
		}
	}
}

func TestATargetThatIsNotAHostPortIsRefused(t *testing.T) {
	cases := []struct {
		name   string
		target string
	}{
		{"no port", "example.com"},
		{"empty", ""},
		{"no host", ":8080"},
		{"port is not a number", "example.com:http"},
		{"port out of range", "example.com:70000"},
	}
	r := New()
	t.Cleanup(r.CloseAll)

	for _, tc := range cases {
		t.Run(tc.name, func(t *testing.T) {
			_, err := r.Open(9999, tc.target, "")
			if err == nil {
				t.Fatalf("Open(%q) succeeded, want a refusal", tc.target)
			}
			// It must be refused on its shape, not by a probe that timed out.
			if errors.Is(err, ErrTargetUnreachable) {
				t.Errorf("Open(%q) probed a target it should have rejected outright", tc.target)
			}
		})
	}
}

func TestTheAddressAClientDialsIsLoopback(t *testing.T) {
	f := Forward{LocalPort: 8080}
	if got, want := f.Addr(), "127.0.0.1:8080"; got != want {
		t.Errorf("Addr() = %q, want %q", got, want)
	}
}

// The package doc makes binding loopback a promise, so it is asserted on the
// listener itself. A second bind cannot stand in for this: 0.0.0.0 and
// 127.0.0.1 collide on the same port in both directions, so the overlap is
// the same whichever one the forward chose — it proves nothing.
func TestAForwardNeverBindsAnythingButLoopback(t *testing.T) {
	target := echoServer(t)
	r := New()
	t.Cleanup(r.CloseAll)

	f, err := r.Open(freePort(t), target, "")
	if err != nil {
		t.Fatalf("Open: %v", err)
	}

	r.mu.Lock()
	e, ok := r.entries[f.ID]
	r.mu.Unlock()
	if !ok {
		t.Fatalf("the registry does not hold the forward it just returned")
	}

	addr, isTCP := e.ln.Addr().(*net.TCPAddr)
	if !isTCP {
		t.Fatalf("the listener is bound to %T, want *net.TCPAddr", e.ln.Addr())
	}
	if !addr.IP.IsLoopback() {
		t.Errorf("the forward is bound to %s, want a loopback address", addr.IP)
	}
}
