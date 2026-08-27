package viewer

import (
	"strings"
	"testing"

	tea "github.com/charmbracelet/bubbletea"
	"github.com/charmbracelet/x/ansi"

	"github.com/anthnel/devdesk/internal/ui/keymap"
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

	// `home` and `end`, not `g` and `G`. No bare letter is navigation (§3.26),
	// and since §3.53 `g` opens the go-to-line prompt — which this test used to
	// press before scrolling, and would now be typing into.
	m = feed(t, m, testutil.Key("home"))
	if m.textViewport.YOffset != 0 {
		t.Errorf("YOffset = %d after home, want the top", m.textViewport.YOffset)
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

	m = feed(t, m, testutil.Key("end"))
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
	for _, key := range []string{"t", keymap.Fetch, keymap.Pager} {
		if !hasShortcut(capable, key) {
			t.Errorf("%q is not offered for a source that supports it", key)
		}
	}

	plain := open(t, fakeSource{name: "notes.md", content: "hello"})
	for _, key := range []string{"t", keymap.Fetch, keymap.Pager} {
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

// ── YAML and TOML ────────────────────────────────────────────────────────────

// The whole chain in one assertion: the extension decides the kind, the kind
// picks the lexer, the class picks the style. Each link is unit-tested elsewhere;
// this is what fails if one of them is not wired to the next.
func TestYAMLAndTOMLReachTheScreenColored(t *testing.T) {
	cases := []struct {
		file    string
		content string
		format  string
		key     string
		value   string
	}{
		{"compose.yaml", "services:\n  api:\n    image: nginx\n", "yaml", "services", "nginx"},
		{"compose.yml", "services:\n  api:\n    image: nginx\n", "yaml", "services", "nginx"},
		{"Cargo.toml", "[package]\nname = \"devdesk\"\n", "toml", "package", `"devdesk"`},
	}

	for _, tc := range cases {
		t.Run(tc.file, func(t *testing.T) {
			m := open(t, fakeSource{name: tc.file, content: tc.content})

			if got := m.formatLabel(); got != tc.format {
				t.Errorf("formatLabel = %q, want %q", got, tc.format)
			}
			rendered := m.View()
			if !strings.Contains(rendered, syntaxStyle(viewerpkg.ClassKey).Render(tc.key)) {
				t.Errorf("%q is not colored as a key", tc.key)
			}
			if !strings.Contains(rendered, syntaxStyle(viewerpkg.ClassString).Render(tc.value)) {
				t.Errorf("%q is not colored as a value", tc.value)
			}
		})
	}
}

// Neither has a tree, so `f` is not offered and does nothing (Rule 130): a
// shortcut advertised for a display that renders an empty pane is worse than no
// shortcut.
func TestNeitherYAMLNorTOMLOffersTheTree(t *testing.T) {
	for _, file := range []string{"compose.yaml", "Cargo.toml"} {
		m := open(t, fakeSource{name: file, content: "a: 1\n"})

		if m.display != displayText {
			t.Errorf("%s did not open on the text display", file)
		}
		for _, s := range m.GetShortcuts() {
			if s.Key == "f" {
				t.Errorf("%s offers f, which has no tree to switch to", file)
			}
		}

		before := m.View()
		if after := feed(t, m, testutil.Key("f")).View(); after != before {
			t.Errorf("f changed the %s display", file)
		}
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

	// Stripped, because the occurrence is a styled run of its own now: the
	// rendered line carries an escape sequence between "disk" and what follows it.
	rendered := ansi.Strip(m.textViewport.View())
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

func TestSearchHighlightsItsOccurrences(t *testing.T) {
	m := searchFor(t, logModel(t, mixedLog), "disk")

	rendered := m.textViewport.View()
	if !strings.Contains(rendered, matchStyle().Render("disk")) {
		t.Error("the occurrence is not highlighted; the line is kept but nothing says where it matched")
	}
	if !strings.Contains(ansi.Strip(rendered), "disk almost full") {
		t.Error("highlighting changed the text, not just its colour")
	}
}

// The test that earns its place. splitTokenLines and wrapTokens each rebuild a
// token literally, so a Match left behind there gives a highlight that works
// until the line is long enough to be cut — which is the line it exists for.
func TestSearchHighlightSurvivesWrap(t *testing.T) {
	beyondTheFold := strings.Repeat("x", 100) + "needle"
	m := logModel(t, beyondTheFold)

	m = feed(t, m, testutil.Key("w"))
	if !m.wrap {
		t.Fatal("w did not turn wrapping on")
	}
	m = searchFor(t, m, "needle")

	if !strings.Contains(m.textViewport.View(), matchStyle().Render("needle")) {
		t.Error("the occurrence lost its highlight past the wrap: Match did not travel with the run")
	}
}

// An occurrence is not syntax coloring, so `c` has no say over it: turning the
// colors off is how someone reads a document as plain text, and a search they
// then could not see would be the one thing they lost by it.
func TestSearchHighlightIgnoresTheColoringToggle(t *testing.T) {
	m := jsonModel(t)
	m = feed(t, m, testutil.Key("f")) // the text display
	m = feed(t, m, testutil.Key("c"))
	if m.highlight {
		t.Fatal("c did not turn highlighting off")
	}

	m = searchFor(t, m, "ports")
	if !strings.Contains(m.textViewport.View(), matchStyle().Render("ports")) {
		t.Error("the occurrence is not highlighted with coloring off")
	}
}

// A log line is styled by its level in one piece, except where a search cut it:
// the level still carries the rest of the line, so an ERROR is picked out at a
// glance and the occurrence is picked out inside it.
func TestSearchHighlightsInsideALogLine(t *testing.T) {
	m := searchFor(t, logModel(t, mixedLog), "disk")

	rendered := m.textViewport.View()
	if !strings.Contains(rendered, matchStyle().Render("disk")) {
		t.Error("the occurrence lost its highlight to the level style")
	}
	if !strings.Contains(rendered, levelStyle(viewerpkg.LevelWarn).Render("2026-01-01 [WARN] ")) {
		t.Error("the rest of the line lost its level; a search must not cost the level color")
	}
}

// The invariant the filter rests on: one rule decides both that a line matches
// and where, so a line the search kept always carries at least one occurrence.
// Two calculations for one question is what produced a finding counted in the
// header and present in no tab.
func TestEveryLineTheSearchKeptCarriesAnOccurrence(t *testing.T) {
	m := searchFor(t, logModel(t, mixedLog), "2026")

	var checked int
	for _, line := range strings.Split(m.textViewport.View(), "\n") {
		if strings.TrimSpace(ansi.Strip(line)) == "" {
			continue
		}
		checked++
		if !strings.Contains(line, matchStyle().Render("2026")) {
			t.Errorf("line %q was kept by the search with nothing highlighted in it", ansi.Strip(line))
		}
	}
	if checked == 0 {
		t.Fatal("the search kept no line at all, so the invariant was never tested")
	}
}

// searchFor drives `/`, the query and enter — the three steps every search test
// starts with.
func searchFor(t *testing.T, m Model, query string) Model {
	t.Helper()

	m = feed(t, m, testutil.Key("/"))
	if !m.InEditMode() {
		t.Fatal("/ did not focus the search field")
	}
	m = feed(t, m, testutil.Type(query)...)
	return feed(t, m, testutil.Key("enter"))
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
