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

	tea "charm.land/bubbletea/v2"
	"github.com/charmbracelet/x/ansi"

	"github.com/anthnel/devdesk/internal/netcheck"
	"github.com/anthnel/devdesk/internal/ui/keymap"
	"github.com/anthnel/devdesk/internal/ui/testutil"
)

// DEVDESK-473 step 4 (red): Diagnostics and the path trace share one reading of
// the local route. These tests run both ends of the story against ONE fake Env:
// the Diagnostics pipeline asks it through Model.checkEnv, and the trace's
// engine asks it through pathEngine.route. The Env counts its Route calls per
// address, so "read once" is a number, and it answers with a different
// interface name on each call (eth1, eth2, ...), so the trace's rows say which
// reading they came from. Nothing leaves the process.

const (
	onceHostA = "93.184.216.34"
	onceHostB = "198.51.100.7"
)

// seqEnv is a countingEnv whose Route answers eth<n> on the n-th call for an
// address, unless answer says otherwise, and whose Resolve can be told what a
// name resolves to.
type seqEnv struct {
	*countingEnv
	// answer, when set, decides one Route call: ip and its 1-based count.
	answer func(ip net.IP, n int) (netcheck.RouteHop, error)
	// resolves maps a name to the addresses Diagnostics resolves it to.
	resolves map[string][]string
}

func newSeqEnv() *seqEnv { return &seqEnv{countingEnv: newCountingEnv()} }

func (e *seqEnv) Resolve(ctx context.Context, host, server string) ([]net.IP, error) {
	if addrs, ok := e.resolves[host]; ok {
		var out []net.IP
		for _, a := range addrs {
			out = append(out, net.ParseIP(a))
		}
		return out, nil
	}
	return e.countingEnv.Resolve(ctx, host, server)
}

func (e *seqEnv) Route(ctx context.Context, ip net.IP) (netcheck.RouteHop, error) {
	_, _ = e.countingEnv.Route(ctx, ip) // counts the call
	n := e.routeCalls(ip.String())
	if e.answer != nil {
		return e.answer(ip, n)
	}
	return seqHop(n), nil
}

func seqHop(n int) netcheck.RouteHop {
	return netcheck.RouteHop{
		Interface: fmt.Sprintf("eth%d", n),
		Source:    net.ParseIP("192.168.1.21"),
		Gateway:   net.ParseIP("192.168.1.1"),
	}
}

// routeOnceModel returns a model whose Diagnostics pipeline and whose trace
// engine share one seqEnv. The fake engine resolves any name to onceHostA unless
// fe already says otherwise.
func routeOnceModel(t *testing.T, fe *fakeEngine) (*Model, *seqEnv) {
	t.Helper()
	env := newSeqEnv()
	fe.routeFn = func(ctx context.Context, a netip.Addr) (netcheck.RouteHop, error) {
		return env.Route(ctx, net.IP(a.AsSlice()))
	}
	if fe.traceFn == nil {
		fe.traceFn = oneHopTrace
	}
	if fe.resolveFn == nil {
		fe.resolveFn = func(context.Context, string) ([]netip.Addr, error) {
			return []netip.Addr{netip.MustParseAddr(onceHostA)}, nil
		}
	}
	m := newTestModel(t)
	m.checkEnv = func(netcheck.Settings) netcheck.Env { return env }
	m.pathModel.engine = fe.engine()
	return m, env
}

// runAgain starts a new run of target from a form that may already hold one.
func runAgain(t *testing.T, m *Model, target string) *Model {
	t.Helper()
	m.targetInput.SetValue("")
	return runDiagnostics(t, m, target)
}

// runStages feeds the stages of a run started by cmd until the results are in.
func runStages(t *testing.T, m *Model, cmd tea.Cmd) *Model {
	t.Helper()
	for i := 0; cmd != nil && m.state == StateRunning; i++ {
		if i > len(netcheck.Steps())+1 {
			t.Fatalf("the pipeline did not settle after %d stages", i)
		}
		done, ok := cmd().(stageDoneMsg)
		if !ok {
			t.Fatal("a stage did not return a stageDoneMsg")
		}
		m, cmd = step(t, m, done)
	}
	if m.state != StateResults {
		t.Fatalf("state = %v after the run, want StateResults", m.state)
	}
	return m
}

// drainTrace feeds the model what a launch Cmd produces, then every message the
// producer sends until it closes, and returns once the trace is done.
func drainTrace(t *testing.T, m *Model, cmd tea.Cmd) *Model {
	t.Helper()
	ch := m.pathModel.msgs
	if cmd == nil || ch == nil {
		t.Fatalf("no trace was launched (cmd nil: %v, channel nil: %v)", cmd == nil, ch == nil)
	}
	for _, msg := range testutil.Msgs(cmd) {
		m = feed(t, m, msg)
	}
	for msg := range ch {
		m = feed(t, m, msg)
	}
	if m.pathModel.state != pathDone {
		t.Fatalf("path state = %v after the drain, want pathDone", m.pathModel.state)
	}
	return m
}

// startTrace presses enter on the trace level and drains the run.
func startTrace(t *testing.T, m *Model) *Model {
	t.Helper()
	if m.state != StateTrace {
		t.Fatalf("state = %v, want StateTrace", m.state)
	}
	m, cmd := step(t, m, testutil.Key("enter"))
	return drainTrace(t, m, cmd)
}

// traceFromResults opens the trace level with H on the Local route row, then
// starts it.
func traceFromResults(t *testing.T, m *Model) *Model {
	t.Helper()
	m = selectCheck(t, m, netcheck.CheckRoute)
	m = feed(t, m, testutil.Key(keymap.Diagnose))
	if m.state != StateTrace {
		t.Fatalf("state = %v after H, want StateTrace", m.state)
	}
	return startTrace(t, m)
}

// traceFromForm presses the Trace Path button on a form holding target.
func traceFromForm(t *testing.T, m *Model, target string) *Model {
	t.Helper()
	if m.state != StateInput {
		t.Fatalf("state = %v, want the form", m.state)
	}
	m.targetInput.SetValue(target)
	m.focusedField = fieldTraceButton
	m, cmd := step(t, m, testutil.Key("enter"))
	if m.state != StateTrace {
		t.Fatalf("state = %v after Trace Path, want StateTrace", m.state)
	}
	return drainTrace(t, m, cmd)
}

// localRows returns the Address cells of the trace's source, interface and
// gateway rows.
func localRows(t *testing.T, m *Model) []string {
	t.Helper()
	rows := pmAddrs(m.pathModel)
	if len(rows) < 3 {
		t.Fatalf("Address column = %q, want the three local rows", rows)
	}
	return rows[:3]
}

// requireLocalRows checks the interface and gateway rows of the last trace.
func requireLocalRows(t *testing.T, m *Model, iface, gateway string) {
	t.Helper()
	want := []string{"this machine", iface, gateway}
	if got := localRows(t, m); !reflect.DeepEqual(got, want) {
		t.Errorf("local rows = %q, want %q", got, want)
	}
}

// requireRouteCalls fails when env saw a number of Route calls other than want
// for ip.
func requireRouteCalls(t *testing.T, env *seqEnv, ip string, want int) {
	t.Helper()
	if got := env.routeCalls(ip); got != want {
		t.Errorf("route calls for %s = %d, want %d (total %d)", ip, got, want, env.totalRouteCalls())
	}
}

// ── Criterion 1: Diagnostics then the trace of the same target ──────────────

func TestRouteOnceDiagnosticsThenTraceFromTheRouteRow(t *testing.T) {
	m, env := routeOnceModel(t, &fakeEngine{})
	m = runDiagnostics(t, m, onceHostA)
	requireRouteCalls(t, env, onceHostA, 1)

	m = traceFromResults(t, m)

	requireRouteCalls(t, env, onceHostA, 1)
	requireLocalRows(t, m, "eth1 (192.168.1.21)", "192.168.1.1")
}

func TestRouteOnceDiagnosticsThenTraceOfANameReusesTheAddressLookup(t *testing.T) {
	m, env := routeOnceModel(t, &fakeEngine{})
	m = runDiagnostics(t, m, "example.com") // the Env resolves it to onceHostA
	requireRouteCalls(t, env, onceHostA, 1)

	m = traceFromResults(t, m)

	requireRouteCalls(t, env, onceHostA, 1)
	requireLocalRows(t, m, "eth1 (192.168.1.21)", "192.168.1.1")
}

// "Trace again" and a return to the results keep the reading while the results
// are still there.
func TestRouteOnceTraceAgainReusesTheReading(t *testing.T) {
	m, env := routeOnceModel(t, &fakeEngine{})
	m = runDiagnostics(t, m, onceHostA)
	m = traceFromResults(t, m)

	m = startTrace(t, m) // enter on a finished trace: Trace again

	requireRouteCalls(t, env, onceHostA, 1)
	requireLocalRows(t, m, "eth1 (192.168.1.21)", "192.168.1.1")
}

func TestRouteOnceBackToTheResultsThenHAgainReuses(t *testing.T) {
	m, env := routeOnceModel(t, &fakeEngine{})
	m = runDiagnostics(t, m, onceHostA)
	m = traceFromResults(t, m)
	m = feed(t, m, testutil.Key("esc"))
	if m.state != StateResults {
		t.Fatalf("state = %v after esc, want StateResults", m.state)
	}

	m = traceFromResults(t, m)

	requireRouteCalls(t, env, onceHostA, 1)
	requireLocalRows(t, m, "eth1 (192.168.1.21)", "192.168.1.1")
}

// ── Criterion 2: no Diagnostics before ──────────────────────────────────────

func TestRouteOnceTracePathWithoutDiagnosticsReadsOnce(t *testing.T) {
	m, env := routeOnceModel(t, &fakeEngine{})

	m = traceFromForm(t, m, onceHostA)

	requireRouteCalls(t, env, onceHostA, 1)
	requireLocalRows(t, m, "eth1 (192.168.1.21)", "192.168.1.1")
}

// ── Criterion 3: no stale reading ───────────────────────────────────────────

// The form is only reachable with the results gone, so Trace Path from it reads
// the route itself, even for the target Diagnostics just read.
func TestRouteOnceBackToTheFormThenTracePathReadsAgain(t *testing.T) {
	m, env := routeOnceModel(t, &fakeEngine{})
	m = runDiagnostics(t, m, onceHostA)
	m = feed(t, m, testutil.Key(keymap.New)) // N: back to the form, results gone
	if m.state != StateInput {
		t.Fatalf("state = %v after N, want the form", m.state)
	}

	m = traceFromForm(t, m, onceHostA)

	requireRouteCalls(t, env, onceHostA, 2)
	requireLocalRows(t, m, "eth2 (192.168.1.21)", "192.168.1.1")
}

func TestRouteOnceEditedFormTargetReadsAgain(t *testing.T) {
	m, env := routeOnceModel(t, &fakeEngine{})
	m = traceFromForm(t, m, onceHostA)
	m = feed(t, m, testutil.Key("esc")) // back to the form
	if m.state != StateInput {
		t.Fatalf("state = %v after esc, want the form", m.state)
	}

	m = traceFromForm(t, m, onceHostB)
	requireRouteCalls(t, env, onceHostB, 1)
	requireRouteCalls(t, env, onceHostA, 1)

	m = feed(t, m, testutil.Key("esc"))
	m = traceFromForm(t, m, onceHostA) // back to A: nothing of the first run is kept
	requireRouteCalls(t, env, onceHostA, 2)
	requireLocalRows(t, m, "eth2 (192.168.1.21)", "192.168.1.1")
}

func TestRouteOnceNewRunOfAnotherTargetReplacesTheReading(t *testing.T) {
	m, env := routeOnceModel(t, &fakeEngine{})
	m = runDiagnostics(t, m, onceHostA)
	m = feed(t, m, testutil.Key(keymap.New))
	m = runAgain(t, m, onceHostB)
	requireRouteCalls(t, env, onceHostB, 1)

	m = traceFromResults(t, m)

	requireRouteCalls(t, env, onceHostA, 1) // the reading of A was not asked for again
	requireRouteCalls(t, env, onceHostB, 1) // and the trace of B used its own
	requireLocalRows(t, m, "eth1 (192.168.1.21)", "192.168.1.1")
}

func TestRouteOnceTraceOfAAfterResultsOfBReadsAgain(t *testing.T) {
	m, env := routeOnceModel(t, &fakeEngine{})
	m = runDiagnostics(t, m, onceHostA)
	m = feed(t, m, testutil.Key(keymap.New))
	m = runAgain(t, m, onceHostB)
	m = feed(t, m, testutil.Key(keymap.New))

	m = traceFromForm(t, m, onceHostA)

	requireRouteCalls(t, env, onceHostA, 2)
	requireRouteCalls(t, env, onceHostB, 1)
	requireLocalRows(t, m, "eth2 (192.168.1.21)", "192.168.1.1")
}

func TestRouteOnceCtrlRReadsAgainAndTheTraceUsesTheNewReading(t *testing.T) {
	m, env := routeOnceModel(t, &fakeEngine{})
	m = runDiagnostics(t, m, onceHostA)
	m, cmd := step(t, m, testutil.Key("ctrl+r"))
	m = runStages(t, m, cmd)
	requireRouteCalls(t, env, onceHostA, 2)

	m = traceFromResults(t, m)

	requireRouteCalls(t, env, onceHostA, 2) // no third read: the trace takes the second
	requireLocalRows(t, m, "eth2 (192.168.1.21)", "192.168.1.1")
}

// Two hosts that resolve to the same address share no reading: the results
// were made for another host. This pins the host half of the validity key,
// which the address match alone would not.
func TestRouteOnceResultsOfAnotherHostAreNotReused(t *testing.T) {
	m, env := routeOnceModel(t, &fakeEngine{})
	m = runDiagnostics(t, m, "a.example") // both names resolve to onceHostA
	m.targetInput.SetValue("b.example")   // the form is not editable here; the key guard still holds

	m = traceFromResults(t, m)

	requireRouteCalls(t, env, onceHostA, 2)
	requireLocalRows(t, m, "eth2 (192.168.1.21)", "192.168.1.1")
}

// The trace's own resolution gave another address than the one Diagnostics
// looked up: that address has no reading yet.
func TestRouteOnceAResolutionToAnotherAddressReadsAgain(t *testing.T) {
	fe := &fakeEngine{resolveFn: func(context.Context, string) ([]netip.Addr, error) {
		return []netip.Addr{netip.MustParseAddr(onceHostB)}, nil
	}}
	m, env := routeOnceModel(t, fe)
	m = runDiagnostics(t, m, "example.com") // looked up onceHostA

	m = traceFromResults(t, m)

	requireRouteCalls(t, env, onceHostA, 1)
	requireRouteCalls(t, env, onceHostB, 1)
	requireLocalRows(t, m, "eth1 (192.168.1.21)", "192.168.1.1")
}

// ── Criterion 4: one reading, one label ─────────────────────────────────────

func TestRouteOnceNoRouteAndUndeterminedShowTheDiagnosticsLabel(t *testing.T) {
	tests := []struct {
		name        string
		err         error
		wantVerdict netcheck.Verdict
		wantRows    []string
	}{
		{
			"no route", fmt.Errorf("route: %w", syscall.ENETUNREACH),
			netcheck.Fail, []string{"this machine", "no route", "-"},
		},
		{
			"undetermined", errors.New("routing table unreadable"),
			netcheck.Unknown, []string{"this machine", "undetermined", "undetermined"},
		},
	}
	for _, tc := range tests {
		t.Run(tc.name, func(t *testing.T) {
			m, env := routeOnceModel(t, &fakeEngine{})
			env.answer = func(net.IP, int) (netcheck.RouteHop, error) { return netcheck.RouteHop{}, tc.err }
			m = runDiagnostics(t, m, onceHostA)
			if c, _ := m.results.Get(netcheck.CheckRoute); c.Verdict != tc.wantVerdict {
				t.Fatalf("Diagnostics verdict = %v, want %v", c.Verdict, tc.wantVerdict)
			}

			m = traceFromResults(t, m)

			requireRouteCalls(t, env, onceHostA, 1)
			if got := localRows(t, m); !reflect.DeepEqual(got, tc.wantRows) {
				t.Errorf("trace rows = %q, want %q", got, tc.wantRows)
			}
		})
	}
}

// A lookup that ended on a cancellation says nothing about the routing table:
// the trace reads again instead of showing a false "undetermined".
func TestRouteOnceACanceledReadIsNotReused(t *testing.T) {
	m, env := routeOnceModel(t, &fakeEngine{})
	env.answer = func(_ net.IP, n int) (netcheck.RouteHop, error) {
		if n == 1 {
			return netcheck.RouteHop{}, fmt.Errorf("route: %w", context.Canceled)
		}
		return seqHop(n), nil
	}
	m = runDiagnostics(t, m, onceHostA)

	m = traceFromResults(t, m)

	requireRouteCalls(t, env, onceHostA, 2)
	requireLocalRows(t, m, "eth2 (192.168.1.21)", "192.168.1.1")
}

// ── Criterion 5: the Diagnostics row does not change ────────────────────────

// routeRowView is the Local route row the way the user reads it: the
// check's verdict, summary and facts, and the text of its explanation.
type routeRowView struct {
	verdict netcheck.Verdict
	reason  netcheck.Reason
	summary string
	facts   []netcheck.Fact
	details string
}

func routeRowOf(t *testing.T, m *Model) routeRowView {
	t.Helper()
	m = selectCheck(t, m, netcheck.CheckRoute)
	m = feed(t, m, testutil.Key("enter"))
	if m.state != StateDetails {
		t.Fatalf("state = %v after enter, want StateDetails", m.state)
	}
	c := m.selected
	row := routeRowView{
		verdict: c.Verdict, reason: c.Reason, summary: c.Summary,
		facts:   append([]netcheck.Fact(nil), c.Facts...),
		details: ansi.Strip(m.renderDetailsContent(100)),
	}
	feed(t, m, testutil.Key("esc"))
	return row
}

func TestRouteOnceDiagnosticsRouteRowIsUnchangedByTheTrace(t *testing.T) {
	alone, aloneEnv := routeOnceModel(t, &fakeEngine{})
	alone = runDiagnostics(t, alone, onceHostA)
	want := routeRowOf(t, alone)

	traced, tracedEnv := routeOnceModel(t, &fakeEngine{})
	traced = runDiagnostics(t, traced, onceHostA)
	traced = traceFromResults(t, traced)
	traced = feed(t, traced, testutil.Key("esc"))
	got := routeRowOf(t, traced)

	if !reflect.DeepEqual(got, want) {
		t.Errorf("route row after a trace = %+v, want the row of a run without one %+v", got, want)
	}
	if want.summary != "Traffic leaves through eth1" {
		t.Errorf("summary = %q, want the first reading's interface", want.summary)
	}
	requireRouteCalls(t, aloneEnv, onceHostA, 1)
	requireRouteCalls(t, tracedEnv, onceHostA, 1) // the trace did not add a second reading
}

// Diagnostics reads every address of the resolution once; the trace of one of
// them adds nothing.
func TestRouteOnceEachAddressIsReadOnce(t *testing.T) {
	m, env := routeOnceModel(t, &fakeEngine{})
	env.resolves = map[string][]string{"multi.example": {onceHostA, onceHostB}}
	m = runDiagnostics(t, m, "multi.example")
	requireRouteCalls(t, env, onceHostA, 1)
	requireRouteCalls(t, env, onceHostB, 1)

	m = traceFromResults(t, m) // the fake engine traces the first address

	requireRouteCalls(t, env, onceHostA, 1)
	requireRouteCalls(t, env, onceHostB, 1)
	requireLocalRows(t, m, "eth1 (192.168.1.21)", "192.168.1.1")
}

// ── A container source and the host the results belong to ───────────────────

// chooseSource opens "Trace from" on the trace level, goes to the top of the
// list (this machine) then down n entries, and presses enter. It launches
// nothing.
func chooseSource(t *testing.T, m *Model, n int) *Model {
	t.Helper()
	m, cmd := step(t, m, testutil.Key("ctrl+o"))
	m = feed(t, m, testutil.Msgs(cmd)...)
	m = feed(t, m, testutil.Key("home"))
	for range n {
		m = feed(t, m, testutil.Key("down"))
	}
	return feed(t, m, testutil.Key("enter"))
}

// A trace launched from a container reads its own route on every launch;
// choosing this machine again goes back to the Diagnostics reading.
func TestRouteOnceAContainerSourceReadsItsOwnRoute(t *testing.T) {
	fe := runningFake()
	fe.inspectNet = appNetworking()
	m, env := routeOnceModel(t, fe)
	m = runDiagnostics(t, m, onceHostA)
	m = selectCheck(t, m, netcheck.CheckRoute)
	m = feed(t, m, testutil.Key(keymap.Diagnose))

	m = chooseSource(t, m, 1)
	if m.pathModel.source.isHost() {
		t.Fatal("no container was chosen as the source")
	}
	m = startTrace(t, m)
	requireRouteCalls(t, env, onceHostA, 2)
	if r := m.pathModel.route; r == nil || r.Interface != "eth2" {
		t.Errorf("container trace route = %+v, want its own reading eth2", r)
	}

	m = startTrace(t, m) // Trace again from the container
	requireRouteCalls(t, env, onceHostA, 3)
	if r := m.pathModel.route; r == nil || r.Interface != "eth3" {
		t.Errorf("second container trace route = %+v, want a new reading eth3", r)
	}

	m = chooseSource(t, m, 0) // this machine again
	if !m.pathModel.source.isHost() {
		t.Fatalf("source = %+v, want this machine", m.pathModel.source)
	}
	m = startTrace(t, m)
	requireRouteCalls(t, env, onceHostA, 3) // the Diagnostics reading answers again
	requireLocalRows(t, m, "eth1 (192.168.1.21)", "192.168.1.1")
}

// StartRun records the host of the run, resetToForm forgets it.
func TestRouteOnceResultsHostFollowsTheResults(t *testing.T) {
	m, _ := routeOnceModel(t, &fakeEngine{})
	if m.resultsHost != "" {
		t.Fatalf("resultsHost = %q on a fresh model, want none", m.resultsHost)
	}

	m = runDiagnostics(t, m, "a.example")
	if m.resultsHost != "a.example" {
		t.Errorf("resultsHost = %q after a run, want a.example", m.resultsHost)
	}

	m = feed(t, m, testutil.Key(keymap.New))
	if m.resultsHost != "" {
		t.Errorf("resultsHost = %q after N, want it cleared with the results", m.resultsHost)
	}

	m = runAgain(t, m, "b.example")
	m, cmd := step(t, m, testutil.Key("ctrl+r"))
	if m.resultsHost != "b.example" {
		t.Errorf("resultsHost = %q after ctrl+r, want b.example", m.resultsHost)
	}
	_ = runStages(t, m, cmd)
}
