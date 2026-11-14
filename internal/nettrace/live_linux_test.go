//go:build linux

package nettrace

import (
	"context"
	"net/netip"
	"os"
	"testing"
	"time"
)

// TestLiveTraceToCloudflare runs a real trace to 1.1.1.1 with the default
// options. It needs the internet, so it is skipped unless
// DEVDESK_NETTRACE_LIVE=1, and never in CI.
func TestLiveTraceToCloudflare(t *testing.T) {
	if os.Getenv("DEVDESK_NETTRACE_LIVE") != "1" {
		t.Skip("set DEVDESK_NETTRACE_LIVE=1 to run the live trace")
	}
	if os.Getenv("CI") != "" {
		t.Skip("the live trace never runs in CI")
	}

	ctx, cancel := context.WithTimeout(context.Background(), 30*time.Second)
	defer cancel()

	res, err := Trace(ctx, netip.MustParseAddr("1.1.1.1"), Options{})
	if err != nil {
		t.Fatalf("Trace: %v", err)
	}

	t.Logf("uid=%d outcome=%s duration=%s hops=%d", os.Getuid(), res.Outcome, res.Duration, len(res.Hops))
	for _, h := range res.Hops {
		addr := "-"
		if h.Addr.IsValid() {
			addr = h.Addr.String()
		}
		t.Logf("ttl=%-2d kind=%-11s addr=%-16s rtt=%s icmp=%d/%d err=%v",
			h.TTL, h.Kind, addr, h.RTT, h.ICMPType, h.ICMPCode, h.Err)
	}

	switch res.Outcome {
	case OutcomeReached:
	case OutcomeMaxHops, OutcomeUnreachable:
		t.Logf("NOTE: the network filtered the trace (outcome %s), not a failure", res.Outcome)
	default:
		t.Errorf("outcome = %s, want reached (or max hops / unreachable if filtered)", res.Outcome)
	}

	answered, prev := 0, 0
	for _, h := range res.Hops {
		if h.TTL <= prev {
			t.Errorf("hop TTLs not increasing: %d after %d", h.TTL, prev)
		}
		prev = h.TTL
		if (h.Kind == HopRouter || h.Kind == HopTarget) && h.Addr.IsValid() && !h.Addr.IsUnspecified() {
			answered++
		}
	}
	if answered < 3 {
		t.Errorf("%d hops with an address, want at least 3", answered)
	}
	if res.Duration >= 5*time.Second {
		t.Errorf("duration = %s, want < 5s", res.Duration)
	}
}
