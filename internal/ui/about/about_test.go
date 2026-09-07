package about

import (
	"strings"
	"testing"

	"github.com/anthnel/devdesk/internal/config"
	"github.com/anthnel/devdesk/internal/ui/testutil"
	"github.com/anthnel/devdesk/internal/version"
	tea "github.com/charmbracelet/bubbletea"
)

// sized builds a screen laid out for a terminal of the given viewport height.
func sized(t *testing.T, height int) Model {
	t.Helper()

	updated, _ := New(&config.Config{}).Update(tea.WindowSizeMsg{Width: 100, Height: height})
	return updated.(Model)
}

// The body opens on a blank line — Rule 131's top padding, applied to a screen
// that is not a form. Without it the first section title is welded to the
// viewport's border and reads as clipped.
//
// The assertion is on the rendered View rather than on lines()[0], because what
// the rule is about is the first row on screen: a padding that lines() carried
// and View dropped would pass a test written the other way. Nothing strips
// escape sequences here — under `go test` lipgloss finds no TTY and emits none.
func TestTheBodyOpensOnABlankLine(t *testing.T) {
	rows := strings.Split(sized(t, 40).View(), "\n")

	if strings.TrimSpace(rows[0]) != "" {
		t.Errorf("the first rendered row is %q, want a blank line between the border and the content", rows[0])
	}
	if !strings.Contains(rows[1], "Build") {
		t.Errorf("the second row is %q, want the first section title", rows[1])
	}
}

// The padding scrolls away rather than being repainted at every offset: it
// belongs to the body, not to the window onto it.
func TestTheTopPaddingScrollsAway(t *testing.T) {
	rows := strings.Split(press(t, sized(t, 5), "down").View(), "\n")

	if strings.TrimSpace(rows[0]) == "" {
		t.Error("the first row is still blank one line down, so the padding is pinned to the window rather than to the body")
	}
}

func TestTheBodyNamesTheBuild(t *testing.T) {
	m := sized(t, 40)
	body := strings.Join(m.lines(), "\n")

	for _, want := range []string{"Build", "Version", "Commit", "Built", "Go", "Platform"} {
		if !strings.Contains(body, want) {
			t.Errorf("the body does not mention %q", want)
		}
	}
	if info := version.Get(); !strings.Contains(body, info.Version) {
		t.Errorf("the body does not carry the running version %q", info.Version)
	}
}

func TestTheBodyNamesWhereFilesLive(t *testing.T) {
	m := sized(t, 40)
	body := strings.Join(m.lines(), "\n")

	for _, want := range []string{"Paths", "Config", "Themes", "Cache", "Log"} {
		if !strings.Contains(body, want) {
			t.Errorf("the body does not mention %q", want)
		}
	}
	if !strings.Contains(body, SourceURL) {
		t.Error("the body does not name the project's repository")
	}
}

// A missing value must read as missing. A blank in its place would read as a
// display glitch, which is what Rule 122 calls the color discipline: gray is
// reserved for what is not there.
func TestAMissingValueReadsAsUnknown(t *testing.T) {
	rendered := renderField(field{Label: "Commit", Value: ""}, 7)

	if !strings.Contains(rendered, version.Unknown) {
		t.Errorf("renderField() = %q, want it to say %q", rendered, version.Unknown)
	}
}

func TestADirtyBuildSaysSo(t *testing.T) {
	if got := commitField(version.Info{Commit: "a1b2c3d", Dirty: true}); got != "a1b2c3d (modified)" {
		t.Errorf("commitField() = %q, want the commit marked modified", got)
	}
	if got := commitField(version.Info{Commit: "a1b2c3d"}); got != "a1b2c3d" {
		t.Errorf("commitField() = %q, want the bare commit", got)
	}
	// Marking "unknown (modified)" would name a state of a tree we failed to
	// identify — two claims when only one is true.
	if got := commitField(version.Info{Commit: version.Unknown, Dirty: true}); got != version.Unknown {
		t.Errorf("commitField() = %q, want an unknown commit left alone", got)
	}
}

func TestBuildDate(t *testing.T) {
	tests := []struct {
		name, in, want string
	}{
		{"an RFC 3339 stamp becomes a readable UTC minute", "2026-09-06T10:25:23Z", "2026-09-06 10:25 UTC"},
		{"an offset is normalised to UTC", "2026-09-06T12:25:23+02:00", "2026-09-06 10:25 UTC"},
		{"anything unparseable is shown as it came", "last tuesday", "last tuesday"},
		{"an absent date stays unknown", version.Unknown, version.Unknown},
		{"an empty date is unknown", "", version.Unknown},
	}

	for _, tt := range tests {
		t.Run(tt.name, func(t *testing.T) {
			if got := buildDate(tt.in); got != tt.want {
				t.Errorf("buildDate(%q) = %q, want %q", tt.in, got, tt.want)
			}
		})
	}
}

// The body always fills the viewport height, otherwise the terminal's
// background shows below the last line (Rule 115).
func TestViewFillsTheViewport(t *testing.T) {
	for _, height := range []int{3, 12, 40} {
		m := sized(t, height)
		if got := len(strings.Split(m.View(), "\n")); got != height {
			t.Errorf("View() rendered %d lines for a viewport of %d", got, height)
		}
	}
}

func TestScrollingStopsAtBothEnds(t *testing.T) {
	m := sized(t, 5)

	m = press(t, m, "up")
	if m.offset != 0 {
		t.Errorf("offset = %d after scrolling up from the top, want 0", m.offset)
	}

	for range 50 {
		m = press(t, m, "down")
	}
	if want := len(m.lines()) - m.height; m.offset != want {
		t.Errorf("offset = %d after scrolling past the end, want %d", m.offset, want)
	}

	m = press(t, m, "home")
	if m.offset != 0 {
		t.Errorf("offset = %d after home, want 0", m.offset)
	}
}

// A body shorter than the viewport has nothing to scroll, and especially not
// toward a negative offset — the View window would then start before the
// first line.
func TestATallTerminalHasNothingToScroll(t *testing.T) {
	m := sized(t, 100)

	m = press(t, m, "end")
	if m.offset != 0 {
		t.Errorf("offset = %d, want 0 — everything already fits", m.offset)
	}
}

func TestTheFooterSaysWhatIsStillBelow(t *testing.T) {
	short := sized(t, 5)
	if got := short.status().Text; !strings.Contains(got, "below") {
		t.Errorf("status() = %q, want it to say how much is below", got)
	}

	tall := sized(t, 100)
	if got := tall.status().Text; got != "" {
		t.Errorf("status() = %q, want nothing when the body fits", got)
	}
}

// Rule 124: what GetFooterHeight budgets is what RenderFooter emits.
func TestFooterHeightMatchesWhatRenderFooterEmits(t *testing.T) {
	m := sized(t, 5)

	if got := len(strings.Split(m.RenderFooter(100), "\n")); got != m.GetFooterHeight() {
		t.Errorf("RenderFooter emitted %d lines, GetFooterHeight budgets %d", got, m.GetFooterHeight())
	}
}

// Rule 130: the set of keys does not change from one state to another of the
// same screen. Here there is only one state, and that is what this test
// pins down — a future branch offering an extra key once scrolled would
// break it.
func TestTheShortcutsDoNotDependOnTheScrollPosition(t *testing.T) {
	m := sized(t, 5)
	before := testutil.ShortcutKeys(m.GetShortcuts())

	m = press(t, m, "end")
	if after := testutil.ShortcutKeys(m.GetShortcuts()); !equal(before, after) {
		t.Errorf("the shortcut keys changed from %v to %v when the screen scrolled", before, after)
	}
}

// There is nothing to reload on this screen, so `ctrl+r` is not a key of
// this view — neither offered, nor grayed out (Rule 130).
func TestRefreshIsNotOffered(t *testing.T) {
	if testutil.HasShortcut(sized(t, 20).GetShortcuts(), "ctrl+r") {
		t.Error("ctrl+r is advertised on a screen that fetches nothing")
	}
}

// Rule 137: descriptions are capitalised imperatives.
func TestShortcutDescriptionsAreCapitalised(t *testing.T) {
	for _, s := range sized(t, 20).GetShortcuts() {
		if s.Description == "" {
			t.Errorf("shortcut %q has no description", s.Key)
			continue
		}
		if first := s.Description[0]; first < 'A' || first > 'Z' {
			t.Errorf("shortcut %q description %q does not start with a capital", s.Key, s.Description)
		}
	}
}

// Rule 134: the shortcuts belong to the header, never to the viewport.
func TestTheBodyCarriesNoInlineHelp(t *testing.T) {
	body := strings.Join(sized(t, 40).lines(), "\n")

	for _, forbidden := range []string{"[esc]", "[enter]", "Press "} {
		if strings.Contains(body, forbidden) {
			t.Errorf("the body carries inline help (%q); it belongs to GetShortcuts", forbidden)
		}
	}
}

// Rule 114: every shortcut the header advertises is documented in the help.
func TestEveryShortcutIsDocumented(t *testing.T) {
	m := sized(t, 20)

	documented := map[string]bool{}
	for _, kb := range m.GetHelpContent().KeyBindings {
		documented[kb.Key] = true
	}
	for _, s := range m.GetShortcuts() {
		if !documented[s.Key] {
			t.Errorf("shortcut %q is advertised in the header but absent from the help", s.Key)
		}
	}
}

func TestTheHeaderNamesTheBuild(t *testing.T) {
	info := sized(t, 20).GetHeaderInfo("work")

	if len(info) != 1 || info[0].Key != "Version" {
		t.Fatalf("GetHeaderInfo() = %+v, want a single Version field", info)
	}
	if info[0].Value != version.Get().Short() {
		t.Errorf("GetHeaderInfo() reported %q, want %q", info[0].Value, version.Get().Short())
	}
}

// Nothing on this screen takes text, so `:` and the other global keys always
// reach the router.
func TestTheScreenNeverHoldsTheKeyboard(t *testing.T) {
	if sized(t, 20).InEditMode() {
		t.Error("InEditMode() is true on a screen with no field")
	}
}

func press(t *testing.T, m Model, key string) Model {
	t.Helper()

	msg, ok := keyByName(key)
	if !ok {
		t.Fatalf("press: unknown key %q", key)
	}
	updated, _ := m.Update(msg)
	return updated.(Model)
}

// keyByName maps the names this screen handles to the KeyMsg bubbletea would
// deliver for them.
func keyByName(name string) (tea.KeyMsg, bool) {
	types := map[string]tea.KeyType{
		"up":     tea.KeyUp,
		"down":   tea.KeyDown,
		"pgup":   tea.KeyPgUp,
		"pgdown": tea.KeyPgDown,
		"home":   tea.KeyHome,
		"end":    tea.KeyEnd,
	}
	kt, ok := types[name]
	return tea.KeyMsg{Type: kt}, ok
}

func equal(a, b []string) bool {
	if len(a) != len(b) {
		return false
	}
	for i := range a {
		if a[i] != b[i] {
			return false
		}
	}
	return true
}
