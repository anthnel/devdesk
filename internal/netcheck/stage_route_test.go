package netcheck

import (
	"context"
	"errors"
	"net"
	"strings"
	"testing"
)

// routeCheck runs the route stage against prior and returns its one check.
func routeCheck(t *testing.T, env Env, tg Target, prior Results) Check {
	t.Helper()
	checks := runRoute(context.Background(), tg, env, DefaultSettings(), &prior)
	if len(checks) != 1 {
		t.Fatalf("route stage produced %d checks, want 1", len(checks))
	}
	return checks[0]
}

// resolvedTo builds the prior Results a resolve stage would have left behind.
func resolvedTo(t *testing.T, ips ...net.IP) Results {
	t.Helper()
	env := fakeEnv{resolve: func(context.Context, string, string) ([]net.IP, error) { return ips, nil }}
	var prior Results
	return ResultsOf(runResolve(context.Background(), target(), env, DefaultSettings(), &prior)...)
}

// TestTheRouteStageReadsWhatTheResolveStageWrote pins the coupling this stage
// rests on. It takes its addresses from the resolve check's facts rather than
// resolving again, so a change to how an address is written there would
// silently leave this stage with nothing to route to — and the check would go
// Unknown on a machine whose routing table is perfectly readable.
func TestTheRouteStageReadsWhatTheResolveStageWrote(t *testing.T) {
	prior := resolvedTo(t, net.ParseIP("93.184.216.34"), net.ParseIP("2606:2800:220:1::1"))

	got := targetAddresses(target(), &prior)
	if len(got) != 2 {
		t.Fatalf("targetAddresses read %d addresses from the resolve check, want 2: %v", len(got), got)
	}
	if got[0].String() != "93.184.216.34" || got[1].String() != "2606:2800:220:1::1" {
		t.Fatalf("addresses = %v, want the two the resolve stage recorded", got)
	}
}

// TestALiteralTargetIsItsOwnAddress covers the case where there is no resolve
// check to read: the target is already an address.
func TestALiteralTargetIsItsOwnAddress(t *testing.T) {
	var none Results
	got := targetAddresses(Target{Host: "10.2.3.4", Port: 443}, &none)
	if len(got) != 1 || got[0].String() != "10.2.3.4" {
		t.Fatalf("addresses = %v, want [10.2.3.4]", got)
	}
}

// TestTheWayOutIsNamedByItsInterface fixes what the summary says. The gateway
// is in the facts; the interface is what the user recognises.
func TestTheWayOutIsNamedByItsInterface(t *testing.T) {
	env := fakeEnv{route: func(context.Context, net.IP) (RouteHop, error) {
		return RouteHop{
			Interface: "ProtonVPN",
			Source:    net.ParseIP("10.2.0.2"),
		}, nil
	}}

	c := routeCheck(t, env, target(), resolvedTo(t, net.ParseIP("93.184.216.34")))
	if c.Verdict != OK {
		t.Fatalf("verdict = %v (%s), want OK", c.Verdict, c.Summary)
	}
	if !strings.Contains(c.Summary, "ProtonVPN") {
		t.Fatalf("summary = %q, want the interface named", c.Summary)
	}
	if factValue(c, "Source address") != "10.2.0.2" {
		t.Fatalf("source address fact = %q, want 10.2.0.2", factValue(c, "Source address"))
	}
}

// TestADirectlyConnectedTargetSaysSoRatherThanNothing covers the empty gateway.
// A blank line where a gateway would be reads as a missing measurement; "no
// gateway" is an answer, and it rules out every hop beyond the local segment.
func TestADirectlyConnectedTargetSaysSoRatherThanNothing(t *testing.T) {
	env := fakeEnv{route: func(context.Context, net.IP) (RouteHop, error) {
		return RouteHop{Interface: "Ethernet 2", Source: net.ParseIP("192.168.1.21")}, nil
	}}

	c := routeCheck(t, env, Target{Host: "192.168.1.1", Port: 443}, Results{})
	if got := factValue(c, "Gateway"); got != "directly connected" {
		t.Fatalf("gateway fact = %q, want it to state the absence", got)
	}
}

// TestARouteLookupThatFailsIsUnknownAndNeverFail is D20 in this stage. A
// lookup that could not answer is not a machine with no route, and reporting
// the first as the second is precisely the defect this codebase keeps finding.
func TestARouteLookupThatFailsIsUnknownAndNeverFail(t *testing.T) {
	env := fakeEnv{route: func(context.Context, net.IP) (RouteHop, error) {
		return RouteHop{}, errors.New("network is unreachable")
	}}

	c := routeCheck(t, env, target(), resolvedTo(t, net.ParseIP("93.184.216.34")))
	if c.Verdict != Unknown {
		t.Fatalf("verdict = %v (%s), want Unknown", c.Verdict, c.Summary)
	}
	if strings.Contains(strings.ToLower(c.Summary), "no route") {
		t.Fatalf("summary = %q — it must not claim there is no route", c.Summary)
	}
}

// TestOneFamilyWithoutARouteWarnsRatherThanHidingIt is the IPv6 case, which is
// the one worth reporting: a client that prefers IPv6 hangs before falling
// back, and every other check in the run reports the symptom instead.
func TestOneFamilyWithoutARouteWarnsRatherThanHidingIt(t *testing.T) {
	env := fakeEnv{route: func(_ context.Context, ip net.IP) (RouteHop, error) {
		if ip.To4() == nil {
			return RouteHop{}, errors.New("network is unreachable")
		}
		return RouteHop{Interface: "Ethernet 2", Source: net.ParseIP("192.168.1.21")}, nil
	}}

	prior := resolvedTo(t, net.ParseIP("93.184.216.34"), net.ParseIP("2606:2800:220:1::1"))
	c := routeCheck(t, env, target(), prior)

	if c.Verdict != Warn || c.Reason != ReasonPartialRoute {
		t.Fatalf("verdict = %v/%q, want Warn/partial-route (%s)", c.Verdict, c.Reason, c.Summary)
	}
	if !strings.Contains(c.Summary, "1 of 2") {
		t.Fatalf("summary = %q, want it to count what is missing", c.Summary)
	}
	if factValue(c, "No route") == "" {
		t.Fatalf("the address without a route is not named in the facts")
	}
}

// TestTheRouteStageGatesNothing keeps a machine whose routing table cannot be
// read from losing every check below it. The reach stage makes the same
// promise for the same reason.
func TestTheRouteStageGatesNothing(t *testing.T) {
	for _, s := range stages() {
		if s.id == StageRoute && s.gate != "" {
			t.Fatalf("the route stage gates %q — a routing table that cannot be read "+
				"must not cascade over the port and the certificate", s.gate)
		}
	}
}

// TestOnlyTheFirstAddressesAreRouted bounds the work for a name behind a large
// pool. The question is which way out, not how many addresses there are.
func TestOnlyTheFirstAddressesAreRouted(t *testing.T) {
	var many []net.IP
	for i := 0; i < maxRoutedAddresses+5; i++ {
		many = append(many, net.IPv4(93, 184, 216, byte(i)))
	}

	got := targetAddresses(target(), func() *Results { r := resolvedTo(t, many...); return &r }())
	if len(got) != maxRoutedAddresses {
		t.Fatalf("routed %d addresses, want the cap of %d", len(got), maxRoutedAddresses)
	}
}

// factValue returns the first value recorded under key, or "".
func factValue(c Check, key string) string {
	for _, f := range c.Facts {
		if f.Key == key {
			return f.Value
		}
	}
	return ""
}
