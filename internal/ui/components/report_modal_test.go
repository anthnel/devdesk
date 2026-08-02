package components

import (
	"fmt"
	"strings"
	"testing"
	"unicode/utf8"

	"github.com/anthnel/devdesk/internal/ui/testutil"
	"github.com/anthnel/devdesk/internal/ui/theme"
)

func sampleReport() PullReport {
	return PullReport{
		Cloned:  []string{"group/a", "group/b"},
		Skipped: []string{"group/c"},
		Errors:  []string{"group/d: permission denied"},
	}
}

func TestNewReportModalStartsOnClonedTab(t *testing.T) {
	m := NewReportModal("Pull report", sampleReport())

	if m.activeTab != 0 {
		t.Errorf("activeTab = %d on a new modal, want 0 (Cloned)", m.activeTab)
	}
	if m.scrollOffset != 0 {
		t.Errorf("scrollOffset = %d on a new modal, want 0", m.scrollOffset)
	}
}

func TestReportModalTabCyclesForwardAndBack(t *testing.T) {
	for _, key := range []string{"tab", "right", "l"} {
		t.Run("forward with "+key, func(t *testing.T) {
			m := NewReportModal("Pull report", sampleReport())

			for want := 1; want <= 3; want++ {
				m, _ = m.Update(testutil.Key(key))
				if got := m.activeTab; got != want%3 {
					t.Fatalf("activeTab = %d after %d presses of %q, want %d", got, want, key, want%3)
				}
			}
		})
	}

	for _, key := range []string{"shift+tab", "left", "h"} {
		t.Run("backward with "+key, func(t *testing.T) {
			m := NewReportModal("Pull report", sampleReport())

			m, _ = m.Update(testutil.Key(key))
			if m.activeTab != 2 {
				t.Errorf("activeTab = %d after %q from Cloned, want 2 (wraps to Errors)", m.activeTab, key)
			}
		})
	}
}

// Each tab has its own list length, so a scroll offset carried across a tab
// switch would point past the end of the new list.
func TestReportModalTabSwitchResetsScroll(t *testing.T) {
	m := NewReportModal("Pull report", longReport(30))
	m, _ = m.Update(testutil.Key("down"))
	m, _ = m.Update(testutil.Key("down"))
	if m.scrollOffset == 0 {
		t.Fatal("precondition failed: scrolling did not move the offset")
	}

	m, _ = m.Update(testutil.Key("tab"))

	if m.scrollOffset != 0 {
		t.Errorf("scrollOffset = %d after switching tabs, want 0", m.scrollOffset)
	}
}

func TestReportModalScrollClampsAtBothEnds(t *testing.T) {
	m := NewReportModal("Pull report", longReport(15))

	for i := 0; i < 3; i++ {
		m, _ = m.Update(testutil.Key("up"))
	}
	if m.scrollOffset != 0 {
		t.Errorf("scrollOffset = %d after scrolling up from the top, want 0", m.scrollOffset)
	}

	for i := 0; i < 50; i++ {
		m, _ = m.Update(testutil.Key("down"))
	}
	maxOffset := 15 - m.maxVisible
	if m.scrollOffset != maxOffset {
		t.Errorf("scrollOffset = %d after scrolling past the end, want %d", m.scrollOffset, maxOffset)
	}
}

func TestReportModalDoesNotScrollShortList(t *testing.T) {
	m := NewReportModal("Pull report", sampleReport()) // 2 entries, maxVisible 10

	m, _ = m.Update(testutil.Key("down"))

	if m.scrollOffset != 0 {
		t.Errorf("scrollOffset = %d on a list shorter than the viewport, want 0", m.scrollOffset)
	}
}

func TestReportModalVimScrollKeys(t *testing.T) {
	m := NewReportModal("Pull report", longReport(30))

	m, _ = m.Update(testutil.Key("j"))
	if m.scrollOffset != 1 {
		t.Errorf("scrollOffset = %d after j, want 1", m.scrollOffset)
	}
	m, _ = m.Update(testutil.Key("k"))
	if m.scrollOffset != 0 {
		t.Errorf("scrollOffset = %d after k, want 0", m.scrollOffset)
	}
}

func TestReportModalCloseKeys(t *testing.T) {
	for _, key := range []string{"enter", "esc", "q"} {
		t.Run(key, func(t *testing.T) {
			m := NewReportModal("Pull report", sampleReport())

			_, cmd := m.Update(testutil.Key(key))
			if _, ok := testutil.MsgOf[ReportModalCloseMsg](cmd); !ok {
				t.Errorf("%q did not close the modal, got %T", key, testutil.Msg(cmd))
			}
		})
	}
}

func TestReportModalGetCurrentList(t *testing.T) {
	m := NewReportModal("Pull report", sampleReport())

	tests := []struct {
		tab  int
		want int
	}{
		{0, 2}, // Cloned
		{1, 1}, // Skipped
		{2, 1}, // Errors
	}
	for _, tt := range tests {
		m.activeTab = tt.tab
		if got := len(m.getCurrentList()); got != tt.want {
			t.Errorf("tab %d list has %d entries, want %d", tt.tab, got, tt.want)
		}
	}

	m.activeTab = 99
	if got := m.getCurrentList(); len(got) != 0 {
		t.Errorf("an out-of-range tab returned %d entries, want 0", len(got))
	}
}

func TestReportModalViewShowsCountsAndEntries(t *testing.T) {
	m := NewReportModal("Pull report", sampleReport())

	view := m.View()
	for _, want := range []string{"Pull report", "2 cloned", "1 skipped", "1 errors", "group/a", "group/b"} {
		if !strings.Contains(view, want) {
			t.Errorf("View() does not contain %q", want)
		}
	}
}

func TestReportModalViewShowsEmptyPlaceholder(t *testing.T) {
	m := NewReportModal("Pull report", PullReport{})

	if !strings.Contains(m.View(), "(empty)") {
		t.Error("View() does not show the empty placeholder for an empty tab")
	}
}

func TestReportModalViewShowsScrollIndicator(t *testing.T) {
	m := NewReportModal("Pull report", longReport(30))

	if !strings.Contains(m.View(), "of 30") {
		t.Error("View() does not show a scroll indicator for a list longer than the viewport")
	}
}

// View() truncates long entries with item[len(item)-52:], slicing bytes rather
// than runes. When the resulting offset lands inside a multi-byte codepoint the
// rendered output is no longer valid UTF-8.
//
// The fixture is chosen so the cut is guaranteed to land mid-rune: 30 three-byte
// runes give a 90-byte string, and 90-52 = 38, which is not a multiple of 3.
func TestReportModalViewKeepsMultibyteRunesIntact(t *testing.T) {
	// 30 × "€" is 90 bytes but only 30 columns, and the old byte-slice
	// truncation cut at byte 38 — inside a codepoint.
	item := strings.Repeat("€", 30)
	m := NewReportModal("Pull report", PullReport{Cloned: []string{item}})

	view := m.View()

	if !utf8.ValidString(view) {
		t.Error("View() emitted invalid UTF-8: truncation cut a multibyte rune")
	}
	// 30 columns is under the 55-column budget, so the entry renders whole.
	if !strings.Contains(view, item) {
		t.Error("View() truncated an entry that fits within the column budget")
	}
}

func TestReportModalViewTruncatesOverlongEntryOnRuneBoundary(t *testing.T) {
	item := strings.Repeat("é", 80) // 80 columns, 160 bytes

	m := NewReportModal("Pull report", PullReport{Cloned: []string{item}})
	view := m.View()

	if !utf8.ValidString(view) {
		t.Fatal("View() emitted invalid UTF-8 while truncating")
	}
	if strings.Contains(view, item) {
		t.Error("View() rendered an 80-column entry without truncating it")
	}
	// The tail is what identifies a path, so it must survive.
	if !strings.Contains(view, theme.Ellipsis+strings.Repeat("é", 52)) {
		t.Error("View() did not keep the tail of the truncated entry")
	}
}

func TestReportModalStoresWindowSize(t *testing.T) {
	m := NewReportModal("Pull report", sampleReport())

	m, _ = m.Update(testutil.Resize(90, 25))
	if m.width != 90 || m.height != 25 {
		t.Errorf("window size = %dx%d, want 90x25", m.width, m.height)
	}
}

// longReport builds a report whose Cloned list is n entries long.
func longReport(n int) PullReport {
	cloned := make([]string, n)
	for i := range cloned {
		cloned[i] = fmt.Sprintf("group/repo-%02d", i)
	}
	return PullReport{Cloned: cloned, Skipped: []string{"one"}}
}
