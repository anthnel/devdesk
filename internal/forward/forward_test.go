package forward

import (
	"errors"
	"io"
	"net"
	"reflect"
	"strconv"
	"strings"
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

	f, err := r.Open(freePort(t), target)
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
	_, err := r.Open(80, "127.0.0.1:1")
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

	if _, err := r.Open(port, target); !errors.Is(err, ErrPortInUse) {
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
	_, err := r.Open(port, "127.0.0.1:1")
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
	f, err := r.Open(port, target)
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

	f, err := r.Open(freePort(t), target)
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

	f, err := r.Open(freePort(t), target)
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
		f, err := r.Open(freePort(t), target)
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
			_, err := r.Open(9999, tc.target)
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

	f, err := r.Open(freePort(t), target)
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

func stateOf(t *testing.T, r *Registry, id string) Forward {
	t.Helper()
	for _, f := range r.List() {
		if f.ID == id {
			return f
		}
	}
	t.Fatalf("no forward %q in %+v", id, r.List())
	return Forward{}
}

func TestRestoreReopensWhatWasSaved(t *testing.T) {
	target := echoServer(t)
	port := freePort(t)
	r := New()
	t.Cleanup(r.CloseAll)

	sum := r.Restore([]Entry{{LocalPort: port, Target: target}})
	if sum != (Restored{Live: 1}) {
		t.Fatalf("Restore = %+v, want one live", sum)
	}

	list := r.List()
	if len(list) != 1 || list[0].State != StateLive {
		t.Fatalf("List = %+v, want one live forward", list)
	}
	conn, err := net.DialTimeout("tcp", list[0].Addr(), time.Second)
	if err != nil {
		t.Fatalf("the restored forward does not listen: %v", err)
	}
	_ = conn.Close()
}

func TestARestoreThatFailsKeepsTheEntry(t *testing.T) {
	target := echoServer(t)
	held, err := net.Listen("tcp", "127.0.0.1:0")
	if err != nil {
		t.Fatal(err)
	}
	t.Cleanup(func() { _ = held.Close() })
	takenPort := held.Addr().(*net.TCPAddr).Port

	r := New()
	t.Cleanup(r.CloseAll)
	sum := r.Restore([]Entry{{LocalPort: takenPort, Target: target}})
	if sum != (Restored{Unbound: 1}) {
		t.Fatalf("Restore = %+v, want one unbound", sum)
	}

	f := r.List()[0]
	if f.State != StateUnbound {
		t.Errorf("State = %v, want unbound", f.State)
	}
	if f.LastErr == "" {
		t.Error("an unbound forward carries no reason")
	}
	// The whole point: an entry that failed to bind today is still wanted
	// tomorrow, so it is still what gets saved.
	want := []Entry{{LocalPort: takenPort, Target: target}}
	if got := r.Entries(); !reflect.DeepEqual(got, want) {
		t.Errorf("Entries = %+v, want %+v", got, want)
	}
}

func TestARestoredTargetThatIsNotUpYetIsUnboundNotDropped(t *testing.T) {
	r := New()
	t.Cleanup(r.CloseAll)
	silent := "127.0.0.1:" + strconv.Itoa(freePort(t))

	sum := r.Restore([]Entry{{LocalPort: freePort(t), Target: silent}})
	if sum.Unbound != 1 || len(r.Entries()) != 1 {
		t.Errorf("Restore = %+v, %d entries; want the entry kept, unbound", sum, len(r.Entries()))
	}
	if err := r.List()[0].LastErr; !strings.Contains(err, "did not answer") {
		t.Errorf("LastErr = %q, want the target's silence named", err)
	}
}

func TestAPausedEntryIsRestoredWithoutBinding(t *testing.T) {
	target := echoServer(t)
	port := freePort(t)
	r := New()
	t.Cleanup(r.CloseAll)

	sum := r.Restore([]Entry{{LocalPort: port, Target: target, Paused: true}})
	if sum != (Restored{Paused: 1}) {
		t.Fatalf("Restore = %+v, want one paused", sum)
	}
	// Bindable proves nothing listened.
	ln, err := net.Listen("tcp", net.JoinHostPort("127.0.0.1", strconv.Itoa(port)))
	if err != nil {
		t.Fatalf("a paused forward holds its port: %v", err)
	}
	_ = ln.Close()

	if got := r.Entries(); len(got) != 1 || !got[0].Paused {
		t.Errorf("Entries = %+v, want the paused flag kept", got)
	}
}

func TestRestoreKeepsTheOrderOfTheFile(t *testing.T) {
	target := echoServer(t)
	var entries []Entry
	for range 12 {
		entries = append(entries, Entry{LocalPort: freePort(t), Target: target, Paused: true})
	}
	r := New()
	t.Cleanup(r.CloseAll)
	r.Restore(entries)

	// Twelve entries created in the same instant, with IDs that sort wrongly
	// as text ("10" before "2"): the order has to be the creation order.
	if got := r.Entries(); !reflect.DeepEqual(got, entries) {
		t.Errorf("Entries reordered the file:\n got %+v\nwant %+v", got, entries)
	}
}

func TestPausingAForwardReleasesItsPortAndKeepsTheRow(t *testing.T) {
	target := echoServer(t)
	r := New()
	t.Cleanup(r.CloseAll)
	port := freePort(t)
	f, err := r.Open(port, target)
	if err != nil {
		t.Fatalf("Open: %v", err)
	}

	got, err := r.Toggle(f.ID)
	if err != nil {
		t.Fatalf("Toggle: %v", err)
	}
	if got.State != StatePaused {
		t.Fatalf("State = %v, want paused", got.State)
	}
	ln, err := net.Listen("tcp", f.Addr())
	if err != nil {
		t.Fatalf("a paused forward still holds %s: %v", f.Addr(), err)
	}
	_ = ln.Close()

	if entries := r.Entries(); len(entries) != 1 || !entries[0].Paused {
		t.Errorf("Entries = %+v, want the one entry, paused", entries)
	}
}

func TestResumingAPausedForwardBindsTheSamePort(t *testing.T) {
	target := echoServer(t)
	r := New()
	t.Cleanup(r.CloseAll)
	f, err := r.Open(freePort(t), target)
	if err != nil {
		t.Fatalf("Open: %v", err)
	}
	if _, err := r.Toggle(f.ID); err != nil {
		t.Fatalf("pause: %v", err)
	}

	got, err := r.Toggle(f.ID)
	if err != nil {
		t.Fatalf("resume: %v", err)
	}
	if got.State != StateLive || got.LocalPort != f.LocalPort {
		t.Fatalf("resume gave %+v, want live on %d", got, f.LocalPort)
	}
	conn, err := net.DialTimeout("tcp", f.Addr(), time.Second)
	if err != nil {
		t.Fatalf("the resumed forward does not listen: %v", err)
	}
	_ = conn.Close()
}

func TestResumingIntoATakenPortLeavesTheRowUnboundWithTheReason(t *testing.T) {
	target := echoServer(t)
	r := New()
	t.Cleanup(r.CloseAll)
	f, err := r.Open(freePort(t), target)
	if err != nil {
		t.Fatalf("Open: %v", err)
	}
	if _, err := r.Toggle(f.ID); err != nil {
		t.Fatalf("pause: %v", err)
	}
	squatter, err := net.Listen("tcp", f.Addr())
	if err != nil {
		t.Fatalf("taking the port: %v", err)
	}

	got, err := r.Toggle(f.ID)
	if !errors.Is(err, ErrPortInUse) {
		t.Fatalf("resume = %v, want ErrPortInUse", err)
	}
	if got.State != StateUnbound || got.LastErr == "" {
		t.Errorf("row = %+v, want unbound with a reason", got)
	}

	// The port frees up; the same key tries again and this time it works.
	_ = squatter.Close()
	if got, err = r.Toggle(f.ID); err != nil || got.State != StateLive {
		t.Errorf("second resume = %+v, %v; want live", got, err)
	}
	if got.LastErr != "" {
		t.Errorf("a live forward kept its old error: %q", got.LastErr)
	}
}

func TestClosingAPausedForwardRemovesItsEntry(t *testing.T) {
	target := echoServer(t)
	r := New()
	t.Cleanup(r.CloseAll)
	f, err := r.Open(freePort(t), target)
	if err != nil {
		t.Fatalf("Open: %v", err)
	}
	if _, err := r.Toggle(f.ID); err != nil {
		t.Fatalf("pause: %v", err)
	}

	if err := r.Close(f.ID); err != nil {
		t.Fatalf("Close: %v", err)
	}
	if got := r.Entries(); len(got) != 0 {
		t.Errorf("Entries = %+v, want none after a delete", got)
	}
}

func TestTogglingAnUnknownForwardSaysSo(t *testing.T) {
	if _, err := New().Toggle("nope"); !errors.Is(err, ErrNoSuchForward) {
		t.Errorf("Toggle = %v, want ErrNoSuchForward", err)
	}
}

func TestAPausedForwardKeepsItsPlaceInTheList(t *testing.T) {
	target := echoServer(t)
	r := New()
	t.Cleanup(r.CloseAll)
	first, _ := r.Open(freePort(t), target)
	second, _ := r.Open(freePort(t), target)

	// Pausing then resuming the first must not move it below the second, which
	// it would if the order followed the last bind.
	_, _ = r.Toggle(first.ID)
	_, _ = r.Toggle(first.ID)

	list := r.List()
	if len(list) != 2 || list[0].ID != first.ID || list[1].ID != second.ID {
		t.Errorf("order = %v, want [%s %s]", []string{list[0].ID, list[1].ID}, first.ID, second.ID)
	}
}

func TestAForwardPausedWhileAConnectionIsOpenDoesNotPanic(t *testing.T) {
	target := echoServer(t)
	r := New()
	t.Cleanup(r.CloseAll)
	f, err := r.Open(freePort(t), target)
	if err != nil {
		t.Fatalf("Open: %v", err)
	}
	conn, err := net.DialTimeout("tcp", f.Addr(), time.Second)
	if err != nil {
		t.Fatalf("dial: %v", err)
	}
	defer func() { _ = conn.Close() }()

	// The accept loop must not read the listener field a pause clears.
	if _, err := r.Toggle(f.ID); err != nil {
		t.Fatalf("Toggle: %v", err)
	}
	time.Sleep(50 * time.Millisecond)
	stateOf(t, r, f.ID)
}
