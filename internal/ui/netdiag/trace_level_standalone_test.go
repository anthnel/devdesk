package netdiag

import (
	"context"
	"net/netip"
	"slices"
	"strings"
	"testing"

	"github.com/charmbracelet/x/ansi"

	"github.com/anthnel/devdesk/internal/command"
	"github.com/anthnel/devdesk/internal/netcheck"
	"github.com/anthnel/devdesk/internal/nettrace"
	"github.com/anthnel/devdesk/internal/ui/components"
	"github.com/anthnel/devdesk/internal/ui/keymap"
	"github.com/anthnel/devdesk/internal/ui/testutil"
)

// DEVDESK-471 step 1 (red phase): the trace sub-level (StateTrace) becomes
// self-sufficient. H on the Local route row opens it without tracing, enter
// starts or restarts the trace, ctrl+o opens "Trace from", esc cancels then
// goes back to where the user came from. The "ready" state is pathReady (the
// zero value: target known or empty, nothing sent).

// routeLevel returns results over host with the cursor on the Local route row,
// after H. The engine resolves to a fixed address.
func routeLevel(t *testing.T, host string) (*Model, *fakeEngine) {
	t.Helper()
	m, fe := routeResults(t, host)
	fe.resolveFn = func(context.Context, string) ([]netip.Addr, error) {
		return []netip.Addr{netip.MustParseAddr("93.184.216.34")}, nil
	}
	m = selectCheck(t, m, netcheck.CheckRoute)
	m = feed(t, m, testutil.Key(keymap.Diagnose))
	if m.state != StateTrace || m.activeTab != tabDiagnostics {
		t.Fatalf("H left state=%v tab=%d, want StateTrace on the Diagnostics tab", m.state, m.activeTab)
	}
	return m, fe
}

func TestHOnTheRouteRowOpensTheTraceLevelWithoutTracing(t *testing.T) {
	m, fe := routeResults(t, "example.com")
	m = selectCheck(t, m, netcheck.CheckRoute)
	gen := m.pathModel.gen

	m, cmd := step(t, m, testutil.Key(keymap.Diagnose))

	if m.state != StateTrace || m.activeTab != tabDiagnostics {
		t.Fatalf("state=%v tab=%d, want StateTrace on the Diagnostics tab", m.state, m.activeTab)
	}
	pm := m.pathModel
	if pm.state != pathReady {
		t.Errorf("Path state = %v, want the ready state (nothing sent)", pm.state)
	}
	if pm.target.Host != "example.com" {
		t.Errorf("target = %q, want the results' host", pm.target.Host)
	}
	if pm.gen != gen || pm.running() {
		t.Errorf("gen %d->%d running=%v, want no run started", gen, pm.gen, pm.running())
	}
	_ = testutil.Msgs(cmd)
	if fe.resolveCalls != 0 || fe.traceCalls != 0 {
		t.Errorf("engine called (resolve %d, trace %d); H must send nothing", fe.resolveCalls, fe.traceCalls)
	}
	title := ansi.Strip(m.GetTitle())
	if !strings.Contains(title, "Path") || !strings.Contains(title, "example.com") {
		t.Errorf("title = %q, want Network > Path > example.com", title)
	}
	if _, ok := headerValue(pm, "Target"); !ok {
		t.Error("no Target in the sub-level header")
	}
}

func TestTraceLevelReadyEnterLaunches(t *testing.T) {
	m, fe := routeLevel(t, "example.com")
	gen := m.pathModel.gen

	m, cmd := step(t, m, testutil.Key("enter"))

	if m.state != StateTrace || !m.pathModel.running() || m.pathModel.gen != gen+1 {
		t.Fatalf("state=%v running=%v gen %d->%d, want a launched run", m.state, m.pathModel.running(), gen, m.pathModel.gen)
	}
	if cmd == nil {
		t.Fatal("enter returned no Cmd: the trace would never start")
	}
	if fe.traceCalls != 0 {
		t.Errorf("trace ran inside Update (%d calls, Rule 110)", fe.traceCalls)
	}
}

func TestTraceLevelDoneEnterTracesAgain(t *testing.T) {
	m, _ := routeLevel(t, "example.com")
	m = feed(t, m, testutil.Key("enter"))
	m = traceHops(t, m, 2)
	m = traceEnded(t, m)
	gen := m.pathModel.gen

	m, cmd := step(t, m, testutil.Key("enter"))

	if !m.pathModel.running() || m.pathModel.gen != gen+1 {
		t.Fatalf("running=%v gen %d->%d, want a new run", m.pathModel.running(), gen, m.pathModel.gen)
	}
	if len(pmHopTTLs(m.pathModel)) != 0 {
		t.Errorf("hops = %v, want the previous rows cleared", pmHopTTLs(m.pathModel))
	}
	if cmd == nil {
		t.Error("no Cmd: the second trace would never start")
	}
}

func TestTraceLevelSourceKeyOpensThePicker(t *testing.T) {
	m, fe := routeLevel(t, "example.com")

	m, cmd := step(t, m, testutil.Key("ctrl+o"))

	if !m.pathModel.pickerOpen() || m.state != StateTrace {
		t.Fatalf("picker=%v state=%v, want the selector open on the sub-level", m.pathModel.pickerOpen(), m.state)
	}
	if cmd == nil {
		t.Fatal("no Cmd: the sources would never be read")
	}
	_ = testutil.Msgs(cmd)
	if fe.traceCalls != 0 {
		t.Error("opening the selector traced")
	}
}

func TestTraceLevelPickerHoldsTabAndEsc(t *testing.T) {
	m, _ := routeLevel(t, "example.com")
	m = feed(t, m, testutil.Key("ctrl+o"))

	m = feed(t, m, testutil.Key("tab"))
	if m.activeTab != tabDiagnostics || !m.pathModel.pickerOpen() {
		t.Fatalf("tab=%d picker=%v after tab, want the selector to keep the key", m.activeTab, m.pathModel.pickerOpen())
	}
	if !m.InEditMode() {
		t.Error("InEditMode() = false with the selector open")
	}

	m = feed(t, m, testutil.Key("esc"))
	if m.pathModel.pickerOpen() || m.state != StateTrace {
		t.Errorf("picker=%v state=%v, want the selector closed and still StateTrace", m.pathModel.pickerOpen(), m.state)
	}
}

func TestTraceLevelChoosingASourceReturnsToReadyWithoutTracing(t *testing.T) {
	m, fe := routeLevel(t, "example.com")
	fe.psRows = runningFake().psRows
	m, cmd := step(t, m, testutil.Key("ctrl+o"))
	m = feed(t, m, testutil.Msgs(cmd)...)
	m = feed(t, m, testutil.Key("down"))
	gen := m.pathModel.gen

	m = feed(t, m, testutil.Key("enter"))

	pm := m.pathModel
	if pm.pickerOpen() || pm.source.ID == "" || pm.source.ID == hostSource().ID {
		t.Fatalf("picker=%v source=%+v, want a container chosen", pm.pickerOpen(), pm.source)
	}
	if pm.state != pathReady || pm.target.Host != "example.com" || pm.gen != gen || pm.running() {
		t.Errorf("state=%v target=%q gen %d->%d, want ready, target kept, nothing launched", pm.state, pm.target.Host, gen, pm.gen)
	}
	if m.state != StateTrace {
		t.Errorf("state = %v, want StateTrace", m.state)
	}
}

func TestTraceLevelSourceKeyWhileRunningIsInert(t *testing.T) {
	m, fe, _ := traceLevelModel(t, traceLevelHost)
	gen := m.pathModel.gen

	m, cmd := step(t, m, testutil.Key("ctrl+o"))

	if !m.pathModel.running() || m.pathModel.gen != gen || m.pathModel.pickerOpen() {
		t.Errorf("running=%v gen %d->%d picker=%v, want the run untouched", m.pathModel.running(), gen, m.pathModel.gen, m.pathModel.pickerOpen())
	}
	if len(testutil.Msgs(cmd)) != 0 || fe.traceCalls != 0 {
		t.Error("a Cmd or a trace came out of the key")
	}
}

// AC4: esc cancels, then returns to the results with their state kept.
func TestTraceLevelFromResultsEscCancelsThenReturnsToTheResults(t *testing.T) {
	m, _ := routeResults(t, "example.com")
	m = selectCheck(t, m, netcheck.CheckRoute)
	m.filterBar.SetTokenActive(problemsToken, true)
	m.OriginView = command.ViewStatus
	want := len(m.results.All())
	m = feed(t, m, testutil.Key(keymap.Diagnose), testutil.Key("enter"))
	m = traceHops(t, m, 2)

	m = feed(t, m, testutil.Key("esc"))
	if m.state != StateTrace || m.pathModel.running() || m.pathModel.state != pathDone {
		t.Fatalf("state=%v running=%v pathState=%v after the first esc, want a canceled trace still on screen", m.state, m.pathModel.running(), m.pathModel.state)
	}

	m = feed(t, m, testutil.Key("esc"))
	if m.state != StateResults {
		t.Fatalf("state = %v after the second esc, want StateResults", m.state)
	}
	if c, ok := m.checksTable.Selected(); !ok || c.ID != netcheck.CheckRoute {
		t.Errorf("selected = %+v, want the Local route row kept", c)
	}
	if !m.filterBar.IsTokenActive(problemsToken) {
		t.Error("the problems filter was reset")
	}
	if m.OriginView != command.ViewStatus {
		t.Errorf("OriginView = %q, want it untouched", m.OriginView)
	}
	if len(m.results.All()) != want {
		t.Errorf("results = %d, want %d kept (no resetToForm)", len(m.results.All()), want)
	}
}

func TestTraceLevelReadyEscGoesStraightBack(t *testing.T) {
	m, _ := routeLevel(t, "example.com")

	m = feed(t, m, testutil.Key("esc"))

	if m.state != StateResults {
		t.Errorf("state = %v, want StateResults", m.state)
	}
}

// Rule 124: the Diagnostics filter bar has no business in the sub-level.
func TestTraceLevelHidesTheDiagnosticsFilterBar(t *testing.T) {
	m, _ := routeResults(t, "example.com")
	m = selectCheck(t, m, netcheck.CheckRoute)
	m.filterBar.SetTokenActive(problemsToken, true)
	if !strings.Contains(ansi.Strip(m.RenderFooter(120)), problemsToken) {
		t.Fatal("precondition: the results footer should show the problems token")
	}

	m = feed(t, m, testutil.Key(keymap.Diagnose))

	if got := m.GetFooterHeight(); got != 3 {
		t.Errorf("GetFooterHeight() = %d in the sub-level, want 3", got)
	}
	footer := ansi.Strip(m.RenderFooter(120))
	if n := strings.Count(footer, "\n") + 1; n != 3 {
		t.Errorf("footer has %d lines, want 3", n)
	}
	if strings.Contains(footer, problemsToken) {
		t.Errorf("footer = %q, want no Diagnostics filter bar", footer)
	}

	m = feed(t, m, testutil.Key("esc"))
	if got, want := m.GetFooterHeight(), 3+m.filterBar.ExtraHeight(); got != want || got == 3 {
		t.Errorf("GetFooterHeight() = %d back on the results, want %d (bar back)", got, want)
	}
}

// AC5: unsupported platform, H greyed with its reason, nothing emitted.
func TestHIsGreyedWithThePlatformReasonAndSendsNothing(t *testing.T) {
	m, fe := routeResults(t, "example.com")
	m.pathModel.engine.supported = platformOff
	m = selectCheck(t, m, netcheck.CheckRoute)
	gen := m.pathModel.gen

	if sc := m.GetShortcuts(); !testutil.HasShortcut(sc, keymap.Diagnose) || !testutil.ShortcutDisabled(sc, keymap.Diagnose) {
		t.Errorf("H must stay listed and be greyed: %v", testutil.ShortcutKeys(sc))
	}
	m, cmd := step(t, m, testutil.Key(keymap.Diagnose))

	if m.state != StateResults || m.activeTab != tabDiagnostics {
		t.Errorf("state=%v tab=%d, want the results untouched", m.state, m.activeTab)
	}
	if m.footer.Level() != components.LevelWarning || m.footer.Text() != reasonPathMacOS {
		t.Errorf("footer = {%v %q}, want Warn %q", m.footer.Level(), m.footer.Text(), reasonPathMacOS)
	}
	if m.pathModel.gen != gen || m.pathModel.target.Host != "" {
		t.Errorf("gen %d->%d target=%q, want the Path model untouched", gen, m.pathModel.gen, m.pathModel.target.Host)
	}
	_ = testutil.Msgs(cmd)
	if fe.resolveCalls != 0 || fe.traceCalls != 0 {
		t.Error("the engine was reached")
	}
}

// H2: a target the trace cannot parse greys H with the parser's reason.
func TestHIsGreyedOnATargetThePathCannotTrace(t *testing.T) {
	m, _ := routeResults(t, "example.com")
	m.targetInput.SetValue("::1")
	m = selectCheck(t, m, netcheck.CheckRoute)
	_, perr := parsePathTarget("::1")
	if perr == nil {
		t.Skip("parsePathTarget accepts ::1; pick another unparsable literal")
	}

	if sc := m.GetShortcuts(); !testutil.ShortcutDisabled(sc, keymap.Diagnose) {
		t.Error("H must be greyed on a target the path trace refuses")
	}
	m = feed(t, m, testutil.Key(keymap.Diagnose))

	if m.state != StateResults {
		t.Errorf("state = %v, want StateResults", m.state)
	}
	if m.footer.Level() != components.LevelWarning || m.footer.Text() != capitalize(perr.Error()) {
		t.Errorf("footer = {%v %q}, want Warn %q", m.footer.Level(), m.footer.Text(), capitalize(perr.Error()))
	}
}

// H7: with a trace already running, H changes nothing and says why.
func TestHWhileATraceRunsWarnsAndChangesNothing(t *testing.T) {
	m, _ := routeResults(t, "example.com")
	m = selectCheck(t, m, netcheck.CheckRoute)
	m.pathModel.state = pathRunning
	gen := m.pathModel.gen

	m = feed(t, m, testutil.Key(keymap.Diagnose))

	if m.state != StateResults || m.activeTab != tabDiagnostics {
		t.Errorf("state=%v tab=%d, want the results untouched", m.state, m.activeTab)
	}
	if m.pathModel.gen != gen || !m.pathModel.running() {
		t.Error("the running trace was touched")
	}
	if m.footer.Level() != components.LevelWarning || m.footer.Text() != reasonPathBusy {
		t.Errorf("footer = {%v %q}, want Warn %q", m.footer.Level(), m.footer.Text(), reasonPathBusy)
	}
}

// Rule 130: ready and done share the key set.
func TestTraceLevelReadyAndDoneShareTheKeySet(t *testing.T) {
	ready, _ := routeLevel(t, "example.com")
	done, _ := routeLevel(t, "example.com")
	done = feed(t, done, testutil.Key("enter"))
	done = traceEnded(t, traceHops(t, done, 2))

	rk, dk := testutil.ShortcutKeys(ready.GetShortcuts()), testutil.ShortcutKeys(done.GetShortcuts())
	if !slices.Equal(rk, dk) {
		t.Errorf("keys ready %v, done %v: the set must not change", rk, dk)
	}
	for _, k := range []string{"enter", "ctrl+o", "esc"} {
		if !testutil.HasShortcut(ready.GetShortcuts(), k) {
			t.Errorf("ready state does not list %q: %v", k, rk)
		}
	}
}

func TestTraceLevelEscLabelFollowsTheOrigin(t *testing.T) {
	fromResults, _ := routeLevel(t, "example.com")
	fromForm, _, _ := traceLevelModel(t, traceLevelHost)
	fromForm = traceEnded(t, fromForm)

	for name, tc := range map[string]struct {
		m    *Model
		want string
	}{
		"results": {fromResults, "Back to the results"},
		"form":    {fromForm, "Back to the form"},
	} {
		found := false
		for _, s := range tc.m.GetShortcuts() {
			if s.Key == "esc" {
				found = true
				if s.Description != tc.want {
					t.Errorf("%s: esc = %q, want %q", name, s.Description, tc.want)
				}
			}
		}
		if !found {
			t.Errorf("%s: no esc shortcut", name)
		}
	}
}

func TestTraceLevelInEditModeOnlyWithThePicker(t *testing.T) {
	m, _ := routeLevel(t, "example.com")
	if m.InEditMode() {
		t.Error("InEditMode() = true in the ready sub-level")
	}
	m = feed(t, m, testutil.Key("ctrl+o"))
	if !m.InEditMode() {
		t.Error("InEditMode() = false with the selector open")
	}
}

// A failed run stays in the sub-level with its error, and enter retries.
func TestTraceLevelAFailedRunStaysInTheSubLevelWithItsError(t *testing.T) {
	m, _, _ := traceLevelModel(t, traceLevelHost)
	m = feed(t, m, pathDoneMsg{gen: m.pathModel.gen, err: context.DeadlineExceeded})

	pm := m.pathModel
	if m.state != StateTrace || pm.state != pathDone || pm.errText == "" {
		t.Fatalf("state=%v pathState=%v errText=%q, want StateTrace, pathDone and the error", m.state, pm.state, pm.errText)
	}
	if pm.pickerOpen() {
		t.Error("the picker is open after a failed run")
	}
	gen := pm.gen
	m = feed(t, m, testutil.Key("enter"))
	if !m.pathModel.running() || m.pathModel.gen != gen+1 {
		t.Errorf("running=%v gen %d->%d, want enter to try again", m.pathModel.running(), gen, m.pathModel.gen)
	}
	_ = nettrace.OutcomeReached
}
