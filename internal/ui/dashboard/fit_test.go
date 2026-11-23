package dashboard

import (
	"fmt"
	"strings"
	"testing"

	tea "charm.land/bubbletea/v2"

	sharedcomponents "github.com/anthnel/devdesk/internal/ui/components"
	"github.com/anthnel/devdesk/internal/ui/shortcut"
)

// These tests pin how the dashboard fits its height (DEVDESK-743): the router's
// viewport does not scroll, so whatever the grid renders past its height is
// cut, and the cut used to fall on the bottom border of the lowest boxes.

// closedBoxes counts the boxes whose bottom border is in the rendering, and
// open the ones whose top border is there without it. The right-hand corners
// are counted: a tree branch draws the same glyph as a bottom-left corner.
func closedBoxes(rendering string) (closed, open int) {
	plain := stripANSI(rendering)
	tops, bottoms := strings.Count(plain, "┐"), strings.Count(plain, "┘")
	return bottoms, tops - bottoms
}

// renderedLines is how many lines the dashboard hands the router.
func renderedLines(m Model) int { return strings.Count(m.Render(), "\n") + 1 }

// TestTheGridHasNoLeadingBlankLine: the blank line above the boxes separated
// them from the title rule, which the dashboard no longer has.
func TestTheGridHasNoLeadingBlankLine(t *testing.T) {
	m, _ := loadedModel(t)
	first := stripANSI(strings.SplitN(m.Render(), "\n", 2)[0])
	if !strings.Contains(first, "┌") {
		t.Errorf("the dashboard opens on %q, want the top border of its first row", first)
	}
}

// TestTheGridKeepsItsAirWhenItFits: density is only given up for a height the
// roomy grid does not fit.
func TestTheGridKeepsItsAirWhenItFits(t *testing.T) {
	m, _ := loadedModel(t)
	need := m.gridHeight(tierStandard)
	m = feed(t, m, tea.WindowSizeMsg{Width: 120, Height: need})
	if p := m.layout(); p.density != densityRoomy || p.floored {
		t.Errorf("at the roomy grid's own height (%d) the layout chose density %d, floored %v",
			need, p.density, p.floored)
	}
}

// TestTheGridTightensBeforeItLosesALine: from the roomy height down to the
// tightest one, every box stays on screen, closed, within the height.
func TestTheGridTightensBeforeItLosesALine(t *testing.T) {
	m, _ := loadedModel(t)
	for _, width := range []int{80, 120, 175} {
		at := feed(t, m, tea.WindowSizeMsg{Width: width, Height: 200})
		roomy := at.gridHeight(layoutTier(width, 200))
		tight := at
		tight.density = densityTight
		least := tight.gridHeight(layoutTier(width, 200))
		if least >= roomy {
			t.Fatalf("width %d: the tight grid needs %d lines, no fewer than the roomy %d", width, least, roomy)
		}
		for height := least; height <= roomy; height++ {
			h := feed(t, m, tea.WindowSizeMsg{Width: width, Height: height})
			if got := renderedLines(h); got > height {
				t.Errorf("%dx%d: %d lines rendered", width, height, got)
			}
			closed, open := closedBoxes(h.Render())
			if open != 0 || closed != 4 {
				t.Errorf("%dx%d: %d boxes closed, %d open, want 4 and 0", width, height, closed, open)
			}
			if p := h.layout(); p.floored {
				t.Errorf("%dx%d: floored while the tight grid fits", width, height)
			}
		}
	}
}

// TestTheTwoColumnGridFitsAThirtyOneRowTerminal: the figure the compact
// density buys, on the overview.
func TestTheTwoColumnGridFitsAThirtyOneRowTerminal(t *testing.T) {
	m, _ := loadedModel(t)
	content := 31 - routerOverhead
	m = feed(t, m, tea.WindowSizeMsg{Width: 120, Height: content})
	if p := m.layout(); p.floored {
		t.Errorf("a 120x31 terminal (%d content lines) still floors: the grid needs %d", content, p.need)
	}
}

// TestTheFloorShowsWholeBoxesAndSaysWhy: below the tightest grid, whole rows of
// boxes are drawn in reading order, never a cut one, and the footer says how
// tall the terminal must be.
func TestTheFloorShowsWholeBoxesAndSaysWhy(t *testing.T) {
	m, _ := loadedModel(t)
	for _, tc := range []struct {
		width, height, wantBoxes int
	}{
		{120, 18, 2},  // the top row only
		{120, 8, 0},   // not even one row
		{80, 12, 1},   // one column: Code alone
		{80, 30, 3},   // one column: Code, Health, Host
		{175, 14, 2},  // the top row only
		{100, 12, 2},  // the top row only
		{80, 1, 0},    // nothing fits
		{120, 100, 4}, // control: everything
	} {
		at := feed(t, m, tea.WindowSizeMsg{Width: tc.width, Height: tc.height})
		rendering := at.Render()
		if got := renderedLines(at); got > tc.height {
			t.Errorf("%dx%d: %d lines rendered", tc.width, tc.height, got)
		}
		closed, open := closedBoxes(rendering)
		if open != 0 || closed != tc.wantBoxes {
			t.Errorf("%dx%d: %d boxes closed, %d open, want %d and 0", tc.width, tc.height, closed, open, tc.wantBoxes)
		}
		footer := stripANSI(at.RenderFooter(tc.width))
		if tc.wantBoxes == 4 {
			if strings.Contains(footer, "too short") {
				t.Errorf("%dx%d: the footer warns while everything fits: %q", tc.width, tc.height, footer)
			}
			continue
		}
		p := at.layout()
		want := fmt.Sprintf("Terminal too short for the dashboard (need %d rows)",
			p.need+shortcut.HeaderRows+1+at.GetFooterHeight())
		if !strings.Contains(footer, want) {
			t.Errorf("%dx%d: footer %q, want it to say %q", tc.width, tc.height, footer, want)
		}
	}
}

// TestTheFloorWarningIsAWarning: Rule 128, orange, through the one component.
func TestTheFloorWarningIsAWarning(t *testing.T) {
	m, _ := loadedModel(t)
	m = feed(t, m, tea.WindowSizeMsg{Width: 120, Height: 8})
	s := m.status()
	if !strings.HasPrefix(s.Text, "Terminal too short for the dashboard") {
		t.Fatalf("status text %q", s.Text)
	}
	if s.Level != sharedcomponents.LevelWarning {
		t.Errorf("status level %v, want a warning", s.Level)
	}
}

// TestTheFloorIsTheOnlyWayToHideABox: TestNoFactIsUnreachableAtAnyPalier holds
// for every layout except the floor, which hides whole boxes and is the one
// state that says so in the footer (the exception the user accepted for
// DEVDESK-743).
func TestTheFloorIsTheOnlyWayToHideABox(t *testing.T) {
	m, _ := loadedModel(t)
	for _, width := range []int{80, 100, 120, 175, 200} {
		for height := 1; height <= 60; height++ {
			at := feed(t, m, tea.WindowSizeMsg{Width: width, Height: height})
			p := at.layout()
			sections := 0
			for _, col := range p.columns {
				sections += len(col)
			}
			closed, _ := closedBoxes(at.Render())
			hidden := closed < sections
			if hidden != p.floored {
				t.Errorf("%dx%d: %d of %d boxes shown, floored %v", width, height, closed, sections, p.floored)
			}
			if hidden && !strings.Contains(stripANSI(at.RenderFooter(width)), "too short") {
				t.Errorf("%dx%d: a box is hidden and the footer does not say why", width, height)
			}
		}
	}
}
