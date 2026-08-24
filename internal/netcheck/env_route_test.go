package netcheck

import (
	"context"
	"net"
	"testing"
)

// TestThisMachineCanRouteToItsOwnLoopback exercises the real implementation
// rather than a fake. Loopback is the one destination every machine routes,
// container and CI runner included, and the lookup sends no packet — so this
// asserts that go-netroute answers on whichever platform the suite runs, which
// is the whole claim the three-implementations-in-one rests on.
func TestThisMachineCanRouteToItsOwnLoopback(t *testing.T) {
	env := SystemEnv(DefaultSettings())

	hop, err := env.Route(context.Background(), net.ParseIP("127.0.0.1"))
	if err != nil {
		t.Fatalf("routing to 127.0.0.1 failed: %v", err)
	}
	if hop.Interface == "" && hop.Source == nil {
		t.Fatalf("the route to 127.0.0.1 came back with neither an interface nor a source")
	}
}

// TestRoutingToNothingIsRefusedRatherThanGuessed keeps a nil address from
// reaching the platform call, where each of the three has its own idea of what
// it means.
func TestRoutingToNothingIsRefusedRatherThanGuessed(t *testing.T) {
	env := SystemEnv(DefaultSettings())

	if _, err := env.Route(context.Background(), nil); err == nil {
		t.Fatal("routing to a nil address succeeded, want an error")
	}
}

// TestACancelledContextStopsBeforeTheLookup fixes the one thing the context can
// do here: the lookup is a syscall with no wait to interrupt, so it is honoured
// before the call or not at all.
func TestACancelledContextStopsBeforeTheLookup(t *testing.T) {
	ctx, cancel := context.WithCancel(context.Background())
	cancel()

	if _, err := SystemEnv(DefaultSettings()).Route(ctx, net.ParseIP("127.0.0.1")); err == nil {
		t.Fatal("a cancelled context did not stop the lookup")
	}
}
