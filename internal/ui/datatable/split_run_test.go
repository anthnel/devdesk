package datatable

import (
	"strings"
	"testing"

	"github.com/charmbracelet/lipgloss"

	"github.com/anthnel/devdesk/internal/ui/theme"
)

// Cut and TailStyle exist for one caller — the containers load gauges
// (§3.71) — but the mechanism lives here, so it is tested here rather than
// through a view.

// splitConfig reuses the fixed-width "Size" column slot as a bar: r.Size
// "#" of fill, the rest left to fit()'s own space padding — exactly how the
// containers gauge's "-" placeholder is shorter than its column and relies on
// the same padding — coloured in two hues that appear nowhere else in this
// package's tests, so a leak in either direction is unambiguous.
func splitConfig() Config[row] {
	cfg := testConfig()
	cfg.Columns[1] = Column[row]{
		Title: "Bar", Sizing: SizingFixed, MinWidth: 10,
		Cell:      func(r row) string { return strings.Repeat("#", r.Size) },
		Style:     func(row) lipgloss.Style { return lipgloss.NewStyle().Foreground(theme.ColorOK) },
		Cut:       func(r row) int { return r.Size },
		TailStyle: func(row) lipgloss.Style { return lipgloss.NewStyle().Foreground(theme.ColorSeverityLow) },
	}
	return cfg
}

func splitTable(t *testing.T) Model[row] {
	t.Helper()
	m := New(splitConfig())
	m.Resize(120, 10)
	m.SetItems(fixtures())
	return m
}

// A split cell carries both colours in the same row: Style's on the fill,
// TailStyle's on the track, neither one alone. Row 1 ("cache", Size 1) rather
// than row 0: the cursor starts on row 0, and a selected row drops both
// colours on purpose (see TestNeitherSplitColourReachesTheSelectedRow).
func TestASplitCellCarriesBothColours(t *testing.T) {
	withTrueColor(t)
	m := splitTable(t)

	line := rowLines(&m)[1]
	if !strings.Contains(line, foreground(theme.ColorOK)) {
		t.Errorf("the fill's colour is missing: %q", line)
	}
	if !strings.Contains(line, foreground(theme.ColorSeverityLow)) {
		t.Errorf("the track's colour is missing: %q", line)
	}
}

// The split is not a gradient: it does not touch what the cell measures. The
// row spans exactly what a single-style cell of the same text would — the
// Rule 122 invariant this package exists to hold, extended to a second run
// rather than bent for it.
func TestASplitCellDoesNotChangeWhatTheRowRenders(t *testing.T) {
	withTrueColor(t)

	plain := loaded(t)
	split := splitTable(t)

	for i, got := range rowLines(&split) {
		want := rowLines(&plain)[i]
		if lipgloss.Width(got) != lipgloss.Width(want) {
			t.Errorf("row %d is %d cells split and %d plain", i, lipgloss.Width(got), lipgloss.Width(want))
		}
	}
}

// The boundary between the two runs is not a gap: stripping every escape
// sequence must recover exactly the plain cell text, padding included —
// nothing extra, nothing missing, at the exact cell where the colour changes.
func TestASplitCellLeavesNoGapAtTheBoundary(t *testing.T) {
	withTrueColor(t)

	plain := loaded(t)
	split := splitTable(t)

	for i := range rowLines(&split) {
		got := visible(rowLines(&split)[i])
		want := visible(rowLines(&plain)[i])
		if got != want {
			t.Errorf("row %d reads %q with the split column, %q without — the boundary inserted or ate a cell", i, got, want)
		}
	}
}

// Neither colour reaches the selected row, for the exact reason a single
// Style does not (Rule 122): a colour that ends before the row does would
// close the selection highlight in the middle of it.
func TestNeitherSplitColourReachesTheSelectedRow(t *testing.T) {
	withTrueColor(t)
	m := splitTable(t)

	selected := rowLines(&m)[0] // row 0 is under the cursor on a fresh table
	if strings.Contains(selected, foreground(theme.ColorOK)) {
		t.Error("the fill colour reached the selected row")
	}
	if strings.Contains(selected, foreground(theme.ColorSeverityLow)) {
		t.Error("the track colour reached the selected row")
	}
	if !strings.Contains(selected, background(theme.ColorTableSelectedBg)) {
		t.Errorf("the selected row does not carry the selection background: %q", selected)
	}
}

// A Cut of 0 or of the full width is not a special case worth its own branch
// in splitCellRun: one run is simply empty, and rendering an empty string is
// already safe. Out-of-range values (negative, or past the cell's width) are
// what a naive slice expression gets wrong, and are clamped rather than left
// to panic — a column narrow enough to have been dropped still calls Cut
// while the widths are being measured, same as Cell.
func TestASplitCellAtEveryCutDoesNotPanicOrChangeWidth(t *testing.T) {
	withTrueColor(t)
	plain := loaded(t)
	plainWidth := lipgloss.Width(rowLines(&plain)[1])

	for name, cut := range map[string]int{
		"nothing filled": 0, "entirely filled": 10, "past the end": 99, "negative": -5,
	} {
		t.Run(name, func(t *testing.T) {
			cfg := testConfig()
			cfg.Columns[1] = Column[row]{
				Title: "Bar", Sizing: SizingFixed, MinWidth: 10,
				Cell:      func(row) string { return strings.Repeat("#", 10) },
				Style:     func(row) lipgloss.Style { return lipgloss.NewStyle().Foreground(theme.ColorOK) },
				Cut:       func(row) int { return cut },
				TailStyle: func(row) lipgloss.Style { return lipgloss.NewStyle().Foreground(theme.ColorSeverityLow) },
			}
			m := New(cfg)
			m.Resize(120, 10)
			m.SetItems(fixtures())
			if got := lipgloss.Width(rowLines(&m)[1]); got != plainWidth {
				t.Errorf("Cut(%d) renders %d cells, want %d", cut, got, plainWidth)
			}
		})
	}
}

// A column with no TailStyle is untouched: the ordinary single-colour path
// still runs, which is what every column but the two gauges leaves in place.
func TestAColumnWithNoTailStyleKeepsTheOrdinaryPath(t *testing.T) {
	withTrueColor(t)
	m := colouredTable(t) // declares Style, not TailStyle, on its State column

	line := rowLines(&m)[1] // "cache", exited
	if !strings.Contains(line, foreground(theme.ColorError)) {
		t.Fatalf("the row is not the coloured one: %q", line)
	}
}
