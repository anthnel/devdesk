package netdiag

import (
	"errors"
	"fmt"
	"net/netip"
	"reflect"
	"strings"
	"testing"

	tea "charm.land/bubbletea/v2"
	"github.com/charmbracelet/x/ansi"

	"github.com/anthnel/devdesk/internal/nettrace"
	"github.com/anthnel/devdesk/internal/ui/components"
	"github.com/anthnel/devdesk/internal/ui/shortcut"
	"github.com/anthnel/devdesk/internal/ui/testutil"
)

// DEVDESK-328 step 4: the path model. No test runs the launch Cmd
// (it would start the producer); step 3 covers the producer.

func newPM(t *testing.T) (*PathModel, *fakeEngine) {
	t.Helper()
	f := &fakeEngine{}
	pm := newPathModel(f.engine())
	pm.resize(120, 30)
	return pm, f
}

// pmFeed delivers msgs in order and returns the model and the last Cmd.
func pmFeed(t *testing.T, pm *PathModel, msgs ...tea.Msg) (*PathModel, tea.Cmd) {
	t.Helper()
	var cmd tea.Cmd
	for _, msg := range msgs {
		pm, cmd = pm.update(msg)
		if pm == nil {
			t.Fatal("update returned a nil model")
		}
	}
	return pm, cmd
}

// launched puts the model in the ready state on target, keeping the source a
// test chose, and presses enter.
func launched(t *testing.T, pm *PathModel, target string) (*PathModel, tea.Cmd) {
	t.Helper()
	src := pm.source
	pm.ready(pathTarget{Host: target})
	pm.source = src
	return pmFeed(t, pm, testutil.Key("enter"))
}

// pmShortcuts is what the sub-level lists for pm: Model.traceShortcuts is the
// only constructor, and it reads only the path model and the origin.
func pmShortcuts(pm *PathModel) shortcut.Shortcuts {
	return (&Model{pathModel: pm}).traceShortcuts()
}

func pmHopTTLs(pm *PathModel) []int {
	var out []int
	for _, s := range pm.table.Items() {
		if s.Kind == stepHop {
			out = append(out, s.Hop.TTL)
		}
	}
	return out
}

func pmFooter(pm *PathModel, width int) string {
	return ansi.Strip(pm.footer.View(width, pm.statusLine()))
}

func headerValue(pm *PathModel, key string) (string, bool) {
	for _, h := range pm.headerInfo() {
		if h.Key == key {
			return h.Value, true
		}
	}
	return "", false
}

func hopOf(ttl int, kind nettrace.HopKind) nettrace.Hop {
	return nettrace.Hop{TTL: ttl, Kind: kind, Addr: netip.MustParseAddr("10.0.0.1")}
}

// Criterion 6: the body is a table even empty, and a new model is ready.
func TestPathNewModelIsAReadyEmptyTable(t *testing.T) {
	pm, _ := newPM(t)
	if pm.state != pathReady {
		t.Errorf("state = %v on a new path model, want pathReady", pm.state)
	}
	if pm.running() {
		t.Error("running() = true before any launch")
	}
	if got, want := pm.view(), pm.table.View(); got != want {
		t.Errorf("view() is not the table view (Rule 139)")
	}
	if n := len(pm.table.Items()); n != 0 {
		t.Errorf("%d rows before any run, want 0", n)
	}
	body := ansi.Strip(pm.view())
	for _, col := range []string{"Hop", "Step", "Address", "RTT"} {
		if !strings.Contains(body, col) {
			t.Errorf("empty table body lacks the %q header:\n%s", col, body)
		}
	}
}

// Criterion 2.
func TestPathEnterLaunchesAndTheFooterSaysSo(t *testing.T) {
	pm, _ := newPM(t)
	gen := pm.gen
	pm, cmd := launched(t, pm, "1.1.1.1")

	if cmd == nil {
		t.Fatal("enter on a valid target returned no Cmd")
	}
	if !pm.running() {
		t.Error("running() = false after enter")
	}
	if pm.gen != gen+1 {
		t.Errorf("gen = %d, want %d", pm.gen, gen+1)
	}
	st := pm.statusLine()
	if !st.Activity {
		t.Error("status has no activity dot while running")
	}
	if !strings.Contains(st.Text, "1.1.1.1") {
		t.Errorf("status text = %q, want the target", st.Text)
	}
	if !strings.Contains(pmFooter(pm, 100), "1.1.1.1") {
		t.Errorf("rendered footer = %q, want the target", pmFooter(pm, 100))
	}
}

func TestPathHopsAppearAsTheyArriveAndTheFooterCountsThem(t *testing.T) {
	pm, _ := newPM(t)
	pm, _ = launched(t, pm, "1.1.1.1")
	pm, _ = pmFeed(t, pm, pathResolvedMsg{gen: pm.gen, addr: netip.MustParseAddr("1.1.1.1")})

	pm, cmd := pmFeed(t, pm, pathHopMsg{gen: pm.gen, hop: hopOf(3, nettrace.HopRouter)})
	if cmd == nil {
		t.Error("a hop did not re-arm the wait Cmd")
	}
	pm, _ = pmFeed(t, pm, pathHopMsg{gen: pm.gen, hop: hopOf(1, nettrace.HopRouter)})

	if got, want := pmHopTTLs(pm), []int{1, 3}; !reflect.DeepEqual(got, want) {
		t.Errorf("rows = %v, want %v (TTL order, as they arrive)", got, want)
	}
	st := pm.statusLine()
	if !st.Activity || !strings.Contains(st.Text, "2 hops") {
		t.Errorf("status = %+v, want the activity dot and a visible \"2 hops\" counter", st)
	}
	if line := pmFooter(pm, 100); !strings.Contains(line, "2 hops") || !strings.Contains(line, "1.1.1.1") {
		t.Errorf("rendered footer = %q, want the counter and the target", line)
	}
}

func TestPathResolvingShowsAnActivityStatus(t *testing.T) {
	pm, _ := newPM(t)
	pm, _ = launched(t, pm, "example.com")
	st := pm.statusLine()
	if !st.Activity || !strings.Contains(st.Text, "Resolving example.com") {
		t.Errorf("status = %+v, want Resolving example.com... with the activity dot", st)
	}
}

// Reconciliation: terminal TTL4 after TTL5 cuts the rows; done replaces hops.
func TestPathReconciliation(t *testing.T) {
	pm, _ := newPM(t)
	pm, _ = launched(t, pm, "1.1.1.1")
	g := pm.gen
	pm, _ = pmFeed(t, pm,
		pathHopMsg{gen: g, hop: hopOf(1, nettrace.HopRouter)},
		pathHopMsg{gen: g, hop: hopOf(2, nettrace.HopRouter)},
		pathHopMsg{gen: g, hop: hopOf(3, nettrace.HopRouter)},
		pathHopMsg{gen: g, hop: hopOf(5, nettrace.HopRouter)},
	)
	if got, want := pmHopTTLs(pm), []int{1, 2, 3, 5}; !reflect.DeepEqual(got, want) {
		t.Fatalf("before the terminal hop: rows = %v, want %v", got, want)
	}
	pm, _ = pmFeed(t, pm, pathHopMsg{gen: g, hop: hopOf(4, nettrace.HopTarget)})
	if got, want := pmHopTTLs(pm), []int{1, 2, 3, 4}; !reflect.DeepEqual(got, want) {
		t.Fatalf("after the terminal TTL4: rows = %v, want %v", got, want)
	}

	final := nettrace.Result{
		Outcome: nettrace.OutcomeReached,
		Hops: []nettrace.Hop{
			hopOf(1, nettrace.HopRouter), hopOf(2, nettrace.HopRouter),
			hopOf(3, nettrace.HopRouter), hopOf(4, nettrace.HopTarget),
		},
	}
	pm, cmd := pmFeed(t, pm, pathDoneMsg{gen: g, result: final})
	if !reflect.DeepEqual(pm.hops, final.Hops) {
		t.Errorf("hops = %v, want Result.Hops %v (replaced at done)", pm.hops, final.Hops)
	}
	if cmd != nil {
		if _, ok := testutil.MsgOf[pathHopMsg](cmd); ok {
			t.Error("done re-armed the wait Cmd")
		}
	}
	if pm.state != pathDone {
		t.Errorf("after done: state=%v, want pathDone", pm.state)
	}
	if v, ok := headerValue(pm, "Path"); !ok || v != "Reached at hop 4" {
		t.Errorf("Path header = %q (%v), want %q", v, ok, "Reached at hop 4")
	}
	if pm.statusLine().Activity {
		t.Error("status still shows activity once done")
	}
	if pm.result == nil {
		t.Fatal("result is nil after a done message")
	}
}

// Criterion 3.
func TestPathUnresolvedNameIsAPersistentFooterError(t *testing.T) {
	pm, _ := newPM(t)
	pm, _ = launched(t, pm, "nope.invalid")
	g := pm.gen
	pm, _ = pmFeed(t, pm, pathDoneMsg{gen: g, err: fmt.Errorf("%w: %w", errPathResolve, errors.New("no such host"))})

	if pm.running() || pm.state != pathDone {
		t.Errorf("running=%v state=%v, want pathDone", pm.running(), pm.state)
	}
	if n := len(pm.table.Items()); n != 0 {
		t.Errorf("%d rows after a resolve failure, want none", n)
	}
	st := pm.statusLine()
	want := "Could not resolve nope.invalid — no probe was sent"
	if st.Level != components.LevelError || st.Text != want {
		t.Errorf("status = {%v %q}, want Error %q", st.Level, st.Text, want)
	}
	if line := pmFooter(pm, 100); !strings.Contains(line, want) {
		t.Errorf("rendered footer = %q, want %q", line, want)
	}
	// Persistent: not a 3 s message, it survives a footer expiry.
	pm, _ = pmFeed(t, pm, components.ClearFooterMsg{ID: pm.footer.ID()})
	if pm.statusLine().Text != want {
		t.Errorf("the error vanished with the footer timer: %+v", pm.statusLine())
	}
	// The next launch clears it.
	pm, _ = pmFeed(t, pm, testutil.Key("enter"))
	if pm.statusLine().Level == components.LevelError {
		t.Errorf("trying again did not clear the error: %+v", pm.statusLine())
	}
}

// errPathPortText is the footer text of a target that carries a port.
const errPathPortText = "Path ignores ports: enter a host or IP address only"

// H2: unsupported platform, enter greyed and refused with the reason.
func TestPathUnsupportedPlatformRefusesEnter(t *testing.T) {
	f := &fakeEngine{}
	e := f.engine()
	e.supported = func() error { return nettrace.ErrUnsupported }
	pm := newPathModel(e)
	pm.resize(120, 30)

	if !testutil.ShortcutDisabled(pmShortcuts(pm), "enter") {
		t.Error("enter is not greyed on an unsupported platform")
	}
	gen := pm.gen
	pm, cmd := launched(t, pm, "1.1.1.1")
	if cmd != nil && pm.running() {
		t.Error("a trace was launched on an unsupported platform")
	}
	if pm.running() || pm.gen != gen || pm.state != pathReady {
		t.Errorf("running=%v gen=%d state=%v, want a refusal that stays ready", pm.running(), pm.gen, pm.state)
	}
	if pm.footer.Level() != components.LevelWarning || pm.footer.Text() != reasonPathUnsupported {
		t.Errorf("footer = {%v %q}, want Warn %q", pm.footer.Level(), pm.footer.Text(), reasonPathUnsupported)
	}
	if f.resolveCalls != 0 || f.traceCalls != 0 {
		t.Error("the engine was reached on an unsupported platform")
	}

	// Rule 130: the key set does not change with the availability.
	ok, _ := newPM(t)
	if got, want := testutil.ShortcutKeys(pmShortcuts(pm)), testutil.ShortcutKeys(pmShortcuts(ok)); !reflect.DeepEqual(got, want) {
		t.Errorf("shortcut keys %v unsupported, %v supported", got, want)
	}
	if reason := pm.launchable().Reason; reason != reasonPathUnsupported {
		t.Errorf("launchable().Reason = %q, want %q", reason, reasonPathUnsupported)
	}
}

// Criterion 4.
func TestPathEscCancelsKeepsHopsAndIgnoresStaleMessages(t *testing.T) {
	pm, _ := newPM(t)
	pm, _ = launched(t, pm, "1.1.1.1")
	oldGen := pm.gen
	var cancelled bool
	orig := pm.cancel
	if orig == nil {
		t.Fatal("no cancel func stored while running")
	}
	pm.cancel = func() { cancelled = true; orig() }
	pm, _ = pmFeed(t, pm, pathHopMsg{gen: oldGen, hop: hopOf(1, nettrace.HopRouter)})

	pm, _ = pmFeed(t, pm, testutil.Key("esc"))
	if !cancelled {
		t.Error("esc did not call the run's cancel func")
	}
	if pm.running() {
		t.Error("running() = true right after esc: the UI must answer at once")
	}
	if pm.gen == oldGen {
		t.Error("gen not bumped on esc, late messages would be accepted")
	}
	if pm.result == nil || pm.result.Outcome != nettrace.OutcomeCancelled {
		t.Errorf("result = %+v, want a Cancelled outcome", pm.result)
	}
	if got, want := pmHopTTLs(pm), []int{1}; !reflect.DeepEqual(got, want) {
		t.Errorf("rows after cancel = %v, want %v kept", got, want)
	}
	if v, _ := headerValue(pm, "Path"); v != "Canceled" {
		t.Errorf("Path header = %q, want Canceled", v)
	}

	// Late messages of the old run change nothing and are not re-armed.
	before := pmHopTTLs(pm)
	var cmd tea.Cmd
	pm, cmd = pmFeed(t, pm, pathHopMsg{gen: oldGen, hop: hopOf(2, nettrace.HopRouter)})
	if cmd != nil {
		t.Error("a stale hop re-armed the wait Cmd")
	}
	pm, cmd = pmFeed(t, pm, pathDoneMsg{gen: oldGen, result: nettrace.Result{Outcome: nettrace.OutcomeReached}})
	if cmd != nil {
		t.Error("a stale done returned a Cmd")
	}
	if !reflect.DeepEqual(pmHopTTLs(pm), before) || pm.result.Outcome != nettrace.OutcomeCancelled {
		t.Errorf("a stale message changed the model: rows %v, result %+v", pmHopTTLs(pm), pm.result)
	}

	// The UI is usable: enter starts a new run on the same target.
	gen := pm.gen
	pm, cmd = pmFeed(t, pm, testutil.Key("enter"))
	if cmd == nil || !pm.running() || pm.gen != gen+1 {
		t.Errorf("relaunch after cancel: cmd=%v running=%v gen=%d, want a run with gen %d", cmd != nil, pm.running(), pm.gen, gen+1)
	}
}

func TestPathStaleGenerationHopIsDropped(t *testing.T) {
	pm, _ := newPM(t)
	pm, _ = launched(t, pm, "1.1.1.1")
	pm, cmd := pmFeed(t, pm, pathHopMsg{gen: pm.gen - 1, hop: hopOf(1, nettrace.HopRouter)})
	if cmd != nil || len(pm.table.Items()) != 0 {
		t.Errorf("stale hop: cmd=%v rows=%d, want ignored", cmd != nil, len(pm.table.Items()))
	}
}

// Letters and pastes do nothing in any state: the target comes from the form.
func TestPathIgnoresLettersAndPasteInEveryState(t *testing.T) {
	states := map[string]func(*testing.T) *PathModel{
		"ready": func(t *testing.T) *PathModel {
			pm, _ := newPM(t)
			pm.ready(pathTarget{Host: "1.1.1.1"})
			return pm
		},
		"running": func(t *testing.T) *PathModel {
			pm, _ := launched(t, mustPM(t), "1.1.1.1")
			return pm
		},
		"done": func(t *testing.T) *PathModel {
			pm, _ := launched(t, mustPM(t), "1.1.1.1")
			pm, _ = pmFeed(t, pm, pathDoneMsg{gen: pm.gen, result: nettrace.Result{Outcome: nettrace.OutcomeReached}})
			return pm
		},
	}
	for name, setup := range states {
		t.Run(name, func(t *testing.T) {
			pm := setup(t)
			state, gen := pm.state, pm.gen
			msgs := append(testutil.Type("evil.org"), testutil.Key("backspace"), testutil.Paste("evil"))
			for i, msg := range msgs {
				var cmd tea.Cmd
				pm, cmd = pm.update(msg)
				if cmd != nil {
					t.Errorf("message %d (%v) returned a Cmd, want none", i, msg)
				}
			}
			if pm.state != state || pm.gen != gen || pm.target.Host != "1.1.1.1" {
				t.Errorf("state %v->%v gen %d->%d target=%q, want nothing to react",
					state, pm.state, gen, pm.gen, pm.target.Host)
			}
		})
	}
}

func TestPathDoneEnterRelaunches(t *testing.T) {
	pm, _ := newPM(t)
	pm, _ = launched(t, pm, "1.1.1.1")
	pm, _ = pmFeed(t, pm, pathDoneMsg{gen: pm.gen, result: nettrace.Result{
		Outcome: nettrace.OutcomeReached, Hops: []nettrace.Hop{hopOf(1, nettrace.HopTarget)},
	}})
	g := pm.gen

	if v, _ := headerValue(pm, "Target"); !strings.Contains(v, "1.1.1.1") {
		t.Errorf("Target header = %q, want the host", v)
	}
	if !testutil.ShortcutEnabled(pmShortcuts(pm), "enter") || !testutil.HasShortcut(pmShortcuts(pm), "esc") {
		t.Errorf("done shortcuts = %v, want enter and esc", testutil.ShortcutKeys(pmShortcuts(pm)))
	}

	again, cmd := pmFeed(t, pm, testutil.Key("enter"))
	if cmd == nil || !again.running() || again.gen != g+1 || again.target != (pathTarget{Host: "1.1.1.1"}) {
		t.Errorf("relaunch: cmd=%v running=%v gen=%d target=%v", cmd != nil, again.running(), again.gen, again.target)
	}
}

// Criterion 6, Rules 128/139: the body is the table in every state and the
// message line stays a single line.
func TestPathBodyIsTheTableAndFooterHeightIsFixed(t *testing.T) {
	idle, _ := newPM(t)
	run, _ := launched(t, mustPM(t), "1.1.1.1")
	done, _ := pmFeed(t, run, pathDoneMsg{gen: run.gen, result: nettrace.Result{Outcome: nettrace.OutcomeReached}})

	for name, pm := range map[string]*PathModel{"idle": idle, "running": run, "done": done} {
		if got, want := pm.view(), pm.table.View(); got != want {
			t.Errorf("%s: view() is not the table view", name)
		}
		if got := strings.Count(pmFooter(pm, 100), "\n"); got != 0 {
			t.Errorf("%s: message line has %d line breaks, want a single line", name, got)
		}
	}
}

func mustPM(t *testing.T) *PathModel {
	t.Helper()
	pm, _ := newPM(t)
	return pm
}

// Rule 116.
func TestPathTableFillsTheViewport(t *testing.T) {
	for _, w := range []int{120, 60} {
		pm, _ := newPM(t)
		pm.resize(w, 30)
		if got, want := pm.table.RenderedWidth(), w-2; got != want {
			t.Errorf("width %d: RenderedWidth() = %d, want %d", w, got, want)
		}
	}
}

// A done message always carries a usable Result: a cancel leaves one, and the
// header never reads a nil result.
func TestPathHeaderNeverReadsANilResult(t *testing.T) {
	pm, _ := newPM(t)
	if _, ok := headerValue(pm, "Path"); ok {
		t.Error("a Path header before any run")
	}
	pm, _ = launched(t, pm, "1.1.1.1")
	pm, _ = pmFeed(t, pm, testutil.Key("esc"))
	if pm.result == nil {
		t.Fatal("result nil after cancel")
	}
	_ = pm.headerInfo()
}

// Rule 110: launching builds a Cmd without touching the model; the stored
// context is live until cancel.
func TestPathLaunchStoresALiveContextAndChannel(t *testing.T) {
	pm, _ := newPM(t)
	pm, _ = launched(t, pm, "1.1.1.1")
	if pm.cancel == nil || pm.msgs == nil {
		t.Fatalf("cancel=%v msgs=%v, want both stored while running", pm.cancel != nil, pm.msgs != nil)
	}
}
