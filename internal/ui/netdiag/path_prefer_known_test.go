package netdiag

import (
	"context"
	"errors"
	"fmt"
	"net"
	"net/netip"
	"reflect"
	"testing"

	"github.com/anthnel/devdesk/internal/netcheck"
	"github.com/anthnel/devdesk/internal/nettrace"
	"github.com/anthnel/devdesk/internal/ui/testutil"
)

// DEVDESK-473 step 6 (D155, decision of the user, 2026-11-16, option b): when
// the trace resolves a name to several addresses and Diagnostics already read
// the route of some of them, the trace takes the first of ITS OWN resolution
// that Diagnostics read with success. Without such results it keeps the first
// IPv4 address (D144). The traced address is always one the trace's own resolution returned
// now: the Diagnostics addresses only choose among them.

const onceHostC = "203.0.113.9"

// preferModel is routeOnceModel with a trace resolution that returns traceAddrs
// for every name, and a Diagnostics resolution of "rr.example" to diagAddrs.
func preferModel(t *testing.T, traceAddrs, diagAddrs []string) (*Model, *seqEnv, *fakeEngine) {
	t.Helper()
	fe := &fakeEngine{resolveFn: func(context.Context, string) ([]netip.Addr, error) {
		var out []netip.Addr
		for _, a := range traceAddrs {
			out = append(out, netip.MustParseAddr(a))
		}
		return out, nil
	}}
	m, env := routeOnceModel(t, fe)
	env.resolves = map[string][]string{"rr.example": diagAddrs, "other.example": diagAddrs}
	return m, env, fe
}

func requireTraced(t *testing.T, fe *fakeEngine, want string) {
	t.Helper()
	if got := fe.traceAddr; got != netip.MustParseAddr(want) {
		t.Errorf("traced address = %v, want %s", got, want)
	}
}

// The trace's resolution returns the address Diagnostics read second: that one
// is traced, and its route is not read again.
func TestTracePrefersAnAddressDiagnosticsAlreadyRead(t *testing.T) {
	m, env, fe := preferModel(t, []string{onceHostA, onceHostB}, []string{onceHostB})
	m = runDiagnostics(t, m, "rr.example")
	requireRouteCalls(t, env, onceHostB, 1)

	m = traceFromResults(t, m)

	requireTraced(t, fe, onceHostB)
	requireRouteCalls(t, env, onceHostB, 1)
	requireRouteCalls(t, env, onceHostA, 0)
	requireLocalRows(t, m, "eth1 (192.168.1.21)", "192.168.1.1")
}

// Several of the trace's addresses were read: the first of the trace's own
// order wins, not the first Diagnostics read.
func TestTracePrefersTheFirstReadAddressOfItsOwnOrder(t *testing.T) {
	m, env, fe := preferModel(t, []string{onceHostC, onceHostB, onceHostA}, []string{onceHostA, onceHostB})
	m = runDiagnostics(t, m, "rr.example")

	_ = traceFromResults(t, m)

	requireTraced(t, fe, onceHostB)
	requireRouteCalls(t, env, onceHostA, 1)
	requireRouteCalls(t, env, onceHostB, 1)
	requireRouteCalls(t, env, onceHostC, 0)
}

// No address in common: the first IPv4 address, as before, and its route is read
// once (the one reading more).
func TestTraceKeepsTheFirstAddressWhenNoneWasRead(t *testing.T) {
	m, env, fe := preferModel(t, []string{onceHostA, onceHostC}, []string{onceHostB})
	m = runDiagnostics(t, m, "rr.example")

	m = traceFromResults(t, m)

	requireTraced(t, fe, onceHostA)
	requireRouteCalls(t, env, onceHostA, 1)
	requireRouteCalls(t, env, onceHostB, 1)
	requireRouteCalls(t, env, onceHostC, 0)
	requireLocalRows(t, m, "eth1 (192.168.1.21)", "192.168.1.1")
}

// Trace Path from the form: no Diagnostics results, so D144 unchanged.
func TestTraceKeepsTheFirstAddressWithoutResults(t *testing.T) {
	m, env, fe := preferModel(t, []string{onceHostA, onceHostB}, []string{onceHostB})

	_ = traceFromForm(t, m, "rr.example")

	requireTraced(t, fe, onceHostA)
	requireRouteCalls(t, env, onceHostA, 1)
	requireRouteCalls(t, env, onceHostB, 0)
}

// Results made for another host say nothing about this one, even if they read an
// address its resolution returns.
func TestTraceKeepsTheFirstAddressForAnotherHost(t *testing.T) {
	m, env, fe := preferModel(t, []string{onceHostA, onceHostB}, []string{onceHostB})
	m = runDiagnostics(t, m, "rr.example")
	m.targetInput.SetValue("other.example") // the form is not editable here; the key guard still holds

	_ = traceFromResults(t, m)

	requireTraced(t, fe, onceHostA)
	requireRouteCalls(t, env, onceHostA, 1)
	requireRouteCalls(t, env, onceHostB, 1)
}

// A container source reads its own route and keeps the first address; choosing
// this machine again prefers the address Diagnostics read.
func TestTraceFromAContainerKeepsTheFirstAddress(t *testing.T) {
	m, env, fe := preferModel(t, []string{onceHostA, onceHostB}, []string{onceHostB})
	fe.psRows = runningFake().psRows
	fe.inspectNet = appNetworking()
	m = runDiagnostics(t, m, "rr.example")
	m = selectCheck(t, m, netcheck.CheckRoute)
	m = feed(t, m, testutil.Key("H"))

	m = chooseSource(t, m, 1)
	if m.pathModel.source.isHost() {
		t.Fatal("no container was chosen as the source")
	}
	m = startTrace(t, m)
	requireTraced(t, fe, onceHostA)
	requireRouteCalls(t, env, onceHostA, 1)

	m = chooseSource(t, m, 0) // this machine again
	_ = startTrace(t, m)
	requireTraced(t, fe, onceHostB)
	requireRouteCalls(t, env, onceHostB, 1) // the Diagnostics reading, not a new one
	requireRouteCalls(t, env, onceHostA, 1)
}

// A lookup that ended on a cancellation is not a reading: its address is not
// preferred.
func TestTraceDoesNotPreferACanceledRead(t *testing.T) {
	m, env, fe := preferModel(t, []string{onceHostA, onceHostB}, []string{onceHostB})
	env.answer = func(ip net.IP, n int) (netcheck.RouteHop, error) {
		if ip.String() == onceHostB && n == 1 {
			return netcheck.RouteHop{}, fmt.Errorf("route: %w", context.Canceled)
		}
		return seqHop(n), nil
	}
	m = runDiagnostics(t, m, "rr.example")

	_ = traceFromResults(t, m)

	requireTraced(t, fe, onceHostA)
	requireRouteCalls(t, env, onceHostA, 1)
	requireRouteCalls(t, env, onceHostB, 1) // only Diagnostics' canceled read
}

// A deadline is no reading either.
func TestTraceDoesNotPreferAnExpiredRead(t *testing.T) {
	m, env, fe := preferModel(t, []string{onceHostA, onceHostB}, []string{onceHostB})
	env.answer = func(ip net.IP, n int) (netcheck.RouteHop, error) {
		if ip.String() == onceHostB && n == 1 {
			return netcheck.RouteHop{}, fmt.Errorf("route: %w", context.DeadlineExceeded)
		}
		return seqHop(n), nil
	}
	m = runDiagnostics(t, m, "rr.example")

	_ = traceFromResults(t, m)

	requireTraced(t, fe, onceHostA)
}

// Only a successful reading is preferred (D155, decision of the user confirmed on
// 2026-11-16): an address whose Diagnostics read ended in an error, whatever the
// error, does not take the lead; the trace keeps the order of its own resolution.
func failRoute(ip string, err error) func(net.IP, int) (netcheck.RouteHop, error) {
	return func(got net.IP, n int) (netcheck.RouteHop, error) {
		if got.String() == ip {
			return netcheck.RouteHop{}, err
		}
		return seqHop(n), nil
	}
}

func TestTraceDoesNotPreferAReadThatFoundNoRoute(t *testing.T) {
	m, env, fe := preferModel(t, []string{onceHostA, onceHostB}, []string{onceHostB})
	env.answer = failRoute(onceHostB, fmt.Errorf("%w: unreachable", nettrace.ErrNoRoute))
	m = runDiagnostics(t, m, "rr.example")

	_ = traceFromResults(t, m)

	requireTraced(t, fe, onceHostA)
	requireRouteCalls(t, env, onceHostA, 1)
	requireRouteCalls(t, env, onceHostB, 1) // only Diagnostics' read
}

func TestTraceDoesNotPreferAnUndeterminedRead(t *testing.T) {
	m, env, fe := preferModel(t, []string{onceHostA, onceHostB}, []string{onceHostB})
	env.answer = failRoute(onceHostB, errors.New("routing table unreadable"))
	m = runDiagnostics(t, m, "rr.example")

	_ = traceFromResults(t, m)

	requireTraced(t, fe, onceHostA)
	requireRouteCalls(t, env, onceHostA, 1)
	requireRouteCalls(t, env, onceHostB, 1)
}

// The first address was read in error, the second successfully: the first
// successful reading is the one preferred.
func TestTracePrefersTheFirstSuccessfulReadOverAFailedOne(t *testing.T) {
	m, env, fe := preferModel(t, []string{onceHostA, onceHostB}, []string{onceHostA, onceHostB})
	env.answer = failRoute(onceHostA, errors.New("routing table unreadable"))
	m = runDiagnostics(t, m, "rr.example")

	_ = traceFromResults(t, m)

	requireTraced(t, fe, onceHostB)
	requireRouteCalls(t, env, onceHostA, 1)
	requireRouteCalls(t, env, onceHostB, 1)
}

// Nothing else to choose: the only address is traced as before, and its failed
// reading is reused as it is (one Route call, criterion 4).
func TestTraceKeepsTheOnlyAddressWhenItsReadFailed(t *testing.T) {
	m, env, fe := preferModel(t, []string{onceHostA}, []string{onceHostA})
	env.answer = failRoute(onceHostA, errors.New("routing table unreadable"))
	m = runDiagnostics(t, m, "rr.example")

	_ = traceFromResults(t, m)

	requireTraced(t, fe, onceHostA)
	requireRouteCalls(t, env, onceHostA, 1)
}

// Trace again keeps the address: the same results, the same resolution order.
func TestTraceAgainKeepsThePreferredAddress(t *testing.T) {
	m, env, fe := preferModel(t, []string{onceHostA, onceHostB}, []string{onceHostB})
	m = runDiagnostics(t, m, "rr.example")
	m = traceFromResults(t, m)

	_ = startTrace(t, m)

	requireTraced(t, fe, onceHostB)
	requireRouteCalls(t, env, onceHostB, 1)
	requireRouteCalls(t, env, onceHostA, 0)
}

// The resolution step on its own, against a snapshot built by hand.
func TestPreferredResolutionOrder(t *testing.T) {
	a, b, c := netip.MustParseAddr(onceHostA), netip.MustParseAddr(onceHostB), netip.MustParseAddr(onceHostC)
	v6 := netip.MustParseAddr("2001:db8::1")
	read := func(host string, ips ...string) pathKnownRoutes {
		k := pathKnownRoutes{host: host}
		for _, ip := range ips {
			k.lookups = append(k.lookups, netcheck.RouteLookup{IP: net.ParseIP(ip), Hop: seqHop(1)})
		}
		return k
	}
	tests := []struct {
		name  string
		known pathKnownRoutes
		host  string
		got   []netip.Addr
		want  []netip.Addr
	}{
		{"nothing read", pathKnownRoutes{}, "h", []netip.Addr{a, b}, []netip.Addr{a, b}},
		{"the first one was read", read("h", onceHostA), "h", []netip.Addr{a, b}, []netip.Addr{a, b}},
		{"the second one was read, the others keep their order", read("h", onceHostB), "h", []netip.Addr{a, b, c}, []netip.Addr{b, a, c}},
		{"another host", read("other", onceHostB), "h", []netip.Addr{a, b}, []netip.Addr{a, b}},
		{"an address of Diagnostics only is never added", read("h", onceHostC), "h", []netip.Addr{a, b}, []netip.Addr{a, b}},
		{"an IPv6 address read is ignored", read("h", "2001:db8::1"), "h", []netip.Addr{a, v6}, []netip.Addr{a, v6}},
		{"an IPv4 address given in 16 bytes matches", read("h", "::ffff:"+onceHostB), "h", []netip.Addr{a, b}, []netip.Addr{b, a}},
		{"a canceled read is not a reading", pathKnownRoutes{host: "h", lookups: []netcheck.RouteLookup{
			{IP: net.ParseIP(onceHostB), Err: context.Canceled},
		}}, "h", []netip.Addr{a, b}, []netip.Addr{a, b}},
		{"a read that failed for another reason is not preferred", pathKnownRoutes{host: "h", lookups: []netcheck.RouteLookup{
			{IP: net.ParseIP(onceHostB), Err: fmt.Errorf("unreadable")},
		}}, "h", []netip.Addr{a, b}, []netip.Addr{a, b}},
		{"a no route read is not preferred", pathKnownRoutes{host: "h", lookups: []netcheck.RouteLookup{
			{IP: net.ParseIP(onceHostB), Err: nettrace.ErrNoRoute},
		}}, "h", []netip.Addr{a, b}, []netip.Addr{a, b}},
		{"the first successful read wins over a failed one", pathKnownRoutes{host: "h", lookups: []netcheck.RouteLookup{
			{IP: net.ParseIP(onceHostA), Err: fmt.Errorf("unreadable")},
			{IP: net.ParseIP(onceHostB), Hop: seqHop(1)},
		}}, "h", []netip.Addr{a, b, c}, []netip.Addr{b, a, c}},
		{"nothing resolved", read("h", onceHostA), "h", nil, nil},
	}
	for _, tc := range tests {
		t.Run(tc.name, func(t *testing.T) {
			pm := &PathModel{known: tc.known, source: hostSource()}
			pm.engine.resolve = func(context.Context, string) ([]netip.Addr, error) {
				return append([]netip.Addr(nil), tc.got...), nil
			}
			got, err := pm.runEngine(pathTarget{Host: tc.host}).resolve(context.Background(), tc.host)
			if err != nil {
				t.Fatal(err)
			}
			if !reflect.DeepEqual(got, tc.want) {
				t.Errorf("resolution = %v, want %v", got, tc.want)
			}
		})
	}
}

// A resolution error goes through untouched.
func TestPreferredResolutionKeepsTheError(t *testing.T) {
	boom := fmt.Errorf("nxdomain")
	pm := &PathModel{known: pathKnownRoutes{host: "h"}, source: hostSource()}
	pm.engine.resolve = func(context.Context, string) ([]netip.Addr, error) { return nil, boom }

	if _, err := pm.runEngine(pathTarget{Host: "h"}).resolve(context.Background(), "h"); err != boom {
		t.Errorf("error = %v, want %v", err, boom)
	}
}
