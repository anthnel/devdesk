package netdiag

import (
	"strings"
	"testing"
	"time"

	tea "charm.land/bubbletea/v2"
	"github.com/charmbracelet/x/ansi"

	"github.com/anthnel/devdesk/internal/config"
	"github.com/anthnel/devdesk/internal/jobs"
	"github.com/anthnel/devdesk/internal/netcheck"
	"github.com/anthnel/devdesk/internal/nettrace"
	"github.com/anthnel/devdesk/internal/ui/components"
	"github.com/anthnel/devdesk/internal/ui/testutil"
)

// DEVDESK-471: the trace sub-level of Diagnostics wired into the netdiag Model.
// No test runs a Cmd that would start the producer: the run is driven by
// feeding messages. The tab-cycling, footer-height, paste and help-wording
// checks that used to live here are in four_tabs_test.go.

// traceReady returns a laid-out model on the trace sub-level of host, in the
// ready state (nothing sent), with a fake engine so that enter never touches a
// socket or a resolver.
func traceReady(t *testing.T, host string) *Model {
	t.Helper()
	m := newTestModel(t)
	m.pathModel.engine = (&fakeEngine{}).engine()
	m.openTrace(StateInput, pathTarget{Host: host})
	return m
}

// tracing returns a model on the sub-level with a trace launched over host.
func tracing(t *testing.T, host string) *Model {
	t.Helper()
	return feed(t, traceReady(t, host), testutil.Key("enter"))
}

func TestTraceLevelRenderIsTheTableInEveryState(t *testing.T) {
	run := tracing(t, "example.com")
	done := tracing(t, "example.com")
	done = feed(t, done, pathDoneMsg{gen: done.pathModel.gen, result: nettrace.Result{}})
	states := map[string]*Model{"ready": traceReady(t, "example.com"), "running": run, "done": done}
	for name, mm := range states {
		if got, want := mm.Render(), mm.pathModel.view(); got != want {
			t.Errorf("%s: Render() is not the table view (Rule 139)", name)
		}
	}
}

func TestTraceLevelFooterHoldsTheTabRowAndNoTargetBar(t *testing.T) {
	m := traceReady(t, "example.com")
	plain := ansi.Strip(m.RenderFooter(120))
	for _, want := range []string{"Diagnostics", "Forward"} {
		if !strings.Contains(plain, want) {
			t.Errorf("RenderFooter lacks %q:\n%s", want, plain)
		}
	}
	if strings.Contains(plain, "Target ›") {
		t.Errorf("the sub-level has no target field, yet the footer shows a bar:\n%s", plain)
	}
	if components.OpensWithBarFrame(m.RenderFooter(80)) {
		t.Error("the sub-level footer starts with a bar frame, so the viewport border would join it")
	}
}

func TestRunningCoversAPathTrace(t *testing.T) {
	m := newTestModel(t)
	if m.Running() {
		t.Fatal("a fresh model is not running")
	}
	m = tracing(t, "example.com")
	if !m.Running() {
		t.Error("Running() = false during a trace: the router would rebuild the view and drop it")
	}
	// Navigating away does not stop the trace, so it still counts.
	m = feed(t, m, testutil.Key("tab"))
	if !m.Running() {
		t.Error("Running() = false after leaving Diagnostics mid-trace")
	}
	m = feed(t, m, testutil.Keys("shift+tab")...)
	m = feed(t, m, pathDoneMsg{gen: m.pathModel.gen, result: nettrace.Result{}})
	if m.Running() {
		t.Error("Running() = true once the trace is done")
	}
}

func TestWantsBlinkFollowsATraceInTheSubLevel(t *testing.T) {
	m := traceReady(t, "example.com")
	if m.WantsBlink() {
		t.Error("WantsBlink() = true while ready")
	}
	m = feed(t, m, testutil.Key("enter"))
	if !m.WantsBlink() {
		t.Error("WantsBlink() = false while tracing: the dot would never blink")
	}
	m = feed(t, m, pathDoneMsg{gen: m.pathModel.gen, result: nettrace.Result{}})
	if m.WantsBlink() {
		t.Error("WantsBlink() = true after the trace ended")
	}
}

func TestAFrameBlinksTheTraceDot(t *testing.T) {
	m := tracing(t, "example.com")
	plain := ansi.Strip(m.RenderFooter(160))
	if on, off := dotCount(plain); on+off != 1 {
		t.Fatalf("the footer holds %d dots, want 1\n%s", on+off, plain)
	}
	m = feed(t, m, jobs.FrameMsg{Blink: true})
	_, onBefore := dotCount(ansi.Strip(m.RenderFooter(160)))
	m = feed(t, m, jobs.FrameMsg{Blink: false})
	_, offAfter := dotCount(ansi.Strip(m.RenderFooter(160)))
	if onBefore != 0 || offAfter != 1 {
		t.Errorf("the phase did not reach the trace footer (off dots: blink on %d, blink off %d)", onBefore, offAfter)
	}
}

func TestPathMessagesAreRoutedWhateverTabIsOnScreen(t *testing.T) {
	m := tracing(t, "example.com")
	gen := m.pathModel.gen
	m = feed(t, m, testutil.Key("shift+tab"), testutil.Key("shift+tab")) // Forward, then Interfaces
	if m.activeTab == tabDiagnostics {
		t.Fatal("precondition: Diagnostics must be off screen")
	}
	m = feed(t, m, pathHopMsg{gen: gen, hop: hopOf(1, nettrace.HopRouter)})
	if got := m.pathModel.hopCount(); got != 1 {
		t.Errorf("a hop delivered off screen left %d hop rows, want 1", got)
	}
}

func TestTraceLevelFollowsTheWindowSize(t *testing.T) {
	m := feed(t, traceReady(t, "example.com"), tea.WindowSizeMsg{Width: 90, Height: 30})
	if m.pathModel.width != 90 {
		t.Errorf("pathModel.width = %d, want 90", m.pathModel.width)
	}
}

// The expiry of a trace message is offered to the trace footer, not only to the
// other four.
func TestTraceFooterMessageIsClearedByItsOwnTimer(t *testing.T) {
	m := traceReady(t, "example.com")
	m.pathModel.footer.Warn("A message")
	read := func() string {
		return strings.TrimSpace(ansi.Strip(m.pathModel.footer.View(120, components.Status{})))
	}
	if read() == "" {
		t.Fatal("precondition: the message should show in the trace footer")
	}
	m = feed(t, m, components.ClearFooterMsg{ID: m.pathModel.footer.ID()})
	if got := read(); got != "" {
		t.Errorf("the trace message survived its expiry: %q", got)
	}
}

// The cursor blink of the focused form field must reach it once the user is
// back on the form: a message that falls through to footer.Handle leaves the
// cursor frozen.
func TestFormTargetCursorBlinksAfterLeavingTheTrace(t *testing.T) {
	m := traceReady(t, "example.com")
	m = feed(t, m, testutil.Key("esc"))
	if m.state != StateInput {
		t.Fatalf("state = %v after esc, want the form", m.state)
	}
	m.focusedField = fieldTarget
	m.targetInput.Focus()
	testutil.RequireBlinks(t, func(msg tea.Msg) tea.Cmd {
		_, cmd := step(t, m, msg)
		return cmd
	}, func() string { return m.targetInput.View() })
}

// ── Help (Rule 114), shortcuts (Rules 123, 130, 134) ─────────────────────────

func traceHelpBody(t *testing.T) string {
	t.Helper()
	for _, s := range New(testConfig()).GetHelpContent().Sections {
		if s.Title == "Diagnostics Tab — Tracing the path" {
			return s.Body
		}
	}
	t.Fatal(`no section "Diagnostics Tab — Tracing the path"`)
	return ""
}

func TestTraceHelpSectionDescribesTheEngine(t *testing.T) {
	body := traceHelpBody(t)
	for _, want := range []string{"33435", "IPv4", "Linux"} {
		if !strings.Contains(body, want) {
			t.Errorf("the trace help lacks %q", want)
		}
	}
}

// The failure wording of the trace help (DEVDESK-335): the three ways a trace
// can stop, and the port being ignored instead of traced.
func TestTraceHelpDescribesFailureReadingAndIgnoredPort(t *testing.T) {
	body := traceHelpBody(t)
	for _, want := range []string{
		"no response beyond", "last to answer", "no route", "undetermined",
		"33435", "IPv4", "port is ignored",
	} {
		if !strings.Contains(body, want) {
			t.Errorf("trace help lacks %q", want)
		}
	}
	for _, gone := range []string{"optionally followed by :port", "does not affect the probes", "Path tab"} {
		if strings.Contains(body, gone) {
			t.Errorf("trace help still contains %q", gone)
		}
	}
}

func TestTraceShortcutsAndHeaderComeFromThePathModel(t *testing.T) {
	m := traceReady(t, "example.com")
	got := m.GetShortcuts()
	if !testutil.HasShortcut(got, "enter") || !testutil.ShortcutEnabled(got, "enter") {
		t.Errorf("ready: enter missing or greyed: %v", testutil.ShortcutKeys(got))
	}
	m = feed(t, m, testutil.Key("enter"))
	if got := testutil.ShortcutKeys(m.GetShortcuts()); len(got) != 1 || got[0] != "esc" {
		t.Errorf("tracing: shortcuts = %v, want only esc", got)
	}
	var target bool
	for _, h := range m.GetHeaderInfo("default") {
		if h.Key == "Context" && h.Value != "default" {
			t.Errorf("Context = %q, want the context passed in", h.Value)
		}
		target = target || (h.Key == "Target" && strings.Contains(h.Value, "example.com"))
	}
	if !target {
		t.Error("GetHeaderInfo on the sub-level lacks the Target entry")
	}
}

func TestTraceEnterGreysOutWithoutChangingTheKeySet(t *testing.T) {
	m := traceReady(t, "example.com")
	before := testutil.ShortcutKeys(m.GetShortcuts())
	m.pathModel.engine.supported = func() error { return nettrace.ErrUnsupported }
	if !testutil.ShortcutDisabled(m.GetShortcuts(), "enter") {
		t.Error("enter is not greyed out on an unsupported platform")
	}
	after := testutil.ShortcutKeys(m.GetShortcuts())
	if strings.Join(before, ",") != strings.Join(after, ",") {
		t.Errorf("the key set changed: %v -> %v", before, after)
	}
}

// ── Construction ─────────────────────────────────────────────────────────────

// The resolve timeout is the config's check timeout, through checkSettings():
// the unit is seconds in the YAML and a zero is normalized, not a zero deadline.
func TestPathResolveTimeoutComesFromCheckSettings(t *testing.T) {
	cfg := config.Default()
	cfg.Network.CheckTimeout = 30
	if got := New(cfg).pathModel.resolveTimeout; got != 30*time.Second {
		t.Errorf("resolveTimeout = %v, want 30s", got)
	}

	cfg.Network.CheckTimeout = 0
	if got, want := New(cfg).pathModel.resolveTimeout, netcheck.DefaultCheckTimeout; got != want {
		t.Errorf("a zeroed config gave resolveTimeout = %v, want the netcheck default %v", got, want)
	}
}
