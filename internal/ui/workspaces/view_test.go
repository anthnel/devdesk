package workspaces

import (
	"errors"
	"path/filepath"
	"strings"
	"testing"
	"time"

	tea "github.com/charmbracelet/bubbletea"

	"github.com/anthnel/devdesk/internal/ui/keymap"
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
	nested = feed(t, nested, EntriesLoadedMsg{Path: nested.currentPath})
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
		{"create", keymap.New, "Create"},
		{"rename", keymap.Rename, "Rename"},
		{"delete", keymap.Delete, "Delete"},
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
	rows := m.table.Table().Rows()

	// devdesk: 2 modified, 1 untracked, 3 unpushed.
	dirty := rows[0][colGitStatus]
	for _, want := range []string{"2", "1", "3"} {
		if !strings.Contains(dirty, want) {
			t.Errorf("the git status cell %q is missing the count %q", dirty, want)
		}
	}

	// clean-repo has nothing outstanding, so its cell must not invent counters.
	if clean := rows[1][colGitStatus]; strings.ContainsAny(clean, "123456789") {
		t.Errorf("a clean repo rendered counters: %q", clean)
	}
}

func TestNonRepoEntriesHaveNoGitStatus(t *testing.T) {
	m := loadedModel(t)

	// clients (a plain directory) and notes.md (a file).
	for _, idx := range []int{2, 4} {
		if got := strings.TrimSpace(m.table.Table().Rows()[idx][colGitStatus]); got != "" {
			t.Errorf("row %d rendered a git status %q for a non-repo", idx, got)
		}
	}
}

// TestTheIconColumnNamesWhatEachRowIs replaced TestProjectTypeIsShown. The
// column it replaced showed a *project* type — "this directory contains a
// go.mod" — where this one says what the row itself is, which is the
// distinction half the keys in this view act on.
func TestTheIconColumnNamesWhatEachRowIs(t *testing.T) {
	m := loadedModel(t)
	rows := m.table.Table().Rows()

	repo := strings.TrimSpace(rows[0][colIcon])
	dir := strings.TrimSpace(rows[2][colIcon])
	file := strings.TrimSpace(rows[4][colIcon])

	for name, got := range map[string]string{"repo": repo, "directory": dir, "file": file} {
		if got == "" {
			t.Errorf("the %s row has no glyph; the column would stop lining up", name)
		}
	}
	if repo == dir {
		t.Error("a git repository and a plain directory share a glyph — half the keys " +
			"in this view act on that difference and nothing else on the row says it")
	}
	if dir == file {
		t.Error("a directory and a file share a glyph")
	}
}

// TestAFileGetsTheGlyphOfItsKind checks the column actually consults the file
// name rather than rendering one generic glyph for everything that is not a
// directory.
func TestAFileGetsTheGlyphOfItsKind(t *testing.T) {
	if entryIcon(Entry{Name: "main.go"}) == entryIcon(Entry{Name: "data.bin"}) {
		t.Error("a .go file and an unknown file share a glyph")
	}
	if entryIcon(Entry{Name: "notes.md"}) == entryIcon(Entry{Name: "run.sh"}) {
		t.Error("a Markdown file and a shell script share a glyph")
	}
}

// TestADirectoryIgnoresItsOwnName keeps the file table out of the directory
// branch: a directory called `config.toml` is still a directory.
func TestADirectoryIgnoresItsOwnName(t *testing.T) {
	plain := entryIcon(Entry{Name: "src", IsDir: true})
	if got := entryIcon(Entry{Name: "config.toml", IsDir: true}); got != plain {
		t.Errorf("a directory named like a file got %q, want the directory glyph %q", got, plain)
	}
}

func TestScanColumnsShowCachedSeverities(t *testing.T) {
	m := scannedModel(t)
	row := m.table.Table().Rows()[0]

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
	row := m.table.Table().Rows()[1] // clean-repo, never scanned

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
	before := m.table.Table().Rows()[0][9]

	m = feed(t, m, WorkspaceScanStartingMsg{RepoPath: "/tmp/workspaces/devdesk"})

	if got := m.table.Table().Rows()[0][9]; got == before {
		t.Errorf("the Scanned cell is unchanged (%q) while a scan is running", got)
	}
}

// Rule 122: no escape sequences in cell values.
func TestTableCellsCarryNoANSISequences(t *testing.T) {
	m := scannedModel(t)

	for _, row := range m.table.Table().Rows() {
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

	if got := m.table.Table().Rows()[0][10]; !strings.Contains(got, "hr") {
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
		{"create overlay", func(t *testing.T) Model { return feed(t, loadedModel(t), testutil.Key(keymap.New)) }},
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

// A breadcrumb tab names the directory, not the path leading to it. It used to
// split on "/" alone, which cuts nothing on Windows: three tabs each carried the
// whole absolute path, the line overflowed, and none of them said where the user
// was any better than one word would have.
//
// filepath.Join is what builds these paths in the view, so it is what builds
// them here. The assertion only tells the two implementations apart on a
// platform whose separator is not "/" — which is precisely where the defect was.
func TestABreadcrumbTabNamesTheDirectoryNotItsPath(t *testing.T) {
	nested := filepath.Join("C:", "Users", "anthoni", "workspaces", "anthnell", "devsecops")
	parent := filepath.Dir(nested)

	m := loadedModel(t)
	m.navigationStack = []string{parent}
	m.currentPath = nested

	footer := m.RenderFooter(160)

	if !strings.Contains(footer, "devsecops") || !strings.Contains(footer, "anthnell") {
		t.Errorf("the breadcrumb does not name both levels:\n%s", footer)
	}
	if strings.Contains(footer, parent) {
		t.Errorf("a breadcrumb tab carries the whole path instead of the directory:\n%s", footer)
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
	m.footer.Error("Scan failed — check logs")

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
	overlay := feed(t, searching, testutil.Key("esc"), testutil.Key(keymap.New))
	if overlay.FilterBarVisible() {
		t.Error("the filter bar stayed visible under the create overlay")
	}
}

// ── Shortcuts (Rule 130) ─────────────────────────────────────────────────────

// This view has the most state-dependent shortcut list in the app: the backlog
// documents which key applies when, and getting it wrong either advertises an
// action that silently does nothing, or greys out one that works.
//
// The entry is always there — what changes is whether it is greyed.
func TestShortcutsFollowTheSelectedEntry(t *testing.T) {
	tests := []struct {
		name     string
		cursor   int
		scanned  bool
		enabled  []string
		disabled []string
	}{
		{
			name: "scanned git repo", cursor: 0, scanned: true,
			enabled: []string{"enter", keymap.Web, keymap.Scan, keymap.Fetch, keymap.Copy},
		},
		{
			name: "unscanned git repo", cursor: 1,
			enabled:  []string{keymap.Web, keymap.Scan, keymap.Fetch},
			disabled: []string{"enter"}, // no cached result to open
		},
		{
			name: "directory with nested repos", cursor: 2,
			enabled:  []string{keymap.Scan, keymap.Fetch},
			disabled: []string{"enter", keymap.Web},
		},
		{
			name: "plain directory", cursor: 3,
			disabled: []string{"enter", keymap.Web, keymap.Scan, keymap.Fetch},
		},
		{
			name: "a file", cursor: 4,
			enabled:  []string{"enter", keymap.Copy},
			disabled: []string{keymap.Web, keymap.Scan, keymap.Fetch},
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
			for _, key := range append(append([]string{}, tc.enabled...), tc.disabled...) {
				if !hasShortcut(got, key) {
					t.Errorf("%q is missing for a %s; an entry is greyed, never dropped", key, tc.name)
				}
			}
			for _, key := range tc.enabled {
				if shortcutDisabled(got, key) {
					t.Errorf("%q is greyed for a %s, where it works", key, tc.name)
				}
			}
			for _, key := range tc.disabled {
				if !shortcutDisabled(got, key) {
					t.Errorf("%q is offered for a %s, where it does nothing", key, tc.name)
				}
			}
		})
	}
}

// The whole point of greying rather than hiding: this column is read out of the
// corner of the eye, and one that re-orders itself as the cursor moves cannot
// be. Every row of the fixture advertises the same keys, in the same order.
func TestTheShortcutColumnDoesNotMoveWithTheCursor(t *testing.T) {
	m := scannedModel(t)

	var want []string
	for cursor := 0; cursor < len(entryFixtures()); cursor++ {
		m.table.SetCursor(cursor)

		got := shortcutKeys(m.GetShortcuts())
		if cursor == 0 {
			want = got
			continue
		}
		if strings.Join(got, " ") != strings.Join(want, " ") {
			t.Errorf("row %d advertises\n%v\nwant the same keys as row 0\n%v", cursor, got, want)
		}
	}

	// And with no row at all — an empty listing is still the normal mode.
	if got := shortcutKeys(newTestModel(t).GetShortcuts()); strings.Join(got, " ") != strings.Join(want, " ") {
		t.Errorf("an empty listing advertises\n%v\nwant\n%v", got, want)
	}
}

func TestShortcutsFollowTheMode(t *testing.T) {
	tests := []struct {
		name string
		open func(t *testing.T) Model
		want string
	}{
		{"create", func(t *testing.T) Model { return feed(t, loadedModel(t), testutil.Key(keymap.New)) }, "Create"},
		{"rename", func(t *testing.T) Model { return feed(t, loadedModel(t), testutil.Key(keymap.Rename)) }, "Rename"},
		{"delete", func(t *testing.T) Model { return feed(t, loadedModel(t), testutil.Key(keymap.Delete)) }, "Confirm"},
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
	if hasShortcut(selection, keymap.Delete) || hasShortcut(selection, keymap.Scan) {
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
		feed(t, loadedModel(t), testutil.Key(keymap.New)).GetShortcuts(),
		feed(t, loadedModel(t), testutil.Key(keymap.Delete)).GetShortcuts(),
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

// shortcutDisabled reports whether the key is advertised but greyed out. A key
// that is missing altogether is not disabled — it is absent, which is a
// different failure, so the two are asserted separately.
func shortcutDisabled(shortcuts shortcut.Shortcuts, key string) bool {
	for _, s := range shortcuts {
		if s.Key == key {
			return s.Disabled
		}
	}
	return false
}

func shortcutKeys(shortcuts shortcut.Shortcuts) []string {
	keys := make([]string, 0, len(shortcuts))
	for _, s := range shortcuts {
		keys = append(keys, s.Key)
	}
	return keys
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
