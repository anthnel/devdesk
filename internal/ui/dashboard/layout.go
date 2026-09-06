package dashboard

import "github.com/anthnel/devdesk/internal/ui/theme"

// tier is the dashboard's layout tier. A terminal doesn't know it's 4K: it
// knows columns and rows, and two font sizes on the same screen make two
// terminals. The tier is therefore computed from tea.WindowSizeMsg, and on
// both dimensions separately — width decides the number of columns, height
// decides what fits.
type tier int

const (
	tierCompact tier = iota
	tierStandard
	tierWide
)

// Tier thresholds. Below `standard` the dashboard stacks; at `wide` it opens
// a third column, because two columns of a 240-cell terminal give 118-cell
// boxes to write "MRs 3 assigned" in.
//
// **The number of columns depends only on width, and a test enforced that.**
// Reducing the columns when the terminal gets shorter is backwards: fewer
// columns means more stacked boxes, hence *more* height needed. At twelve
// lines of content, a 2×2 grid loses six of them and a single column loses
// twenty-four. Height therefore only governs `wide`, whose braille charts are
// genuinely taller.
//
// Height is in *content* lines, not terminal lines: the view receives what
// the router leaves it — header 9, title line 1, footer 3.
const (
	standardMinWidth = 100
	wideMinWidth     = 180
	// The three-column grid stacks three rows and its boxes carry trees: its
	// skeleton — without a single curve — comes to about forty lines. Below
	// that, two columns lose far fewer, which
	// TestTheChosenPalierLosesTheFewestLines checks at every height.
	wideMinHeight = 42
)

// gridHeight is what a two-row grid of boxes needs, in content lines.
//
// **The router's viewport does not scroll**: nothing forwards a key to it, it
// just cuts off. A layout that's too tall therefore does not bring up a
// scrollbar, it silently loses lines — which §3.19 fixes elsewhere. Below
// gridHeight() no layout fits, and the tier no longer picks the best one but
// the least bad one.
// It is **measured**, not computed: a constant describing box height goes
// stale the moment a line is added, and that happened three times in two
// days — the certificate deadline, the trees, the blank line under each
// chart.
func (m Model) gridHeight(t tier) int {
	columns := m.columnsFor(t)
	total := leadingBlank
	for _, height := range m.innerHeights(columns, t.columnWidth(m.width), t) {
		total += height + theme.BoxChrome
	}
	return total
}

// leadingBlank is the empty line between the title rule and the first row.
const leadingBlank = 1

// trailingBlank is the empty line every box keeps under its last fact.
//
// It is added to the **row's** height, not to each section's rendering:
// padTo then fills up to that height, so the tallest box in the row gets
// exactly one and the others get more. A line added by each render() would
// have the same visual effect on the tallest box and one too many everywhere
// else.
//
// Without it, the tallest box's last value touches its bottom border — and
// that's precisely the box the eye reads first.
const trailingBlank = 1

// Box geometry. Stacked boxes are not separated by a blank line: their
// borders take care of that, and at 30 lines the budget is exactly two boxes
// (2 × (6 + 2) = 16).
//
// nominalInnerHeight only serves to express the tier thresholds: a box's
// actual height is *derived from the sections on screen* (see innerHeight in
// view.go), so a section that grows cannot be truncated by a constant that
// nobody thought to keep in sync.
const (
	nominalInnerHeight = 6
	columnGap          = 1
	sidePadding        = 1
)

// layoutTier is the only thing that decides a tier. No renderer computes its
// own: two tier rules, and the boxes of the same grid stop agreeing on their
// height.
func layoutTier(width, height int) tier {
	switch {
	case width >= wideMinWidth && height >= wideMinHeight:
		return tierWide
	case width >= standardMinWidth:
		return tierStandard
	default:
		return tierCompact
	}
}

// columns returns how many columns of boxes the palier lays out.
func (t tier) columns() int {
	switch t {
	case tierWide:
		return 3
	case tierStandard:
		return 2
	default:
		return 1
	}
}

// tabCountFor returns how many tabs the tier offers. At `wide`, the third
// column already carries the Resources boxes: offering the tab as well would
// display the same three boxes twice, which is exactly the duplication a tab
// is supposed to avoid.
//
// So there is no tab bar at `wide`: a single tab is not a choice, and the bar
// would say "you are here", which the screen already says.
//
// This requires `App.resize` to iterate: it asks the view for its footer's
// height before handing it its new size, so a bar that appears with the tier
// would answer based on the previous size. Two passes are enough, and the
// second queries a view that knows its size.
func tabCountFor(t tier) int {
	if t == tierWide {
		return 1
	}
	return int(tabCount)
}

// columnWidth returns the width of one box column for a total view width.
func (t tier) columnWidth(width int) int {
	n := t.columns()
	available := width - 2*sidePadding - columnGap*(n-1)
	return max(available/n, minColumnWidth)
}

// minColumnWidth is the narrowest column that still holds a label and a value.
const minColumnWidth = 24
