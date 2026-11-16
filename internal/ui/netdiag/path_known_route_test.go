package netdiag

import (
	"context"
	"errors"
	"fmt"
	"net"
	"net/netip"
	"reflect"
	"syscall"
	"testing"

	"github.com/anthnel/devdesk/internal/netcheck"
	"github.com/anthnel/devdesk/internal/nettrace"
)

// DEVDESK-473, step 2: the copy of Diagnostics' route reads the path trace
// holds (pathKnownRoutes) and the route function that answers from it first.
// Everything here is pure: no engine, no model, no goroutine.

const knownHost = "a.example"

// knownHop is the hop a Diagnostics lookup recorded.
func knownHop(iface string) netcheck.RouteHop {
	return netcheck.RouteHop{
		Interface: iface,
		Source:    net.ParseIP("192.168.1.21"),
		Gateway:   net.ParseIP("192.168.1.1"),
	}
}

// knownLookup is one recorded route read. ip is a textual address; net.ParseIP
// gives a 16-byte form for an IPv4, as the route stage's own addresses do.
func knownLookup(ip, iface string) netcheck.RouteLookup {
	return netcheck.RouteLookup{IP: net.ParseIP(ip), Hop: knownHop(iface)}
}

func knownFailure(ip string, err error) netcheck.RouteLookup {
	return netcheck.RouteLookup{IP: net.ParseIP(ip), Err: err}
}

func knownFor(host string, lookups ...netcheck.RouteLookup) pathKnownRoutes {
	return pathKnownRoutes{host: host, lookups: lookups}
}

// cloneLookups copies the lookups deeply enough to detect a write into them.
func cloneLookups(in []netcheck.RouteLookup) []netcheck.RouteLookup {
	if in == nil {
		return nil
	}
	out := make([]netcheck.RouteLookup, len(in))
	for i, l := range in {
		out[i] = netcheck.RouteLookup{
			IP: append(net.IP(nil), l.IP...),
			Hop: netcheck.RouteHop{
				Interface: l.Hop.Interface,
				Source:    append(net.IP(nil), l.Hop.Source...),
				Gateway:   append(net.IP(nil), l.Hop.Gateway...),
			},
			Err: l.Err,
		}
	}
	return out
}

// readCounter is a real route function that counts its calls and records the
// addresses it was asked for. Its answer is recognizable: interface "real".
type readCounter struct {
	calls int
	addrs []netip.Addr
	hop   netcheck.RouteHop
	err   error
}

func newReadCounter() *readCounter {
	return &readCounter{hop: knownHop("real")}
}

func (r *readCounter) read(_ context.Context, a netip.Addr) (netcheck.RouteHop, error) {
	r.calls++
	r.addrs = append(r.addrs, a)
	return r.hop, r.err
}

// routeResultsWith builds Results whose route check carries lookups.
func routeResultsWith(lookups ...netcheck.RouteLookup) netcheck.Results {
	return netcheck.ResultsOf(netcheck.Check{ID: netcheck.CheckRoute, Verdict: netcheck.OK, Routes: lookups})
}

func TestRouteFirstAnswersFromTheCopyOrReadsOnce(t *testing.T) {
	routed := knownLookup("93.184.216.34", "eth0")
	other := knownLookup("93.184.216.35", "wg0")
	mapped := netip.AddrFrom16(netip.MustParseAddr("93.184.216.34").As16()) // ::ffff:93.184.216.34

	tests := []struct {
		name      string
		known     pathKnownRoutes
		host      string
		addr      netip.Addr
		wantCalls int
		wantHop   string // interface of the hop the caller must get
	}{
		{
			name:  "an address that was read is not read again",
			known: knownFor(knownHost, routed, other),
			host:  knownHost, addr: netip.MustParseAddr("93.184.216.34"),
			wantCalls: 0, wantHop: "eth0",
		},
		{
			name:  "the lookup of the traced address is used, not the first one",
			known: knownFor(knownHost, routed, other),
			host:  knownHost, addr: netip.MustParseAddr("93.184.216.35"),
			wantCalls: 0, wantHop: "wg0",
		},
		{
			name:  "an address that was not read is read once",
			known: knownFor(knownHost, routed),
			host:  knownHost, addr: netip.MustParseAddr("203.0.113.9"),
			wantCalls: 1, wantHop: "real",
		},
		{
			name:  "another host reads even when the address matches",
			known: knownFor(knownHost, routed),
			host:  "b.example", addr: netip.MustParseAddr("93.184.216.34"),
			wantCalls: 1, wantHop: "real",
		},
		{
			name:  "an empty copy reads",
			known: pathKnownRoutes{},
			host:  knownHost, addr: netip.MustParseAddr("93.184.216.34"),
			wantCalls: 1, wantHop: "real",
		},
		{
			name:  "a copy for the host with no lookup reads",
			known: knownFor(knownHost),
			host:  knownHost, addr: netip.MustParseAddr("93.184.216.34"),
			wantCalls: 1, wantHop: "real",
		},
		{
			name:  "an empty host never matches an empty copy",
			known: pathKnownRoutes{},
			host:  "", addr: netip.MustParseAddr("93.184.216.34"),
			wantCalls: 1, wantHop: "real",
		},
		{
			name:  "an IPv4-mapped IPv6 address finds the IPv4 lookup",
			known: knownFor(knownHost, routed),
			host:  knownHost, addr: mapped,
			wantCalls: 0, wantHop: "eth0",
		},
	}
	for _, tc := range tests {
		t.Run(tc.name, func(t *testing.T) {
			rc := newReadCounter()
			route := tc.known.routeFirst(tc.host, rc.read)

			hop, err := route(context.Background(), tc.addr)
			if err != nil {
				t.Fatalf("route() error = %v, want nil", err)
			}
			if rc.calls != tc.wantCalls {
				t.Errorf("real route calls = %d, want %d", rc.calls, tc.wantCalls)
			}
			if hop.Interface != tc.wantHop {
				t.Errorf("hop interface = %q, want %q", hop.Interface, tc.wantHop)
			}
			if tc.wantCalls == 1 && (len(rc.addrs) != 1 || rc.addrs[0] != tc.addr) {
				t.Errorf("real route asked for %v, want exactly [%v]", rc.addrs, tc.addr)
			}
		})
	}
}

func TestRouteFirstGivesBackTheRecordedHopAsIs(t *testing.T) {
	known := knownFor(knownHost, knownLookup("93.184.216.34", "eth0"))
	rc := newReadCounter()

	hop, err := known.routeFirst(knownHost, rc.read)(context.Background(), netip.MustParseAddr("93.184.216.34"))

	if err != nil {
		t.Fatalf("err = %v, want nil", err)
	}
	if want := knownHop("eth0"); !reflect.DeepEqual(hop, want) {
		t.Errorf("hop = %+v, want %+v", hop, want)
	}
	if rc.calls != 0 {
		t.Errorf("real route calls = %d, want 0", rc.calls)
	}
}

// AC4 at unit level: the error comes back untouched, so the trace classifies
// it exactly as Diagnostics did (nettrace.IsNoRoute).
func TestRouteFirstReturnsAKnownErrorAsIs(t *testing.T) {
	noRoute := fmt.Errorf("route 93.184.216.34: %w", syscall.ENETUNREACH)
	hostUnreachable := fmt.Errorf("route 93.184.216.34: %w", syscall.EHOSTUNREACH)
	undetermined := errors.New("routing table unreadable")

	tests := []struct {
		name        string
		err         error
		wantNoRoute bool
	}{
		{"no route (network unreachable)", noRoute, true},
		{"no route (host unreachable)", hostUnreachable, true},
		{"undetermined", undetermined, false},
	}
	for _, tc := range tests {
		t.Run(tc.name, func(t *testing.T) {
			known := knownFor(knownHost, knownFailure("93.184.216.34", tc.err))
			rc := newReadCounter()

			hop, err := known.routeFirst(knownHost, rc.read)(context.Background(), netip.MustParseAddr("93.184.216.34"))

			if rc.calls != 0 {
				t.Errorf("real route calls = %d, want 0", rc.calls)
			}
			if err != tc.err {
				t.Fatalf("err = %v, want the very same error %v", err, tc.err)
			}
			if !reflect.DeepEqual(hop, netcheck.RouteHop{}) {
				t.Errorf("hop = %+v, want the zero hop for a failed lookup", hop)
			}
			if got := nettrace.IsNoRoute(err); got != tc.wantNoRoute {
				t.Errorf("nettrace.IsNoRoute(err) = %v, want %v", got, tc.wantNoRoute)
			}
			pr := newPathRoute(hop, err)
			if pr.Known || pr.NoRoute != tc.wantNoRoute {
				t.Errorf("newPathRoute = %+v, want Known=false NoRoute=%v", pr, tc.wantNoRoute)
			}
		})
	}
}

func TestRouteFirstNeverWritesIntoTheCopy(t *testing.T) {
	known := knownFor(knownHost,
		knownLookup("93.184.216.34", "eth0"),
		knownFailure("93.184.216.35", syscall.ENETUNREACH),
	)
	before := cloneLookups(known.lookups)
	rc := newReadCounter()
	route := known.routeFirst(knownHost, rc.read)
	other := known.routeFirst("b.example", rc.read)

	for _, ip := range []string{"93.184.216.34", "93.184.216.35", "203.0.113.9"} {
		_, _ = route(context.Background(), netip.MustParseAddr(ip))
	}
	_, _ = other(context.Background(), netip.MustParseAddr("93.184.216.34"))

	if !reflect.DeepEqual(known.lookups, before) {
		t.Errorf("lookups changed:\n got %+v\nwant %+v", known.lookups, before)
	}
	if known.host != knownHost {
		t.Errorf("host = %q, want %q", known.host, knownHost)
	}
}

func TestRouteFirstReadsNothingOnRepeatedKnownCalls(t *testing.T) {
	known := knownFor(knownHost, knownLookup("93.184.216.34", "eth0"))
	rc := newReadCounter()
	route := known.routeFirst(knownHost, rc.read)
	a := netip.MustParseAddr("93.184.216.34")

	first, err1 := route(context.Background(), a)
	second, err2 := route(context.Background(), a)

	if err1 != nil || err2 != nil {
		t.Fatalf("errors = %v, %v, want nil", err1, err2)
	}
	if rc.calls != 0 {
		t.Errorf("real route calls = %d after two calls, want 0", rc.calls)
	}
	if !reflect.DeepEqual(first, second) {
		t.Errorf("second hop = %+v, first %+v: the same reading must come back", second, first)
	}
}

// A miss is not memoized: the copy is read-only, so the second call for an
// address Diagnostics never looked up reads again.
func TestRouteFirstDoesNotLearnFromAMiss(t *testing.T) {
	rc := newReadCounter()
	route := knownFor(knownHost, knownLookup("93.184.216.34", "eth0")).routeFirst(knownHost, rc.read)
	a := netip.MustParseAddr("203.0.113.9")

	_, _ = route(context.Background(), a)
	_, _ = route(context.Background(), a)

	if rc.calls != 2 {
		t.Errorf("real route calls = %d, want 2 (a miss is never stored)", rc.calls)
	}
}

func TestRouteFirstPassesTheRealErrorOnAMiss(t *testing.T) {
	rc := newReadCounter()
	rc.err = fmt.Errorf("real: %w", syscall.ENETUNREACH)
	route := knownFor(knownHost, knownLookup("93.184.216.34", "eth0")).routeFirst(knownHost, rc.read)

	_, err := route(context.Background(), netip.MustParseAddr("203.0.113.9"))

	if err != rc.err {
		t.Errorf("err = %v, want the real route's error %v", err, rc.err)
	}
}

// The source condition (1): only a trace that starts on this machine answers
// from the copy. PathModel.runEngine is where the plan applies it.
func TestRunEngineAnswersFromTheCopyOnlyForThisMachine(t *testing.T) {
	routed := knownLookup("93.184.216.34", "eth0")
	addr := netip.MustParseAddr("93.184.216.34")
	container := pathSource{ID: "0123456789ab", Name: "web", Image: "nginx"}

	tests := []struct {
		name      string
		source    pathSource
		host      string
		wantCalls int
	}{
		{"this machine, same host and address", hostSource(), knownHost, 0},
		{"a container reads its own route even when the address was read", container, knownHost, 1},
		{"this machine, another host", hostSource(), "b.example", 1},
	}
	for _, tc := range tests {
		t.Run(tc.name, func(t *testing.T) {
			f := &fakeEngine{}
			pm := newPathModel(f.engine())
			pm.known = knownFor(knownHost, routed)
			pm.source = tc.source

			e := pm.runEngine(pathTarget{Host: tc.host})
			hop, err := e.route(context.Background(), addr)

			if err != nil {
				t.Fatalf("route() error = %v, want nil", err)
			}
			if f.routeCalls != tc.wantCalls {
				t.Errorf("real route calls = %d, want %d", f.routeCalls, tc.wantCalls)
			}
			if tc.wantCalls == 0 && hop.Interface != "eth0" {
				t.Errorf("hop interface = %q, want the recorded eth0", hop.Interface)
			}
		})
	}
}

func TestRunEngineGoingBackToThisMachineReusesAgain(t *testing.T) {
	f := &fakeEngine{}
	pm := newPathModel(f.engine())
	pm.known = knownFor(knownHost, knownLookup("93.184.216.34", "eth0"))
	addr := netip.MustParseAddr("93.184.216.34")
	tg := pathTarget{Host: knownHost}

	pm.source = pathSource{ID: "0123456789ab", Name: "web", Image: "nginx"}
	_, _ = pm.runEngine(tg).route(context.Background(), addr)
	pm.source = hostSource()
	_, _ = pm.runEngine(tg).route(context.Background(), addr)

	if f.routeCalls != 1 {
		t.Errorf("real route calls = %d, want 1 (the container run only)", f.routeCalls)
	}
}

func TestRunEngineKeepsTheOtherEngineFunctions(t *testing.T) {
	f := &fakeEngine{}
	pm := newPathModel(f.engine())
	pm.known = knownFor(knownHost, knownLookup("93.184.216.34", "eth0"))

	e := pm.runEngine(pathTarget{Host: knownHost})

	if e.resolve == nil || e.trace == nil || e.supported == nil || e.inspect == nil {
		t.Errorf("runEngine dropped a function: resolve=%v trace=%v supported=%v inspect=%v",
			e.resolve != nil, e.trace != nil, e.supported != nil, e.inspect != nil)
	}
	if pm.engine.route == nil {
		t.Error("runEngine replaced the model's own engine route")
	}
}

func TestKnownRoutesMatchAnIPv4GivenIn16Bytes(t *testing.T) {
	ip := net.ParseIP("93.184.216.34")
	if len(ip) != net.IPv6len {
		t.Fatalf("net.ParseIP gave %d bytes, the test needs the 16-byte form", len(ip))
	}
	k := knownRoutesOf(knownHost, routeResultsWith(knownLookup("93.184.216.34", "eth0")))

	got, ok := k.find(knownHost, netip.MustParseAddr("93.184.216.34"))

	if !ok {
		t.Fatal("find() = not found, want the lookup")
	}
	if got.Hop.Interface != "eth0" {
		t.Errorf("hop interface = %q, want eth0", got.Hop.Interface)
	}
}

func TestKnownRoutesFindAnIPv4MappedAddress(t *testing.T) {
	k := knownRoutesOf(knownHost, routeResultsWith(knownLookup("93.184.216.34", "eth0")))
	mapped := netip.AddrFrom16(netip.MustParseAddr("93.184.216.34").As16())

	if _, ok := k.find(knownHost, mapped); !ok {
		t.Errorf("find(%v) = not found, want the IPv4 lookup", mapped)
	}
}

func TestKnownRoutesRefuseAnotherHost(t *testing.T) {
	k := knownRoutesOf("a.example", routeResultsWith(knownLookup("93.184.216.34", "eth0")))

	if _, ok := k.find("b.example", netip.MustParseAddr("93.184.216.34")); ok {
		t.Error("find() for another host = found, want not found")
	}
}

func TestKnownRoutesRefuseAnAddressNotLookedUp(t *testing.T) {
	k := knownRoutesOf(knownHost, routeResultsWith(knownLookup("93.184.216.34", "eth0")))

	if _, ok := k.find(knownHost, netip.MustParseAddr("93.184.216.35")); ok {
		t.Error("find() for an address nobody looked up = found, want not found")
	}
}

func TestKnownRoutesOfEmptyResultsKnowsNothing(t *testing.T) {
	k := knownRoutesOf(knownHost, netcheck.ResultsOf())

	if _, ok := k.find(knownHost, netip.MustParseAddr("93.184.216.34")); ok {
		t.Error("find() on empty results = found, want not found")
	}
}

func TestKnownRoutesOfAnEmptyHostKnowsNothing(t *testing.T) {
	k := knownRoutesOf("", routeResultsWith(knownLookup("93.184.216.34", "eth0")))

	if _, ok := k.find("", netip.MustParseAddr("93.184.216.34")); ok {
		t.Error("find() with an empty host = found, want not found")
	}
}

func TestKnownRoutesOfAPlaceholderKnowsNothing(t *testing.T) {
	placeholder := netcheck.ResultsOf(netcheck.Check{ID: netcheck.CheckRoute, Verdict: netcheck.Unknown})
	k := knownRoutesOf(knownHost, placeholder)

	if _, ok := k.find(knownHost, netip.MustParseAddr("93.184.216.34")); ok {
		t.Error("find() on a placeholder route check = found, want not found")
	}
}

func TestKnownRoutesKeepAKnownError(t *testing.T) {
	want := fmt.Errorf("route: %w", syscall.ENETUNREACH)
	k := knownRoutesOf(knownHost, routeResultsWith(knownFailure("93.184.216.34", want)))

	got, ok := k.find(knownHost, netip.MustParseAddr("93.184.216.34"))

	if !ok {
		t.Fatal("find() = not found, want the failed lookup")
	}
	if got.Err != want {
		t.Errorf("Err = %v, want the very same error %v", got.Err, want)
	}
}

func TestKnownRoutesDoNotShareTheResultsSlice(t *testing.T) {
	lookups := []netcheck.RouteLookup{knownLookup("93.184.216.34", "eth0")}
	k := knownRoutesOf(knownHost, routeResultsWith(lookups...))

	lookups[0] = knownLookup("203.0.113.9", "wg0")

	got, ok := k.find(knownHost, netip.MustParseAddr("93.184.216.34"))
	if !ok || got.Hop.Interface != "eth0" {
		t.Errorf("find() after the results changed = %+v, %v; want the original eth0 lookup", got, ok)
	}
	if _, ok := k.find(knownHost, netip.MustParseAddr("203.0.113.9")); ok {
		t.Error("the copy followed a later change of the results")
	}
}
