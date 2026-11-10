package datatable

import (
	"strings"
	"testing"

	"charm.land/lipgloss/v2"
	"github.com/charmbracelet/x/ansi"

	"github.com/anthnel/devdesk/internal/ui/theme"
)

// TestSelectedRowWithEmojiMeasuresRenderedWidth pins Rule 116 for glyphs that
// runewidth and ansi.StringWidth count differently (U+2764 U+FE0F, flags): the
// selected line must span exactly RenderedWidth() in the measure lipgloss pads by.
func TestSelectedRowWithEmojiMeasuresRenderedWidth(t *testing.T) {
	for _, name := range []string{"a❤️x", "a\U0001F1EB\U0001F1F7x", "a☀️x"} {
		m := New(indicatorConfig(nil))
		m.Resize(60, 10)
		m.SetItems([]row{{Name: name}, {Name: "zz"}})
		cursorOn(t, &m, name)
		want := m.RenderedWidth()
		if got := ansi.StringWidth(lineOf(t, &m, "a")); got != want {
			t.Errorf("%q: selected line is %d cells, want %d", name, got, want)
		}
	}
}

// TestSelectedRowSlackIsPaintedWithTheSelectionBackground covers a table wider
// than its columns: the gap up to the border must carry ColorTableLineSelected.
func TestSelectedRowSlackIsPaintedWithTheSelectionBackground(t *testing.T) {
	withTrueColor(t)
	m := New(Config[row]{
		Key: func(r row) string { return r.Name },
		Columns: []Column[row]{
			{Title: "Name", Sizing: SizingFixed, MinWidth: 6, Cell: func(r row) string { return r.Name }},
		},
	})
	m.Resize(60, 10)
	m.SetItems([]row{{Name: "ab"}})
	m.SetCursor(0)
	m.width += 6 // a viewport wider than its columns can fill
	gap := m.width - 2 - m.RenderedWidth()
	if gap <= 0 {
		t.Fatalf("setup: no slack (width %d, rendered %d)", m.width, m.RenderedWidth())
	}
	line := lineOf(t, &m, "ab")
	if got := ansi.StringWidth(line); got != m.width-2 {
		t.Errorf("selected line is %d cells, want %d", got, m.width-2)
	}
	sgr := lipgloss.NewStyle().Background(theme.ColorTableLineSelected).Render("x")
	sgr = strings.TrimSuffix(strings.TrimPrefix(strings.SplitN(sgr, "x", 2)[0], "\x1b["), "m")
	reset := "\x1b[0m"
	if !strings.HasSuffix(line, reset) {
		t.Fatalf("line does not end with a reset: %q", line)
	}
	before := line
	for strings.HasSuffix(before, reset) { // the slack's own reset, then the row's
		before = strings.TrimSuffix(before, reset)
	}
	if !strings.HasSuffix(before, strings.Repeat(" ", gap)) {
		t.Fatalf("line does not end with the %d-cell slack: %q", gap, line)
	}
	if head := strings.TrimSuffix(before, strings.Repeat(" ", gap)); !strings.HasSuffix(head, sgr+"m") {
		t.Errorf("slack not painted with %q: %q", sgr, line)
	}
}
