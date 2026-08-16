package viewer

import (
	"strings"
	"testing"

	tea "github.com/charmbracelet/bubbletea"
	"github.com/charmbracelet/x/ansi"

	"github.com/anthnel/devdesk/internal/ui/testutil"
	viewerpkg "github.com/anthnel/devdesk/internal/viewer"
)

const mixedLog = `2026-01-01 [INFO] started
2026-01-01 [ERROR] request failed
  at handler.go:42
  at server.go:10
2026-01-01 [DEBUG] cache warm
2026-01-01 [WARN] disk almost full`

// ── Scrolling, wrap, reload: the logs pane's behaviour, moved ────────────────

func TestScrollKeys(t *testing.T) {
	m := logModel(t, strings.Repeat("line\n", 200))

	m = feed(t, m, testutil.Key("g"))
	if m.textViewport.YOffset != 0 {
		t.Errorf("YOffset = %d after g, want the top", m.textViewport.YOffset)
	}

	m = feed(t, m, testutil.Key("down"))
	if m.textViewport.YOffset != 1 {
		t.Errorf("YOffset = %d after down, want 1", m.textViewport.YOffset)
	}

	m = feed(t, m, testutil.Key("up"))
	if m.textViewport.YOffset != 0 {
		t.Errorf("YOffset = %d after up, want 0", m.textViewport.YOffset)
	}

	m = feed(t, m, testutil.Key("pgdown"))
	if m.textViewport.YOffset == 0 {
		t.Error("pgdown did not scroll")
	}

	m = feed(t, m, testutil.Key("G"))
	atBottom := m.textViewport.YOffset
	m = feed(t, m, testutil.Key("pgup"))
	if m.textViewport.YOffset >= atBottom {
		t.Error("pgup did not scroll back up")
	}
}

// `q` is the application's quit key (Rule 111). The logs pane swallowed it; this
// one does not.
func TestQIsNotBound(t *testing.T) {
	m := logModel(t, "line")

	if _, cmd := step(t, m, testutil.Key("q")); cmd != nil {
		t.Error("q produced a command; it belongs to the application")
	}
}

func TestWrapSplitsLongLines(t *testing.T) {
	long := strings.Repeat("x", 300)
	m := logModel(t, long)

	// The total, not the visible window: the viewport only ever renders its own
	// height, so counting the rendered lines would report 20 either way.
	unwrapped := m.textViewport.TotalLineCount()
	m = feed(t, m, testutil.Key("w"))
	if !m.wrap {
		t.Fatal("w did not enable wrapping")
	}
	if got := m.textViewport.TotalLineCount(); got <= unwrapped {
		t.Errorf("wrapping a 300-character line at width 80 gives %d lines, was %d", got, unwrapped)
	}

	m = feed(t, m, testutil.Key("w"))
	if m.wrap {
		t.Error("w did not disable wrapping")
	}
}

// Narrowing must re-split wrapped content rather than leave lines longer than
// the pane.
func TestResizeReflowsWrappedText(t *testing.T) {
	m := logModel(t, strings.Repeat("x", 300))
	m = feed(t, m, testutil.Key("w"))

	m = feed(t, m, tea.WindowSizeMsg{Width: 40, Height: 20})

	if m.textViewport.Width != 40 {
		t.Errorf("text viewport width = %d after the resize, want 40", m.textViewport.Width)
	}
	for _, line := range strings.Split(m.textViewport.View(), "\n") {
		if width := len([]rune(ansi.Strip(line))); width > 40 {
			t.Fatalf("a line is %d cells wide after narrowing to 40: %q", width, line)
		}
	}
}

func TestReloadRefetches(t *testing.T) {
	m := logModel(t, "line")

	m, cmd := step(t, m, testutil.Key("ctrl+r"))

	if !m.loading {
		t.Error("loading = false after ctrl+r, so the spinner never shows")
	}
	if cmd == nil {
		t.Error("ctrl+r issued no fetch")
	}
}

// Returning from the pager or the follow stream reloads: the document may have
// moved on while the TUI was suspended.
func TestReturningFromAPagerReloads(t *testing.T) {
	m := logModel(t, "line")

	m, cmd := step(t, m, PagerExitMsg{})

	if !m.loading {
		t.Error("loading = false after returning from the pager")
	}
	if cmd == nil {
		t.Error("returning from the pager did not refetch")
	}
}

// ── The source's optional capabilities (Rule 130) ────────────────────────────

func TestTimestampsAndFollowAppearOnlyForASourceThatSupportsThem(t *testing.T) {
	capable := logModel(t, "line")
	for _, key := range []string{"t", "ctrl+f", "e"} {
		if !hasShortcut(capable, key) {
			t.Errorf("%q is not offered for a source that supports it", key)
		}
	}

	plain := open(t, fakeSource{name: "notes.md", content: "hello"})
	for _, key := range []string{"t", "ctrl+f", "e"} {
		if hasShortcut(plain, key) {
			t.Errorf("%q is offered for a source that cannot do it", key)
		}
	}
}

func TestTimestampsToggleRefetches(t *testing.T) {
	m := logModel(t, "line")

	m, cmd := step(t, m, testutil.Key("t"))

	if !m.showTimestamps {
		t.Error("t did not enable timestamps")
	}
	if !m.loading {
		t.Error("loading = false after toggling timestamps")
	}
	if cmd == nil {
		t.Error("toggling timestamps did not refetch")
	}
	if !m.source.(capableSource).timestamps {
		t.Error("the source was not replaced with one that carries the flag")
	}
}

// A source with no timestamp capability must not have its flag flipped by a key
// that cannot act on it.
func TestTOnAPlainSourceIsInert(t *testing.T) {
	m := open(t, fakeSource{name: "notes.md", content: "hello"})

	m, cmd := step(t, m, testutil.Key("t"))

	if m.showTimestamps || cmd != nil {
		t.Error("t acted on a source that cannot re-fetch with timestamps")
	}
}

// ── Verbosity (the log filter) ───────────────────────────────────────────────

func TestOnlyALogDocumentOffersTheVerbosityFilter(t *testing.T) {
	if !hasShortcut(logModel(t, mixedLog), "v") {
		t.Error("a log does not offer v")
	}

	plain := feed(t, jsonModel(t), testutil.Key("f")) // to its text display
	if hasShortcut(plain, "v") {
		t.Error("a JSON document offers a verbosity filter it has no levels for")
	}
}

func TestVerbosityCyclesAllThroughToError(t *testing.T) {
	m := logModel(t, mixedLog)

	want := []viewerpkg.Level{
		viewerpkg.LevelTrace, viewerpkg.LevelDebug, viewerpkg.LevelInfo,
		viewerpkg.LevelWarn, viewerpkg.LevelError, viewerpkg.LevelUnknown,
	}
	for _, level := range want {
		m = feed(t, m, testutil.Key("v"))
		if m.minLevel != level {
			t.Fatalf("v went to %v, want %v", m.minLevel, level)
		}
	}
}

// The decision the filter rests on: a stack trace has no level of its own and
// must survive a filter set to its parent's.
func TestAStackTraceSurvivesAWarnFilter(t *testing.T) {
	m := logModel(t, mixedLog)

	// all → trace → debug → info → warn
	for range 4 {
		m = feed(t, m, testutil.Key("v"))
	}
	if m.minLevel != viewerpkg.LevelWarn {
		t.Fatalf("minLevel = %v, want warn", m.minLevel)
	}

	rendered := m.textViewport.View()
	for _, want := range []string{"request failed", "handler.go:42", "server.go:10", "disk almost full"} {
		if !strings.Contains(rendered, want) {
			t.Errorf("≥ warn dropped %q", want)
		}
	}
	for _, gone := range []string{"cache warm", "started"} {
		if strings.Contains(rendered, gone) {
			t.Errorf("≥ warn kept %q", gone)
		}
	}
}

// The bar shows the level in force, and disappears entirely at `all` — no
// filter, no bar (Rule 136).
func TestTheFilterBarFollowsTheVerbosity(t *testing.T) {
	m := logModel(t, mixedLog)

	if m.bar.IsVisible() {
		t.Error("the filter bar is showing with no filter active")
	}

	for range 4 {
		m = feed(t, m, testutil.Key("v"))
	}
	if !m.bar.IsVisible() {
		t.Fatal("the filter bar is hidden with a verbosity filter active")
	}
	if !m.bar.IsTokenActive("≥ warn") {
		t.Error("the bar does not name the level in force")
	}
	if got := m.formatLabel(); got != "log · ≥ warn" {
		t.Errorf("formatLabel = %q, want \"log · ≥ warn\"", got)
	}

	for range 2 {
		m = feed(t, m, testutil.Key("v"))
	}
	if m.bar.IsVisible() {
		t.Error("the bar is still showing at `all`")
	}
}

// ── Search ───────────────────────────────────────────────────────────────────

func TestSearchNarrowsTheText(t *testing.T) {
	m := logModel(t, mixedLog)

	m = feed(t, m, testutil.Key("/"))
	if !m.InEditMode() {
		t.Fatal("/ did not focus the search field")
	}
	m = feed(t, m, testutil.Type("disk")...)
	m = feed(t, m, testutil.Key("enter"))

	rendered := m.textViewport.View()
	if !strings.Contains(rendered, "disk almost full") {
		t.Error("the matching line is missing")
	}
	if strings.Contains(rendered, "cache warm") {
		t.Error("a non-matching line survived the search")
	}
}

// An empty pane says which of the three reasons it is empty for: only one of
// them is the user's own doing.
func TestAnEmptyResultSaysWhy(t *testing.T) {
	m := logModel(t, mixedLog)
	m = feed(t, m, testutil.Key("/"))
	m = feed(t, m, testutil.Type("nothingmatchesthis")...)
	m = feed(t, m, testutil.Key("enter"))

	if !strings.Contains(m.textViewport.View(), "No line matches") {
		t.Errorf("an empty search result says nothing: %q", m.textViewport.View())
	}
}

// ── Colour ───────────────────────────────────────────────────────────────────

func TestCTogglesColoring(t *testing.T) {
	m := jsonModel(t)
	m = feed(t, m, testutil.Key("f")) // the text display

	coloured := m.textViewport.View()
	m = feed(t, m, testutil.Key("c"))
	if m.highlight {
		t.Fatal("c did not turn highlighting off")
	}
	plain := m.textViewport.View()

	if coloured == plain {
		t.Error("turning coloring off changed nothing")
	}
	if ansi.Strip(coloured) != ansi.Strip(plain) {
		t.Error("turning coloring off changed the text, not just its colour")
	}
}

// Rule 115: lipgloss inherits no background, so a line that stopped short of the
// pane would show the terminal's own from there to the right margin.
func TestEveryTextLineCarriesTheAppBackground(t *testing.T) {
	m := logModel(t, mixedLog)

	for _, line := range strings.Split(m.textViewport.View(), "\n") {
		if strings.TrimSpace(ansi.Strip(line)) == "" {
			continue
		}
		if !strings.Contains(line, "\x1b[") {
			t.Fatalf("line %q carries no styling at all, so it has no background", line)
		}
	}
}

// ── The header ───────────────────────────────────────────────────────────────

func TestTheHeaderNamesTheFormatAndTheFilter(t *testing.T) {
	m := logModel(t, mixedLog)

	info := headerValue(t, m, "Lines")
	if info != "6" {
		t.Errorf("Lines = %q, want 6", info)
	}

	for range 4 { // ≥ warn
		m = feed(t, m, testutil.Key("v"))
	}
	if got := headerValue(t, m, "Lines"); got != "4/6" {
		t.Errorf("Lines = %q under a filter, want 4/6 — a bare count would read as the whole document", got)
	}
}

func TestTheHeaderCountsNodesForATree(t *testing.T) {
	m := jsonModel(t)

	if got := headerValue(t, m, "Nodes"); got == "" {
		t.Error("a tree does not report its node count")
	}
	if got := headerValue(t, m, "Lines"); got != "" {
		t.Error("a tree reports a line count, which is the text display's answer")
	}
}

func headerValue(t *testing.T, m Model, key string) string {
	t.Helper()
	for _, info := range m.GetHeaderInfo("default") {
		if info.Key == key {
			return info.Value
		}
	}
	return ""
}
