package workspaces

import (
	"strings"
	"testing"

	tea "github.com/charmbracelet/bubbletea"

	"github.com/anthnel/devdesk/internal/ui/testutil"
)

func TestGOpensTheFuzzyFindPrompt(t *testing.T) {
	m := newTestModel(t)

	m, _ = step(t, m, testutil.Key("g"))

	if m.mode != ModeFuzzyFinding {
		t.Fatalf("mode = %v, want ModeFuzzyFinding", m.mode)
	}
	if m.fuzzyFinder == nil {
		t.Fatal("g did not create a FuzzyFinder")
	}
	if got := m.GetFooterHeight(); got != 4 {
		t.Errorf("GetFooterHeight = %d, want 4 (bar + empty line + info line)", got)
	}
}

// FilterBarVisible is what tells the router to turn the viewport's bottom
// corners into T-junctions so the bar below joins into one closed rectangle
// (Rule 136) — without it, the query bar renders as a second, disconnected
// box under an otherwise fully closed viewport.
func TestFuzzyFindReportsFilterBarVisibleForTheClosedBorder(t *testing.T) {
	m := newTestModel(t)
	if m.FilterBarVisible() {
		t.Error("FilterBarVisible = true before g was pressed")
	}

	m, _ = step(t, m, testutil.Key("g"))
	if !m.FilterBarVisible() {
		t.Error("FilterBarVisible = false while the fuzzy-find bar is on screen, border will not close")
	}

	m = feed(t, m, FuzzyFindCancelMsg{})
	if m.FilterBarVisible() {
		t.Error("FilterBarVisible = true after the prompt was cancelled")
	}
}

// The query sits in the same footer bar frame the "/" filter and the
// viewer's own "g" (go to line) already use, not a line inside the
// viewport — RenderFooter, not View, is what draws it.
func TestFuzzyFindQuerySitsInTheFilterBarSlot(t *testing.T) {
	m := newTestModel(t)
	m, _ = step(t, m, testutil.Key("g"))
	m = feed(t, m, FuzzyPathsLoadedMsg{Candidates: []fuzzyCandidate{{Abs: "/tmp/workspaces/devdesk", Rel: "devdesk"}}})
	m, _ = step(t, m, tea.KeyMsg{Type: tea.KeyRunes, Runes: []rune("dev")})

	footer := m.RenderFooter(80)
	if !strings.Contains(footer, "Find") {
		t.Errorf("RenderFooter = %q, want the bar's \"Find\" label", footer)
	}
	if !strings.Contains(footer, "dev") {
		t.Errorf("RenderFooter = %q, want the typed query", footer)
	}
	if !strings.Contains(footer, "1 match(es)") {
		t.Errorf("RenderFooter = %q, want the match count", footer)
	}
	if strings.Contains(m.View(), "Find") {
		t.Errorf("View() = %q, the query bar belongs in the footer, not the viewport", m.View())
	}
}

// End to end: g opens the prompt, the whole-tree walk lands, a query of at
// least 3 characters narrows the ranked results, and Enter jumps there —
// landing on the matched directory's parent with the drill-down state
// navigateIn would have built one step at a time.
func TestFuzzyFindJumpsToTheSelectedDirectory(t *testing.T) {
	m := newTestModel(t)
	m, _ = step(t, m, testutil.Key("g"))

	m = feed(t, m, FuzzyPathsLoadedMsg{Candidates: []fuzzyCandidate{
		{Abs: "/tmp/workspaces/a/b", Rel: "a/b"},
		{Abs: "/tmp/workspaces/other", Rel: "other"},
	}})

	m, _ = step(t, m, tea.KeyMsg{Type: tea.KeyRunes, Runes: []rune("a/b")})

	if got := m.fuzzyFinder.matchHint(); got != "1 match(es)" {
		t.Fatalf("matchHint = %q, want a single match reported", got)
	}

	_, cmd := step(t, m, testutil.Key("enter"))
	confirm, ok := testutil.MsgOf[FuzzyFindConfirmMsg](cmd)
	if !ok {
		t.Fatal("Enter on the selected result did not confirm a path")
	}
	if confirm.Path != "/tmp/workspaces/a/b" {
		t.Fatalf("confirmed path = %q, want %q", confirm.Path, "/tmp/workspaces/a/b")
	}

	m = feed(t, m, confirm)

	if m.mode != ModeNormal {
		t.Errorf("mode = %v, want ModeNormal after confirming", m.mode)
	}
	if m.fuzzyFinder != nil {
		t.Error("fuzzyFinder was not cleared after confirming")
	}
	if m.currentPath != "/tmp/workspaces/a" {
		t.Errorf("currentPath = %q, want %q", m.currentPath, "/tmp/workspaces/a")
	}
	if len(m.navigationStack) != 0 {
		t.Errorf("navigationStack = %v, want empty", m.navigationStack)
	}
	if m.pendingSelectPath != "/tmp/workspaces/a/b" {
		t.Errorf("pendingSelectPath = %q, want the confirmed path", m.pendingSelectPath)
	}

	// The load lands: the matched directory's row is selected by path, not
	// by whatever position the listing happens to return it at.
	m = feed(t, m, EntriesLoadedMsg{Path: "/tmp/workspaces/a", Entries: []Entry{
		{Name: "other-child", Path: "/tmp/workspaces/a/other-child", IsDir: true},
		{Name: "b", Path: "/tmp/workspaces/a/b", IsDir: true},
	}})

	if m.pendingSelectPath != "" {
		t.Errorf("pendingSelectPath = %q, want cleared after the load landed", m.pendingSelectPath)
	}
	entry, ok := m.selectedEntry()
	if !ok || entry.Path != "/tmp/workspaces/a/b" {
		t.Errorf("selected entry = %+v, ok=%v, want the matched directory selected", entry, ok)
	}
}

func TestFuzzyFindBelowThreeCharactersRunsNoQuery(t *testing.T) {
	m := newTestModel(t)
	m, _ = step(t, m, testutil.Key("g"))
	m = feed(t, m, FuzzyPathsLoadedMsg{Candidates: []fuzzyCandidate{{Abs: "/tmp/workspaces/ab", Rel: "ab"}}})

	m, _ = step(t, m, tea.KeyMsg{Type: tea.KeyRunes, Runes: []rune("ab")})

	if got := m.fuzzyFinder.StatusText(); got != "Type at least 3 characters to search" {
		t.Errorf("StatusText = %q, want the character-count prompt", got)
	}
}

func TestEscCancelsFuzzyFind(t *testing.T) {
	m := newTestModel(t)
	m, _ = step(t, m, testutil.Key("g"))

	_, cmd := step(t, m, testutil.Key("esc"))
	cancel, ok := testutil.MsgOf[FuzzyFindCancelMsg](cmd)
	if !ok {
		t.Fatal("esc did not cancel the prompt")
	}

	m = feed(t, m, cancel)
	if m.mode != ModeNormal || m.fuzzyFinder != nil {
		t.Errorf("mode = %v, fuzzyFinder = %v, want ModeNormal and nil after cancel", m.mode, m.fuzzyFinder)
	}
}
