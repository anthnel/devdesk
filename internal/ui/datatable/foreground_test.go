package datatable

import (
	"strings"
	"testing"

	"github.com/charmbracelet/lipgloss"

	"github.com/anthnel/devdesk/internal/ui/theme"
)

// backgroundOnlyStyle declares a background and nothing else, which a column
// is entitled to do.
func backgroundOnlyStyle(row) lipgloss.Style {
	return lipgloss.NewStyle().Background(theme.ColorBackground)
}

// The **text** color of a cell, which was missing.
//
// Neither `theme.DefaultTableStyles()` nor bubbles' styles set a foreground
// on `Cell`: a column that declares no `Style` therefore rendered in the
// terminal's default color, over which the theme has no control. Four views
// had rewritten `Foreground(theme.ColorText)` in a `Style` of their own to
// recover it, which is the shape a missing default takes.

// A column with no Style takes the theme's text color.
func TestACellWithNoStyleCarriesTheThemeForeground(t *testing.T) {
	withTrueColor(t)
	m := loaded(t) // no column declares a Style

	unselected := rowLines(&m)[1]

	if !strings.Contains(unselected, foreground(theme.ColorText)) {
		t.Errorf("a row of a table with no styled column carries no theme foreground: %q", unselected)
	}
}

// Every segment **that carries text** opens both colors.
//
// The background is required of every segment by the neighboring test,
// because it shows on a space; text only shows where there is a character,
// and lipgloss emits a cell's padding as segments carrying only its
// background color. Requiring a foreground on a space would require a
// sequence that changes nothing on screen.
func TestEveryRunThatShowsTextCarriesTheThemeForeground(t *testing.T) {
	withTrueColor(t)
	m := loaded(t)

	unselected := rowLines(&m)[1]
	segments := strings.Split(unselected, "\x1b[0m")

	checked := 0
	for i, segment := range segments[:len(segments)-1] {
		if strings.TrimSpace(visible(segment)) == "" {
			continue
		}
		checked++
		if !strings.Contains(segment, foreground(theme.ColorText)) {
			t.Errorf("run %d (%q) carries no foreground", i, visible(segment))
		}
		if !strings.Contains(segment, background(theme.ColorBackground)) {
			t.Errorf("run %d (%q) carries no background", i, visible(segment))
		}
	}
	if checked == 0 {
		t.Fatal("no run carried any text, so this test asserted nothing")
	}
}

// A column that declares only a background receives the theme's text — the
// exact symmetric case of TestAColumnThatDeclaresNoBackgroundGetsTheAppOne.
func TestAColumnThatDeclaresNoForegroundGetsTheThemeOne(t *testing.T) {
	withTrueColor(t)

	cfg := testConfig()
	cfg.Columns[2].Style = backgroundOnlyStyle
	m := New(cfg)
	m.Resize(120, 10)
	m.SetItems(fixtures())

	if got := rowLines(&m)[1]; !strings.Contains(got, foreground(theme.ColorText)) {
		t.Errorf("a column declaring only a background renders its text in the terminal's colour: %q", got)
	}
}

// **And this is what forbids putting the color on `styles.Cell`, for a row a
// view has coloured whole.** Cells are rendered, then the row is passed to
// `styles.Selected`: a cell color there would open a sequence whose reset
// would close the highlight in the middle of the row. The plain "normal"
// selection is the exception — see TestTheDefaultSelectionKeepsTheColumnColours
// in render_test.go — because there every cell repaints the same background
// itself instead of relying on one outer wrap.
func TestAStateColouredSelectionKeepsItsHighlightWhole(t *testing.T) {
	withTrueColor(t)
	m := errorStyledTable(t, colouredConfig) // the State column is colored, and row 0 is under the cursor

	selected := rowLines(&m)[0]

	if strings.Contains(selected, foreground(theme.ColorText)) {
		t.Error("the selected row carries a cell foreground, which would end the highlight at its reset")
	}
	if strings.Contains(selected, foreground(theme.ColorError)) || strings.Contains(selected, foreground(theme.ColorOK)) {
		t.Error("a column colour reached the selected row; styles.Selected must own it whole")
	}
	if !strings.Contains(selected, background(theme.ColorError)) {
		t.Errorf("the selected row does not carry the error selection background: %q", selected)
	}
	// Only one reset, right at the end: that is what "the whole highlight"
	// means, and what a per-cell foreground would break.
	if n := strings.Count(selected, "\x1b[0m"); n != 1 {
		t.Errorf("the selected row closes %d times, want one reset at its end", n)
	}
}
