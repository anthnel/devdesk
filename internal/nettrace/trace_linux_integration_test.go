//go:build linux

package nettrace

import (
	"context"
	"encoding/binary"
	"errors"
	"net"
	"net/netip"
	"os"
	"runtime"
	"strings"
	"syscall"
	"testing"
	"time"
	"unsafe"
)

// These tests drive linuxProber over real loopback sockets, without any
// privilege. Expected from the implementation (step 4):
//
//	type linuxProber struct{ port int }  // port 0 means defaultProbePort
//	const defaultProbePort = 33435
//	func (linuxProber) open(ctx, dst, ttl) (probeConn, error)
//	func unsupportedSocketError(err error) error // socket/setsockopt failures
//	func connectError(err error) error           // connect failures
//	func parseErrQueue(oob []byte) (reply, error)
//
// systemProber() must return linuxProber{}.

var loopback = netip.MustParseAddr("127.0.0.1")

// silentListener binds a UDP socket on loopback that never answers: probes
// sent to its port are delivered and produce no ICMP.
func silentListener(t *testing.T) (*net.UDPConn, int) {
	t.Helper()
	c, err := net.ListenUDP("udp4", &net.UDPAddr{IP: net.IPv4(127, 0, 0, 1)})
	if err != nil {
		t.Fatal(err)
	}
	t.Cleanup(func() { _ = c.Close() })
	return c, c.LocalAddr().(*net.UDPAddr).Port
}

// freePort returns a loopback UDP port nobody listens on (bound, then
// released: ports are not reserved, so a rare race is possible).
func freePort(t *testing.T) int {
	t.Helper()
	c, err := net.ListenUDP("udp4", &net.UDPAddr{IP: net.IPv4(127, 0, 0, 1)})
	if err != nil {
		t.Fatal(err)
	}
	port := c.LocalAddr().(*net.UDPAddr).Port
	_ = c.Close()
	return port
}

func TestSystemProberIsLinuxProber(t *testing.T) {
	if _, ok := systemProber().(linuxProber); !ok {
		t.Errorf("systemProber() = %T, want linuxProber", systemProber())
	}
}

func TestLinuxProberReachesLoopbackAtTTL1(t *testing.T) {
	if defaultProbePort != 33435 {
		t.Fatalf("defaultProbePort = %d, want 33435", defaultProbePort)
	}
	if l, err := net.ListenUDP("udp4", &net.UDPAddr{IP: net.IPv4(127, 0, 0, 1), Port: defaultProbePort}); err != nil {
		t.Skipf("trace port %d is taken: %v", defaultProbePort, err)
	} else {
		_ = l.Close()
	}
	res, err := Trace(context.Background(), loopback, Options{MaxHops: 3, ProbeTimeout: time.Second})
	if err != nil {
		t.Fatal(err)
	}
	if res.Outcome != OutcomeReached {
		t.Fatalf("outcome = %v, want reached", res.Outcome)
	}
	if len(res.Hops) != 1 {
		t.Fatalf("got %d hops, want 1 (cut at the target): %+v", len(res.Hops), res.Hops)
	}
	h := res.Hops[0]
	if h.TTL != 1 || h.Kind != HopTarget || h.Addr != loopback {
		t.Errorf("hop = %+v, want TTL 1, HopTarget, 127.0.0.1", h)
	}
	if h.RTT <= 0 {
		t.Errorf("RTT = %v, want > 0", h.RTT)
	}
	if h.ICMPType != icmpDestUnreachable || h.ICMPCode != icmpCodePortUnreach {
		t.Errorf("ICMP = %d/%d, want 3/3", h.ICMPType, h.ICMPCode)
	}
}

func TestLinuxProberUsesTheConfiguredPort(t *testing.T) {
	res, err := run(context.Background(), loopback, Options{MaxHops: 2, ProbeTimeout: time.Second}, linuxProber{port: freePort(t)})
	if err != nil {
		t.Fatal(err)
	}
	if res.Outcome != OutcomeReached || len(res.Hops) == 0 || res.Hops[0].Kind != HopTarget {
		t.Errorf("result = %+v, want reached at TTL 1", res)
	}
}

func TestLinuxProberPayloadIsNeutral(t *testing.T) {
	l, port := silentListener(t)
	opts := Options{MaxHops: 1, ProbeTimeout: 100 * time.Millisecond}
	if _, err := run(context.Background(), loopback, opts, linuxProber{port: port}); err != nil {
		t.Fatal(err)
	}
	buf := make([]byte, 2048)
	_ = l.SetReadDeadline(time.Now().Add(time.Second))
	n, _, err := l.ReadFromUDP(buf)
	if err != nil {
		t.Fatalf("the probe never arrived: %v", err)
	}
	if n == 0 {
		t.Error("empty payload, want a few neutral bytes")
	}
	if strings.Contains(strings.ToLower(string(buf[:n])), "devdesk") {
		t.Errorf("payload %q names the product", buf[:n])
	}
}

func TestLinuxProberSilentTargetEndsAtMaxHops(t *testing.T) {
	_, port := silentListener(t)
	res, err := run(context.Background(), loopback, Options{MaxHops: 3, ProbeTimeout: 200 * time.Millisecond}, linuxProber{port: port})
	if err != nil {
		t.Fatal(err)
	}
	if res.Outcome != OutcomeMaxHops {
		t.Fatalf("outcome = %v, want max hops", res.Outcome)
	}
	if len(res.Hops) != 3 {
		t.Fatalf("got %d hops, want 3", len(res.Hops))
	}
	for i, h := range res.Hops {
		if h.TTL != i+1 || h.Kind != HopSilent {
			t.Errorf("hop %d = %+v, want silent TTL %d", i, h, i+1)
		}
	}
}

func TestLinuxProberDeadlineShorterThanProbeTimeout(t *testing.T) {
	_, port := silentListener(t)
	opts := Options{MaxHops: 3, ProbeTimeout: 5 * time.Second, Deadline: 100 * time.Millisecond}
	start := time.Now()
	res, err := run(context.Background(), loopback, opts, linuxProber{port: port})
	if err != nil {
		t.Fatal(err)
	}
	if res.Outcome != OutcomeDeadline {
		t.Fatalf("outcome = %v, want deadline", res.Outcome)
	}
	if el := time.Since(start); el > time.Second {
		t.Errorf("took %v, want about the 100ms deadline", el)
	}
	if len(res.Hops) != 3 {
		t.Fatalf("got %d hops, want MaxHops entries", len(res.Hops))
	}
	for i, h := range res.Hops {
		if h.TTL != i+1 || h.Kind != HopUnresolved {
			t.Errorf("hop %d = %+v, want unresolved TTL %d", i, h, i+1)
		}
	}
}

func TestLinuxProberCancellation(t *testing.T) {
	_, port := silentListener(t)
	ctx, cancel := context.WithCancel(context.Background())
	time.AfterFunc(50*time.Millisecond, cancel)
	start := time.Now()
	res, err := run(ctx, loopback, Options{MaxHops: 3, ProbeTimeout: 5 * time.Second}, linuxProber{port: port})
	if err != nil {
		t.Fatal(err)
	}
	if res.Outcome != OutcomeCancelled {
		t.Errorf("outcome = %v, want canceled", res.Outcome)
	}
	if el := time.Since(start); el > time.Second {
		t.Errorf("took %v after a 50ms cancel", el)
	}
}

func TestLinuxProbeConnAwaitIsReleasedByContext(t *testing.T) {
	_, port := silentListener(t)
	ctx, cancel := context.WithCancel(context.Background())
	defer cancel()
	c, err := linuxProber{port: port}.open(ctx, loopback, 1)
	if err != nil {
		t.Fatal(err)
	}
	defer func() { _ = c.close() }()
	sentAt, err := c.send()
	if err != nil {
		t.Fatal(err)
	}
	if sentAt.IsZero() {
		t.Error("send returned a zero send time")
	}
	type answer struct{ err error }
	done := make(chan answer, 1)
	go func() {
		_, err := c.await(ctx, time.Now().Add(10*time.Second))
		done <- answer{err}
	}()
	time.Sleep(50 * time.Millisecond)
	cancelAt := time.Now()
	cancel()
	select {
	case a := <-done:
		if !errors.Is(a.err, context.Canceled) {
			t.Errorf("await err = %v, want context.Canceled", a.err)
		}
		if d := time.Since(cancelAt); d > 500*time.Millisecond {
			t.Errorf("await returned %v after the cancel, want <= 500ms", d)
		}
	case <-time.After(2 * time.Second):
		t.Fatal("await still blocked 2s after the cancel")
	}
}

// When the context's deadline is not after await's until, the trace deadline is
// what ran out: await reports the context's error, not a silent hop. The
// control case, a later context deadline, is a plain silent timeout.
func TestLinuxProbeConnAwaitDeadlineBranch(t *testing.T) {
	_, port := silentListener(t)
	tests := []struct {
		name     string
		ctxAfter time.Duration
		want     error
	}{
		{"context deadline equals until", 100 * time.Millisecond, context.DeadlineExceeded},
		{"context deadline before until", 50 * time.Millisecond, context.DeadlineExceeded},
		{"context deadline after until", 5 * time.Second, errSilent},
	}
	for _, tt := range tests {
		t.Run(tt.name, func(t *testing.T) {
			for i := 0; i < 5; i++ {
				c, err := linuxProber{port: port}.open(context.Background(), loopback, 1)
				if err != nil {
					t.Fatal(err)
				}
				now := time.Now()
				ctx, cancel := context.WithDeadline(context.Background(), now.Add(tt.ctxAfter))
				_, err = c.await(ctx, now.Add(100*time.Millisecond))
				cancel()
				_ = c.close()
				if !errors.Is(err, tt.want) {
					t.Fatalf("run %d: await err = %v, want %v", i, err, tt.want)
				}
			}
		})
	}
}

func TestLinuxProbeConnAwaitTimesOutSilent(t *testing.T) {
	_, port := silentListener(t)
	c, err := linuxProber{port: port}.open(context.Background(), loopback, 1)
	if err != nil {
		t.Fatal(err)
	}
	defer func() { _ = c.close() }()
	if _, err := c.send(); err != nil {
		t.Fatal(err)
	}
	if _, err := c.await(context.Background(), time.Now().Add(100*time.Millisecond)); !errors.Is(err, errSilent) {
		t.Errorf("await err = %v, want errSilent", err)
	}
}

func openFDs(t *testing.T) int {
	t.Helper()
	ents, err := os.ReadDir("/proc/self/fd")
	if err != nil {
		t.Skipf("no /proc/self/fd: %v", err)
	}
	return len(ents)
}

// L7: cancelling a run leaves no descriptor and no goroutine behind.
func TestLinuxProberCancellationLeaksNothing(t *testing.T) {
	_, port := silentListener(t)
	cycle := func(i int) {
		ctx, cancel := context.WithCancel(context.Background())
		if i%2 == 0 {
			cancel() // before anything is sent
		} else {
			time.AfterFunc(5*time.Millisecond, cancel)
		}
		if _, err := run(ctx, loopback, Options{MaxHops: 5, ProbeTimeout: 5 * time.Second}, linuxProber{port: port}); err != nil {
			t.Fatalf("cycle %d: %v", i, err)
		}
		cancel()
	}
	cycle(1) // warm-up: runtime pollers and the like
	fds, gor := openFDs(t), runtime.NumGoroutine()
	for i := 0; i < 100; i++ {
		cycle(i)
	}
	deadline := time.Now().Add(2 * time.Second)
	for (openFDs(t) != fds || runtime.NumGoroutine() > gor) && time.Now().Before(deadline) {
		time.Sleep(10 * time.Millisecond)
	}
	if got := openFDs(t); got != fds {
		t.Errorf("open descriptors: %d before, %d after 100 cycles", fds, got)
	}
	if got := runtime.NumGoroutine(); got > gor {
		t.Errorf("goroutines: %d before, %d after 100 cycles", gor, got)
	}
}

func TestUnsupportedSocketError(t *testing.T) {
	for _, errno := range []syscall.Errno{syscall.ENOPROTOOPT, syscall.EOPNOTSUPP, syscall.EPERM, syscall.EACCES} {
		t.Run(errno.Error(), func(t *testing.T) {
			raw := os.NewSyscallError("setsockopt", errno)
			err := unsupportedSocketError(raw)
			if !errors.Is(err, ErrUnsupported) {
				t.Errorf("%v does not wrap ErrUnsupported", err)
			}
			if !errors.Is(err, errno) {
				t.Errorf("%v lost the raw errno %v", err, errno)
			}
		})
	}
	// Anything else is not "unsupported", and nil stays nil.
	if err := unsupportedSocketError(syscall.EMFILE); errors.Is(err, ErrUnsupported) || !errors.Is(err, syscall.EMFILE) {
		t.Errorf("EMFILE: got %v, want the raw error, not ErrUnsupported", err)
	}
	if err := unsupportedSocketError(nil); err != nil {
		t.Errorf("nil: got %v", err)
	}
}

func TestConnectErrorIsNeverUnsupported(t *testing.T) {
	for _, errno := range []syscall.Errno{syscall.EACCES, syscall.EPERM, syscall.ENETUNREACH, syscall.EHOSTUNREACH} {
		err := connectError(os.NewSyscallError("connect", errno))
		if errors.Is(err, ErrUnsupported) {
			t.Errorf("%v: a refused connect is not ErrUnsupported", errno)
		}
		if !errors.Is(err, errno) {
			t.Errorf("%v: raw errno lost: %v", errno, err)
		}
	}
	// ENETUNREACH and EHOSTUNREACH at connect mean "no route".
	for _, errno := range []syscall.Errno{syscall.ENETUNREACH, syscall.EHOSTUNREACH} {
		if err := connectError(errno); !errors.Is(err, ErrNoRoute) || !errors.Is(err, errno) {
			t.Errorf("%v: got %v, want ErrNoRoute wrapping the raw errno", errno, err)
		}
	}
}

// errQueueMsg builds the control-message buffer Recvmsg returns for one
// IP_RECVERR error: a cmsghdr (SOL_IP, IP_RECVERR) then payload.
func errQueueMsg(level, typ int32, payload []byte) []byte {
	buf := make([]byte, syscall.CmsgSpace(len(payload)))
	h := (*syscall.Cmsghdr)(unsafe.Pointer(&buf[0]))
	h.Level, h.Type = level, typ
	h.SetLen(syscall.CmsgLen(len(payload)))
	copy(buf[syscall.CmsgLen(0):], payload)
	return buf
}

func extErrPayload(family uint16, origin, typ, code uint8, ip [4]byte) []byte {
	b := make([]byte, extendedErrLen+sockaddrInLen)
	binary.NativeEndian.PutUint32(b[0:4], uint32(syscall.EHOSTUNREACH))
	b[4], b[5], b[6] = origin, typ, code
	binary.NativeEndian.PutUint16(b[extendedErrLen:], family)
	copy(b[extendedErrLen+sockaddrInAddrOff:], ip[:])
	return b
}

const (
	solIP     = 0  // SOL_IP
	ipRecvErr = 11 // IP_RECVERR
)

func TestParseErrQueue(t *testing.T) {
	ip := [4]byte{192, 0, 2, 7}
	for _, tt := range []struct {
		name    string
		oob     []byte
		wantErr error // nil: success; errAny: any error
		want    reply
	}{
		{
			name: "time exceeded",
			oob:  errQueueMsg(solIP, ipRecvErr, extErrPayload(afInet, originICMP, icmpTimeExceeded, 0, ip)),
			want: reply{Addr: netip.AddrFrom4(ip), ICMPType: icmpTimeExceeded, Origin: originICMP, Errno: uint32(syscall.EHOSTUNREACH)},
		},
		{
			name:    "unhandled origin, AF_UNSPEC offender",
			oob:     errQueueMsg(solIP, ipRecvErr, extErrPayload(0, originLocal, 0, 0, [4]byte{})),
			wantErr: errNotAFInet,
		},
		{name: "empty", oob: nil, wantErr: errAny},
		{name: "truncated payload", oob: errQueueMsg(solIP, ipRecvErr, make([]byte, 8)), wantErr: errShortExtendedErr},
		{name: "other cmsg type", oob: errQueueMsg(solIP, 1, extErrPayload(afInet, originICMP, 11, 0, ip)), wantErr: errAny},
	} {
		t.Run(tt.name, func(t *testing.T) {
			got, err := parseErrQueue(tt.oob)
			switch {
			case tt.wantErr == nil:
				if err != nil || got != tt.want {
					t.Errorf("got %+v, %v; want %+v", got, err, tt.want)
				}
			case err == nil:
				t.Errorf("got %+v, want an error", got)
			case tt.wantErr != errAny && !errors.Is(err, tt.wantErr):
				t.Errorf("err = %v, want %v", err, tt.wantErr)
			}
		})
	}
}

var errAny = errors.New("any error")

// An error await cannot decode becomes a HopError carrying it, not a panic.
func TestUndecodableReplyBecomesHopError(t *testing.T) {
	_, perr := parseErrQueue(errQueueMsg(solIP, ipRecvErr, extErrPayload(0, originLocal, 0, 0, [4]byte{})))
	if perr == nil {
		t.Fatal("parseErrQueue accepted an AF_UNSPEC offender")
	}
	res := probeOne(context.Background(), failingConn{perr}, 4, loopback, time.Second)
	if !res.resolved || res.hop.Kind != HopError || !errors.Is(res.hop.Err, errNotAFInet) || res.hop.TTL != 4 {
		t.Errorf("got %+v, want a resolved HopError wrapping errNotAFInet at TTL 4", res)
	}
}

type failingConn struct{ err error }

func (failingConn) send() (time.Time, error) { return time.Now(), nil }
func (f failingConn) await(context.Context, time.Time) (reply, error) {
	return reply{}, f.err
}
func (failingConn) close() error { return nil }
