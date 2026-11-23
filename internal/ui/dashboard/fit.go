package dashboard

import (
	"fmt"

	"github.com/anthnel/devdesk/internal/ui/shortcut"
	"github.com/anthnel/devdesk/internal/ui/theme"
)

// density is how much blank space the grid keeps (DEVDESK-743).
//
// The router's viewport does not scroll: a grid taller than its height loses
// its last lines, and those are the bottom borders of the lowest boxes. Before
// that happens, the grid gives up its blank lines, one kind at a time, in the
// order that costs the reading least. The choice is **measured**, like
// fitCharts: each density is rendered and the first that fits wins, so no
// threshold has to follow the boxes as they grow.
type density int

const (
	// densityRoomy keeps every blank line: the default, whenever it fits.
	densityRoomy density = iota
	// densityNoTrailing drops trailingBlank under the tallest box of a row.
	densityNoTrailing
	// densityNoTopPadding also drops the blank line under each box's title.
	densityNoTopPadding
	// densityTight also drops the blank line under each chart.
	densityTight
)

// trailing is the blank line a row keeps under its tallest box at this density.
func (d density) trailing() int {
	if d >= densityNoTrailing {
		return 0
	}
	return trailingBlank
}

// topPadding is the blank line under a box's titled border at this density.
func (d density) topPadding() int {
	if d >= densityNoTopPadding {
		return 0
	}
	return theme.BoxTopPadding
}

// chartGap reports whether a chart keeps its blank line at this density.
func (d density) chartGap() bool { return d < densityTight }

// boxChrome is what a box costs beyond its content: its two borders and its
// top padding.
func (d density) boxChrome() int { return 2 + d.topPadding() }

// routerRows is what the router keeps above the dashboard: the header rows and
// the blank line under them. The dashboard has no title line (TitlelessView).
const routerRows = shortcut.HeaderRows + 1

// plan is one frame's layout: what Render draws and what the footer says.
type plan struct {
	tier     tier
	colWidth int
	columns  [][]section
	// model carries the measured chartLines and density, on a copy.
	model   Model
	density density
	inner   []int
	// shownRows is how many rows of boxes are drawn: all of them, unless the
	// grid does not fit even at densityTight (floored).
	shownRows int
	floored   bool
	// need is the content height the chosen density takes.
	need int
}

// layout measures the frame. An unknown height (no WindowSizeMsg yet) is no
// constraint: the roomy grid, whole.
func (m Model) layout() plan {
	t := layoutTier(m.width, m.height)
	p := plan{tier: t, colWidth: t.columnWidth(m.width), columns: m.columnsFor(t)}

	m.density = densityRoomy
	m.chartLines = m.fitCharts(p.columns, p.colWidth, t)
	for {
		p.inner = m.innerHeights(p.columns, p.colWidth, t)
		p.need = m.stackHeight(p.inner)
		if m.height <= 0 || p.need <= m.height || m.density == densityTight {
			break
		}
		m.density++
	}
	p.model, p.density, p.shownRows = m, m.density, len(p.inner)

	if m.height > 0 && p.need > m.height {
		// The floor: whole rows of boxes, top first, as long as they fit.
		// Never a cut box: a box without its bottom border reads as one whose
		// content goes on below the screen.
		p.floored = true
		used := 0
		p.shownRows = 0
		for _, h := range p.inner {
			if used+h+m.density.boxChrome() > m.height {
				break
			}
			used += h + m.density.boxChrome()
			p.shownRows++
		}
	}
	return p
}

// stackHeight is what rows of boxes of these inner heights take, stacked.
func (m Model) stackHeight(inner []int) int {
	total := 0
	for _, h := range inner {
		total += h + m.density.boxChrome()
	}
	return total
}

// floorStatus is the footer line of the floor: how tall the terminal must be
// for the dashboard to show every box. It counts what the router keeps above
// and below the grid, so the figure is terminal rows.
func (m Model) floorStatus(p plan) string {
	return fmt.Sprintf("Terminal too short for the dashboard (need %d rows)",
		p.need+routerRows+m.GetFooterHeight())
}
