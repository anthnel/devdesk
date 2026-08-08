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

// borderWidth is the viewport's two vertical borders.
const borderWidth = 2

// cellPadding is what bubbles/table adds per column — Padding(0, 1).
const cellPadding = 2

// availableFor returns the width the columns have to share, given the full
// viewport width. Negative when the terminal cannot even hold the borders and
// the padding, which the solver reads as "no room at all".
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
// "CRIT ▼" into it and lost exactly the character that says how it is sorted.
//
// The room is reserved for every sortable column, not only the one currently
// sorted: reserving it on demand would resize the column each time `.` moved
// the sort, shifting every column beside it.
func askFor[T any](c Column[T]) int {
	width := max(c.MinWidth, 0)
	if c.Less == nil {
		return width
	}
	return max(width, lipgloss.Width(c.Title)+sortArrowWidth)
}

// solveWidths distributes available across the columns.
//
// Fixed columns (Flex == 0) ask for MinWidth. Flexible ones ask for MinWidth
// and share whatever is left in proportion to their Flex weight. When there is
// not enough for everyone, the shortfall comes out of the flexible columns
// first and the fixed ones only after — a fixed column is fixed because its
// content has a known size, so it is the last thing worth truncating.
//
// The result always sums to exactly max(available, 0).
func solveWidths[T any](columns []Column[T], available int) []int {
	widths := make([]int, len(columns))
	if len(columns) == 0 || available <= 0 {
		return widths
	}

	total := 0
	for i, c := range columns {
		widths[i] = askFor(c)
		total += widths[i]
	}

	switch {
	case total < available:
		grow(columns, widths, available-total)
	case total > available:
		shrink(columns, widths, total-available)
	}
	return widths
}

// grow hands the surplus to the flexible columns by weight, giving the last one
// the rounding remainder so the sum is exact. With no flexible column the last
// column absorbs it, which is what every view did by hand.
func grow[T any](columns []Column[T], widths []int, surplus int) {
	weight := 0
	last := -1
	for i, c := range columns {
		if c.Flex > 0 {
			weight += c.Flex
			last = i
		}
	}
	if weight == 0 {
		widths[len(widths)-1] += surplus
		return
	}

	given := 0
	for i, c := range columns {
		if c.Flex <= 0 || i == last {
			continue
		}
		share := surplus * c.Flex / weight
		widths[i] += share
		given += share
	}
	widths[last] += surplus - given
}

// shrink reclaims the shortfall, taking from the flexible columns before the
// fixed ones and from the widest before the narrowest, so that one long column
// gives way before several short ones become unreadable.
func shrink[T any](columns []Column[T], widths []int, shortfall int) {
	shortfall = takeFrom(widths, order(columns, true), shortfall)
	takeFrom(widths, order(columns, false), shortfall)
}

// order returns the column indices to reclaim from, widest first, restricted to
// the flexible ones or the fixed ones.
func order[T any](columns []Column[T], flexible bool) []int {
	var idx []int
	for i, c := range columns {
		if (c.Flex > 0) == flexible {
			idx = append(idx, i)
		}
	}
	return idx
}

// takeFrom removes shortfall from the given columns one unit at a time, always
// from the widest, and returns what could not be taken. One unit at a time
// keeps it exact and level: no column is emptied while another stays wide.
func takeFrom(widths []int, idx []int, shortfall int) int {
	for shortfall > 0 {
		widest, at := 0, -1
		for _, i := range idx {
			if widths[i] > widest {
				widest, at = widths[i], i
			}
		}
		if at < 0 {
			return shortfall // nothing left with any width to give
		}
		widths[at]--
		shortfall--
	}
	return 0
}
