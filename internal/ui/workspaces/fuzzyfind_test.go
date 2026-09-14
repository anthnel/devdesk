package workspaces

import (
	tea "github.com/charmbracelet/bubbletea"

	"github.com/anthnel/devdesk/internal/ui/testutil"

	"testing"
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

	if got := m.fuzzyFinder.StatusText(); got != "1 match(es)" {
		t.Fatalf("StatusText = %q, want a single match reported", got)
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
