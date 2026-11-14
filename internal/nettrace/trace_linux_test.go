//go:build linux

package nettrace

import (
	"context"
	"errors"
	"net/netip"
	"testing"
)

func TestSupportedOnLinux(t *testing.T) {
	if err := Supported(); err != nil {
		t.Errorf("Supported() = %v, want nil on linux", err)
	}
}

func TestTraceRejectsNonIPv4(t *testing.T) {
	for _, tt := range []struct {
		name   string
		target netip.Addr
	}{
		{"pure IPv6", netip.MustParseAddr("2001:db8::1")},
		{"IPv6 loopback", netip.MustParseAddr("::1")},
		{"zero address", netip.Addr{}},
	} {
		t.Run(tt.name, func(t *testing.T) {
			res, err := Trace(context.Background(), tt.target, Options{})
			if !errors.Is(err, ErrNotIPv4) {
				t.Fatalf("err = %v, want ErrNotIPv4", err)
			}
			if len(res.Hops) != 0 {
				t.Errorf("got %d hops, want none: nothing may be sent", len(res.Hops))
			}
		})
	}
}

func TestTraceRejectsNonUnicast(t *testing.T) {
	for _, tt := range []struct{ name, target string }{
		{"unspecified", "0.0.0.0"},
		{"multicast low end", "224.0.0.1"},
		{"multicast high end", "239.255.255.255"},
		{"limited broadcast", "255.255.255.255"},
		{"IPv4-mapped multicast", "::ffff:224.0.0.1"},
	} {
		t.Run(tt.name, func(t *testing.T) {
			res, err := Trace(context.Background(), netip.MustParseAddr(tt.target), Options{})
			if !errors.Is(err, ErrNotUnicast) {
				t.Fatalf("err = %v, want ErrNotUnicast", err)
			}
			if len(res.Hops) != 0 {
				t.Errorf("got %d hops, want none: nothing may be sent", len(res.Hops))
			}
		})
	}
}

// An IPv4-mapped IPv6 address is converted, so it must get past the
// ErrNotIPv4 check. The context is already canceled so that nothing waits on
// the network; whatever else Trace returns is not this test's business.
func TestTraceAcceptsIPv4Mapped(t *testing.T) {
	ctx, cancel := context.WithCancel(context.Background())
	cancel()
	_, err := Trace(ctx, netip.MustParseAddr("::ffff:192.0.2.1"), Options{})
	if errors.Is(err, ErrNotIPv4) {
		t.Errorf("an IPv4-mapped address must be converted, got %v", err)
	}
}
