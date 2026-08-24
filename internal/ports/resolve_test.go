package ports

import (
	"context"
	"errors"
	"strings"
	"sync"
	"sync/atomic"
	"testing"
)

// withLookup installs a fake resolver and an empty cache for one test, and puts
// both back afterwards. The cache is package-level because the lookups run
// inside a Cmd, which may not touch the model (Rule 110) — so a test that left
// it populated would decide the next one's outcome.
func withLookup(t *testing.T, lookup func(context.Context, string) ([]string, error)) {
	t.Helper()
	prev := resolveLookupAddr
	resolveLookupAddr = lookup
	reverseMu.Lock()
	reverseCache = map[string]string{}
	reverseMu.Unlock()
	t.Cleanup(func() {
		resolveLookupAddr = prev
		reverseMu.Lock()
		reverseCache = map[string]string{}
		reverseMu.Unlock()
	})
}

// The port is not resolved with the host. `ss` without -n also turned 22 into
// "ssh", and that half is deliberately not reproduced: Go resolves a name to a
// port and not the other way round, so honouring it would mean shipping a copy
// of /etc/services and calling the result the system's answer.
func TestOnlyTheHostHalfIsResolved(t *testing.T) {
	withLookup(t, func(_ context.Context, ip string) ([]string, error) {
		return []string{"example.com."}, nil
	})

	sockets := []Socket{{LocalAddr: "10.0.0.5:22", PeerAddr: "93.184.216.34:52344"}}
	resolveAddrs(context.Background(), sockets)

	if sockets[0].LocalAddr != "example.com:22" {
		t.Errorf("local = %q, want example.com:22", sockets[0].LocalAddr)
	}
	if sockets[0].PeerAddr != "example.com:52344" {
		t.Errorf("peer = %q, want example.com:52344", sockets[0].PeerAddr)
	}
}

// The trailing dot of a fully-qualified name is DNS notation, not part of what a
// person reads.
func TestTheTrailingDotOfAFullyQualifiedNameIsDropped(t *testing.T) {
	withLookup(t, func(context.Context, string) ([]string, error) {
		return []string{"host.example.com."}, nil
	})
	if got := lookupOne(context.Background(), "10.0.0.5"); got != "host.example.com" {
		t.Errorf("lookupOne = %q, want the name without its root dot", got)
	}
}

// A wildcard, a loopback and an unspecified address name no host worth asking
// about: resolving them would put a query on the wire for a socket that never
// leaves the machine, every two seconds.
func TestTheAddressesWorthNoQueryAreNeverAsked(t *testing.T) {
	var asked []string
	var mu sync.Mutex
	withLookup(t, func(_ context.Context, ip string) ([]string, error) {
		mu.Lock()
		asked = append(asked, ip)
		mu.Unlock()
		return []string{"somewhere"}, nil
	})

	sockets := []Socket{
		{LocalAddr: "0.0.0.0:445", PeerAddr: "0.0.0.0:*"},
		{LocalAddr: "[::]:111", PeerAddr: "[::]:*"},
		{LocalAddr: "127.0.0.1:6463", PeerAddr: "127.0.0.1:52001"},
	}
	resolveAddrs(context.Background(), sockets)

	if len(asked) != 0 {
		t.Errorf("asked reverse DNS for %v; none of those name a host", asked)
	}
	if sockets[0].LocalAddr != "0.0.0.0:445" || sockets[1].LocalAddr != "[::]:111" {
		t.Errorf("an address nobody asked about was rewritten anyway: %+v", sockets)
	}
}

// An address that does not resolve is cached as itself. Without that, a machine
// talking to hosts with no PTR record re-asks for every one of them on every
// refresh — which is the storm the cache exists to prevent, arriving through the
// failures instead of the successes.
func TestAnAddressThatDoesNotResolveIsNotAskedTwice(t *testing.T) {
	var calls atomic.Int32
	withLookup(t, func(context.Context, string) ([]string, error) {
		calls.Add(1)
		return nil, errors.New("no PTR record")
	})

	sockets := []Socket{{LocalAddr: "10.0.0.5:22", PeerAddr: "0.0.0.0:*"}}
	resolveAddrs(context.Background(), sockets)
	resolveAddrs(context.Background(), sockets)

	if got := calls.Load(); got != 1 {
		t.Errorf("%d lookups for one unresolvable address across two passes, want 1", got)
	}
	if sockets[0].LocalAddr != "10.0.0.5:22" {
		t.Errorf("local = %q; an address that did not resolve must stay as it was", sockets[0].LocalAddr)
	}
}

// One address asked for by many sockets is one query, not one per row: an
// established connection to a busy host shows up on as many rows as it has
// sockets.
func TestOneAddressIsAskedOnceHoweverManyRowsCarryIt(t *testing.T) {
	var calls atomic.Int32
	withLookup(t, func(context.Context, string) ([]string, error) {
		calls.Add(1)
		return []string{"busy.example.com"}, nil
	})

	sockets := make([]Socket, 20)
	for i := range sockets {
		sockets[i] = Socket{LocalAddr: "10.0.0.5:0", PeerAddr: "93.184.216.34:443"}
	}
	resolveAddrs(context.Background(), sockets)

	if got := calls.Load(); got != 2 {
		t.Errorf("%d lookups for two distinct addresses across 20 rows, want 2", got)
	}
}

// The whole pass is bounded by the caller's context, which is what stops a
// resolution outliving the refresh interval that started it.
func TestACancelledContextEndsTheResolutionPass(t *testing.T) {
	ctx, cancel := context.WithCancel(context.Background())
	cancel()

	var calls atomic.Int32
	withLookup(t, func(context.Context, string) ([]string, error) {
		calls.Add(1)
		return []string{"slow.example.com"}, nil
	})

	sockets := make([]Socket, 200)
	for i := range sockets {
		// A distinct address per row, so there is real work to abandon.
		sockets[i] = Socket{LocalAddr: "10.0.0.5:0", PeerAddr: addrFor(i)}
	}
	resolveAddrs(ctx, sockets)

	if got := calls.Load(); got >= 200 {
		t.Errorf("%d lookups ran under a cancelled context", got)
	}
}

// addrFor builds a distinct peer address per row.
func addrFor(i int) string {
	return "93.184." + itoa(i/256) + "." + itoa(i%256) + ":443"
}

func itoa(n int) string {
	if n == 0 {
		return "0"
	}
	var b strings.Builder
	var digits []byte
	for n > 0 {
		digits = append(digits, byte('0'+n%10))
		n /= 10
	}
	for i := len(digits) - 1; i >= 0; i-- {
		b.WriteByte(digits[i])
	}
	return b.String()
}
