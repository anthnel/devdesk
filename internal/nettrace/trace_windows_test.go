//go:build windows

package nettrace

import (
	"context"
	"errors"
	"net/netip"
	"testing"
	"time"
	"unsafe"

	"golang.org/x/sys/windows"
)

func TestSupportedWindows(t *testing.T) {
	if err := Supported(); err != nil {
		t.Fatalf("Supported() = %v, want nil (iphlpapi echo procs must resolve)", err)
	}
}

// TestEchoReplyLayout pins the mirror structs to the Windows SDK layout
// (ipexport.h ICMP_ECHO_REPLY and IP_OPTION_INFORMATION), 32 and 64 bits.
func TestEchoReplyLayout(t *testing.T) {
	var r icmpEchoReply
	var o ipOptionInformation
	ptr := unsafe.Sizeof(uintptr(0))

	// Data sits at 16 on both widths; Options follows it.
	wantOpts, wantSize := uintptr(24), uintptr(40) // 64-bit
	if ptr == 4 {
		wantOpts, wantSize = 20, 28
	}

	checks := []struct {
		name      string
		got, want uintptr
	}{
		{"Address", unsafe.Offsetof(r.Address), 0},
		{"Status", unsafe.Offsetof(r.Status), 4},
		{"RoundTripTime", unsafe.Offsetof(r.RoundTripTime), 8},
		{"DataSize", unsafe.Offsetof(r.DataSize), 12},
		{"Reserved", unsafe.Offsetof(r.Reserved), 14},
		{"Data", unsafe.Offsetof(r.Data), 16},
		{"Options", unsafe.Offsetof(r.Options), wantOpts},
		{"sizeof(icmpEchoReply)", unsafe.Sizeof(r), wantSize},
		{"Options.Ttl", unsafe.Offsetof(o.Ttl), 0},
		{"Options.Tos", unsafe.Offsetof(o.Tos), 1},
		{"Options.Flags", unsafe.Offsetof(o.Flags), 2},
		{"Options.OptionsSize", unsafe.Offsetof(o.OptionsSize), 3},
		{"Options.OptionsData", unsafe.Offsetof(o.OptionsData), ptr},
		{"sizeof(ipOptionInformation)", unsafe.Sizeof(o), 2 * ptr},
	}
	for _, c := range checks {
		if c.got != c.want {
			t.Errorf("%s = %d, want %d (pointer size %d)", c.name, c.got, c.want, ptr)
		}
	}
}

func TestSystemProberIsEchoProber(t *testing.T) {
	p, ok := systemProber().(*echoProber)
	if !ok {
		t.Fatalf("systemProber() = %T, want *echoProber", systemProber())
	}
	if _, ok := p.api.(winEcho); !ok {
		t.Errorf("api = %T, want winEcho", p.api)
	}
	if p.preflight == nil {
		t.Error("preflight is nil, want udpPreflight")
	}
}

func TestUDPPreflightLoopbackHasARoute(t *testing.T) {
	if err := udpPreflight(context.Background(), netip.MustParseAddr("127.0.0.1")); err != nil {
		t.Fatalf("udpPreflight(127.0.0.1) = %v, want nil", err)
	}
}

// A no-route failure from the preflight must reach the caller as ErrNoRoute,
// before any echo handle is created.
func TestEchoProberNoRouteFromPreflight(t *testing.T) {
	api := &fakeCreateCounter{}
	p := &echoProber{
		api: api,
		preflight: func(context.Context, netip.Addr) error {
			return windows.WSAENETUNREACH
		},
	}
	_, err := p.open(context.Background(), netip.MustParseAddr("192.0.2.1"), 1)
	if !errors.Is(err, ErrNoRoute) {
		t.Fatalf("open err = %v, want ErrNoRoute", err)
	}
	if api.created != 0 {
		t.Errorf("created %d handles before the preflight verdict, want 0", api.created)
	}
}

type fakeCreateCounter struct{ created int }

func (f *fakeCreateCounter) create() (uintptr, error) { f.created++; return 1, nil }
func (f *fakeCreateCounter) echo(uintptr, netip.Addr, int, time.Duration) (reply, error) {
	return reply{}, errSilent
}
func (f *fakeCreateCounter) closeHandle(uintptr) error { return nil }

// A real echo to loopback through IcmpSendEcho2, no external network.
func TestTraceLoopbackReached(t *testing.T) {
	ctx, cancel := context.WithTimeout(context.Background(), 10*time.Second)
	defer cancel()

	var streamed []Hop
	res, err := TraceEach(ctx, netip.MustParseAddr("127.0.0.1"), Options{ProbeTimeout: 2 * time.Second},
		func(h Hop) { streamed = append(streamed, h) })
	if err != nil {
		t.Fatalf("TraceEach: %v", err)
	}
	if res.Outcome != OutcomeReached {
		t.Fatalf("outcome = %s, want reached", res.Outcome)
	}
	if len(res.Hops) == 0 {
		t.Fatal("no hops")
	}
	last := res.Hops[len(res.Hops)-1]
	first := res.Hops[0]
	if first.TTL != 1 || first.Kind != HopTarget || first.Addr != netip.MustParseAddr("127.0.0.1") {
		t.Errorf("first hop = %+v, want TTL 1, HopTarget, 127.0.0.1", first)
	}
	if last.Kind != HopTarget {
		t.Errorf("last hop kind = %s, want target", last.Kind)
	}
	if len(streamed) == 0 {
		t.Error("onHop never called")
	}
}

func TestTraceRefusesNonIPv4Windows(t *testing.T) {
	_, err := Trace(context.Background(), netip.MustParseAddr("2001:db8::1"), Options{})
	if !errors.Is(err, ErrNotIPv4) {
		t.Errorf("err = %v, want ErrNotIPv4", err)
	}
}
