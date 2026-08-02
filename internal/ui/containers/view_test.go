package containers

import (
	"errors"
	"strings"
	"testing"

	"github.com/anthnel/devdesk/internal/ui/shortcut"
	"github.com/anthnel/devdesk/internal/ui/testutil"
)

// ── Header and metadata ──────────────────────────────────────────────────────

func TestGetTitleNamesTheContainerInTheLogsView(t *testing.T) {
	m := loadedModel(t)

	if got := m.GetTitle(); !strings.Contains(got, "Containers") {
		t.Errorf("GetTitle() = %q, want it to name the view", got)
	}

	m = feed(t, m, testutil.Key("l"))
	if got := m.GetTitle(); !strings.Contains(got, "api") {
		t.Errorf("GetTitle() = %q in the logs view, want the container name", got)
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
	if !hasShortcut(main, "ctrl+d") || !hasShortcut(main, "l") {
		t.Error("the table state does not expose the lifecycle shortcuts")
	}

	logs := feed(t, m, testutil.Key("l")).GetShortcuts()
	if hasShortcut(logs, "ctrl+d") {
		t.Error("the logs state still offers delete, which does nothing there")
	}
	if !hasShortcut(logs, "esc") || !hasShortcut(logs, "f") {
		t.Error("the logs state does not offer back and follow")
	}

	modal := feed(t, m, testutil.Key("ctrl+d")).GetShortcuts()
	if len(modal) != 2 || !hasShortcut(modal, "esc") {
		t.Errorf("the confirmation state exposes %v, want just confirm and cancel", modal)
	}
}

// The logs shortcuts double as status indicators, so their descriptions have to
// track the toggles.
func TestLogsShortcutsReportToggleState(t *testing.T) {
	m := logsModel(t, "line")

	if got := shortcutDescription(m.GetShortcuts(), "w"); got != "Wrap: off" {
		t.Errorf("wrap shortcut = %q, want \"Wrap: off\"", got)
	}
	if got := shortcutDescription(m.GetShortcuts(), "t"); got != "Timestamps: off" {
		t.Errorf("timestamps shortcut = %q, want \"Timestamps: off\"", got)
	}

	m = feed(t, m, testutil.Key("w"), testutil.Key("t"))
	if got := shortcutDescription(m.GetShortcuts(), "w"); got != "Wrap: on" {
		t.Errorf("wrap shortcut = %q after toggling, want \"Wrap: on\"", got)
	}
	if got := shortcutDescription(m.GetShortcuts(), "t"); got != "Timestamps: on" {
		t.Errorf("timestamps shortcut = %q after toggling, want \"Timestamps: on\"", got)
	}
}

// Rule 137: descriptions are capitalised imperatives.
func TestShortcutDescriptionsAreCapitalised(t *testing.T) {
	m := loadedModel(t)
	all := append(m.GetShortcuts(), feed(t, m, testutil.Key("l")).GetShortcuts()...)
	all = append(all, feed(t, m, testutil.Key("ctrl+d")).GetShortcuts()...)

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
	if !feed(t, m, testutil.Key("ctrl+d")).InEditMode() {
		t.Error("InEditMode() is false while the confirmation is open")
	}
	if !feed(t, m, testutil.Key("l")).InEditMode() {
		t.Error("InEditMode() is false in the logs view, where q and g are bound")
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
	m := feed(t, loadedModel(t), testutil.Key("ctrl+d"))

	out := m.View()
	if !strings.Contains(out, "Delete Container") {
		t.Error("View() does not render the confirmation")
	}
	if strings.Contains(out, "nginx") {
		t.Error("the table is still visible under the confirmation")
	}
}

func TestViewRendersTheLogs(t *testing.T) {
	m := logsModel(t, "hello from the container")

	if !strings.Contains(m.View(), "hello from the container") {
		t.Error("the logs viewport does not render its content")
	}
}

func TestViewShowsTheSpinnerWhileLogsLoad(t *testing.T) {
	m := feed(t, loadedModel(t), testutil.Key("l")) // logsLoading is true

	if !strings.Contains(m.View(), "Loading logs") {
		t.Error("View() does not report an in-flight log fetch")
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
		{"logs", func(t *testing.T) Model { return logsModel(t, "line") }},
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
