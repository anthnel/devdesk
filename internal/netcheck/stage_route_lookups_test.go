package netcheck

import (
	"context"
	"errors"
	"fmt"
	"net"
	"reflect"
	"sync"
	"syscall"
	"testing"

	"github.com/anthnel/devdesk/internal/nettrace"
)

// DEVDESK-473: the route stage keeps every lookup it made on its check, so the
// path trace can reuse the reading instead of asking the routing table again.
// These tests pin what is kept; the stage's verdicts, summary and facts are
// pinned by stage_route_test.go and by the golden test at the end of this file.

// countingRoute wraps a per-address answer and counts the calls it receives.
type countingRoute struct {
	mu    sync.Mutex
	calls []string
	reply func(ip net.IP) (RouteHop, error)
}

func (c *countingRoute) route(_ context.Context, ip net.IP) (RouteHop, error) {
	c.mu.Lock()
	c.calls = append(c.calls, ip.String())
	c.mu.Unlock()
	return c.reply(ip)
}

func (c *countingRoute) seen() []string {
	c.mu.Lock()
	defer c.mu.Unlock()
	return append([]string(nil), c.calls...)
}

// lookupHop is a distinct hop per interface name, so a lookup can be told apart
// from another one.
func lookupHop(iface, source, gateway string) RouteHop {
	h := RouteHop{Interface: iface, Source: net.ParseIP(source)}
	if gateway != "" {
		h.Gateway = net.ParseIP(gateway)
	}
	return h
}

func sameHop(a, b RouteHop) bool {
	return a.Interface == b.Interface && a.Source.Equal(b.Source) && a.Gateway.Equal(b.Gateway)
}

// TestRouteStageKeepsOneLookupPerAddressInOrder covers (a), (b) and (c): a
// routed address keeps its hop, a proven no-route and an undetermined lookup
// keep their error untouched, and several addresses give one entry each, in the
// order they were asked.
func TestRouteStageKeepsOneLookupPerAddressInOrder(t *testing.T) {
	const (
		a = "93.184.216.34"
		b = "93.184.216.35"
		c = "2606:2800:220:1::1"
	)
	hopA := lookupHop("eth0", "192.168.1.21", "192.168.1.1")
	hopB := lookupHop("wg0", "10.2.0.2", "")
	enetunreach := fmt.Errorf("lookup %s: %w", c, syscall.ENETUNREACH)
	generic := errors.New("routing table unreadable")

	type reply struct {
		hop RouteHop
		err error
	}
	tests := []struct {
		name    string
		ips     []string
		replies map[string]reply
		// wantNoRoute is the set of addresses whose kept error must satisfy
		// nettrace.IsNoRoute.
		wantNoRoute map[string]bool
	}{
		{
			name:    "one routed address keeps its hop and no error",
			ips:     []string{a},
			replies: map[string]reply{a: {hop: hopA}},
		},
		{
			name:        "a proven no-route keeps its error",
			ips:         []string{c},
			replies:     map[string]reply{c: {err: enetunreach}},
			wantNoRoute: map[string]bool{c: true},
		},
		{
			name:    "an undetermined lookup keeps its error and is not a proof",
			ips:     []string{a},
			replies: map[string]reply{a: {err: generic}},
		},
		{
			name: "three addresses give three entries in the order asked",
			ips:  []string{b, c, a},
			replies: map[string]reply{
				b: {hop: hopB},
				c: {err: enetunreach},
				a: {err: generic},
			},
			wantNoRoute: map[string]bool{c: true},
		},
		{
			name: "two routed addresses keep their own hop each",
			ips:  []string{a, b},
			replies: map[string]reply{
				a: {hop: hopA},
				b: {hop: hopB},
			},
		},
	}

	for _, tc := range tests {
		t.Run(tc.name, func(t *testing.T) {
			env := fakeEnv{route: func(_ context.Context, ip net.IP) (RouteHop, error) {
				r := tc.replies[ip.String()]
				return r.hop, r.err
			}}
			var ips []net.IP
			for _, s := range tc.ips {
				ips = append(ips, net.ParseIP(s))
			}
			prior := resolvedTo(t, ips...)

			check := routeCheck(t, env, target(), prior)
			fromRun := RunStep(context.Background(), target(), env, DefaultSettings(), StageRoute, prior).RouteLookups()

			for name, got := range map[string][]RouteLookup{
				"Check.Routes":           check.Routes,
				"Results.RouteLookups()": fromRun,
			} {
				if len(got) != len(tc.ips) {
					t.Fatalf("%s has %d entries, want %d (one per address, in order): %v",
						name, len(got), len(tc.ips), got)
				}
				for i, want := range tc.ips {
					l := got[i]
					if l.IP.String() != want {
						t.Errorf("%s[%d].IP = %v, want %s", name, i, l.IP, want)
					}
					r := tc.replies[want]
					if !sameHop(l.Hop, r.hop) {
						t.Errorf("%s[%d].Hop = %+v, want %+v", name, i, l.Hop, r.hop)
					}
					if !errors.Is(l.Err, r.err) && l.Err != r.err {
						t.Errorf("%s[%d].Err = %v, want %v untouched", name, i, l.Err, r.err)
					}
					if r.err == nil && l.Err != nil {
						t.Errorf("%s[%d].Err = %v, want nil for a routed address", name, i, l.Err)
					}
					if r.err != nil && l.Err == nil {
						t.Errorf("%s[%d].Err = nil, want the lookup's error", name, i)
					}
					if l.Err != nil && nettrace.IsNoRoute(l.Err) != tc.wantNoRoute[want] {
						t.Errorf("%s[%d]: IsNoRoute(%v) = %v, want %v", name, i, l.Err,
							nettrace.IsNoRoute(l.Err), tc.wantNoRoute[want])
					}
				}
			}
		})
	}
}

// TestRouteStageCallsRouteOncePerAddress pins AC5 (the number of readings is
// unchanged) and the cap: with ten addresses, eight are asked and eight are
// kept, neither more nor fewer.
func TestRouteStageCallsRouteOncePerAddress(t *testing.T) {
	tests := []struct {
		name  string
		count int
		want  int
	}{
		{"one address", 1, 1},
		{"three addresses", 3, 3},
		{"exactly the cap", maxRoutedAddresses, maxRoutedAddresses},
		{"ten addresses are capped at the lookup limit", 10, maxRoutedAddresses},
	}
	for _, tc := range tests {
		t.Run(tc.name, func(t *testing.T) {
			counter := &countingRoute{reply: func(net.IP) (RouteHop, error) {
				return lookupHop("eth0", "192.168.1.21", "192.168.1.1"), nil
			}}
			var ips []net.IP
			for i := 0; i < tc.count; i++ {
				ips = append(ips, net.IPv4(93, 184, 216, byte(i+1)))
			}

			check := routeCheck(t, fakeEnv{route: counter.route}, target(), resolvedTo(t, ips...))

			if got := len(counter.seen()); got != tc.want {
				t.Fatalf("env.Route called %d times, want %d", got, tc.want)
			}
			if len(check.Routes) != tc.want {
				t.Fatalf("Check.Routes has %d entries, want %d", len(check.Routes), tc.want)
			}
			for i, l := range check.Routes {
				if l.IP.String() != counter.seen()[i] {
					t.Errorf("Routes[%d].IP = %v, want the %dth address asked (%s)", i, l.IP, i+1, counter.seen()[i])
				}
			}
		})
	}
}

// TestRouteLookupsComeFromTheRouteCheckOfAFullRun covers (d): the accessor on a
// complete pipeline run returns what the route check kept.
func TestRouteLookupsComeFromTheRouteCheckOfAFullRun(t *testing.T) {
	hop := lookupHop("eth0", "192.168.1.21", "192.168.1.1")
	env := fakeEnv{route: func(context.Context, net.IP) (RouteHop, error) { return hop, nil }}

	res, err := Run(context.Background(), target(), env, DefaultSettings())
	if err != nil {
		t.Fatalf("Run: %v", err)
	}
	c, ok := res.Get(CheckRoute)
	if !ok {
		t.Fatal("the run produced no route check")
	}
	got := res.RouteLookups()
	if len(got) != 1 || got[0].IP.String() != "93.184.216.34" || !sameHop(got[0].Hop, hop) || got[0].Err != nil {
		t.Fatalf("RouteLookups() = %+v, want one routed lookup of 93.184.216.34 via eth0", got)
	}
	if !reflect.DeepEqual(got, c.Routes) {
		t.Fatalf("RouteLookups() = %+v differs from the route check's Routes %+v", got, c.Routes)
	}
}

// TestRouteLookupsIsEmptyWhenTheStageDidNotRun covers (d): no panic and nothing
// to reuse when the route check is absent, or is a placeholder.
func TestRouteLookupsIsEmptyWhenTheStageDidNotRun(t *testing.T) {
	failedResolve := fakeEnv{resolve: func(context.Context, string, string) ([]net.IP, error) {
		return nil, errors.New("no such host")
	}}
	skipped := RunStep(context.Background(), target(), failedResolve, DefaultSettings(), StageResolve, Results{})
	skipped = RunStep(context.Background(), target(), failedResolve, DefaultSettings(), StageRoute, skipped)

	tests := []struct {
		name string
		res  Results
	}{
		{"zero Results", Results{}},
		{"only the resolve stage ran", RunStep(context.Background(), target(), fakeEnv{}, DefaultSettings(), StageResolve, Results{})},
		{"route stage skipped after a failed resolution", skipped},
		{"route check built without lookups", ResultsOf(Check{ID: CheckRoute, Stage: StageRoute})},
	}
	for _, tc := range tests {
		t.Run(tc.name, func(t *testing.T) {
			if got := tc.res.RouteLookups(); len(got) != 0 {
				t.Fatalf("RouteLookups() = %+v, want none", got)
			}
		})
	}

	t.Run("a skipped route stage carries no lookups on its check", func(t *testing.T) {
		c, ok := skipped.Get(CheckRoute)
		if !ok {
			t.Skip("the skip rule leaves no route check behind in this build")
		}
		if c.Routes != nil {
			t.Fatalf("placeholder Routes = %+v, want nil", c.Routes)
		}
	})
}

// TestRouteLookupsReturnsACopy keeps a reader from changing what the results
// hold: the slice it gets is its own.
func TestRouteLookupsReturnsACopy(t *testing.T) {
	env := fakeEnv{route: func(_ context.Context, ip net.IP) (RouteHop, error) {
		return lookupHop("eth0", "192.168.1.21", "192.168.1.1"), nil
	}}
	res := RunStep(context.Background(), target(), env, DefaultSettings(), StageRoute,
		resolvedTo(t, net.ParseIP("93.184.216.34"), net.ParseIP("93.184.216.35")))

	first := res.RouteLookups()
	if len(first) != 2 {
		t.Fatalf("RouteLookups() has %d entries, want 2", len(first))
	}
	first[0] = RouteLookup{IP: net.ParseIP("10.0.0.1"), Err: errors.New("changed by a reader")}

	again := res.RouteLookups()
	if len(again) != 2 {
		t.Fatalf("after a reader changed its copy, RouteLookups() has %d entries, want 2", len(again))
	}
	if again[0].IP.String() != "93.184.216.34" || again[0].Err != nil {
		t.Fatalf("after a reader overwrote its copy, RouteLookups()[0] = %+v, want the original lookup", again[0])
	}
}

// TestRouteStageVisibleOutputIsUnchanged is the non-regression guard for AC5:
// keeping the lookups changes neither the verdict, the reason, the summary nor
// a single fact of the route check. The expectations below are what the stage
// produced before DEVDESK-473 (the table of TestRouteErrorsAreClassifiedProvenVersusUndetermined
// remains the reference for the classification itself).
func TestRouteStageVisibleOutputIsUnchanged(t *testing.T) {
	const (
		v4  = "93.184.216.34"
		v6  = "2606:2800:220:1::1"
		v4b = "93.184.216.35"
	)
	routed := lookupHop("eth0", "192.168.1.21", "192.168.1.1")
	textErr := errors.New("network is unreachable")

	tests := []struct {
		name        string
		ips         []string
		errs        map[string]error
		wantVerdict Verdict
		wantReason  Reason
		wantSummary string
		wantFacts   []Fact
	}{
		{
			name: "routed",
			ips:  []string{v4}, wantVerdict: OK,
			wantSummary: "Traffic leaves through eth0",
			wantFacts: []Fact{
				{Key: "Interface", Value: "eth0"},
				{Key: "Source address", Value: "192.168.1.21"},
				{Key: "Gateway", Value: "192.168.1.1"},
				{Key: "Route", Value: v4 + " via eth0 (gateway 192.168.1.1)"},
			},
		},
		{
			name: "all proven",
			ips:  []string{v4}, errs: map[string]error{v4: nettrace.ErrNoRoute},
			wantVerdict: Fail, wantSummary: "example.com has no local route",
			wantFacts: []Fact{{Key: "No route", Value: v4 + " — " + nettrace.ErrNoRoute.Error()}},
		},
		{
			name: "none routed, undetermined",
			ips:  []string{v4}, errs: map[string]error{v4: textErr},
			wantVerdict: Unknown, wantSummary: "The local route to example.com could not be determined",
			wantFacts: []Fact{{Key: "Route undetermined", Value: v4 + " — network is unreachable"}},
		},
		{
			name: "routed plus proven plus undetermined",
			ips:  []string{v4, v6, v4b}, errs: map[string]error{v6: nettrace.ErrNoRoute, v4b: textErr},
			wantVerdict: Warn, wantReason: ReasonPartialRoute,
			wantSummary: "1 of 3 addresses have no local route; route undetermined for 1",
			wantFacts: []Fact{
				{Key: "No route", Value: v6 + " — " + nettrace.ErrNoRoute.Error()},
				{Key: "Route undetermined", Value: v4b + " — network is unreachable"},
				{Key: "Interface", Value: "eth0"},
				{Key: "Source address", Value: "192.168.1.21"},
				{Key: "Gateway", Value: "192.168.1.1"},
				{Key: "Route", Value: v4 + " via eth0 (gateway 192.168.1.1)"},
			},
		},
		{
			name: "routed, one undetermined",
			ips:  []string{v4, v6}, errs: map[string]error{v6: textErr},
			wantVerdict: OK, wantReason: ReasonRouteUndetermined,
			wantSummary: "Traffic leaves through eth0; route undetermined for 1 of 2 addresses",
			wantFacts: []Fact{
				{Key: "Route undetermined", Value: v6 + " — network is unreachable"},
				{Key: "Interface", Value: "eth0"},
				{Key: "Source address", Value: "192.168.1.21"},
				{Key: "Gateway", Value: "192.168.1.1"},
				{Key: "Route", Value: v4 + " via eth0 (gateway 192.168.1.1)"},
			},
		},
	}

	for _, tc := range tests {
		t.Run(tc.name, func(t *testing.T) {
			env := fakeEnv{route: func(_ context.Context, ip net.IP) (RouteHop, error) {
				if err, bad := tc.errs[ip.String()]; bad {
					return RouteHop{}, err
				}
				return routed, nil
			}}
			var ips []net.IP
			for _, s := range tc.ips {
				ips = append(ips, net.ParseIP(s))
			}
			c := routeCheck(t, env, target(), resolvedTo(t, ips...))

			if c.Verdict != tc.wantVerdict || c.Reason != tc.wantReason {
				t.Errorf("verdict/reason = %v/%q, want %v/%q", c.Verdict, c.Reason, tc.wantVerdict, tc.wantReason)
			}
			if c.Summary != tc.wantSummary {
				t.Errorf("summary = %q, want %q", c.Summary, tc.wantSummary)
			}
			if !reflect.DeepEqual(c.Facts, tc.wantFacts) {
				t.Errorf("facts = %+v, want %+v", c.Facts, tc.wantFacts)
			}
			// The red part: the unchanged output must come with the kept lookups.
			if len(c.Routes) != len(tc.ips) {
				t.Errorf("Routes has %d entries, want %d alongside the unchanged output", len(c.Routes), len(tc.ips))
			}
		})
	}
}
