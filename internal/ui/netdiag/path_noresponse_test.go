package netdiag

import (
	"context"
	"errors"
	"fmt"
	"net/netip"
	"reflect"
	"strings"
	"syscall"
	"testing"

	tea "charm.land/bubbletea/v2"
	"github.com/charmbracelet/x/ansi"

	"github.com/anthnel/devdesk/internal/nettrace"
	"github.com/anthnel/devdesk/internal/ui/testutil"
)

// DEVDESK-335 step 4: a silent end of the path is one row, not a wall of rows.

const lastAnswerTag = " · last to answer"

// nrHops builds TTLs 1..total: routers up to and including routers, then the
// given kind (HopSilent by default) for the rest.
func nrHops(total, routers int, tail ...nettrace.HopKind) []nettrace.Hop {
	kind := nettrace.HopSilent
	if len(tail) > 0 {
		kind = tail[0]
	}
	out := make([]nettrace.Hop, 0, total)
	for ttl := 1; ttl <= total; ttl++ {
		if ttl <= routers {
			out = append(out, nettrace.Hop{TTL: ttl, Kind: nettrace.HopRouter, Addr: netip.MustParseAddr(fmt.Sprintf("10.0.0.%d", ttl))})
			continue
		}
		out = append(out, nettrace.Hop{TTL: ttl, Kind: kind})
	}
	return out
}

// nrTerminalBeforeTail is the shape nettrace.Result gives after a deadline once
// a terminal hop is known: 1 router, 2 unresolved, 3 the terminal hop (kind),
// then TTLs 4..30 unresolved.
func nrTerminalBeforeTail(kind nettrace.HopKind) []nettrace.Hop {
	hops := nrHops(30, 1, nettrace.HopUnresolved)
	hops[2] = nettrace.Hop{TTL: 3, Kind: kind, Addr: netip.MustParseAddr("10.0.0.3")}
	return hops
}

func nrResult(o nettrace.Outcome, hops []nettrace.Hop) *nettrace.Result {
	return &nettrace.Result{Outcome: o, Hops: hops}
}

// nrRows is pathSteps + withNoResponse for res, as refreshSteps composes them.
func nrRows(res *nettrace.Result) []pathStep {
	var hops []nettrace.Hop
	if res != nil {
		hops = res.Hops
	}
	route := &pathRoute{Known: true, Interface: "eth0", Gateway: netip.MustParseAddr("10.0.0.1")}
	return withNoResponse(pathSteps(nil, route, hops), res)
}

func nrHopRows(steps []pathStep) []pathStep {
	var out []pathStep
	for _, s := range steps {
		if s.Kind >= stepHop {
			out = append(out, s)
		}
	}
	return out
}

// nrCells is "hop|step|address|rtt" per hop-level row.
func nrCells(steps []pathStep) []string {
	var out []string
	for _, s := range nrHopRows(steps) {
		out = append(out, s.hopCell()+"|"+s.stepCell()+"|"+s.addressCell()+"|"+s.rttCell())
	}
	return out
}

func TestWithNoResponse(t *testing.T) {
	deadlineMixed := nrHops(8, 3)
	deadlineMixed[5].Kind = nettrace.HopUnresolved
	deadlineMixed[7].Kind = nettrace.HopUnresolved
	withError := nrHops(6, 2)
	withError[2] = nettrace.Hop{TTL: 3, Kind: nettrace.HopError, Err: errors.New("send failed")}
	middle := nrHops(6, 5)
	middle[2].Kind = nettrace.HopSilent
	middle[2].Addr = netip.Addr{}

	tests := []struct {
		name     string
		res      *nettrace.Result
		wantTail pathTail
		wantOK   bool
		wantRows int    // hop-level rows after grouping
		wantLast string // address cell of the last-to-answer row ("" = none)
		wantAddr string // address cell of the grouped row
		wantHop  string // hop cell of the grouped row
	}{
		{"1 to 3 routers then silent", nrResult(nettrace.OutcomeMaxHops, nrHops(30, 3)),
			pathTail{First: 4, Last: 30, Anchor: 3}, true, 4, "10.0.0.3" + lastAnswerTag,
			"no response beyond hop 3 (hops 4 to 30)", "4+"},
		{"criterion 3: one router then silent", nrResult(nettrace.OutcomeMaxHops, nrHops(30, 1)),
			pathTail{First: 2, Last: 30, Anchor: 1}, true, 2, "10.0.0.1" + lastAnswerTag,
			"no response beyond hop 1 (hops 2 to 30)", "2+"},
		{"nothing answered", nrResult(nettrace.OutcomeMaxHops, nrHops(30, 0)),
			pathTail{First: 1, Last: 30}, true, 1, "",
			"no response from any hop (hops 1 to 30)", "1+"},
		{"a silent hop in the middle stays", nrResult(nettrace.OutcomeMaxHops, middle),
			pathTail{First: 6, Last: 6, Anchor: 5}, true, 6, "10.0.0.5" + lastAnswerTag,
			"no response beyond hop 5 (hop 6)", "6"},
		{"single silent hop at the end", nrResult(nettrace.OutcomeMaxHops, nrHops(30, 29)),
			pathTail{First: 30, Last: 30, Anchor: 29}, true, 30, "10.0.0.29" + lastAnswerTag,
			"no response beyond hop 29 (hop 30)", "30"},
		{"router on the last TTL", nrResult(nettrace.OutcomeMaxHops, nrHops(30, 30)),
			pathTail{}, false, 30, "", "", ""},
		{"deadline: target answered before the unresolved tail", nrResult(nettrace.OutcomeDeadline, nrTerminalBeforeTail(nettrace.HopTarget)),
			pathTail{}, false, 2, "", "", ""},
		{"deadline: unreachable answered before the unresolved tail", nrResult(nettrace.OutcomeDeadline, nrTerminalBeforeTail(nettrace.HopUnreachable)),
			pathTail{}, false, 2, "", "", ""},
		{"max hops: unreachable answered before the silent tail", nrResult(nettrace.OutcomeMaxHops, nrTerminalBeforeTail(nettrace.HopUnreachable)),
			pathTail{}, false, 2, "", "", ""},
		{"deadline, unresolved mixed with silent", nrResult(nettrace.OutcomeDeadline, deadlineMixed),
			pathTail{First: 4, Last: 8, Anchor: 3}, true, 4, "10.0.0.3" + lastAnswerTag,
			"no response beyond hop 3 (hops 4 to 8)", "4+"},
		{"an error row before the silent end stays", nrResult(nettrace.OutcomeMaxHops, withError),
			pathTail{First: 4, Last: 6, Anchor: 2}, true, 4, "10.0.0.2" + lastAnswerTag,
			"no response beyond hop 2 (hops 4 to 6)", "4+"},
	}
	for _, tc := range tests {
		t.Run(tc.name, func(t *testing.T) {
			tail, ok := silentTail(tc.res)
			if ok != tc.wantOK || (ok && tail != tc.wantTail) {
				t.Fatalf("silentTail = %+v, %v; want %+v, %v", tail, ok, tc.wantTail, tc.wantOK)
			}
			hops := nrHopRows(nrRows(tc.res))
			if len(hops) != tc.wantRows {
				t.Fatalf("hop-level rows = %d (%q), want %d", len(hops), nrCells(nrRows(tc.res)), tc.wantRows)
			}
			if !tc.wantOK {
				for _, s := range hops {
					if s.Kind == stepNoResponse || s.LastAnswer || strings.Contains(s.addressCell(), lastAnswerTag) {
						t.Errorf("row %q carries a mark, want none", s.addressCell())
					}
				}
				return
			}
			g := hops[len(hops)-1]
			if g.Kind != stepNoResponse || g.Tail != tc.wantTail {
				t.Fatalf("last row = kind %v tail %+v, want a stepNoResponse with %+v", g.Kind, g.Tail, tc.wantTail)
			}
			if g.hopCell() != tc.wantHop || g.stepCell() != "silent" || g.rttCell() != "-" || g.addressCell() != tc.wantAddr {
				t.Errorf("cells = %q|%q|%q|%q, want %q|silent|%q|-", g.hopCell(), g.stepCell(), g.addressCell(), g.rttCell(), tc.wantHop, tc.wantAddr)
			}
			var marked []string
			for _, s := range hops {
				if s.LastAnswer {
					marked = append(marked, s.addressCell())
				}
			}
			var want []string
			if tc.wantLast != "" {
				want = []string{tc.wantLast}
			}
			if !reflect.DeepEqual(marked, want) {
				t.Errorf("last-to-answer rows = %q, want %q", marked, want)
			}
		})
	}
}

// A hop that answered as the target (or refused) before the silent end
// contradicts "no response beyond hop N": the rows must stay as observed.
func TestWithNoResponseKeepsRowsWhenATerminalHopPrecedesTheTail(t *testing.T) {
	cases := []struct {
		name string
		res  *nettrace.Result
	}{
		{"deadline, target at TTL 3", nrResult(nettrace.OutcomeDeadline, nrTerminalBeforeTail(nettrace.HopTarget))},
		{"deadline, unreachable at TTL 3", nrResult(nettrace.OutcomeDeadline, nrTerminalBeforeTail(nettrace.HopUnreachable))},
		{"max hops, unreachable at TTL 3", nrResult(nettrace.OutcomeMaxHops, nrTerminalBeforeTail(nettrace.HopUnreachable))},
	}
	for _, tc := range cases {
		t.Run(tc.name, func(t *testing.T) {
			if tail, ok := silentTail(tc.res); ok {
				t.Errorf("silentTail = %+v, true; want false", tail)
			}
			route := &pathRoute{Known: true, Interface: "eth0", Gateway: netip.MustParseAddr("10.0.0.1")}
			steps := pathSteps(nil, route, tc.res.Hops)
			got := withNoResponse(steps, tc.res)
			if !reflect.DeepEqual(got, steps) {
				t.Errorf("rows changed: %q, want %q", nrCells(got), nrCells(steps))
			}
			for _, s := range got {
				if s.Kind == stepNoResponse || s.LastAnswer || strings.Contains(s.addressCell(), lastAnswerTag) {
					t.Errorf("row %q carries a mark, want none", s.addressCell())
				}
			}
		})
	}
}

func TestWithNoResponseLeavesOtherOutcomesAlone(t *testing.T) {
	for _, o := range []nettrace.Outcome{nettrace.OutcomeReached, nettrace.OutcomeUnreachable, nettrace.OutcomeCancelled} {
		t.Run(o.String(), func(t *testing.T) {
			res := nrResult(o, nrHops(10, 3))
			steps := pathSteps(nil, nil, res.Hops)
			if got := withNoResponse(steps, res); !reflect.DeepEqual(got, steps) {
				t.Errorf("rows changed for %v", o)
			}
			if _, ok := silentTail(res); ok {
				t.Errorf("silentTail ok for %v, want false", o)
			}
		})
	}
	steps := pathSteps(nil, nil, nrHops(10, 3))
	if got := withNoResponse(steps, nil); !reflect.DeepEqual(got, steps) {
		t.Errorf("nil result changed the rows")
	}
	if _, ok := silentTail(nil); ok {
		t.Errorf("silentTail(nil) ok, want false")
	}
}

func TestWithNoResponseDoesNotModifyItsInputs(t *testing.T) {
	res := nrResult(nettrace.OutcomeMaxHops, nrHops(30, 2))
	steps := pathSteps(nil, &pathRoute{Known: true}, res.Hops)
	stepsBefore := append([]pathStep(nil), steps...)
	hopsBefore := append([]nettrace.Hop(nil), res.Hops...)
	_ = withNoResponse(steps, res)
	if !reflect.DeepEqual(steps, stepsBefore) || !reflect.DeepEqual(res.Hops, hopsBefore) {
		t.Errorf("withNoResponse modified its input")
	}
}

// Criterion 3: no wording that says the target is down.
func TestNoResponseNeverSaysDown(t *testing.T) {
	res := nrResult(nettrace.OutcomeMaxHops, nrHops(30, 1))
	pm := resolvedWith(t, lanRoute(), nil)
	pm, _ = pmFeed(t, pm, pathDoneMsg{gen: pm.gen, result: *res})

	var texts []string
	for _, s := range pm.table.Items() {
		texts = append(texts, s.hopCell(), s.stepCell(), s.addressCell(), s.rttCell())
	}
	for _, h := range pm.headerInfo() {
		if h.Key == "Path" {
			texts = append(texts, h.Value)
		}
	}
	joined := strings.ToLower(strings.Join(texts, "\n"))
	for _, w := range []string{"down", "offline", "failed", "unreachable"} {
		if strings.Contains(joined, w) {
			t.Errorf("table or header contains %q:\n%s", w, joined)
		}
	}
	if !strings.Contains(joined, "no response beyond hop 1 (hops 2 to 30)") {
		t.Errorf("grouped row missing:\n%s", joined)
	}
}

func TestWithNoResponseWSLMarksGatewayAndLastAnswer(t *testing.T) {
	res := nrResult(nettrace.OutcomeMaxHops, []nettrace.Hop{wslHop1,
		{TTL: 2, Kind: nettrace.HopSilent}, {TTL: 3, Kind: nettrace.HopSilent}})
	steps := withWSL(withNoResponse(pathSteps(nil, wslRoute, res.Hops), res), wslInfoNAT)
	hops := nrHopRows(steps)
	if len(hops) != 2 {
		t.Fatalf("hop rows = %d, want 2", len(hops))
	}
	if got, want := hops[0].addressCell(), wslHostCell+lastAnswerTag; got != want {
		t.Errorf("TTL 1 cell = %q, want %q (Windows host, then last to answer)", got, want)
	}
	if got, want := hops[1].addressCell(), "no response beyond hop 1 (hops 2 to 3)"; got != want {
		t.Errorf("grouped cell = %q, want %q", got, want)
	}
}

func TestHopCellOfTheGroupedRowFitsTheColumn(t *testing.T) {
	for _, first := range []int{1, 9, 12, 30} {
		for _, last := range []int{first, 30} {
			s := pathStep{Kind: stepNoResponse, Tail: pathTail{First: first, Last: last}}
			if w := ansi.StringWidth(s.hopCell()); w > 3 {
				t.Errorf("hopCell %q is %d wide, the Hop column is 3", s.hopCell(), w)
			}
		}
	}
	if got := (pathStep{Kind: stepNoResponse, Tail: pathTail{First: 12, Last: 30}}).hopCell(); got != "12+" {
		t.Errorf("hopCell = %q, want 12+", got)
	}
}

// The stream shows hops one by one; only the done message groups them.
func TestPathFlowSilentEndIsGroupedOnDone(t *testing.T) {
	final := nrHops(30, 3)
	f := &fakeEngine{traceFn: func(_ context.Context, a netip.Addr, _ nettrace.Options, onHop func(nettrace.Hop)) (nettrace.Result, error) {
		for _, h := range final {
			onHop(h)
		}
		return nettrace.Result{Target: a, Outcome: nettrace.OutcomeMaxHops, Hops: final}, nil
	}}
	pm := newPathModel(f.engine())
	pm.resize(120, 40)
	pm, _ = pmFeed(t, pm, testutil.Type("1.1.1.1")...)
	pm, _ = pmFeed(t, pm, testutil.Key("enter"))

	ch := make(chan tea.Msg, pathChanCap)
	producePath(context.Background(), pm.gen, pm.target, hostSource(), pm.resolveTimeout, pm.engine, ch)
	streamed := -1
	for msg := range ch {
		pm, _ = pmFeed(t, pm, msg)
		if pm.result == nil {
			streamed = len(pm.table.Items())
		}
	}
	if pm.result == nil {
		t.Fatal("no result after the done message")
	}
	if streamed < 30 {
		t.Errorf("rows while streaming = %d, want every hop shown", streamed)
	}
	want := []string{
		"1|router|10.0.0.1|-", "2|router|10.0.0.2|-",
		"3|router|10.0.0.3" + lastAnswerTag + "|-",
		"4+|silent|no response beyond hop 3 (hops 4 to 30)|-",
	}
	if got := nrCells(pm.table.Items()); !reflect.DeepEqual(got, want) {
		t.Errorf("hop rows = %q, want %q", got, want)
	}
	for _, h := range pm.headerInfo() {
		if h.Key == "Path" && h.Value != "No answer from the target within 30 hops" {
			t.Errorf("Path header = %q, want it unchanged", h.Value)
		}
	}
}

// Step 3 review: the platform survives a no-route end.
func TestPathNoRouteKeepsThePlatform(t *testing.T) {
	pm, _ := newPM(t)
	pm, _ = launched(t, pm, "1.1.1.1")
	pm, _ = pmFeed(t, pm, pathResolvedMsg{
		gen: pm.gen, addr: netip.MustParseAddr("1.1.1.1"), route: defaultRouteHop(), platform: wslInfoNAT,
	})
	pm = pmAfterNoRoute(t, pm)
	if pm.platform != wslInfoNAT {
		t.Errorf("pm.platform = %+v, want it kept (%+v)", pm.platform, wslInfoNAT)
	}
	if wslLabelled(pm.table.Items()) {
		t.Errorf("Address column = %q, want no Windows host label without a proven gateway", pmAddrs(pm))
	}
}

// Step 3 review: a raw errno, not wrapped in ErrNoRoute, is still a no-route end.
func TestPathRawENETUNREACHKeepsTheLocalRows(t *testing.T) {
	for _, errno := range []error{syscall.ENETUNREACH, syscall.EHOSTUNREACH} {
		pm := resolvedWith(t, lanRoute(), nil)
		pm, _ = pmFeed(t, pm, pathDoneMsg{gen: pm.gen, err: errno})
		if got, want := pmSteps(pm), []string{"source", "interface", "gateway"}; !reflect.DeepEqual(got, want) {
			t.Errorf("%v: Step column = %v, want %v", errno, got, want)
		}
		if pm.route == nil || !pm.route.NoRoute {
			t.Errorf("%v: route = %+v, want NoRoute", errno, pm.route)
		}
	}
}
