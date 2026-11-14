package netdiag

import (
	"context"
	"errors"
	"fmt"
	"net/netip"
	"slices"
	"strings"
	"testing"

	tea "charm.land/bubbletea/v2"
	"github.com/charmbracelet/x/ansi"

	"github.com/anthnel/devdesk/internal/docker"
	"github.com/anthnel/devdesk/internal/nettrace"
	"github.com/anthnel/devdesk/internal/ui/components"
	"github.com/anthnel/devdesk/internal/ui/testutil"
)

// DEVDESK-330 step 6: the selector and the container source wired into the
// Path tab. The engine is always the fake: nothing here reads a real engine.

const (
	loadingText = "Reading running containers..."
)

var webSource = pathSource{ID: idWeb, Name: "web-1", Image: "nginx"}

// runningFake offers api and web-1 (running) and db (paused).
func runningFake() *fakeEngine {
	return &fakeEngine{psRows: []docker.Container{
		{ID: idWeb, Name: "web-1", Image: "nginx", State: "running"},
		{ID: idAPI, Name: "api", Image: "app", State: "running"},
		{ID: idDB, Name: "db", Image: "postgres", State: "paused"},
	}}
}

// appNetworking is web-1 on one network, app_net.
func appNetworking() docker.ContainerNetworking {
	return docker.ContainerNetworking{
		Name: "web-1", Running: true, Mode: "app_net",
		Networks: []docker.EndpointNetwork{{
			Name:    "app_net",
			IPv4:    netip.MustParseAddr("172.18.0.5"),
			Gateway: netip.MustParseAddr("172.18.0.1"),
		}},
	}
}

func pmWith(t *testing.T, f *fakeEngine) *PathModel {
	t.Helper()
	pm := newPathModel(f.engine())
	pm.resize(120, 30)
	return pm
}

// pressedSourceKey presses ctrl+o and returns the model, the Cmd and the
// message the Cmd answers with.
func pressedSourceKey(t *testing.T, pm *PathModel) (*PathModel, tea.Msg) {
	t.Helper()
	pm, cmd := pmFeed(t, pm, testutil.Key("ctrl+o"))
	return pm, testutil.Msg(cmd)
}

// openedPicker presses ctrl+o and delivers the answer of the listing.
func openedPicker(t *testing.T, pm *PathModel) *PathModel {
	t.Helper()
	pm, msg := pressedSourceKey(t, pm)
	if _, ok := msg.(pathSourcesMsg); !ok {
		t.Fatalf("ctrl+o produced %T, want a pathSourcesMsg from the listing Cmd", msg)
	}
	pm, _ = pmFeed(t, pm, msg)
	return pm
}

// chosen opens the selector, moves down n entries (0 is this machine) and
// presses enter.
func chosen(t *testing.T, pm *PathModel, n int) *PathModel {
	t.Helper()
	pm = openedPicker(t, pm)
	for range n {
		pm, _ = pmFeed(t, pm, testutil.Key("down"))
	}
	pm, _ = pmFeed(t, pm, testutil.Key("enter"))
	return pm
}

func rowKinds(pm *PathModel) []string {
	var out []string
	for _, s := range pm.table.Items() {
		out = append(out, s.stepCell())
	}
	return out
}

func rowAddress(pm *PathModel, i int) string {
	items := pm.table.Items()
	if i >= len(items) {
		return "<no row>"
	}
	return items[i].addressCell()
}

// donePM returns a model whose trace of 1.1.1.1 from src is done, with one hop.
func donePM(t *testing.T, f *fakeEngine, src pathSource) *PathModel {
	t.Helper()
	pm := pmWith(t, f)
	if !src.isHost() {
		pm.source = src
	}
	pm, _ = launched(t, pm, "1.1.1.1")
	gen := pm.gen
	pm, _ = pmFeed(t, pm,
		pathResolvedMsg{gen: gen, addr: netip.MustParseAddr("1.1.1.1"), route: defaultRouteHop(), source: src, ctr: f.inspectNet},
		pathHopMsg{gen: gen, hop: hopAt(1)},
		pathDoneMsg{gen: gen, result: nettrace.Result{Outcome: nettrace.OutcomeReached, Hops: []nettrace.Hop{hopAt(1)}}},
	)
	if pm.state != pathDone {
		t.Fatalf("precondition: state = %v, want pathDone", pm.state)
	}
	return pm
}

// Criterion 1 and the key: ctrl+o opens the selector from editing and from
// done, reads the engine in the Cmd only, and shows the loading state in the
// footer while the body stays the table (Rules 110, 128, 139).
func TestPathSourceKeyOpensTheSelector(t *testing.T) {
	for _, state := range []string{"editing", "done"} {
		t.Run(state, func(t *testing.T) {
			f := runningFake()
			pm := pmWith(t, f)
			if state == "done" {
				pm.state = pathDone
				pm.input.Blur()
			}
			baseLines := len(strings.Split(pm.view(), "\n"))
			before := pm.sourcesGen

			pm, msg := pressedSourceKey(t, pm)

			if !pm.pickerOpen() {
				t.Fatal("ctrl+o did not open the selector")
			}
			if pm.sourcesGen <= before {
				t.Errorf("sourcesGen = %d, want a fresh value above %d", pm.sourcesGen, before)
			}
			got, ok := msg.(pathSourcesMsg)
			if !ok || got.gen != pm.sourcesGen {
				t.Fatalf("the Cmd answered %#v, want a pathSourcesMsg of gen %d", msg, pm.sourcesGen)
			}
			if st := pm.statusLine(); !st.Activity || st.Text != loadingText {
				t.Errorf("statusLine while loading = %+v, want the shared activity dot with %q", st, loadingText)
			}
			body := ansi.Strip(pm.view())
			if !strings.Contains(body, "Trace from") || !strings.Contains(body, "this machine") {
				t.Errorf("the selector is not drawn over the table:\n%s", body)
			}
			if n := len(strings.Split(pm.view(), "\n")); n != baseLines {
				t.Errorf("the view has %d lines with the selector, %d without: the body must stay the table", n, baseLines)
			}
		})
	}
}

// The engine is read by the Cmd, not by Update (Rule 110).
func TestPathSourceKeyReadsTheEngineInTheCmdOnly(t *testing.T) {
	f := runningFake()
	pm := pmWith(t, f)

	pm, cmd := pmFeed(t, pm, testutil.Key("ctrl+o"))

	if !pm.pickerOpen() || cmd == nil {
		t.Fatalf("pickerOpen = %v, cmd nil = %v, want an open selector and a listing Cmd", pm.pickerOpen(), cmd == nil)
	}
	if f.osCalls+f.psCalls != 0 {
		t.Errorf("Update read the engine (info %d, ps %d); only the Cmd may", f.osCalls, f.psCalls)
	}
}

func TestPathSourceKeyReopenedGetsANewGeneration(t *testing.T) {
	pm := pmWith(t, runningFake())
	pm = openedPicker(t, pm)
	first := pm.sourcesGen
	pm, _ = pmFeed(t, pm, testutil.Key("esc"))

	pm = openedPicker(t, pm)

	if pm.sourcesGen <= first {
		t.Errorf("sourcesGen = %d after the second opening, want above %d", pm.sourcesGen, first)
	}
}

// ctrl+o is refused cleanly, with its reason at the footer, while a trace runs
// and where the trace is unsupported (Rule 130).
func TestPathSourceKeyIsRefusedWithAReason(t *testing.T) {
	cases := []struct {
		name  string
		setup func(t *testing.T, pm *PathModel) *PathModel
		want  string
	}{
		{"trace running", func(t *testing.T, pm *PathModel) *PathModel {
			pm, _ = launched(t, pm, "1.1.1.1")
			return pm
		}, reasonPathBusy},
		{"platform without a trace", func(_ *testing.T, pm *PathModel) *PathModel {
			pm.engine.supported = func() error { return errors.New("not linux") }
			return pm
		}, reasonPathUnsupported},
	}
	for _, tc := range cases {
		t.Run(tc.name, func(t *testing.T) {
			f := runningFake()
			pm := tc.setup(t, pmWith(t, f))

			pm, _ = pmFeed(t, pm, testutil.Key("ctrl+o"))

			if pm.pickerOpen() {
				t.Fatal("the selector opened")
			}
			if pm.footer.Text() != tc.want || pm.footer.Level() != components.LevelWarning {
				t.Errorf("footer = %q (level %v), want the warning %q", pm.footer.Text(), pm.footer.Level(), tc.want)
			}
			if f.osCalls+f.psCalls != 0 {
				t.Errorf("the engine was read (info %d, ps %d)", f.osCalls, f.psCalls)
			}
		})
	}
}

// Criteria 1 and 5, and the fixed notes: the list, then what the footer says.
func TestPathSourceListsAndNotes(t *testing.T) {
	cases := []struct {
		name       string
		fake       *fakeEngine
		wantRows   []string
		wantAbsent []string
		wantNote   string
	}{
		{"running containers", runningFake(), []string{"this machine", "api", "web-1"}, []string{"db"}, ""},
		{"no running container", &fakeEngine{}, []string{"this machine"}, []string{"api", "web-1"}, noteNoContainer},
		{"podman", &fakeEngine{engineNameVal: "podman", psRows: runningFake().psRows}, []string{"this machine"}, []string{"api", "web-1"}, notePodman},
		{"docker desktop", &fakeEngine{osName: "Docker Desktop", psRows: runningFake().psRows}, []string{"this machine"}, []string{"api", "web-1"}, noteDesktop},
		{"engine absent", &fakeEngine{osErr: errors.New("exec: docker: not found secret-raw")}, []string{"this machine"}, []string{"api"}, noteNoEngine},
		{"ps failure", &fakeEngine{psErr: errors.New("daemon down secret-raw")}, []string{"this machine"}, []string{"api"}, noteNoEngine},
	}
	for _, tc := range cases {
		t.Run(tc.name, func(t *testing.T) {
			captureLog(t)
			pm := openedPicker(t, pmWith(t, tc.fake))

			body := ansi.Strip(pm.view())
			for _, want := range tc.wantRows {
				if !strings.Contains(body, want) {
					t.Errorf("the selector lacks %q:\n%s", want, body)
				}
			}
			for _, absent := range tc.wantAbsent {
				if strings.Contains(body, absent) {
					t.Errorf("the selector offers %q, which it must not:\n%s", absent, body)
				}
			}
			st := pm.statusLine()
			if st.Text != tc.wantNote || st.Level != components.LevelInfo || st.Activity {
				t.Errorf("statusLine = %+v, want the neutral note %q with no activity dot", st, tc.wantNote)
			}
			if strings.Contains(st.Text, "secret-raw") {
				t.Errorf("the raw engine error reached the footer: %q", st.Text)
			}
		})
	}
}

// A listing of another generation, or one arriving after the selector closed,
// changes nothing.
func TestPathSourcesMessageStaleIsDropped(t *testing.T) {
	list := pathSourceList{Containers: []pathSource{webSource}}

	t.Run("older generation", func(t *testing.T) {
		pm, msg := pressedSourceKey(t, pmWith(t, &fakeEngine{}))
		_ = msg
		pm, _ = pmFeed(t, pm, pathSourcesMsg{gen: pm.sourcesGen - 1, list: list})

		if st := pm.statusLine(); !st.Activity || st.Text != loadingText {
			t.Errorf("statusLine = %+v, want the selector still loading", st)
		}
		if strings.Contains(ansi.Strip(pm.view()), "web-1") {
			t.Error("a stale listing filled the selector")
		}
	})
	t.Run("selector closed", func(t *testing.T) {
		pm := pmWith(t, &fakeEngine{})
		pm, _ = pmFeed(t, pm, pathSourcesMsg{gen: pm.sourcesGen, list: list})
		if pm.pickerOpen() {
			t.Error("a listing opened the selector by itself")
		}

		pm, _ = pressedSourceKey(t, pm)
		gen := pm.sourcesGen
		pm, _ = pmFeed(t, pm, testutil.Key("esc"), pathSourcesMsg{gen: gen, list: list})
		if pm.pickerOpen() || strings.Contains(ansi.Strip(pm.view()), "web-1") {
			t.Error("a listing that answers a closed selector was not dropped")
		}
	})
}

// Leaving the view while the list loads drops the answer (the router sends it to
// the active view). Entering the view again must not leave the selector open on
// a loading dot for good: Init closes it.
func TestPathSelectorIsClosedWhenTheViewIsEnteredAgain(t *testing.T) {
	m := New(testConfig())
	m.pathModel = pmWith(t, runningFake())
	m.activeTab = tabPath
	m = feed(t, m, testutil.Key("ctrl+o"))
	if !m.pathModel.pickerOpen() {
		t.Fatal("setup: the selector did not open")
	}
	gen := m.pathModel.sourcesGen

	m.Init()

	if m.pathModel.pickerOpen() {
		t.Error("the selector is still open after the view was entered again")
	}
	m = feed(t, m, pathSourcesMsg{gen: gen, list: pathSourceList{Containers: []pathSource{webSource}}})
	if m.pathModel.pickerOpen() {
		t.Error("a late listing reopened the selector")
	}
}

// enter chooses, esc closes without a change.
func TestPathSelectorEnterChoosesAndEscCloses(t *testing.T) {
	t.Run("enter", func(t *testing.T) {
		pm := pmWith(t, runningFake())
		pm, _ = pmFeed(t, pm, testutil.Type("1.1.1.1")...)

		pm = chosen(t, pm, 2)

		if pm.pickerOpen() || pm.source.ID != idWeb || pm.source.Name != "web-1" {
			t.Errorf("pickerOpen = %v, source = %+v, want it closed on web-1", pm.pickerOpen(), pm.source)
		}
		if pm.state != pathEditing || pm.input.Value() != "1.1.1.1" {
			t.Errorf("state = %v, field = %q, want editing with the value kept", pm.state, pm.input.Value())
		}
		if v, ok := headerValue(pm, "Source"); !ok || v != "web-1" {
			t.Errorf("header Source = %q (present %v), want web-1", v, ok)
		}
	})
	t.Run("esc", func(t *testing.T) {
		pm := pmWith(t, runningFake())
		pm = openedPicker(t, pm)
		pm, _ = pmFeed(t, pm, testutil.Key("down"), testutil.Key("esc"))

		if pm.pickerOpen() || !pm.source.isHost() {
			t.Errorf("pickerOpen = %v, source = %+v, want it closed and unchanged", pm.pickerOpen(), pm.source)
		}
	})
}

// The selector keeps the keyboard: tab, a paste, letters and the palette key
// reach neither the tab row nor the field (Rules 111, 135).
func TestPathSelectorHoldsTheKeyboard(t *testing.T) {
	for _, state := range []string{"editing", "done"} {
		t.Run(state, func(t *testing.T) {
			m := onPathTab(t)
			m.pathModel.engine = runningFake().engine()
			m.pathModel.input.SetValue("abc")
			if state == "done" {
				m.pathModel.state = pathDone
				m.pathModel.input.Blur()
			}
			m, cmd := step(t, m, testutil.Key("ctrl+o"))
			m = feed(t, m, testutil.Msg(cmd))
			if !m.pathModel.pickerOpen() {
				t.Fatal("the selector is not open")
			}
			if !m.InEditMode() {
				t.Error("InEditMode is false while the selector is open: the router would open the palette on ':'")
			}

			held := []tea.Msg{
				testutil.Key("tab"), testutil.Key("shift+tab"), testutil.Key("q"), testutil.Key(":"),
				testutil.Key("x"), testutil.Paste("zz\nyy"),
			}
			for _, msg := range held {
				m = feed(t, m, msg)
				if m.activeTab != tabPath {
					t.Fatalf("%v switched the tab to %d", msg, m.activeTab)
				}
				if !m.pathModel.pickerOpen() {
					t.Fatalf("%v closed the selector", msg)
				}
				if got := m.pathModel.input.Value(); got != "abc" {
					t.Fatalf("%v reached the field: value = %q", msg, got)
				}
			}
		})
	}
}

// esc from done closes the selector and keeps the rows; esc with it closed
// returns to editing and keeps the source.
func TestPathEscKeepsTheSourceAndTheRows(t *testing.T) {
	f := runningFake()
	f.inspectNet = appNetworking()
	pm := donePM(t, f, webSource)
	rows := len(pm.table.Items())

	pm = openedPicker(t, pm)
	pm, _ = pmFeed(t, pm, testutil.Key("esc"))
	if pm.pickerOpen() || pm.state != pathDone || len(pm.table.Items()) != rows {
		t.Errorf("after esc on the selector: open %v, state %v, rows %d (want %d)", pm.pickerOpen(), pm.state, len(pm.table.Items()), rows)
	}

	pm, _ = pmFeed(t, pm, testutil.Key("esc"))
	if pm.state != pathEditing || pm.source.ID != idWeb {
		t.Errorf("after esc from done: state %v, source %+v, want editing on web-1", pm.state, pm.source)
	}
}

// Criteria 2 and 6: the launch carries the chosen source to the producer, the
// column starts with the container and its network, and the trace row says the
// hops are traced from this machine.
func TestPathLaunchWithAContainerShowsItBeforeThisMachine(t *testing.T) {
	f := runningFake()
	f.inspectNet = appNetworking()
	f.traceFn = func(_ context.Context, a netip.Addr, _ nettrace.Options, onHop func(nettrace.Hop)) (nettrace.Result, error) {
		hops := []nettrace.Hop{hopAt(1), hopAt(2)}
		for _, h := range hops {
			onHop(h)
		}
		return nettrace.Result{Target: a, Outcome: nettrace.OutcomeReached, Hops: hops}, nil
	}
	pm := pmWith(t, f)
	pm = chosen(t, pm, 2)
	pm, cmd := launched(t, pm, "1.1.1.1")

	// The batch runs the producer synchronously, then yields its first message.
	for _, msg := range testutil.Msgs(cmd) {
		pm, _ = pmFeed(t, pm, msg)
	}
	for msg := range pm.msgs {
		pm, _ = pmFeed(t, pm, msg)
	}

	if !slices.Equal(f.inspectIDs, []string{idWeb}) {
		t.Errorf("inspected %v, want exactly the chosen container %q", f.inspectIDs, idWeb)
	}
	if f.traceAddr != netip.MustParseAddr("1.1.1.1") {
		t.Errorf("traced %v, want 1.1.1.1 (the probes leave from this machine)", f.traceAddr)
	}
	kinds := rowKinds(pm)
	if want := []string{"container", "network", "source", "interface", "gateway", "trace"}; len(kinds) != 8 || !slices.Equal(kinds[:6], want) {
		t.Fatalf("rows = %v, want %v then two hops", kinds, want)
	}
	for i, want := range []string{"web-1 (172.18.0.5)", "app_net via 172.18.0.1", "this machine"} {
		if got := rowAddress(pm, i); got != want {
			t.Errorf("row %d address = %q, want %q", i, got, want)
		}
	}
	if got := rowAddress(pm, 5); got != textTraceFromHost {
		t.Errorf("trace row = %q, want %q", got, textTraceFromHost)
	}
}

// With this machine as the source nothing is inspected and neither the
// container rows nor the trace row exist.
func TestPathLaunchFromThisMachineReadsNoContainer(t *testing.T) {
	f := runningFake()
	pm := pmWith(t, f)
	pm, cmd := launched(t, pm, "1.1.1.1")

	for _, msg := range testutil.Msgs(cmd) {
		pm, _ = pmFeed(t, pm, msg)
	}

	if f.inspectCalls != 0 {
		t.Errorf("inspect called %d times for this machine, want 0", f.inspectCalls)
	}
	if kinds := rowKinds(pm); len(kinds) < 1 || kinds[0] != "source" || slices.Contains(kinds, "trace") || slices.Contains(kinds, "container") {
		t.Errorf("rows = %v, want the column to start at source with no container or trace row", kinds)
	}
}

// Criteria 3, 4 and the failed inspection, read through the model.
func TestPathResolvedBuildsTheContainerRows(t *testing.T) {
	net := func(name string, last byte, p *int) docker.EndpointNetwork {
		return docker.EndpointNetwork{
			Name:       name,
			IPv4:       netip.AddrFrom4([4]byte{172, 18, last, 5}),
			Gateway:    netip.AddrFrom4([4]byte{172, 18, last, 1}),
			GwPriority: p,
		}
	}
	cases := []struct {
		name      string
		ctr       docker.ContainerNetworking
		err       error
		wantNet   string
		wantKinds int // rows before the first hop
	}{
		{"two gateways, same priority",
			docker.ContainerNetworking{Name: "web-1", Running: true, Mode: "a", Networks: []docker.EndpointNetwork{net("a", 1, prioOf(0)), net("b", 2, prioOf(0))}},
			nil, "undetermined (2 networks)", 6},
		{"two gateways, one priority wins",
			docker.ContainerNetworking{Name: "web-1", Running: true, Mode: "a", Networks: []docker.EndpointNetwork{net("a", 1, prioOf(0)), net("b", 2, prioOf(10))}},
			nil, "b via 172.18.2.1", 6},
		{"host mode",
			docker.ContainerNetworking{Name: "web-1", Running: true, Mode: "host", Networks: []docker.EndpointNetwork{{Name: "host"}}},
			nil, textHostNetwork, 6},
		{"inspection failed",
			docker.ContainerNetworking{}, errors.New("boom-raw-secret"), textUndetermined, 6},
	}
	for _, tc := range cases {
		t.Run(tc.name, func(t *testing.T) {
			captureLog(t)
			pm := pmWith(t, runningFake())
			pm.source = webSource
			pm, _ = launched(t, pm, "1.1.1.1")
			gen := pm.gen

			pm, _ = pmFeed(t, pm,
				pathResolvedMsg{gen: gen, addr: netip.MustParseAddr("1.1.1.1"), route: defaultRouteHop(), source: webSource, ctr: tc.ctr, ctrErr: tc.err},
				pathHopMsg{gen: gen, hop: hopAt(1)},
			)

			kinds := rowKinds(pm)
			if len(kinds) != tc.wantKinds+1 || !slices.Equal(kinds[:3], []string{"container", "network", "source"}) || kinds[5] != "trace" {
				t.Fatalf("rows = %v, want container, network, source, interface, gateway, trace, then the hop", kinds)
			}
			if got := rowAddress(pm, 1); got != tc.wantNet {
				t.Errorf("network row = %q, want %q", got, tc.wantNet)
			}
			if tc.err != nil {
				if got := rowAddress(pm, 0); got != "web-1" {
					t.Errorf("container row = %q, want web-1", got)
				}
				if st := pm.statusLine(); strings.Contains(st.Text, "boom-raw-secret") || strings.Contains(pm.footer.Text(), "boom-raw-secret") {
					t.Errorf("the raw inspection error reached the footer: %q", st.Text)
				}
			}
			if tc.name == "host mode" {
				if got := rowAddress(pm, 5); got != textTraceSharedHost {
					t.Errorf("trace row = %q, want %q", got, textTraceSharedHost)
				}
			}
		})
	}
}

// The trace row sits between the gateway and the first hop in running and done
// states.
func TestPathTraceRowSitsBetweenTheGatewayAndTheHops(t *testing.T) {
	f := runningFake()
	f.inspectNet = appNetworking()

	running := pmWith(t, f)
	running.source = webSource
	running, _ = launched(t, running, "1.1.1.1")
	running, _ = pmFeed(t, running,
		pathResolvedMsg{gen: running.gen, addr: netip.MustParseAddr("1.1.1.1"), route: defaultRouteHop(), source: webSource, ctr: f.inspectNet},
		pathHopMsg{gen: running.gen, hop: hopAt(1)},
	)
	done := donePM(t, f, webSource)

	for name, pm := range map[string]*PathModel{"running": running, "done": done} {
		kinds := rowKinds(pm)
		g, tr := slices.Index(kinds, "gateway"), slices.Index(kinds, "trace")
		if g < 0 || tr != g+1 || len(kinds) != tr+2 {
			t.Errorf("%s: rows = %v, want the trace row right after the gateway and before the hop", name, kinds)
		}
	}
}

// A source change returns a done tab to editing with an empty column; choosing
// the source in use only closes the selector.
func TestPathSourceChangeClearsTheColumn(t *testing.T) {
	f := runningFake()
	f.inspectNet = appNetworking()

	t.Run("another source", func(t *testing.T) {
		pm := donePM(t, runningFake(), hostSource())
		pm = chosen(t, pm, 2) // web-1

		if n := len(pm.table.Items()); n != 0 {
			t.Errorf("rows = %d after a source change, want the column cleared", n)
		}
		if pm.state != pathEditing || !pm.input.Focused() || pm.input.Value() != "1.1.1.1" {
			t.Errorf("state %v, focused %v, value %q: want editing, focused, value kept", pm.state, pm.input.Focused(), pm.input.Value())
		}
		if pm.result != nil || pm.route != nil || pm.container != nil || len(pm.hops) != 0 {
			t.Errorf("stale run state kept: result %v route %v container %v hops %d", pm.result, pm.route, pm.container, len(pm.hops))
		}
		if v, _ := headerValue(pm, "Source"); v != "web-1" {
			t.Errorf("header Source = %q, want web-1", v)
		}
	})
	t.Run("same source", func(t *testing.T) {
		pm := donePM(t, f, webSource)
		rows := rowKinds(pm)

		pm = openedPicker(t, pm) // the cursor starts on web-1, the entry in use
		pm, _ = pmFeed(t, pm, testutil.Key("enter"))

		if pm.state != pathDone || !slices.Equal(rowKinds(pm), rows) {
			t.Errorf("state %v rows %v, want done with the rows %v kept", pm.state, rowKinds(pm), rows)
		}
	})
}

// Trace again and the Diagnostics hand-off reset what the column showed.
func TestPathRelaunchAndPrefillResetTheContainer(t *testing.T) {
	f := runningFake()
	f.inspectNet = appNetworking()

	pm := donePM(t, f, webSource)
	if pm.container == nil {
		t.Fatal("precondition: a done run from a container holds its container")
	}
	pm, _ = pmFeed(t, pm, testutil.Key("enter"))
	if pm.container != nil || len(pm.table.Items()) != 0 {
		t.Errorf("trace again kept container %v and %d rows", pm.container, len(pm.table.Items()))
	}

	pm = donePM(t, f, webSource)
	pm.prefill("example.org")
	if !pm.source.isHost() || pm.container != nil {
		t.Errorf("prefill left source %+v, container %v, want this machine and none", pm.source, pm.container)
	}
}

// Criterion 7 of the view: the composed body is exactly as wide as the table,
// at 80 and 60 columns, with no escape sequence cut.
func TestPathSelectorComposesAtTheTableWidth(t *testing.T) {
	var rows []docker.Container
	for i := range 40 {
		rows = append(rows, docker.Container{
			ID: fmt.Sprintf("%012x", i+1), Name: fmt.Sprintf("container-%02d-%s", i, strings.Repeat("x", 60)),
			Image: "registry.example.com/team/image", State: "running",
		})
	}
	for _, width := range []int{80, 60} {
		t.Run(fmt.Sprint(width), func(t *testing.T) {
			f := &fakeEngine{psRows: rows}
			pm := newPathModel(f.engine())
			pm.resize(width, 24)
			route := pathRoute{Known: true, Interface: "eth0", Source: netip.MustParseAddr("192.168.1.2"), Gateway: netip.MustParseAddr("192.168.1.1")}
			pm.route = &route
			for ttl := 1; ttl <= 10; ttl++ {
				pm.hops = append(pm.hops, hopAt(ttl))
			}
			pm.refreshSteps()
			base := strings.Split(pm.view(), "\n")

			pm = openedPicker(t, pm)
			got := strings.Split(pm.view(), "\n")

			if len(got) != len(base) {
				t.Fatalf("%d lines with the selector, %d without", len(got), len(base))
			}
			if ansi.Strip(got[0]) != ansi.Strip(base[0]) {
				t.Errorf("the table header row is covered: %q, want %q", ansi.Strip(got[0]), ansi.Strip(base[0]))
			}
			for i, line := range got {
				if w, want := ansi.StringWidth(line), ansi.StringWidth(base[i]); w != want {
					t.Errorf("line %d is %d cells wide, want %d: %q", i, w, want, ansi.Strip(line))
				}
				if plain := ansi.Strip(line); strings.ContainsRune(plain, '\x1b') || strings.Contains(plain, "38;2;") {
					t.Errorf("line %d holds a cut escape sequence: %q", i, plain)
				}
			}
			if !strings.Contains(ansi.Strip(strings.Join(got, "\n")), "Trace from") {
				t.Error("the selector is not drawn")
			}
		})
	}
}

// Rule 130: the key is advertised in editing and done, greyed where the trace
// is unsupported; the open selector replaces the list (a mode).
func TestPathSourceShortcuts(t *testing.T) {
	pm := pmWith(t, runningFake())
	for _, state := range []pathState{pathEditing, pathDone} {
		pm.state = state
		sc := pm.shortcuts()
		if !testutil.ShortcutEnabled(sc, "ctrl+o") {
			t.Errorf("state %v: ctrl+o is not advertised and enabled", state)
		}
		for _, s := range sc {
			if s.Key == "ctrl+o" && s.Description != "Trace from…" {
				t.Errorf("state %v: ctrl+o description = %q, want %q", state, s.Description, "Trace from…")
			}
		}
	}

	off := pmWith(t, &fakeEngine{})
	off.engine.supported = func() error { return errors.New("not linux") }
	if sc := off.shortcuts(); !testutil.HasShortcut(sc, "ctrl+o") || !testutil.ShortcutDisabled(sc, "ctrl+o") {
		t.Error("ctrl+o must stay listed and greyed where the trace is unsupported")
	}

	pm.state = pathEditing
	pm = openedPicker(t, pm)
	// Rule 138: the arrows are obvious in a list, so only enter and esc are listed.
	if got, want := testutil.ShortcutKeys(pm.shortcuts()), []string{"enter", "esc"}; !slices.Equal(got, want) {
		t.Errorf("selector shortcuts = %v, want %v", got, want)
	}
}

// Rule 114: the help names the key.
func TestPathSourceKeyIsInTheHelp(t *testing.T) {
	c := New(testConfig()).GetHelpContent()
	found := false
	for _, kb := range c.KeyBindings {
		if strings.HasPrefix(kb.Key, "ctrl+o") {
			found = true
		}
	}
	if !found {
		t.Error("no KeyBinding starts with ctrl+o")
	}
	for _, s := range c.Sections {
		if strings.HasPrefix(s.Title, "Path Tab") && strings.Contains(s.Body, "ctrl+o") {
			return
		}
	}
	t.Error("the Path Tab help section does not mention ctrl+o")
}
