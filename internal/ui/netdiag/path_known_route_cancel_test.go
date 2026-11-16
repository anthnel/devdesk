package netdiag

import (
	"context"
	"errors"
	"fmt"
	"net/netip"
	"testing"
)

// A lookup that ended on the caller's own cancellation or deadline proves
// nothing about the routing table. Reusing it would make the trace show a false
// "undetermined", so it is not a known reading: the real route is called.
func TestRouteFirstIgnoresACanceledOrExpiredLookup(t *testing.T) {
	tests := []struct {
		name string
		err  error
	}{
		{"canceled", context.Canceled},
		{"canceled and wrapped", fmt.Errorf("route 93.184.216.34: %w", context.Canceled)},
		{"deadline exceeded", context.DeadlineExceeded},
		{"deadline exceeded and wrapped", fmt.Errorf("route 93.184.216.34: %w", context.DeadlineExceeded)},
	}
	addr := netip.MustParseAddr("93.184.216.34")
	for _, tc := range tests {
		t.Run(tc.name, func(t *testing.T) {
			known := knownFor(knownHost, knownFailure("93.184.216.34", tc.err))

			if _, ok := known.find(knownHost, addr); ok {
				t.Errorf("find() = found, want not found for %v", tc.err)
			}

			rc := newReadCounter()
			hop, err := known.routeFirst(knownHost, rc.read)(context.Background(), addr)

			if rc.calls != 1 {
				t.Errorf("real route calls = %d, want 1", rc.calls)
			}
			if err != nil || hop.Interface != "real" {
				t.Errorf("route() = %+v, %v; want the real route's answer", hop, err)
			}
		})
	}
}

// A failure that is not a cancellation stays a known reading.
func TestRouteFirstKeepsAnOrdinaryErrorKnown(t *testing.T) {
	known := knownFor(knownHost, knownFailure("93.184.216.34", errors.New("routing table unreadable")))

	if _, ok := known.find(knownHost, netip.MustParseAddr("93.184.216.34")); !ok {
		t.Error("find() = not found, want the failed lookup")
	}
}
