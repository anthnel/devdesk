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

// DEVDESK-328 step 5: the Path tab wired into the netdiag Model. No test runs a
// Cmd that would start the producer: the run is driven by feeding messages.

// onPathTab returns a laid-out model standing on the Path tab, with a fake
// engine so that enter never touches a socket or a resolver.
func onPathTab(t *testing.T) *Model {
	t.Helper()
	m := newTestModel(t)
	if m.pathModel == nil {
		t.Fatal("New() built no pathModel")
	}
	m.pathModel.engine = (&fakeEngine{}).engine()
	return feed(t, m, testutil.Keys("tab", "tab", "tab", "tab")...)
}

// tracing returns a model on the Path tab with a trace launched over host.
func tracing(t *testing.T, host string) *Model {
	t.Helper()
	m := onPathTab(t)
	m = feed(t, m, testutil.Type(host)...)
	return feed(t, m, testutil.Key("enter"))
}

func TestPathIsTheFifthTab(t *testing.T) {
	if tabPath != 4 {
		t.Errorf("tabPath = %d, want 4 (the fifth tab, after Forward)", tabPath)
	}
	if tabCount != 5 {
		t.Errorf("tabCount = %d, want 5", tabCount)
	}
}

func TestPathIsReachedByTabAloneAndCyclesBothWays(t *testing.T) {
	m := newTestModel(t)
	for _, want := range []int{tabPorts, tabInterfaces, tabForward, tabPath, tabDiagnostics} {
		m = feed(t, m, testutil.Key("tab"))
		if m.activeTab != want {
			t.Fatalf("tab: activeTab = %d, want %d", m.activeTab, want)
		}
	}
	for _, want := range []int{tabPath, tabForward, tabInterfaces, tabPorts, tabDiagnostics} {
		m = feed(t, m, testutil.Key("shift+tab"))
		if m.activeTab != want {
			t.Fatalf("shift+tab: activeTab = %d, want %d", m.activeTab, want)
		}
	}
}

// tab is never a field key on Path, so it leaves the tab even while the field
// has the keyboard.
func TestTabLeavesPathWhileTheFieldIsEditing(t *testing.T) {
	m := onPathTab(t)
	if !m.InEditMode() {
		t.Fatal("precondition: the field should be editing")
	}
	m = feed(t, m, testutil.Key("tab"))
	if m.activeTab != tabDiagnostics {
		t.Errorf("activeTab = %d, want %d", m.activeTab, tabDiagnostics)
	}
}

func TestPathRenderIsTheTableInEveryState(t *testing.T) {
	m := onPathTab(t)
	states := map[string]*Model{"editing": m}

	run := feed(t, onPathTab(t), testutil.Type("example.com")...)
	run = feed(t, run, testutil.Key("enter"))
	states["running"] = run

	done := feed(t, run, pathDoneMsg{gen: run.pathModel.gen, result: nettrace.Result{}})
	states["done"] = done

	for name, mm := range states {
		if got, want := mm.Render(), mm.pathModel.view(); got != want {
			t.Errorf("%s: Render() is not the table view (Rule 139)", name)
		}
	}
}

func TestPathFooterHeightIsFiveInEveryState(t *testing.T) {
	m := onPathTab(t)
	if got := m.GetFooterHeight(); got != 5 {
		t.Errorf("editing: GetFooterHeight() = %d, want 5", got)
	}
	m = feed(t, m, testutil.Type("example.com")...)
	m = feed(t, m, testutil.Key("enter"))
	if got := m.GetFooterHeight(); got != 5 {
		t.Errorf("running: GetFooterHeight() = %d, want 5", got)
	}
	m = feed(t, m, pathDoneMsg{gen: m.pathModel.gen, result: nettrace.Result{}})
	if got := m.GetFooterHeight(); got != 5 {
		t.Errorf("done: GetFooterHeight() = %d, want 5", got)
	}
}

func TestPathFooterHoldsTheBarAndTheTabLabel(t *testing.T) {
	plain := ansi.Strip(onPathTab(t).RenderFooter(120))
	for _, want := range []string{"Target ›", "Path", "Diagnostics", "Forward"} {
		if !strings.Contains(plain, want) {
			t.Errorf("RenderFooter lacks %q:\n%s", want, plain)
		}
	}
	if got := strings.Count(plain, "\n") + 1; got != 5 {
		t.Errorf("RenderFooter is %d lines, want 5 (bar 2, tabs, blank, info)", got)
	}
}

func TestPathEditModeFollowsTheField(t *testing.T) {
	m := onPathTab(t)
	if !m.InEditMode() {
		t.Error("editing: InEditMode() = false, a ':' typed must land in the field")
	}
	if m.FilterBarVisible() {
		t.Error("Path has no filter bar")
	}
	m = feed(t, m, testutil.Type("example.com")...)
	m = feed(t, m, testutil.Key("enter"))
	if m.InEditMode() {
		t.Error("running: InEditMode() = true, the field is blurred")
	}
}

func TestPathPasteReachesTheField(t *testing.T) {
	m := onPathTab(t)
	m = feed(t, m, testutil.Paste("example.com"))
	if got := m.pathModel.input.Value(); got != "example.com" {
		t.Errorf("path field = %q, want the pasted text", got)
	}
	if got := m.targetInput.Value(); got != "" {
		t.Errorf("the Diagnostics target field received the paste: %q", got)
	}
}

func TestPathPasteIsDroppedWhileTracing(t *testing.T) {
	m := tracing(t, "example.com")
	m = feed(t, m, testutil.Paste("evil.example"))
	if got := m.pathModel.input.Value(); got != "example.com" {
		t.Errorf("path field = %q, a paste must not edit it while tracing", got)
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
		t.Error("Running() = false after leaving the Path tab mid-trace")
	}
	m = feed(t, m, testutil.Keys("shift+tab")...)
	m = feed(t, m, pathDoneMsg{gen: m.pathModel.gen, result: nettrace.Result{}})
	if m.Running() {
		t.Error("Running() = true once the trace is done")
	}
}

func TestWantsBlinkFollowsATraceOnThePathTab(t *testing.T) {
	m := onPathTab(t)
	if m.WantsBlink() {
		t.Error("WantsBlink() = true while editing")
	}
	m = feed(t, m, testutil.Type("example.com")...)
	m = feed(t, m, testutil.Key("enter"))
	if !m.WantsBlink() {
		t.Error("WantsBlink() = false while tracing: the dot would never blink")
	}
	m = feed(t, m, pathDoneMsg{gen: m.pathModel.gen, result: nettrace.Result{}})
	if m.WantsBlink() {
		t.Error("WantsBlink() = true after the trace ended")
	}
}

func TestAFrameBlinksThePathDot(t *testing.T) {
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
		t.Errorf("the phase did not reach the Path footer (off dots: blink on %d, blink off %d)", onBefore, offAfter)
	}
}

func TestPathMessagesAreRoutedWhateverTabIsOnScreen(t *testing.T) {
	m := tracing(t, "example.com")
	gen := m.pathModel.gen
	m = feed(t, m, testutil.Key("shift+tab"), testutil.Key("shift+tab")) // Forward, then Interfaces
	if m.activeTab == tabPath {
		t.Fatal("precondition: Path must be off screen")
	}
	m = feed(t, m, pathHopMsg{gen: gen, hop: hopOf(1, nettrace.HopRouter)})
	if got := len(m.pathModel.table.Items()); got != 1 {
		t.Errorf("a hop delivered off screen left %d rows on Path, want 1", got)
	}
}

func TestPathFollowsTheWindowSize(t *testing.T) {
	m := feed(t, onPathTab(t), tea.WindowSizeMsg{Width: 90, Height: 30})
	if m.pathModel.width != 90 {
		t.Errorf("pathModel.width = %d, want 90", m.pathModel.width)
	}
}

// The expiry of a Path message is offered to the Path footer, not only to the
// other four.
func TestPathFooterMessageIsClearedByItsOwnTimer(t *testing.T) {
	m := onPathTab(t)
	m = feed(t, m, testutil.Key("enter")) // empty target: refused, footer Error
	read := func() string {
		return strings.TrimSpace(ansi.Strip(m.pathModel.footer.View(120, components.Status{})))
	}
	if read() == "" {
		t.Fatal("precondition: the refusal should show in the Path footer")
	}
	m = feed(t, m, components.ClearFooterMsg{ID: m.pathModel.footer.ID()})
	if got := read(); got != "" {
		t.Errorf("the Path message survived its expiry: %q", got)
	}
}

// The cursor blink of the focused field must reach it: a message that falls
// through to footer.Handle leaves the cursor frozen.
func TestPathFieldCursorBlinks(t *testing.T) {
	m := onPathTab(t)
	testutil.RequireBlinks(t, func(msg tea.Msg) tea.Cmd {
		_, cmd := step(t, m, msg)
		return cmd
	}, func() string { return m.pathModel.input.View() })
}

func TestPathModelForwardsTheCursorBlinkToTheField(t *testing.T) {
	pm, _ := newPM(t)
	testutil.RequireBlinks(t, func(msg tea.Msg) tea.Cmd {
		var cmd tea.Cmd
		pm, cmd = pm.update(msg)
		return cmd
	}, func() string { return pm.input.View() })
}

// ── Help (Rule 114), shortcuts (Rules 123, 130, 134) ─────────────────────────

func TestHelpDescribesFiveTabsAndPathKeys(t *testing.T) {
	c := New(testConfig()).GetHelpContent()
	if !strings.Contains(c.Description, "Five-tab") || !strings.Contains(c.Description, "Path") {
		t.Errorf("Description = %q, want a five-tab summary naming Path", c.Description)
	}
	keys := map[string]bool{}
	for _, kb := range c.KeyBindings {
		keys[kb.Key] = true
	}
	for _, want := range []string{"enter (Path)", "esc (Path, running)", "esc (Path, done)"} {
		if !keys[want] {
			t.Errorf("help lacks the binding %q", want)
		}
	}
	var tabs string
	for _, kb := range c.KeyBindings {
		if strings.HasPrefix(kb.Key, "tab") {
			tabs = kb.Description
		}
	}
	if !strings.Contains(tabs, "Path") {
		t.Errorf("the tab binding %q does not mention Path", tabs)
	}
	var how bool
	for _, s := range c.Sections {
		if s.Title == "Path Tab — How it works" {
			how = strings.Contains(s.Body, "33435") && strings.Contains(s.Body, "IPv4") && strings.Contains(s.Body, "Linux")
		}
	}
	if !how {
		t.Error(`no section "Path Tab — How it works" mentioning Linux, IPv4 and UDP port 33435`)
	}
}

func TestPathShortcutsAndHeaderComeFromThePathModel(t *testing.T) {
	m := onPathTab(t)
	got := m.GetShortcuts()
	if !testutil.HasShortcut(got, "enter") || !testutil.ShortcutEnabled(got, "enter") {
		t.Errorf("editing: enter missing or greyed: %v", testutil.ShortcutKeys(got))
	}
	m = feed(t, m, testutil.Type("example.com")...)
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
		t.Error("GetHeaderInfo on Path lacks the Target entry")
	}
}

func TestPathEnterGreysOutWithoutChangingTheKeySet(t *testing.T) {
	m := onPathTab(t)
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
