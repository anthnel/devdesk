package datatable

import "github.com/charmbracelet/lipgloss"

// Rule 116: the column widths must sum to exactly the space available, or the
// selected row stops short of the right viewport border.
//
// Every view enforced that the same way — last column absorbs the remainder —
// and several then applied a per-column `max(…, floor)` *after* the remainder
// was computed, which silently pushes the sum back over. `workspaces` overflows
// by 34 columns on a 120-wide terminal that way.
//
// The fix is not a better clamp. It is to distribute the shortfall across the
// columns instead of letting each one defend its own floor: a floor that cannot
// be honoured has to be given up by somebody, and deciding that here is what
// makes the invariant hold at every width rather than most of them.
//
// # How the shortfall is given up (§3.45)
//
// It used to be given up by the flexible columns, one cell at a time, with no
// floor at all: a column reached zero and vanished, and MinWidth turned out to
// be a request rather than a minimum. Two things were wrong with that. A column
// that renders nothing still had its two padding cells subtracted from the
// budget, so the rendered line came up short of the viewport interior — Rule 116
// in default, which is D61. And what to give up was decided by Flex, a number
// about *surplus*, which says nothing about whether the column can be truncated
// at all.
//
// So the column declares instead. Sizing says whether the width is exact or
// follows the content; Optional says whether the column can be dropped. The
// degradation reads:
//
//  1. the SizingContent columns shrink towards their MinWidth, truncating;
//  2. then whole columns are removed, the rightmost first, and the budget is
//     solved again — a removed column hands back its two padding cells, which is
//     what closes D61;
//  3. the Optional ones go first; when only columns nobody marked optional are
//     left, they go too, still right to left.
//
// Point 3 makes the declaration a preference and not a guarantee, deliberately:
// fewer columns that are right beats every column wrong. The alternative —
// truncating the exact-width columns as a last resort — renders 142 as "14…",
// and nothing on screen distinguishes a truncated number from a small one.

// borderWidth is the viewport's two vertical borders.
const borderWidth = 2

// cellPadding is what bubbles/table adds per column — Padding(0, 1).
const cellPadding = 2

// IconColumnWidth is the width of a first column that carries a glyph and
// nothing else: the glyph, plus one cell so it never touches the text beside
// it. Two, not one — a Nerd Font glyph renders at double width on some
// terminals and single on others, and one cell would clip it wherever it
// renders wide.
//
// It is exported because it is the same column in three tables, and a magic 2
// repeated in three packages is a number three places are free to disagree
// about. TestAnIconColumnIsUntitledAndTwoCellsWide reads the sources for the
// ones that do.
const IconColumnWidth = 2

// availableFor returns the width the columns have to share, given the full
// viewport width and the number of columns that will actually be rendered.
// Negative when the terminal cannot even hold the borders and the padding,
// which the solver reads as "no room at all".
//
// The count is the *kept* columns, not the declared ones. render.go skips a
// zero-width column entirely — no header cell, no padding — so counting a
// column that will not be drawn is what left the rendered line two cells short
// per dropped column (D61).
func availableFor(viewportWidth, numColumns int) int {
	return viewportWidth - borderWidth - numColumns*cellPadding
}

// sortArrowWidth is what titleFor appends to the sorted column's header.
// Measured rather than assumed: the arrows are not ASCII, and bubbles/table
// truncates the header with the same rune widths lipgloss counts.
var sortArrowWidth = max(lipgloss.Width(sortArrowAsc), lipgloss.Width(sortArrowDesc))

// askFor is the width a column needs, which is not always the MinWidth it
// states.
//
// MinWidth is the view's statement about the column's *content*. A sortable
// column also carries a header this package widens by an arrow, and nothing
// told the view about those two cells — so a count column asking for 5 rendered
// "CRIT" plus an arrow into it and lost exactly the character that says how it
// is sorted.
//
// The room is reserved for every sortable column, not only the one currently
// sorted: reserving it on demand would resize the column each time `.` moved
// the sort, shifting every column beside it. It is reserved for a SizingFixed
// column too — a width stated narrower than its own sorted header is a defect
// in the declaration, not a case to handle.
func askFor[T any](c Column[T]) int {
	width := max(c.MinWidth, 0)
	if c.Less == nil {
		return width
	}
	return max(width, lipgloss.Width(c.Title)+sortArrowWidth)
}

// floorFor is what a column that is being *kept* may never go under.
//
// One cell, always, even for a column that asks for none. A kept column of zero
// width renders nothing while its padding has already been spent, which is
// exactly the shape of D61 — so "narrower than anything useful" and "not there"
// have to stay different states, and the second one is a removal.
func floorFor[T any](c Column[T]) int { return max(askFor(c), 1) }

// wantFor is the width a column would take if the terminal were generous.
//
// A SizingFixed column wants what it declared, whatever its content: a count, a
// state, an icon have a size known when the code was written, and measuring
// them would only let one long value stretch a column that never needs it. A
// SizingContent column wants its widest visible value, floored by MinWidth and
// capped by MaxWidth.
//
// SizingUnset behaves as SizingFixed here. It is refused by
// TestEveryColumnDeclaresItsSizing rather than by this function: a zero value
// that quietly means something is D12, and the point is that no column arrives
// here undeclared at all.
//
// A MaxWidth under the floor is a contradictory declaration; the floor wins,
// because a column narrower than its own header says less than nothing.
func wantFor[T any](c Column[T], measured int) int {
	floor := floorFor(c)
	if c.Sizing != SizingContent {
		return floor
	}
	want := max(measured, floor)
	if c.MaxWidth > 0 {
		want = min(want, max(c.MaxWidth, floor))
	}
	return want
}

// solveWidths lays the columns out in a viewport of this width, returning one
// width per declared column — zero for a column that had to be dropped.
//
// measured carries the widest content each column has produced, as the model
// last measured it; it may be shorter than columns, and a missing entry reads
// as no measurement, which leaves a SizingContent column at its MinWidth.
//
// The result always sums to exactly the room the kept columns have, so the
// rendered line spans the viewport interior at every width (Rule 116).
func solveWidths[T any](columns []Column[T], viewportWidth int, measured []int) []int {
	if len(columns) == 0 {
		return nil
	}

	kept := make([]int, len(columns))
	for i := range kept {
		kept[i] = i
	}

	for {
		available := availableFor(viewportWidth, len(kept))
		if available <= 0 {
			// Not even room for this many paddings. Dropping a column hands two
			// cells back, so the loop makes progress; with one column left
			// there is genuinely nothing to draw.
			if len(kept) == 1 {
				return make([]int, len(columns))
			}
			kept = drop(columns, kept)
			continue
		}

		widths := make([]int, len(columns))
		shortfall := fitInto(columns, kept, widths, available, measured)
		if shortfall == 0 {
			return widths
		}
		if len(kept) == 1 {
			// The end of the line: one column, still too wide. It gives up the
			// remainder itself, below its own declared minimum, because the
			// alternative is not rendering the table at all.
			takeFrom(widths, kept, shortfall)
			return widths
		}
		kept = drop(columns, kept)
	}
}

// fitInto solves the kept columns into available and returns what it could not
// reclaim without removing a column.
func fitInto[T any](columns []Column[T], kept, widths []int, available int, measured []int) int {
	total := 0
	for _, i := range kept {
		widths[i] = wantFor(columns[i], measureOf(measured, i))
		total += widths[i]
	}

	switch {
	case total < available:
		grow(columns, kept, widths, available-total)
	case total > available:
		return shrinkContent(columns, kept, widths, total-available)
	}
	return 0
}

// measureOf reads a measurement that may not have been taken. An unmeasured
// column is not a zero-width one: it falls back to what it declared.
func measureOf(measured []int, i int) int {
	if i < len(measured) {
		return measured[i]
	}
	return 0
}

// drop removes one column from the kept set: the rightmost Optional one, and
// only when there is none, the rightmost of all.
//
// Right to left because the column order is already an order of importance in
// every table here — the identifying column is first — so degrading from the
// right sheds the least. The first column is never dropped: a table with no
// columns at all is not a narrower table, it is a blank one.
func drop[T any](columns []Column[T], kept []int) []int {
	at := len(kept) - 1
	for i := len(kept) - 1; i > 0; i-- {
		if columns[kept[i]].Optional {
			at = i
			break
		}
	}
	return append(kept[:at:at], kept[at+1:]...)
}

// grow hands the surplus to the flexible columns by weight, giving the last one
// the rounding remainder so the sum is exact. With no flexible column the last
// kept column absorbs it, which is what every view did by hand.
//
// MaxWidth is not consulted here, and that is a decision: it caps what the
// *content* may ask for, not what the surplus may grant. Something has to
// absorb the remainder for Rule 116 to hold, and a column declaring both a
// ceiling and a Flex weight has asked for two incompatible things.
func grow[T any](columns []Column[T], kept, widths []int, surplus int) {
	weight := 0
	last := -1
	for _, i := range kept {
		if columns[i].Flex > 0 {
			weight += columns[i].Flex
			last = i
		}
	}
	if weight == 0 {
		widths[kept[len(kept)-1]] += surplus
		return
	}

	given := 0
	for _, i := range kept {
		if columns[i].Flex <= 0 || i == last {
			continue
		}
		share := surplus * columns[i].Flex / weight
		widths[i] += share
		given += share
	}
	widths[last] += surplus - given
}

// shrinkContent reclaims the shortfall from the SizingContent columns, widest
// first and one cell at a time, and never below their floor. It returns what is
// still missing, which is the caller's signal to remove a column instead.
//
// Widest first is what stops one column being emptied while another stays long;
// the floor is what stops it being emptied at all.
func shrinkContent[T any](columns []Column[T], kept, widths []int, shortfall int) int {
	for shortfall > 0 {
		widest, at := 0, -1
		for _, i := range kept {
			if columns[i].Sizing != SizingContent || widths[i] <= floorFor(columns[i]) {
				continue
			}
			if widths[i] > widest {
				widest, at = widths[i], i
			}
		}
		if at < 0 {
			return shortfall // every content column is at its floor
		}
		widths[at]--
		shortfall--
	}
	return 0
}

// takeFrom removes shortfall from the given columns one unit at a time, always
// from the widest, ignoring every floor. It is reached only with a single
// column left that still does not fit.
func takeFrom(widths []int, idx []int, shortfall int) {
	for shortfall > 0 {
		widest, at := 0, -1
		for _, i := range idx {
			if widths[i] > widest {
				widest, at = widths[i], i
			}
		}
		if at < 0 {
			return // nothing left with any width to give
		}
		widths[at]--
		shortfall--
	}
}
