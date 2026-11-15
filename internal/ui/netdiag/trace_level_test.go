package netdiag

import (
	"context"
	"net/netip"
	"slices"
	"strings"
	"testing"

	tea "charm.land/bubbletea/v2"
	"github.com/charmbracelet/x/ansi"

	"github.com/anthnel/devdesk/internal/netcheck"
	"github.com/anthnel/devdesk/internal/nettrace"
	"github.com/anthnel/devdesk/internal/ui/components"
	"github.com/anthnel/devdesk/internal/ui/testutil"
)

// DEVDESK-470 task 470.4: Trace Path opens a sub-level of the Diagnostics tab
// (StateTrace) that reuses the path trace's model (m.pathModel). No test runs a
// real trace: the engine is a fakeEngine and the messages are fed by hand.

const traceLevelHost = "example.com"

// traceLevelModel returns a model that pressed Trace Path on traceLevelHost,
// with the Cmd that enter returned.
func traceLevelModel(t *testing.T, host string) (*Model, *fakeEngine, tea.Cmd) {
	t.Helper()
	m, fe := traceFormModel(t, true)
	fe.resolveFn = func(context.Context, string) ([]netip.Addr, error) {
		return []netip.Addr{netip.MustParseAddr("93.184.216.34")}, nil
	}
	m.targetInput.SetValue(host)
	m.focusedField = fieldTraceButton
	m, cmd := step(t, m, testutil.Key("enter"))
	return m, fe, cmd
}

// traceHops feeds n router hops of the current run.
func traceHops(t *testing.T, m *Model, n int) *Model {
	t.Helper()
	for ttl := 1; ttl <= n; ttl++ {
		m = feed(t, m, pathHopMsg{gen: m.pathModel.gen, hop: hopOf(ttl, nettrace.HopRouter)})
	}
	return m
}

func traceEnded(t *testing.T, m *Model) *Model {
	t.Helper()
	// The engine's final Result carries every hop; handleDone replaces the rows by them.
	res := nettrace.Result{Outcome: nettrace.OutcomeReached, Hops: slices.Clone(m.pathModel.hops)}
	return feed(t, m, pathDoneMsg{gen: m.pathModel.gen, result: res})
}

func traceCanceled(t *testing.T, m *Model) *Model {
	t.Helper()
	return feed(t, m, testutil.Key("esc"))
}

// tracePhases returns one fresh model per phase of a trace, so no case sees the
// mutations of another (a Model is updated in place).
func tracePhases(t *testing.T) map[string]*Model {
	t.Helper()
	build := func(end func(*testing.T, *Model) *Model) *Model {
		m, _, _ := traceLevelModel(t, traceLevelHost)
		m = traceHops(t, m, 2)
		if end == nil {
			return m
		}
		return end(t, m)
	}
	return map[string]*Model{
		"running":  build(nil),
		"done":     build(traceEnded),
		"canceled": build(traceCanceled),
	}
}

func tracePlainTitle(m *Model) string { return ansi.Strip(m.GetTitle()) }

func TestTraceLevelStateIsAppended(t *testing.T) {
	for _, s := range []ViewState{StateInput, StateRunning, StateResults, StateDetails} {
		if s >= StateTrace {
			t.Errorf("StateTrace (%d) must come after every existing state, found %d", StateTrace, s)
		}
	}
}

func TestPathModelStartFromFormReadiesThenLaunches(t *testing.T) {
	pm, fe := newPM(t)
	fe.resolveFn = func(context.Context, string) ([]netip.Addr, error) {
		return []netip.Addr{netip.MustParseAddr("1.1.1.1")}, nil
	}
	pm.errText = "stale"
	gen := pm.gen

	cmd := pm.startFromForm(pathTarget{Host: "one.one.one.one"})

	if !pm.running() || pm.gen != gen+1 {
		t.Fatalf("running=%v gen %d->%d, want a launched run", pm.running(), gen, pm.gen)
	}
	if pm.target.Host != "one.one.one.one" || pm.errText != "" {
		t.Errorf("target=%q errText=%q, want the new target and no stale error", pm.target.Host, pm.errText)
	}
	if cmd == nil {
		t.Fatal("startFromForm returned no Cmd: the trace would never start")
	}
	_ = testutil.Msgs(cmd)
	if fe.traceCalls != 1 {
		t.Errorf("trace calls = %d after running the Cmd, want 1", fe.traceCalls)
	}
}

func TestPathModelStartFromFormRefusedOnAnUnsupportedPlatform(t *testing.T) {
	m, fe := traceFormModel(t, false)
	pm := m.pathModel
	gen := pm.gen
	_ = pm.startFromForm(pathTarget{Host: traceLevelHost})
	if pm.running() || pm.gen != gen || fe.traceCalls != 0 || fe.resolveCalls != 0 {
		t.Errorf("running=%v gen %d->%d trace=%d resolve=%d, want nothing started", pm.running(), gen, pm.gen, fe.traceCalls, fe.resolveCalls)
	}
}

func TestTraceLevelEnterOpensTheSubLevelAndStartsTheTrace(t *testing.T) {
	m, fe, cmd := traceLevelModel(t, traceLevelHost)

	if m.state != StateTrace {
		t.Fatalf("state = %v, want StateTrace", m.state)
	}
	if m.activeTab != tabDiagnostics {
		t.Errorf("activeTab = %d, want the Diagnostics tab", m.activeTab)
	}
	if !m.pathModel.running() || m.pathModel.target.Host != traceLevelHost {
		t.Errorf("running=%v target=%q, want a run on %q", m.pathModel.running(), m.pathModel.target.Host, traceLevelHost)
	}
	if !m.Running() {
		t.Error("Running() = false: the router could rebuild the view mid-trace")
	}
	if m.traceValidation.Invalid(traceKeyTarget) {
		t.Error("an accepted trace left the target marked invalid")
	}

	if fe.traceCalls != 0 {
		t.Fatalf("trace ran inside Update (%d calls): it belongs to the Cmd (Rule 110)", fe.traceCalls)
	}
	msgs := testutil.Msgs(cmd)
	if fe.traceCalls != 1 || fe.resolveHost != traceLevelHost {
		t.Errorf("trace calls=%d host=%q after the Cmd, want 1 on %q", fe.traceCalls, fe.resolveHost, traceLevelHost)
	}
	if len(msgs) == 0 {
		t.Fatal("the Cmd produced no message for Update")
	}
	m = feed(t, m, msgs...)
	if m.state != StateTrace {
		t.Errorf("state = %v after the first messages, want StateTrace", m.state)
	}
}

func TestTraceLevelTitleNamesThePathAndTheHost(t *testing.T) {
	m, _, _ := traceLevelModel(t, traceLevelHost)
	title := tracePlainTitle(m)

	n, p, h := strings.Index(title, "Network"), strings.Index(title, "Path"), strings.Index(title, traceLevelHost)
	if n < 0 || p < n || h < p {
		t.Errorf("title = %q, want Network, then Path, then %q", title, traceLevelHost)
	}

	// The title is the same once the trace ended or was canceled.
	if got := tracePlainTitle(traceEnded(t, m)); got != title {
		t.Errorf("title after the end = %q, want %q", got, title)
	}
}

func TestTraceLevelTitleTruncatesALongHostToTheWidth(t *testing.T) {
	long := strings.Repeat("abcdefghi.", 8) + "com"
	m, _ := traceFormModel(t, true)
	m = feed(t, m, tea.WindowSizeMsg{Width: 50, Height: 30})
	m.targetInput.SetValue(long)
	m.focusedField = fieldTraceButton
	m = feed(t, m, testutil.Key("enter"))
	if m.state != StateTrace {
		t.Fatalf("state = %v, want StateTrace", m.state)
	}

	title := tracePlainTitle(m)
	if w := ansi.StringWidth(title); w > m.width {
		t.Errorf("title is %d cells wide at width %d: %q", w, m.width, title)
	}
	if strings.Contains(title, long) {
		t.Errorf("the whole host is in the title %q, want it truncated", title)
	}
	if !strings.Contains(title, "…") && !strings.Contains(title, "...") {
		t.Errorf("title %q is cut without an ellipsis", title)
	}
	if !strings.Contains(title, "Network") || !strings.Contains(title, "Path") {
		t.Errorf("title %q lost its prefix to the truncation", title)
	}
}

func TestTraceLevelTitleIsPlainOutsideTheSubLevel(t *testing.T) {
	m, _ := traceFormModel(t, true)
	if title := tracePlainTitle(m); strings.Contains(title, "Path") {
		t.Errorf("title = %q on the form, want no Path trail", title)
	}
}

func TestTraceLevelHopsFillTheTableAndTheBodyStaysTheTable(t *testing.T) {
	m, _, _ := traceLevelModel(t, traceLevelHost)
	if got, want := m.Render(), m.pathModel.table.View(); got != want {
		t.Error("Render() is not the table view while nothing arrived (Rule 139)")
	}

	m = traceHops(t, m, 3)
	if got := pmHopTTLs(m.pathModel); !slices.Equal(got, []int{1, 2, 3}) {
		t.Fatalf("hop rows = %v, want [1 2 3]", got)
	}
	if got, want := m.Render(), m.pathModel.table.View(); got != want {
		t.Error("Render() is not the table view with hops (Rule 139)")
	}

	m = traceEnded(t, m)
	if got, want := m.Render(), m.pathModel.table.View(); got != want {
		t.Error("Render() is not the table view once the trace ended (Rule 139)")
	}
}

func TestTraceLevelFooterHeightIsThreeAndMatchesTheRender(t *testing.T) {
	for name, mm := range tracePhases(t) {
		if got := mm.GetFooterHeight(); got != 3 {
			t.Errorf("%s: GetFooterHeight() = %d, want 3 (Rule 124)", name, got)
		}
		if got := strings.Count(mm.RenderFooter(120), "\n") + 1; got != 3 {
			t.Errorf("%s: RenderFooter emits %d lines, want 3", name, got)
		}
	}
}

func TestTraceLevelFooterShowsTheTraceProgress(t *testing.T) {
	m, _, _ := traceLevelModel(t, traceLevelHost)
	m = traceHops(t, m, 2)
	footer := ansi.Strip(m.RenderFooter(120))
	if !strings.Contains(footer, "Tracing "+traceLevelHost) {
		t.Errorf("footer = %q, want the trace progress line", footer)
	}
	if !strings.Contains(footer, "Diagnostics") || !strings.Contains(footer, "Forward") {
		t.Errorf("footer = %q, want the tab row", footer)
	}
}

func TestTraceLevelHeaderInfoIsThePathModels(t *testing.T) {
	for name, mm := range tracePhases(t) {
		got := mm.GetHeaderInfo("ctx")
		want := append([]string{"Context=ctx"}, headerPairs(mm.pathModel)...)
		var gotPairs []string
		for _, h := range got {
			gotPairs = append(gotPairs, h.Key+"="+h.Value)
		}
		if !slices.Equal(gotPairs, want) {
			t.Errorf("%s: header = %v, want %v", name, gotPairs, want)
		}
		if _, ok := headerValue(mm.pathModel, "Target"); !ok {
			t.Errorf("%s: no Target in the header", name)
		}
	}
	m, _, _ := traceLevelModel(t, traceLevelHost)
	if _, ok := headerValue(traceEnded(t, m).pathModel, "Path"); !ok {
		t.Error("an ended trace shows no Path outcome in the header")
	}
}

func headerPairs(pm *PathModel) []string {
	var out []string
	for _, h := range pm.headerInfo() {
		out = append(out, h.Key+"="+h.Value)
	}
	return out
}

// esc while tracing cancels; a second esc goes back to the form.
func TestTraceLevelEscCancelsThenGoesBackToTheForm(t *testing.T) {
	m, _, _ := traceLevelModel(t, traceLevelHost)
	m.portInput.SetValue("8443")
	m.dnsServerInput.SetValue("9.9.9.9")
	m.results = netcheck.ResultsOf(check(netcheck.CheckResolve, netcheck.OK, "kept"))
	m = traceHops(t, m, 2)
	gen := m.pathModel.gen

	m = feed(t, m, testutil.Key("esc"))
	if m.state != StateTrace {
		t.Fatalf("state = %v after the first esc, want StateTrace", m.state)
	}
	pm := m.pathModel
	if pm.running() || pm.result == nil || pm.result.Outcome != nettrace.OutcomeCancelled {
		t.Errorf("running=%v result=%+v, want a canceled run", pm.running(), pm.result)
	}
	if pm.gen == gen {
		t.Error("the run's generation did not change: its late messages would still land")
	}
	if got := pmHopTTLs(pm); !slices.Equal(got, []int{1, 2}) {
		t.Errorf("hops = %v after cancel, want [1 2] kept", got)
	}
	if m.Running() {
		t.Error("Running() = true after the cancel")
	}

	m = feed(t, m, testutil.Key("esc"))
	if m.state != StateInput {
		t.Fatalf("state = %v after the second esc, want StateInput", m.state)
	}
	assertFormKept(t, m)
}

// assertFormKept checks what going back to the form must not lose.
func assertFormKept(t *testing.T, m *Model) {
	t.Helper()
	if got := m.targetInput.Value(); got != traceLevelHost {
		t.Errorf("Target = %q, want %q kept", got, traceLevelHost)
	}
	if got := m.portInput.Value(); got != "8443" {
		t.Errorf("Port = %q, want 8443 kept", got)
	}
	if got := m.dnsServerInput.Value(); got != "9.9.9.9" {
		t.Errorf("DNS = %q, want 9.9.9.9 kept", got)
	}
	if m.focusedField != fieldTraceButton {
		t.Errorf("focusedField = %d, want the Trace Path button (%d)", m.focusedField, fieldTraceButton)
	}
	if m.targetInput.Focused() {
		t.Error("the target field has the focus while the button is focused")
	}
	if len(m.results.All()) != 1 {
		t.Error("going back reset the Diagnostics results: it must not call resetToForm")
	}
	if m.InEditMode() {
		t.Error("InEditMode() = true with the button focused")
	}
}

// Done and canceled are the same screen for the keys: esc goes back, enter
// traces again, ctrl+o opens the source selector.
func TestTraceLevelKeysAreTheSameWhenDoneAndWhenCanceled(t *testing.T) {
	ends := map[string]func(*testing.T, *Model) *Model{
		"done":     func(t *testing.T, m *Model) *Model { return traceEnded(t, traceHops(t, m, 2)) },
		"canceled": func(t *testing.T, m *Model) *Model { return traceCanceled(t, traceHops(t, m, 2)) },
	}
	for name, end := range ends {
		t.Run(name+"/esc goes back", func(t *testing.T) {
			m, _, _ := traceLevelModel(t, traceLevelHost)
			m.portInput.SetValue("8443")
			m.dnsServerInput.SetValue("9.9.9.9")
			m.results = netcheck.ResultsOf(check(netcheck.CheckResolve, netcheck.OK, "kept"))
			m = feed(t, end(t, m), testutil.Key("esc"))
			if m.state != StateInput {
				t.Fatalf("state = %v, want StateInput", m.state)
			}
			assertFormKept(t, m)
		})

		t.Run(name+"/ctrl+r does nothing", func(t *testing.T) {
			m, fe, _ := traceLevelModel(t, traceLevelHost)
			m = end(t, m)
			gen, calls := m.pathModel.gen, fe.traceCalls

			m, cmd := step(t, m, testutil.Key("ctrl+r"))

			if m.state != StateTrace || m.pathModel.running() || m.pathModel.gen != gen {
				t.Errorf("state=%v running=%v gen %d->%d, want the finished trace untouched", m.state, m.pathModel.running(), gen, m.pathModel.gen)
			}
			if msgs := testutil.Msgs(cmd); len(msgs) != 0 || fe.traceCalls != calls {
				t.Errorf("msgs=%v trace calls %d->%d, want none", msgs, calls, fe.traceCalls)
			}
			if got := pmHopTTLs(m.pathModel); !slices.Equal(got, []int{1, 2}) {
				t.Errorf("hops = %v, want [1 2] kept", got)
			}
		})

		t.Run(name+"/enter traces again", func(t *testing.T) {
			m, _, _ := traceLevelModel(t, traceLevelHost)
			m = end(t, m)
			gen := m.pathModel.gen

			m, cmd := step(t, m, testutil.Key("enter"))

			if m.state != StateTrace || !m.pathModel.running() || m.pathModel.gen != gen+1 || cmd == nil {
				t.Errorf("state=%v running=%v gen %d->%d, want a new run", m.state, m.pathModel.running(), gen, m.pathModel.gen)
			}
		})

		t.Run(name+"/ctrl+o opens the selector", func(t *testing.T) {
			m, _, _ := traceLevelModel(t, traceLevelHost)
			m = end(t, m)

			m = feed(t, m, testutil.Key("ctrl+o"))

			if !m.pathModel.pickerOpen() || m.state != StateTrace {
				t.Errorf("picker=%v state=%v, want the selector open on the sub-level", m.pathModel.pickerOpen(), m.state)
			}
		})
	}
}

func TestTraceLevelNoRelaunchOrSourceKeyWhileRunning(t *testing.T) {
	for _, key := range []string{"enter", "ctrl+r", "ctrl+o"} {
		m, fe, _ := traceLevelModel(t, traceLevelHost)
		gen := m.pathModel.gen
		m, cmd := step(t, m, testutil.Key(key))
		if !m.pathModel.running() || m.pathModel.gen != gen || m.pathModel.pickerOpen() {
			t.Errorf("%s: running=%v gen %d->%d picker=%v, want the run untouched", key, m.pathModel.running(), gen, m.pathModel.gen, m.pathModel.pickerOpen())
		}
		if len(testutil.Msgs(cmd)) != 0 || fe.traceCalls != 0 {
			t.Errorf("%s: a Cmd or a trace came out of the key", key)
		}
	}
}

// A trace already running (started earlier) refuses a second one.
func TestTraceLevelRefusedWhileATraceRuns(t *testing.T) {
	m, fe := traceFormModel(t, true)
	_, _ = m.pathModel.launch(pathTarget{Host: "1.1.1.1"})
	gen := m.pathModel.gen
	if !m.pathModel.running() {
		t.Fatal("precondition: a trace must be running")
	}
	m.targetInput.SetValue(traceLevelHost)

	m.focusedField = fieldTraceButton
	m = feed(t, m, testutil.Key("enter"))

	if m.state != StateInput {
		t.Errorf("state = %v, want StateInput", m.state)
	}
	if m.footer.Level() != components.LevelWarning || m.footer.Text() != reasonPathBusy {
		t.Errorf("footer = {%v %q}, want Warn %q", m.footer.Level(), m.footer.Text(), reasonPathBusy)
	}
	if m.pathModel.gen != gen || m.pathModel.target.Host != "1.1.1.1" || fe.traceCalls != 0 {
		t.Errorf("gen %d->%d target=%q trace=%d, want the running trace untouched", gen, m.pathModel.gen, m.pathModel.target.Host, fe.traceCalls)
	}
}

func TestTraceLevelShortcuts(t *testing.T) {
	m, _, _ := traceLevelModel(t, traceLevelHost)

	running := m.GetShortcuts()
	if got := testutil.ShortcutKeys(running); !slices.Equal(got, []string{"esc"}) {
		t.Fatalf("running shortcuts = %v, want [esc] alone", got)
	}
	if running[0].Description != "Cancel the trace" || running[0].Disabled {
		t.Errorf("running esc = %+v, want an enabled %q", running[0], "Cancel the trace")
	}

	for name, mm := range tracePhases(t) {
		if name == "running" {
			continue
		}
		sc := mm.GetShortcuts()
		if got := testutil.ShortcutKeys(sc); !slices.Equal(got, []string{"tab", "enter", "ctrl+o", "esc", "?"}) {
			t.Errorf("%s: shortcuts = %v, want [tab enter ctrl+o esc ?]", name, got)
			continue
		}
		if sc[3].Description != "Back to the form" {
			t.Errorf("%s: esc says %q, want %q", name, sc[3].Description, "Back to the form")
		}
		if testutil.HasShortcut(sc, "ctrl+r") {
			t.Errorf("%s: ctrl+r is advertised but does nothing here", name)
		}
		if !testutil.ShortcutEnabled(sc, "esc") || !testutil.ShortcutEnabled(sc, "tab") {
			t.Errorf("%s: tab and esc must be enabled", name)
		}
	}

	phases := tracePhases(t)
	done, cancel := phases["done"].GetShortcuts(), phases["canceled"].GetShortcuts()
	if !slices.Equal(testutil.ShortcutKeys(done), testutil.ShortcutKeys(cancel)) {
		t.Errorf("done %v and canceled %v advertise different keys", testutil.ShortcutKeys(done), testutil.ShortcutKeys(cancel))
	}
}

// The enter entry says what it does: it starts in the ready state, restarts
// once a trace ended.
func TestTraceLevelEnterLabelFollowsTheState(t *testing.T) {
	ready, _ := routeLevel(t, "example.com")
	done := tracePhases(t)["done"]
	for name, tc := range map[string]struct {
		m    *Model
		want string
	}{"ready": {ready, "Trace the path"}, "done": {done, "Trace again"}} {
		var got string
		for _, s := range tc.m.GetShortcuts() {
			if s.Key == "enter" {
				got = s.Description
			}
		}
		if got != tc.want {
			t.Errorf("%s: enter says %q, want %q", name, got, tc.want)
		}
	}
}

func TestTraceLevelNavigationKeysMoveTheTable(t *testing.T) {
	for name, prep := range map[string]func(*Model) *Model{
		"running": func(m *Model) *Model { return m },
		"done":    func(m *Model) *Model { return traceEnded(t, m) },
	} {
		m, _, _ := traceLevelModel(t, traceLevelHost)
		m = prep(traceHops(t, m, 5))
		ttl := func() int {
			s, ok := m.pathModel.table.Selected()
			if !ok || s.Kind != stepHop {
				t.Fatalf("%s: no hop selected (%v, %v)", name, s.Kind, ok)
			}
			return s.Hop.TTL
		}

		m = feed(t, m, testutil.Key("end"))
		if got := ttl(); got != 5 {
			t.Errorf("%s: end selects TTL %d, want 5", name, got)
		}
		m = feed(t, m, testutil.Key("up"))
		if got := ttl(); got != 4 {
			t.Errorf("%s: up selects TTL %d, want 4", name, got)
		}
		// home reaches the first row, which is the local source row, not hop 1.
		m = feed(t, m, testutil.Key("home"))
		if s, ok := m.pathModel.table.Selected(); !ok || s.Kind != stepSource {
			t.Errorf("%s: home selects %v (%v), want the source row", name, s.Kind, ok)
		}
		m = feed(t, m, testutil.Key("down"))
		if got := ttl(); got != 1 {
			t.Errorf("%s: down selects TTL %d, want 1", name, got)
		}
		if m.state != StateTrace {
			t.Errorf("%s: state = %v, want StateTrace", name, m.state)
		}
	}
}

// The Path model has no target field: letters, pastes and backspace do nothing
// in the sub-level, in any state of the run.
func TestTraceLevelLettersAreInert(t *testing.T) {
	typed := append(testutil.Type("abcXYZ:"), testutil.Key("backspace"), testutil.Paste("pasted"))
	ends := map[string]func(*testing.T, *Model) *Model{
		"running":  func(_ *testing.T, m *Model) *Model { return m },
		"done":     traceEnded,
		"canceled": traceCanceled,
	}
	for name, end := range ends {
		t.Run(name, func(t *testing.T) {
			m, _, _ := traceLevelModel(t, traceLevelHost)
			m = end(t, traceHops(t, m, 1))
			footer := m.footer.Text()
			pm := m.pathModel
			state, gen := pm.state, pm.gen

			m = feed(t, m, typed...)

			if pm.state != state || pm.gen != gen || pm.target.Host != traceLevelHost {
				t.Errorf("state %v->%v gen %d->%d target=%q, want all unchanged", state, pm.state, gen, pm.gen, pm.target.Host)
			}
			if got := m.targetInput.Value(); got != traceLevelHost {
				t.Errorf("form target = %q, want %q untouched", got, traceLevelHost)
			}
			if m.state != StateTrace || m.footer.Text() != footer {
				t.Errorf("state=%v footer=%q, want nothing to react to a letter", m.state, m.footer.Text())
			}
			if m.InEditMode() {
				t.Error("InEditMode() = true: the sub-level has no field to edit")
			}
		})
	}
}

// A failed run ends in pathDone; the sub-level keeps the keyboard out of any
// field, and esc still leads back.
func TestTraceLevelAFailedRunKeepsTheKeyboardInert(t *testing.T) {
	m, _, _ := traceLevelModel(t, traceLevelHost)
	m = feed(t, m, pathDoneMsg{gen: m.pathModel.gen, err: context.DeadlineExceeded})

	m = feed(t, m, testutil.Type("abcXYZ")...)
	if m.pathModel.state != pathDone || m.pathModel.target.Host != traceLevelHost {
		t.Errorf("state=%v target=%q after a failed run and letters, want pathDone on %q", m.pathModel.state, m.pathModel.target.Host, traceLevelHost)
	}
	if m.InEditMode() {
		t.Error("InEditMode() = true after a failed run: the router would swallow ':' and the keys")
	}
	if m.Running() {
		t.Error("Running() = true after a failed run")
	}

	m = feed(t, m, testutil.Key("esc"))
	if m.state != StateInput {
		t.Errorf("state = %v after esc on a failed run, want StateInput: the user must not be stranded", m.state)
	}
}

func TestTraceLevelSurvivesALeaveAndReturn(t *testing.T) {
	for name, prep := range map[string]func(*testing.T, *Model) *Model{
		"running": func(_ *testing.T, m *Model) *Model { return m },
		"done":    traceEnded,
	} {
		t.Run(name, func(t *testing.T) {
			m, _, _ := traceLevelModel(t, traceLevelHost)
			m = prep(t, traceHops(t, m, 2))
			title := tracePlainTitle(m)

			m = feed(t, m, testutil.Key("tab"))
			if m.activeTab != tabPorts {
				t.Fatalf("activeTab = %d, want Ports", m.activeTab)
			}
			if got := tracePlainTitle(m); strings.Contains(got, "Path") {
				t.Errorf("another tab shows the sub-level title %q", got)
			}
			m = feed(t, m, testutil.Key("shift+tab"))

			if m.activeTab != tabDiagnostics || m.state != StateTrace {
				t.Fatalf("tab=%d state=%v, want the sub-level back", m.activeTab, m.state)
			}
			if got := tracePlainTitle(m); got != title {
				t.Errorf("title = %q, want %q", got, title)
			}
			if got, want := m.Render(), m.pathModel.table.View(); got != want {
				t.Error("Render() is not the trace table after returning")
			}
			if got := pmHopTTLs(m.pathModel); !slices.Equal(got, []int{1, 2}) {
				t.Errorf("hops = %v, want [1 2] kept", got)
			}
			if got := m.GetFooterHeight(); got != 3 {
				t.Errorf("GetFooterHeight() = %d, want 3", got)
			}
		})
	}
}

// A trace goes on from the other tabs: its hops keep landing in the table.
func TestTraceLevelHopsKeepArrivingFromAnotherTab(t *testing.T) {
	m, _, _ := traceLevelModel(t, traceLevelHost)
	m = feed(t, m, testutil.Key("tab"))
	m = traceHops(t, m, 2)
	m = feed(t, m, testutil.Key("shift+tab"))
	if got := pmHopTTLs(m.pathModel); !slices.Equal(got, []int{1, 2}) {
		t.Errorf("hops = %v, want [1 2]", got)
	}
}

// A second trace from the form works after coming back.
func TestTraceLevelCanTraceAgainAfterGoingBack(t *testing.T) {
	m, fe, _ := traceLevelModel(t, traceLevelHost)
	m = traceHops(t, m, 2)
	m = traceCanceled(t, m)
	m = feed(t, m, testutil.Key("esc"))
	gen := m.pathModel.gen

	m, cmd := step(t, m, testutil.Key("enter"))
	if m.state != StateTrace || !m.pathModel.running() || m.pathModel.gen == gen {
		t.Fatalf("state=%v running=%v gen %d->%d, want a new run", m.state, m.pathModel.running(), gen, m.pathModel.gen)
	}
	if got := pmHopTTLs(m.pathModel); len(got) != 0 {
		t.Errorf("hops = %v, want the previous run's rows cleared", got)
	}
	_ = testutil.Msgs(cmd)
	if fe.traceCalls != 1 {
		t.Errorf("trace calls = %d, want 1 for the second run", fe.traceCalls)
	}
}

// Rule 116 for the sub-level: the table fills the viewport at any width, and its
// height follows the router's, which hands the view 2 lines more than on the
// old Path tab (footer of 3 lines instead of 5).
func TestTraceLevelTableFillsTheViewport(t *testing.T) {
	const window = 40
	for _, w := range []int{120, 60} {
		m, _, _ := traceLevelModel(t, traceLevelHost)
		m = traceHops(t, m, 3)

		m = feed(t, m, tea.WindowSizeMsg{Width: w, Height: window - 3})
		if got, want := m.pathModel.table.RenderedWidth(), w-2; got != want {
			t.Errorf("width %d: RenderedWidth() = %d, want %d", w, got, want)
		}
		three := strings.Count(m.Render(), "\n")

		m = feed(t, m, tea.WindowSizeMsg{Width: w, Height: window - 5})
		five := strings.Count(m.Render(), "\n")

		if three-five != 2 {
			t.Errorf("width %d: table is %d lines with a 3-line footer and %d with 5, want exactly 2 more", w, three+1, five+1)
		}
	}
}

// The sub-level and the path trace size their table the same way for the same
// viewport.
func TestTraceLevelTableSizeMatchesThePathTab(t *testing.T) {
	m, _, _ := traceLevelModel(t, traceLevelHost)
	m = feed(t, m, tea.WindowSizeMsg{Width: 100, Height: 30})
	pm, _ := newPM(t)
	pm.resize(100, 30)
	if got, want := strings.Count(m.Render(), "\n"), strings.Count(pm.view(), "\n"); got != want {
		t.Errorf("sub-level table has %d line breaks, the path trace's has %d at the same size", got, want)
	}
}
