package netdiag

import (
	"errors"
	"net"
	"net/netip"
	"reflect"
	"strings"
	"testing"

	"github.com/charmbracelet/x/ansi"

	"github.com/anthnel/devdesk/internal/netcheck"
	"github.com/anthnel/devdesk/internal/nettrace"
	"github.com/anthnel/devdesk/internal/ui/components"
	"github.com/anthnel/devdesk/internal/ui/testutil"
)

// DEVDESK-329 step 3: the table shows source, interface, gateway, then hops.

// pmColumn returns one cell per table row, read through the column functions.
func pmColumn(pm *PathModel, cell func(pathStep) string) []string {
	var out []string
	for _, s := range pm.table.Items() {
		out = append(out, cell(s))
	}
	return out
}

func pmSteps(pm *PathModel) []string    { return pmColumn(pm, pathStep.stepCell) }
func pmAddrs(pm *PathModel) []string    { return pmColumn(pm, pathStep.addressCell) }
func pmHopCells(pm *PathModel) []string { return pmColumn(pm, pathStep.hopCell) }
func pmRTTCells(pm *PathModel) []string { return pmColumn(pm, pathStep.rttCell) }

func lanRoute() netcheck.RouteHop { return defaultRouteHop() }

// resolvedWith launches 1.1.1.1 and delivers the resolved message of the run.
func resolvedWith(t *testing.T, route netcheck.RouteHop, routeErr error) *PathModel {
	t.Helper()
	pm, _ := newPM(t)
	pm, _ = launched(t, pm, "1.1.1.1")
	pm, _ = pmFeed(t, pm, pathResolvedMsg{
		gen: pm.gen, addr: netip.MustParseAddr("1.1.1.1"), route: route, routeErr: routeErr,
	})
	return pm
}

func targetHop(ttl int) nettrace.Hop {
	return nettrace.Hop{TTL: ttl, Kind: nettrace.HopTarget, Addr: netip.MustParseAddr("1.1.1.1")}
}

// Criterion 1: source, interface, gateway, then the hops.
func TestPathColumnStartsWithTheLocalSteps(t *testing.T) {
	pm := resolvedWith(t, lanRoute(), nil)
	pm, _ = pmFeed(t, pm,
		pathHopMsg{gen: pm.gen, hop: hopOf(2, nettrace.HopRouter)},
		pathHopMsg{gen: pm.gen, hop: hopOf(1, nettrace.HopRouter)},
	)
	pm, _ = pmFeed(t, pm, pathDoneMsg{gen: pm.gen, result: nettrace.Result{
		Outcome: nettrace.OutcomeReached,
		Hops:    []nettrace.Hop{hopOf(1, nettrace.HopRouter), targetHop(2)},
	}})

	if got, want := pmSteps(pm), []string{"source", "interface", "gateway", "router", "target"}; !reflect.DeepEqual(got, want) {
		t.Errorf("Step column = %v, want %v", got, want)
	}
	addrs := pmAddrs(pm)
	if len(addrs) < 3 {
		t.Fatalf("Address column = %v, want at least the three local rows", addrs)
	}
	if got, want := addrs[:3], []string{"this machine", "eth0 (192.168.1.21)", "192.168.1.1"}; !reflect.DeepEqual(got, want) {
		t.Errorf("Address of the local rows = %v, want %v", got, want)
	}
}

// The local rows asked nobody: Hop and RTT are empty, not "-".
func TestPathLocalRowsHaveNoHopAndNoRTT(t *testing.T) {
	pm := resolvedWith(t, lanRoute(), nil)
	pm, _ = pmFeed(t, pm, pathHopMsg{gen: pm.gen, hop: hopOf(1, nettrace.HopRouter)})

	if got, want := pmHopCells(pm), []string{"", "", "", "1"}; !reflect.DeepEqual(got, want) {
		t.Errorf("Hop cells = %q, want %q", got, want)
	}
	if got := pmRTTCells(pm); len(got) < 3 || !reflect.DeepEqual(got[:3], []string{"", "", ""}) {
		t.Errorf("RTT cells = %q, want the local rows empty", got)
	}
	body := ansi.Strip(pm.view())
	for _, want := range []string{"Step", "this machine", "eth0 (192.168.1.21)"} {
		if !strings.Contains(body, want) {
			t.Errorf("rendered table lacks %q:\n%s", want, body)
		}
	}
}

// Criterion 2, model level: the interface is the one the route names.
func TestPathVPNRouteShowsTheTunnel(t *testing.T) {
	pm := resolvedWith(t, netcheck.RouteHop{Interface: "wg0", Source: net.ParseIP("10.2.0.2")}, nil)
	got := pmAddrs(pm)
	if len(got) != 3 || got[1] != "wg0 (10.2.0.2)" || got[2] != "directly connected" {
		t.Errorf("Address column = %q, want [this machine, wg0 (10.2.0.2), directly connected]", got)
	}
}

// A failed lookup is shown as undetermined; the trace goes on; no raw error.
func TestPathRouteErrorIsUndeterminedAndNeverInTheFooter(t *testing.T) {
	const raw = "netroute: no source IP found"
	pm := resolvedWith(t, netcheck.RouteHop{}, errors.New(raw))
	pm, _ = pmFeed(t, pm, pathHopMsg{gen: pm.gen, hop: hopOf(1, nettrace.HopRouter)})

	addrs := pmAddrs(pm)
	if len(addrs) < 3 {
		t.Fatalf("Address column = %v, want the three local rows", addrs)
	}
	if got, want := addrs[:3], []string{"this machine", "undetermined", "undetermined"}; !reflect.DeepEqual(got, want) {
		t.Errorf("Address of the local rows = %v, want %v", got, want)
	}
	if pm.hopCount() != 1 {
		t.Errorf("hop rows = %d, want the hop appended after the undetermined rows", pm.hopCount())
	}
	st := pm.statusLine()
	if st.Level == components.LevelError || strings.Contains(st.Text, raw) || strings.Contains(st.Text, "netroute") {
		t.Errorf("status = %+v, want no error and no raw route error (Rule 128)", st)
	}
	if line := pmFooter(pm, 120); strings.Contains(line, raw) {
		t.Errorf("rendered footer = %q, leaks the raw route error", line)
	}
}

// Decision D-d: nothing before the route is known; then the three local rows.
func TestPathNoRowsBeforeTheRouteThenThreeLocalRows(t *testing.T) {
	pm, _ := newPM(t)
	pm, _ = launched(t, pm, "1.1.1.1")
	if n := len(pm.table.Items()); n != 0 {
		t.Errorf("%d rows before the resolved message, want 0", n)
	}
	if st := pm.statusLine(); !strings.HasPrefix(st.Text, "Resolving 1.1.1.1") {
		t.Errorf("status = %q, want Resolving 1.1.1.1...", st.Text)
	}

	pm, _ = pmFeed(t, pm, pathResolvedMsg{gen: pm.gen, addr: netip.MustParseAddr("1.1.1.1"), route: lanRoute()})
	if got, want := pmSteps(pm), []string{"source", "interface", "gateway"}; !reflect.DeepEqual(got, want) {
		t.Errorf("rows after the resolved message = %v, want %v", got, want)
	}
	// H5: the counter counts hops, not the three local rows.
	if st := pm.statusLine(); !strings.Contains(st.Text, "Tracing 1.1.1.1") || !strings.Contains(st.Text, "0 hops answered") {
		t.Errorf("status = %q, want Tracing 1.1.1.1 with 0 hops answered", st.Text)
	}
	pm, _ = pmFeed(t, pm, pathHopMsg{gen: pm.gen, hop: hopOf(1, nettrace.HopRouter)})
	if st := pm.statusLine(); !strings.Contains(st.Text, "1 hop answered") {
		t.Errorf("status = %q, want 1 hop answered", st.Text)
	}
}

// Criterion 5: the first row is this machine in every state, and only that row.
func TestPathSourceIsFirstInEveryState(t *testing.T) {
	running := func() *PathModel {
		pm := resolvedWith(t, lanRoute(), nil)
		pm, _ = pmFeed(t, pm, pathHopMsg{gen: pm.gen, hop: hopOf(1, nettrace.HopRouter)})
		return pm
	}
	done := func() *PathModel {
		pm := running()
		pm, _ = pmFeed(t, pm, pathDoneMsg{gen: pm.gen, result: nettrace.Result{
			Outcome: nettrace.OutcomeReached, Hops: []nettrace.Hop{hopOf(1, nettrace.HopRouter), targetHop(2)},
		}})
		return pm
	}
	canceled := func() *PathModel {
		pm, _ := pmFeed(t, running(), testutil.Key("esc"))
		return pm
	}
	for name, build := range map[string]func() *PathModel{"running": running, "done": done, "canceled": canceled} {
		t.Run(name, func(t *testing.T) {
			pm := build()
			addrs := pmAddrs(pm)
			if len(addrs) == 0 || pmSteps(pm)[0] != "source" || addrs[0] != "this machine" {
				t.Fatalf("rows = %q / %q, want the first to be source / this machine", pmSteps(pm), addrs)
			}
			for i, a := range addrs[1:] {
				if a == "this machine" {
					t.Errorf("row %d also says this machine", i+1)
				}
			}
		})
	}
}

func TestPathStaleResolvedMessageIsIgnored(t *testing.T) {
	pm, _ := newPM(t)
	pm, _ = launched(t, pm, "1.1.1.1")
	pm, cmd := pmFeed(t, pm, pathResolvedMsg{gen: pm.gen - 1, addr: netip.MustParseAddr("9.9.9.9"), route: lanRoute()})
	if cmd != nil || pm.route != nil || len(pm.table.Items()) != 0 || pm.addr.IsValid() {
		t.Errorf("stale resolved: cmd=%v route=%v rows=%d addr=%v, want ignored", cmd != nil, pm.route, len(pm.table.Items()), pm.addr)
	}
}

// A new run forgets the route of the previous one until its own resolved message.
func TestPathRelaunchDropsTheRouteUntilTheNewOneArrives(t *testing.T) {
	pm := resolvedWith(t, lanRoute(), nil)
	pm, _ = pmFeed(t, pm, pathDoneMsg{gen: pm.gen, result: nettrace.Result{
		Outcome: nettrace.OutcomeReached, Hops: []nettrace.Hop{targetHop(1)},
	}})
	if n := len(pm.table.Items()); n != 4 { // source, interface, gateway, target
		t.Fatalf("rows after done = %d, want 4", n)
	}
	pm, _ = pmFeed(t, pm, testutil.Key("enter"))
	if pm.route != nil || len(pm.table.Items()) != 0 {
		t.Errorf("after relaunch: route=%v rows=%d, want nil and 0", pm.route, len(pm.table.Items()))
	}
	pm, _ = pmFeed(t, pm, pathResolvedMsg{gen: pm.gen, addr: netip.MustParseAddr("1.1.1.1"), route: lanRoute()})
	if pm.route == nil || len(pm.table.Items()) != 3 {
		t.Errorf("after the new resolved message: route=%v rows=%d, want set and 3", pm.route, len(pm.table.Items()))
	}
}

// H hands a target over: the previous rows and route are dropped.
func TestPathPrefillDropsTheRoute(t *testing.T) {
	pm := resolvedWith(t, lanRoute(), nil)
	pm.prefill("example.com")
	if pm.route != nil || len(pm.table.Items()) != 0 {
		t.Errorf("after prefill: route=%v rows=%d, want nil and 0", pm.route, len(pm.table.Items()))
	}
}

// H4: an error done clears every row, the local ones included.
func TestPathTraceErrorClearsEveryRow(t *testing.T) {
	pm := resolvedWith(t, lanRoute(), nil)
	if len(pm.table.Items()) != 3 {
		t.Fatalf("precondition: %d rows, want 3", len(pm.table.Items()))
	}
	pm, _ = pmFeed(t, pm, pathDoneMsg{gen: pm.gen, err: nettrace.ErrNoRoute})
	if n := len(pm.table.Items()); n != 0 {
		t.Errorf("%d rows after an error done, want 0", n)
	}
	if pm.route != nil {
		t.Errorf("route = %v after an error done, want nil", pm.route)
	}
	if !pm.InEditMode() || pm.statusLine().Level != components.LevelError {
		t.Errorf("editing=%v status=%+v, want editing with a persistent error", pm.InEditMode(), pm.statusLine())
	}
}
