package workspaces

import (
	"errors"
	"strings"
	"testing"
	"time"

	tea "github.com/charmbracelet/bubbletea"

	"github.com/anthnel/devdesk/internal/ui/shortcut"
	"github.com/anthnel/devdesk/internal/ui/testutil"
)

// ── View states ──────────────────────────────────────────────────────────────

func TestViewRendersTheTable(t *testing.T) {
	out := loadedModel(t).View()

	for _, want := range []string{"devdesk", "clean-repo", "anthnel/devdesk"} {
		if !strings.Contains(out, want) {
			t.Errorf("the table is missing %q", want)
		}
	}
}

// The two empty states say different things: nothing configured at the root,
// versus a directory that happens to be empty.
func TestViewDistinguishesTheTwoEmptyStates(t *testing.T) {
	root := feed(t, newTestModel(t), EntriesLoadedMsg{Entries: nil})
	if !strings.Contains(root.View(), "No workspaces found") {
		t.Errorf("the root empty state does not say so:\n%s", root.View())
	}

	nested := loadedModel(t)
	nested.table.SetCursor(3)
	nested = feed(t, nested, testutil.Key("right"))
	nested = feed(t, nested, EntriesLoadedMsg{Entries: nil})
	if !strings.Contains(nested.View(), "Empty directory") {
		t.Errorf("the nested empty state does not say so:\n%s", nested.View())
	}
}

func TestViewRendersTheError(t *testing.T) {
	m := feed(t, newTestModel(t), LoadErrorMsg{Error: errors.New("permission denied")})

	if !strings.Contains(m.View(), "permission denied") {
		t.Error("View() does not surface the load error")
	}
}

func TestViewRendersOverlays(t *testing.T) {
	tests := []struct {
		name string
		key  string
		want string
	}{
		{"create", "ctrl+n", "Create"},
		{"rename", "r", "Rename"},
		{"delete", "ctrl+d", "Delete"},
	}

	for _, tc := range tests {
		t.Run(tc.name, func(t *testing.T) {
			m := feed(t, loadedModel(t), testutil.Key(tc.key))

			if out := m.View(); !strings.Contains(out, tc.want) {
				t.Errorf("the %s overlay is not rendered:\n%s", tc.name, out)
			}
		})
	}
}

// ── Cell formatting ──────────────────────────────────────────────────────────

func TestGitStatusCounters(t *testing.T) {
	m := loadedModel(t)
	rows := m.table.Rows()

	// devdesk: 2 modified, 1 untracked, 3 unpushed.
	dirty := rows[0][2]
	for _, want := range []string{"2", "1", "3"} {
		if !strings.Contains(dirty, want) {
			t.Errorf("the git status cell %q is missing the count %q", dirty, want)
		}
	}

	// clean-repo has nothing outstanding, so its cell must not invent counters.
	if clean := rows[1][2]; strings.ContainsAny(clean, "123456789") {
		t.Errorf("a clean repo rendered counters: %q", clean)
	}
}

func TestNonRepoEntriesHaveNoGitStatus(t *testing.T) {
	m := loadedModel(t)

	// clients (a plain directory) and notes.md (a file).
	for _, idx := range []int{2, 4} {
		if got := strings.TrimSpace(m.table.Rows()[idx][2]); got != "" {
			t.Errorf("row %d rendered a git status %q for a non-repo", idx, got)
		}
	}
}

func TestProjectTypeIsShown(t *testing.T) {
	m := loadedModel(t)

	if got := strings.TrimSpace(m.table.Rows()[0][3]); got == "" {
		t.Error("the project type cell is empty for a Go repo")
	}
	if got := strings.TrimSpace(m.table.Rows()[4][3]); got != "" {
		t.Errorf("a file rendered a project type %q", got)
	}
}

func TestScanColumnsShowCachedSeverities(t *testing.T) {
	m := scannedModel(t)
	row := m.table.Rows()[0]

	// Critical, High, Medium, Low.
	for i, want := range map[int]string{5: "1", 6: "2", 7: "3", 8: "4"} {
		if !strings.Contains(row[i], want) {
			t.Errorf("column %d = %q, want the cached count %q", i, row[i], want)
		}
	}
	if strings.TrimSpace(row[9]) == "" {
		t.Error("the Scanned column is empty for a repo with cached results")
	}
}

func TestScanColumnsAreBlankBeforeAnyScan(t *testing.T) {
	m := loadedModel(t)
	row := m.table.Rows()[1] // clean-repo, never scanned

	for i := 5; i <= 8; i++ {
		if got := strings.TrimSpace(row[i]); strings.ContainsAny(got, "0123456789") {
			t.Errorf("column %d = %q for an unscanned repo, want no counts", i, got)
		}
	}
}

// While a scan runs the Scanned column animates, so the user can tell it is
// working rather than stalled.
func TestScanningRepoShowsProgress(t *testing.T) {
	m := loadedModel(t)
	before := m.table.Rows()[0][9]

	m = feed(t, m, WorkspaceScanStartingMsg{RepoPath: "/tmp/workspaces/devdesk"})

	if got := m.table.Rows()[0][9]; got == before {
		t.Errorf("the Scanned cell is unchanged (%q) while a scan is running", got)
	}
}

// Rule 122: no escape sequences in cell values.
func TestTableCellsCarryNoANSISequences(t *testing.T) {
	m := scannedModel(t)

	for _, row := range m.table.Rows() {
		for i, cell := range row {
			if strings.Contains(cell, "\x1b") {
				t.Errorf("cell %d = %q contains an escape sequence", i, cell)
			}
		}
	}
}

func TestModTimeIsRelative(t *testing.T) {
	m := feed(t, newTestModel(t), EntriesLoadedMsg{Entries: []Entry{
		{Name: "recent", Path: "/tmp/workspaces/recent", IsDir: true, ModTime: time.Now().Add(-2 * time.Hour)},
	}})

	if got := m.table.Rows()[0][10]; !strings.Contains(got, "hr") {
		t.Errorf("the modified cell = %q, want a relative label (Rule 127)", got)
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
		{"create overlay", func(t *testing.T) Model { return feed(t, loadedModel(t), testutil.Key("ctrl+n")) }},
		{"error", func(t *testing.T) Model {
			return feed(t, newTestModel(t), LoadErrorMsg{Error: errors.New("boom")})
		}},
		{"selection", func(t *testing.T) Model {
			m := feed(t, NewForSelection(testConfig(), "Pick a repository"), tea.WindowSizeMsg{Width: 160, Height: 30})
			return feed(t, m, EntriesLoadedMsg{Entries: entryFixtures()})
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

// The breadcrumb has to track the drill-down, since it is the only thing
// telling the user where they are.
func TestFooterBreadcrumbFollowsTheNavigation(t *testing.T) {
	m := loadedModel(t)
	m.table.SetCursor(2) // clients

	m = feed(t, m, testutil.Key("right"))

	if !strings.Contains(m.RenderFooter(160), "clients") {
		t.Errorf("the breadcrumb does not name the current directory:\n%s", m.RenderFooter(160))
	}
}

func TestFooterCarriesTheSelectionMessage(t *testing.T) {
	m := feed(t, NewForSelection(testConfig(), "Pick a repository to scan"), tea.WindowSizeMsg{Width: 160, Height: 30})
	m = feed(t, m, EntriesLoadedMsg{Entries: entryFixtures()})

	if !strings.Contains(m.RenderFooter(160), "Pick a repository to scan") {
		t.Error("the selection prompt is not shown in the footer")
	}
}

func TestFooterCarriesScanMessages(t *testing.T) {
	m := loadedModel(t)
	m.footerError = "Scan failed — check logs"

	if !strings.Contains(m.RenderFooter(160), "Scan failed") {
		t.Error("the footer does not surface the scan error")
	}
}

func TestFilterBarVisibilityFollowsTheState(t *testing.T) {
	m := loadedModel(t)
	if m.FilterBarVisible() {
		t.Error("the filter bar is visible before any search")
	}

	searching := feed(t, m, testutil.Key("/"))
	if !searching.FilterBarVisible() {
		t.Error("the filter bar is hidden while searching")
	}

	// An overlay covers the table, so its filter bar has nothing to filter.
	overlay := feed(t, m, testutil.Key("ctrl+n"))
	overlay.filterBar = searching.filterBar
	if overlay.FilterBarVisible() {
		t.Error("the filter bar stayed visible under the create overlay")
	}
}

// ── Shortcuts (Rule 130) ─────────────────────────────────────────────────────

// This view has the most state-dependent shortcut list in the app: the backlog
// documents which key appears when, and getting it wrong advertises actions
// that silently do nothing.
func TestShortcutsFollowTheSelectedEntry(t *testing.T) {
	tests := []struct {
		name    string
		cursor  int
		scanned bool
		present []string
		absent  []string
	}{
		{
			name: "scanned git repo", cursor: 0, scanned: true,
			present: []string{"enter", "ctrl+w", "ctrl+s"},
			absent:  []string{"ctrl+n"}, // creating inside a repo is not offered
		},
		{
			name: "unscanned git repo", cursor: 1,
			present: []string{"ctrl+w", "ctrl+s"},
			absent:  []string{"enter", "ctrl+n"}, // no cached result to open
		},
		{
			name: "directory with nested repos", cursor: 2,
			present: []string{"ctrl+s", "ctrl+n"},
			absent:  []string{"enter", "ctrl+w"},
		},
		{
			name: "plain directory", cursor: 3,
			present: []string{"ctrl+n"},
			absent:  []string{"enter", "ctrl+w", "ctrl+s"},
		},
	}

	for _, tc := range tests {
		t.Run(tc.name, func(t *testing.T) {
			m := loadedModel(t)
			if tc.scanned {
				m = scannedModel(t)
			}
			m.table.SetCursor(tc.cursor)

			got := m.GetShortcuts()
			for _, key := range tc.present {
				if !hasShortcut(got, key) {
					t.Errorf("%q is not advertised for a %s", key, tc.name)
				}
			}
			for _, key := range tc.absent {
				if hasShortcut(got, key) {
					t.Errorf("%q is advertised for a %s, where it does nothing", key, tc.name)
				}
			}
		})
	}
}

func TestShortcutsFollowTheMode(t *testing.T) {
	tests := []struct {
		name string
		open func(t *testing.T) Model
		want string
	}{
		{"create", func(t *testing.T) Model { return feed(t, loadedModel(t), testutil.Key("ctrl+n")) }, "Create"},
		{"rename", func(t *testing.T) Model { return feed(t, loadedModel(t), testutil.Key("r")) }, "Rename"},
		{"delete", func(t *testing.T) Model { return feed(t, loadedModel(t), testutil.Key("ctrl+d")) }, "Confirm"},
	}

	for _, tc := range tests {
		t.Run(tc.name, func(t *testing.T) {
			got := tc.open(t).GetShortcuts()

			if len(got) != 2 {
				t.Errorf("the %s overlay advertises %d shortcuts, want just confirm and cancel: %v", tc.name, len(got), got)
			}
			if shortcutDescription(got, "enter") != tc.want && shortcutDescription(got, "y/n") != tc.want {
				t.Errorf("the %s overlay does not advertise %q", tc.name, tc.want)
			}
		})
	}

	selection := feed(t, NewForSelection(testConfig(), "pick"), tea.WindowSizeMsg{Width: 160, Height: 30}).GetShortcuts()
	if hasShortcut(selection, "ctrl+d") || hasShortcut(selection, "ctrl+s") {
		t.Error("selection mode advertises destructive actions it does not perform")
	}
	if !hasShortcut(selection, "enter") {
		t.Error("selection mode does not advertise how to confirm")
	}
}

// Rule 137: descriptions are capitalised imperatives.
func TestShortcutDescriptionsAreCapitalised(t *testing.T) {
	sets := []shortcut.Shortcuts{
		scannedModel(t).GetShortcuts(),
		feed(t, loadedModel(t), testutil.Key("ctrl+n")).GetShortcuts(),
		feed(t, loadedModel(t), testutil.Key("ctrl+d")).GetShortcuts(),
		feed(t, NewForSelection(testConfig(), "pick"), tea.WindowSizeMsg{Width: 160, Height: 30}).GetShortcuts(),
	}

	for _, set := range sets {
		for _, s := range set {
			if s.Description == "" {
				t.Errorf("shortcut %q has no description", s.Key)
				continue
			}
			if first := s.Description[0]; first < 'A' || first > 'Z' {
				t.Errorf("shortcut %q description %q does not start with a capital", s.Key, s.Description)
			}
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

// ── Header and help ──────────────────────────────────────────────────────────

func TestGetTitleAndIcon(t *testing.T) {
	m := loadedModel(t)

	if !strings.Contains(m.GetTitle(), "Workspaces") {
		t.Errorf("GetTitle() = %q, want it to name the view", m.GetTitle())
	}
	if m.GetIcon() != "" {
		t.Errorf("GetIcon() = %q, want empty — the title carries the icon", m.GetIcon())
	}
}

func TestGetHeaderInfoCarriesTheContext(t *testing.T) {
	info := loadedModel(t).GetHeaderInfo("work")

	if len(info) != 1 || info[0].Key != "Context" || info[0].Value != "work" {
		t.Errorf("GetHeaderInfo() = %+v, want the active context", info)
	}
}

func TestGetHelpContentIsPopulated(t *testing.T) {
	content := loadedModel(t).GetHelpContent()

	if content.Title == "" || content.Description == "" {
		t.Error("the help content has no title or description")
	}
	if len(content.KeyBindings) == 0 {
		t.Error("the help content lists no key bindings")
	}
	// The description names the configured directory, so it has to reflect it.
	if !strings.Contains(content.Description, "/tmp/workspaces") {
		t.Error("the help description does not name the configured workspaces directory")
	}
}

// Every key the header advertises should be explained in the help — the check
// that caught the drift in the status and containers views.
func TestHelpDocumentsTheAdvertisedShortcuts(t *testing.T) {
	m := scannedModel(t)
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
		if s.Key == "?" || s.Key == "←→" {
			continue // rendered as separate arrow entries in the help
		}
		if !documented[s.Key] {
			t.Errorf("shortcut %q is advertised in the header but absent from the help", s.Key)
		}
	}
}
