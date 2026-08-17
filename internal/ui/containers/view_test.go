package containers

import (
	"errors"
	"strings"
	"testing"

	"github.com/anthnel/devdesk/internal/ui/keymap"
	"github.com/anthnel/devdesk/internal/ui/shortcut"
	"github.com/anthnel/devdesk/internal/ui/testutil"
)

// ── Header and metadata ──────────────────────────────────────────────────────

// The title has one value again: naming the container was the logs pane's job,
// and the viewer's own title does it now.
func TestGetTitleNamesTheView(t *testing.T) {
	m := loadedModel(t)

	if got := m.GetTitle(); !strings.Contains(got, "Containers") {
		t.Errorf("GetTitle() = %q, want it to name the view", got)
	}

	m = feed(t, m, testutil.Key(keymap.Logs))
	if got := m.GetTitle(); !strings.Contains(got, "Containers") {
		t.Errorf("GetTitle() = %q after opening the logs, want it unchanged", got)
	}
}

func TestGetIconIsEmptyBecauseTheTitleCarriesIt(t *testing.T) {
	if got := loadedModel(t).GetIcon(); got != "" {
		t.Errorf("GetIcon() = %q, want empty", got)
	}
}

// The header count reflects the filter, not the raw list — otherwise it
// contradicts what the table shows.
func TestGetHeaderInfoCountsTheFilteredList(t *testing.T) {
	m := loadedModel(t)

	info := m.GetHeaderInfo("")
	if len(info) != 1 {
		t.Fatalf("GetHeaderInfo() returned %d entries, want 1", len(info))
	}
	if info[0].Value != "4 (Active)" {
		t.Errorf("header value = %q, want \"4 (Active)\"", info[0].Value)
	}

	m = feed(t, m, testutil.Key("/"))
	m = feed(t, m, testutil.Type("redis")...)
	if got := m.GetHeaderInfo("")[0].Value; got != "1 (Active)" {
		t.Errorf("header value = %q under a filter, want \"1 (Active)\"", got)
	}

	m = feed(t, loadedModel(t), testutil.Key("a"))
	if got := m.GetHeaderInfo("")[0].Value; !strings.Contains(got, "(All)") {
		t.Errorf("header value = %q after toggling scope, want the All label", got)
	}
}

// Rule 130: shortcuts follow the state.
func TestGetShortcutsSwitchWithTheState(t *testing.T) {
	m := loadedModel(t)

	main := m.GetShortcuts()
	if !hasShortcut(main, keymap.Delete) || !hasShortcut(main, keymap.Logs) {
		t.Error("the table state does not expose the lifecycle shortcuts")
	}

	// There is no logs state any more: L hands the document to the viewer and
	// this view keeps the shortcuts it had.
	afterLogs := feed(t, m, testutil.Key(keymap.Logs)).GetShortcuts()
	if !hasShortcut(afterLogs, keymap.Delete) {
		t.Error("opening the logs changed this view's shortcuts; it has one state now")
	}

	modal := feed(t, m, testutil.Key(keymap.Delete)).GetShortcuts()
	if len(modal) != 2 || !hasShortcut(modal, "esc") {
		t.Errorf("the confirmation state exposes %v, want just confirm and cancel", modal)
	}
}

// The wrap and timestamps shortcuts left with the logs pane: they belong to the
// viewer now, where they are offered only for a source that supports them.
func TestTheLogsPaneShortcutsAreGone(t *testing.T) {
	m := feed(t, loadedModel(t), testutil.Key(keymap.Logs))

	for _, key := range []string{"w", "t", "e"} {
		if shortcutDescription(m.GetShortcuts(), key) != "" {
			t.Errorf("%q is still advertised here; it belongs to the viewer", key)
		}
	}
	if shortcutDescription(m.GetShortcuts(), keymap.Logs) != "Logs" {
		t.Error("L stopped advertising itself as the way to the logs")
	}
}

// Rule 137: descriptions are capitalised imperatives.
func TestShortcutDescriptionsAreCapitalised(t *testing.T) {
	m := loadedModel(t)
	all := append(m.GetShortcuts(), feed(t, m, testutil.Key(keymap.Logs)).GetShortcuts()...)
	all = append(all, feed(t, m, testutil.Key(keymap.Delete)).GetShortcuts()...)

	for _, s := range all {
		if s.Description == "" {
			t.Errorf("shortcut %q has no description", s.Key)
			continue
		}
		if first := s.Description[0]; first < 'A' || first > 'Z' {
			t.Errorf("shortcut %q description %q does not start with a capital", s.Key, s.Description)
		}
	}
}

func hasShortcut(shortcuts shortcut.Shortcuts, key string) bool {
	return shortcutDescription(shortcuts, key) != ""
}

func shortcutDescription(shortcuts shortcut.Shortcuts, key string) string {
	for _, s := range shortcuts {
		if s.Key == key {
			return s.Description
		}
	}
	return ""
}

// ── Edit mode ────────────────────────────────────────────────────────────────

// InEditMode tells the app router to stop capturing ":" for command mode. It
// has to be true wherever a keystroke means something local.
func TestInEditModeCoversEveryCapturingState(t *testing.T) {
	m := loadedModel(t)
	if m.InEditMode() {
		t.Error("InEditMode() is true in the plain table state")
	}

	if !feed(t, m, testutil.Key("/")).InEditMode() {
		t.Error("InEditMode() is false while the filter input has focus")
	}
	if !feed(t, m, testutil.Key(keymap.Delete)).InEditMode() {
		t.Error("InEditMode() is false while the confirmation is open")
	}
	// The logs pane used to be a third capturing state. It is the viewer's now,
	// and the viewer answers for its own keys.
	if feed(t, m, testutil.Key(keymap.Logs)).InEditMode() {
		t.Error("InEditMode() is true after opening the logs; this view captures nothing then")
	}
}

// ── View states ──────────────────────────────────────────────────────────────

func TestViewShowsTheSpinnerOnlyBeforeTheFirstList(t *testing.T) {
	m := newTestModel(t) // loading, no containers yet

	if !strings.Contains(m.View(), "Loading containers") {
		t.Error("View() does not show the spinner before the first list arrives")
	}

	loaded := loadedModel(t)
	if strings.Contains(loaded.View(), "Loading containers") {
		t.Error("View() still shows the spinner after the list arrived")
	}
}

func TestViewShowsTheEmptyState(t *testing.T) {
	m := feed(t, newTestModel(t), ContainersListMsg{Containers: nil})

	if !strings.Contains(m.View(), "No containers found") {
		t.Error("View() does not report an empty list")
	}
}

// With a filter active the table must stay rendered even when it matches
// nothing, so the filter bar keeps its place at the bottom of the viewport.
func TestViewKeepsTheTableWhenAFilterMatchesNothing(t *testing.T) {
	m := feed(t, loadedModel(t), testutil.Key("/"))
	m = feed(t, m, testutil.Type("nomatch")...)

	if strings.Contains(m.View(), "No containers found") {
		t.Error("View() replaced the table with the empty state while a filter was active")
	}
}

func TestViewRendersTheConfirmationOverTheTable(t *testing.T) {
	m := feed(t, loadedModel(t), testutil.Key(keymap.Delete))

	out := m.View()
	if !strings.Contains(out, "Delete Container") {
		t.Error("View() does not render the confirmation")
	}
	if strings.Contains(out, "nginx") {
		t.Error("the table is still visible under the confirmation")
	}
}

// The view has one screen again: `l` hands the document to the router and this
// one goes on showing its table underneath.
func TestViewStaysOnTheTableWhenTheLogsAreOpened(t *testing.T) {
	m := feed(t, loadedModel(t), testutil.Key(keymap.Logs))

	if !strings.Contains(m.View(), "api") {
		t.Error("the table stopped rendering when the logs were opened elsewhere")
	}
}

// ── Footer (Rule 124) ────────────────────────────────────────────────────────

func TestFooterHeightMatchesWhatRenderFooterEmits(t *testing.T) {
	tests := []struct {
		name  string
		build func(t *testing.T) Model
	}{
		{"loaded", loadedModel},
		{"empty", newTestModel},
		{"searching", func(t *testing.T) Model { return feed(t, loadedModel(t), testutil.Key("/")) }},
		{"error", func(t *testing.T) Model {
			return feed(t, loadedModel(t), ContainersListMsg{Err: errors.New("boom")})
		}},
	}

	for _, tc := range tests {
		t.Run(tc.name, func(t *testing.T) {
			m := tc.build(t)

			want := m.GetFooterHeight()
			got := strings.Count(m.RenderFooter(160), "\n") + 1
			if got != want {
				t.Errorf("RenderFooter() emitted %d lines, GetFooterHeight() promised %d", got, want)
			}
		})
	}
}

func TestFooterCarriesTheErrorMessage(t *testing.T) {
	m := feed(t, loadedModel(t), ContainersListMsg{Err: errors.New("boom")})

	if !strings.Contains(m.RenderFooter(160), "Failed to load containers") {
		t.Error("the footer does not surface the error message")
	}
}

func TestFilterBarVisibilityFollowsTheSearch(t *testing.T) {
	m := loadedModel(t)
	if m.FilterBarVisible() {
		t.Error("the filter bar is visible before any search")
	}

	if !feed(t, m, testutil.Key("/")).FilterBarVisible() {
		t.Error("the filter bar is hidden while searching")
	}
}

// ── Help (Rule 114) ──────────────────────────────────────────────────────────

func TestGetHelpContentIsPopulated(t *testing.T) {
	content := loadedModel(t).GetHelpContent()

	if content.Title == "" || content.Description == "" {
		t.Error("the help content has no title or description")
	}
	if len(content.KeyBindings) == 0 || len(content.Sections) == 0 {
		t.Error("the help content has no key bindings or sections")
	}
}

// Every key the header advertises should be explained in the help.
func TestHelpDocumentsTheAdvertisedShortcuts(t *testing.T) {
	m := loadedModel(t)
	documented := map[string]bool{}
	for _, kb := range m.GetHelpContent().KeyBindings {
		documented[kb.Key] = true
		for _, key := range strings.Split(kb.Key, "/") {
			if trimmed := strings.TrimSpace(key); trimmed != "" {
				documented[trimmed] = true
			}
		}
	}

	for _, s := range m.GetShortcuts() {
		key := s.Key
		if key == "space" {
			key = "Space" // the help capitalises it
		}
		if key == "?" {
			continue
		}
		if !documented[key] {
			t.Errorf("shortcut %q is advertised in the header but absent from the help", s.Key)
		}
	}
}

// A modal takes the keyboard from priority 1 of the key chain, so a modal that
// renders nothing does not merely look wrong — it kills the view. K's choice
// modal shipped exactly like that: wired into Update and into the key chain,
// left out of View, InEditMode and GetShortcuts. One keypress and every key
// after it went to a modal nobody could see.
//
// Driven by the keys rather than by setting the fields, so a modal reachable by
// no key would be caught too.
func TestEveryModalIsVisibleAndDeclared(t *testing.T) {
	for _, tc := range []struct {
		name  string
		key   string
		title string
	}{
		{"stop or restart", keymap.Kill, "Container"},
		{"delete", keymap.Delete, "Delete Container"},
		{"prune", keymap.Prune, "Prune Containers"},
	} {
		t.Run(tc.name, func(t *testing.T) {
			m := feed(t, loadedModel(t), testutil.Key(tc.key))

			if !strings.Contains(m.View(), tc.title) {
				t.Errorf("the modal is not rendered; View() = %q", m.View())
			}
			if !m.InEditMode() {
				t.Error("InEditMode() is false while a modal holds the keyboard, " +
					"so the router still claims q, ? and :")
			}
			if got := m.GetShortcuts(); len(got) > 3 {
				t.Errorf("GetShortcuts() advertises %d keys, want the modal's own", len(got))
			}
			if !hasShortcut(m.GetShortcuts(), "esc") {
				t.Error("the modal does not advertise the way out")
			}
		})
	}
}

// The corollary: a key that opens nothing must leave the view usable. This is
// what makes the test above meaningful rather than tautological.
func TestAKeyThatOpensNoModalLeavesTheViewAlive(t *testing.T) {
	m := feed(t, loadedModel(t), testutil.Key("a"))

	if m.InEditMode() {
		t.Error("toggling the scope captured the keyboard")
	}
	if len(m.GetShortcuts()) < 5 {
		t.Error("the view lost its shortcuts without a modal to explain it")
	}
}
