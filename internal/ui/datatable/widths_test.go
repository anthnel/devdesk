package datatable

import (
	"testing"
)

// The invariant this package exists to hold: the rendered line spans the
// viewport interior exactly, at every terminal width, so the selected row
// reaches the right border (Rule 116). Testing it once here is what replaces
// checking twelve hand-written copies in review.
//
// It is stated on the *rendered* span rather than on the sum of the widths,
// which is what D61 was: a column squeezed to zero is skipped by render.go —
// header, cell and padding — while its two padding cells had already been taken
// out of the budget. The widths summed correctly and the line still came up
// short. Counting the padding of the columns that survive is the only form of
// the invariant that can tell the difference.

// fixed is a column of exactly this width.
func fixed(width int) Column[string] {
	return Column[string]{Sizing: SizingFixed, MinWidth: width}
}

// content is a column that follows its content, floored at minWidth.
func content(minWidth, flex int) Column[string] {
	return Column[string]{Sizing: SizingContent, MinWidth: minWidth, Flex: flex}
}

// optional marks a column as droppable.
func optional(c Column[string]) Column[string] {
	c.Optional = true
	return c
}

// titled names a column, and sorts by it when asked — which is what makes the
// arrow reserve bite.
func titled(c Column[string], title string, sortable bool) Column[string] {
	c.Title = title
	if sortable {
		c.Less = func(a, b string) bool { return a < b }
	}
	return c
}

// inventoryShape reproduces the security inventory: four narrow sortable count
// columns whose headers are wider than the width they ask for.
func inventoryShape() []Column[string] {
	return []Column[string]{
		titled(content(24, 1), "Target", true),
		titled(fixed(5), "CRIT", true), titled(fixed(5), "HIGH", true),
		titled(fixed(5), "MED", true), titled(fixed(5), "LOW", true),
		titled(fixed(14), "Scanned", true),
	}
}

// workspacesShape reproduces the layout that overflowed before this package
// existed: eleven columns, 105 of fixed width, and two of them clamping after
// the remainder is computed.
func workspacesShape() []Column[string] {
	return []Column[string]{
		content(24, 3), fixed(10), fixed(15), fixed(8), fixed(8),
		fixed(8), fixed(8), fixed(8), fixed(8), fixed(8), fixed(8),
	}
}

// interfacesShape is the one §3.49 wrote a cliff about: two address columns
// sharing the flex, and two counter columns that are exact and expendable.
func interfacesShape() []Column[string] {
	return []Column[string]{
		content(16, 0), fixed(7), fixed(6), fixed(17),
		optional(fixed(8)), optional(fixed(8)),
		content(15, 1), content(15, 1),
	}
}

func sum(widths []int) int {
	total := 0
	for _, w := range widths {
		total += w
	}
	return total
}

// renderedSpan is what the header line will actually measure: every surviving
// column plus the padding bubbles puts around it. It is contentWidth() from
// render.go, expressed on the solver's output.
func renderedSpan(widths []int) (span, kept int) {
	for _, w := range widths {
		if w > 0 {
			span += w + cellPadding
			kept++
		}
	}
	return span, kept
}

func allLayouts() map[string][]Column[string] {
	return map[string][]Column[string]{
		"one flexible column":     {content(20, 1)},
		"fixed and flexible":      {fixed(16), content(20, 1), fixed(10)},
		"two flexible columns":    {content(10, 1), content(10, 3)},
		"all fixed":               {fixed(12), fixed(8), fixed(30)},
		"the workspaces shape":    workspacesShape(),
		"the interfaces shape":    interfacesShape(),
		"a column asking for one": {fixed(1), content(60, 1)},
		// The arrow reserve raises what a column asks for, so it is another way
		// to push the total past what is available — Rule 116 has to survive it.
		"the inventory shape": inventoryShape(),
	}
}

func TestTheRenderedLineAlwaysSpansTheViewportInterior(t *testing.T) {
	for name, columns := range allLayouts() {
		for width := 0; width <= 200; width++ {
			widths := solveWidths(columns, width, nil)
			span, kept := renderedSpan(widths)

			if kept > 0 && span != width-borderWidth {
				t.Fatalf("%s at width %d: the line spans %d, want %d (%v)",
					name, width, span, width-borderWidth, widths)
			}
			if span > max(width-borderWidth, 0) {
				t.Fatalf("%s at width %d: the line spans %d, past the interior (%v)",
					name, width, span, widths)
			}
			for i, w := range widths {
				if w < 0 {
					t.Fatalf("%s at width %d: column %d is %d wide", name, width, i, w)
				}
			}
		}
	}
}

// A column is removed whole or it stays readable; it is never kept at nothing.
// That is the other half of D61, and the reason MinWidth can be a floor at all.
func TestAColumnIsRemovedRatherThanEmptied(t *testing.T) {
	for name, columns := range allLayouts() {
		for width := 0; width <= 200; width++ {
			widths := solveWidths(columns, width, nil)
			if _, kept := renderedSpan(widths); kept < 2 {
				continue // the last column left may go under anything
			}
			for i, w := range widths {
				if w < 0 || (w == 0) {
					continue // dropped, which is the legitimate answer
				}
				if w < 1 {
					t.Fatalf("%s at width %d: column %d survives at %d cells", name, width, i, w)
				}
			}
		}
	}
}

// The measured content is what a SizingContent column asks for — this is the
// ports table at 200 columns, where Peer Address stayed frozen at its declared
// 26 and cut an IPv6 endpoint in half while Process took 110 cells for
// "svchost.exe".
func TestAContentColumnFollowsItsWidestValue(t *testing.T) {
	columns := []Column[string]{content(26, 0), content(10, 1)}

	widths := solveWidths(columns, 100, []int{45, 11})

	if widths[0] != 45 {
		t.Errorf("the content column is %d wide, want its measured 45 (%v)", widths[0], widths)
	}
	if widths[1] <= 11 {
		t.Errorf("widths = %v, want the flexible column to still absorb the surplus", widths)
	}
}

// A fixed column is fixed: one long value in it does not stretch it.
func TestAFixedColumnIgnoresTheMeasurement(t *testing.T) {
	widths := solveWidths([]Column[string]{fixed(6), content(10, 1)}, 100, []int{40, 12})

	if widths[0] != 6 {
		t.Errorf("the fixed column is %d wide, want the 6 it declared (%v)", widths[0], widths)
	}
}

// MaxWidth is the ceiling on what the content may ask for.
func TestMaxWidthCapsTheContent(t *testing.T) {
	capped := content(10, 0)
	capped.MaxWidth = 20

	widths := solveWidths([]Column[string]{capped, content(10, 1)}, 120, []int{80, 10})

	if widths[0] != 20 {
		t.Errorf("the capped column is %d wide, want 20 (%v)", widths[0], widths)
	}
}

// A ceiling under the floor is a contradictory declaration, and the floor wins:
// a column narrower than its own header says less than nothing.
func TestACeilingUnderTheFloorLosesToTheFloor(t *testing.T) {
	silly := content(12, 0)
	silly.MaxWidth = 4

	widths := solveWidths([]Column[string]{silly, content(10, 1)}, 120, []int{80, 10})

	if widths[0] != 12 {
		t.Errorf("the column is %d wide, want its floor of 12 (%v)", widths[0], widths)
	}
}

// An unmeasured content column is not a zero-width one: it falls back to what
// it declared, which is what a table renders before its first measurement.
func TestAnUnmeasuredContentColumnKeepsItsMinimum(t *testing.T) {
	widths := solveWidths([]Column[string]{content(20, 0), fixed(10)}, 60, nil)

	if widths[0] != 20 {
		t.Errorf("the unmeasured column is %d wide, want its declared 20 (%v)", widths[0], widths)
	}
}

// The content columns give way before anything is removed, and the fixed ones
// keep their exact width while they do.
func TestTheShortfallComesOutOfTheContentColumnsFirst(t *testing.T) {
	columns := []Column[string]{content(10, 1), fixed(10)}

	widths := solveWidths(columns, 40, []int{30, 0}) // wants 30+10, has 34

	if widths[1] != 10 {
		t.Errorf("the fixed column lost %d while a content one still had slack", 10-widths[1])
	}
	if widths[0] != 24 {
		t.Errorf("widths = %v, want the content column squeezed to 24", widths)
	}
}

// Past their floors, whole columns go — and the Optional ones go first, even
// when a non-optional one is wider. That is the interfaces shape: dropping the
// two counter columns hands 20 cells back to the addresses.
func TestTheOptionalColumnsGoFirst(t *testing.T) {
	columns := []Column[string]{content(20, 1), optional(fixed(8)), fixed(12)}

	widths := solveWidths(columns, 40, nil)

	if widths[1] != 0 {
		t.Fatalf("widths = %v, want the optional column dropped", widths)
	}
	if widths[2] != 12 {
		t.Errorf("widths = %v, want the fixed column kept at its width", widths)
	}
}

// A DropFirst column goes before every other Optional one, however far left it
// sits. That is the whole of §3.71's third option: a gauge sits beside the
// number it draws, in the middle of the table, and its position therefore says
// nothing about what it is worth.
func TestADropFirstColumnGoesBeforeTheOptionalOnesToItsRight(t *testing.T) {
	dropFirst := func(c Column[string]) Column[string] {
		c.Optional, c.DropFirst = true, true
		return c
	}
	columns := []Column[string]{content(20, 1), dropFirst(fixed(6)), optional(fixed(9)), fixed(12)}

	widths := solveWidths(columns, 50, nil)

	if widths[1] != 0 {
		t.Fatalf("widths = %v, want the DropFirst column dropped first", widths)
	}
	if widths[2] != 9 {
		t.Errorf("widths = %v, want the plain optional column kept — it goes second, not first", widths)
	}
}

// And once it is gone the ordinary right-to-left order resumes: DropFirst
// promotes a column in the queue, it does not exempt the others.
func TestOnceTheDropFirstColumnsAreGoneTheOrdinaryOrderResumes(t *testing.T) {
	dropFirst := func(c Column[string]) Column[string] {
		c.Optional, c.DropFirst = true, true
		return c
	}
	columns := []Column[string]{content(20, 1), dropFirst(fixed(6)), optional(fixed(9)), optional(fixed(9))}

	widths := solveWidths(columns, 40, nil)

	if widths[1] != 0 || widths[3] != 0 {
		t.Fatalf("widths = %v, want the DropFirst column then the rightmost optional one dropped", widths)
	}
	if widths[2] != 9 {
		t.Errorf("widths = %v, want the middle optional column still there", widths)
	}
}

// Removing a column hands its two padding cells back to the others. Without
// that the rendered line is two cells short per dropped column, which is D61.
func TestARemovedColumnHandsBackItsPadding(t *testing.T) {
	columns := []Column[string]{content(20, 1), optional(fixed(8)), fixed(12)}
	const width = 40

	widths := solveWidths(columns, width, nil)

	span, kept := renderedSpan(widths)
	if kept != 2 {
		t.Fatalf("widths = %v, want two columns kept", widths)
	}
	if span != width-borderWidth {
		t.Errorf("the line spans %d against an interior of %d — D61 is back", span, width-borderWidth)
	}
}

// When only columns nobody marked optional are left, they are removed anyway,
// right to left. Fewer columns that are right beats every column wrong: the
// alternative renders a count of 142 as "14…", and nothing on screen
// distinguishes that from a small number.
func TestTheColumnsNobodyMarkedOptionalGoTooRatherThanLie(t *testing.T) {
	columns := []Column[string]{fixed(20), fixed(20), fixed(20)}

	widths := solveWidths(columns, 24, nil)

	if widths[0] != 20 || widths[1] != 0 || widths[2] != 0 {
		t.Errorf("widths = %v, want the rightmost columns removed and the first kept whole", widths)
	}
}

// The first column is never removed: a table with no columns at all is not a
// narrower table, it is a blank one.
func TestTheFirstColumnIsNeverRemoved(t *testing.T) {
	widths := solveWidths([]Column[string]{optional(fixed(20)), optional(fixed(20))}, 12, nil)

	if widths[0] == 0 {
		t.Errorf("widths = %v, want the first column kept whatever it costs", widths)
	}
}

// A terminal too narrow for the borders and the padding alone has no room to
// give, and the answer has to be zeros rather than negative widths.
func TestNoRoomAtAllGivesZeroWidths(t *testing.T) {
	widths := solveWidths([]Column[string]{content(10, 1), fixed(10)}, 3, nil)

	if sum(widths) != 0 {
		t.Errorf("widths = %v, want all zero", widths)
	}
}

func TestNoColumnsIsNotACrash(t *testing.T) {
	if got := solveWidths[string](nil, 80, nil); len(got) != 0 {
		t.Errorf("solveWidths(nil) = %v", got)
	}
}

// The leftover goes to the flexible columns in proportion to their weight.
func TestTheSurplusIsSharedByWeight(t *testing.T) {
	columns := []Column[string]{content(10, 1), content(10, 3), fixed(10)}

	widths := solveWidths(columns, 78, nil) // 30 asked for, 40 to share

	if widths[2] != 10 {
		t.Errorf("the fixed column is %d wide, want its %d", widths[2], 10)
	}
	if widths[0] != 20 || widths[1] != 40 {
		t.Errorf("widths = %v, want the leftover split 1:3 on top of the minimums", widths)
	}
}

// With nothing flexible the last column absorbs the remainder, which is what
// every view did by hand and what keeps the selected row reaching the border.
func TestWithNothingFlexibleTheLastColumnAbsorbs(t *testing.T) {
	widths := solveWidths([]Column[string]{fixed(10), fixed(10)}, 41, nil)

	if widths[0] != 10 || widths[1] != 25 {
		t.Errorf("widths = %v, want the remainder on the last column", widths)
	}
}

// Reclaiming from the widest first is what stops one column being emptied while
// another stays long.
func TestShrinkingLevelsTheColumnsRatherThanEmptyingOne(t *testing.T) {
	columns := []Column[string]{content(4, 1), content(4, 1)}

	widths := solveWidths(columns, 30, []int{40, 10}) // 50 wanted, 24 available

	if widths[1] < 10 {
		t.Errorf("widths = %v, want the long column to give way before the short one is cut", widths)
	}
	if widths[0] < widths[1] {
		t.Errorf("widths = %v, want the wider column to still be the wider one", widths)
	}
}

// A negative MinWidth is a typo. It is read as the one cell every kept column
// gets, not as an offer of width to the others: a column kept at nothing is
// exactly the state D61 came out of.
func TestANegativeMinimumIsReadAsOneCell(t *testing.T) {
	columns := []Column[string]{fixed(-5), content(10, 1)}
	const width = 40

	widths := solveWidths(columns, width, nil)

	if widths[0] != 1 {
		t.Errorf("widths = %v, want the negative minimum read as one cell", widths)
	}
	if span, _ := renderedSpan(widths); span != width-borderWidth {
		t.Errorf("the line spans %d against an interior of %d", span, width-borderWidth)
	}
}

// The reserve is exactly what the header needs and no more: a sortable column
// wide enough already is not widened, and one that is not gets the two cells.
func TestTheArrowReserveOnlyRaisesWhatIsTooNarrow(t *testing.T) {
	tests := []struct {
		name   string
		column Column[string]
		want   int
	}{
		{"too narrow for its arrow", titled(fixed(5), "CRIT", true), 6},
		{"exactly wide enough", titled(fixed(6), "CRIT", true), 6},
		{"already wider", titled(fixed(20), "CRIT", true), 20},
		{"a one-letter header", titled(fixed(4), "C", true), 4},
		{"not sortable at all", titled(fixed(5), "CRIT", false), 5},
	}

	for _, tt := range tests {
		t.Run(tt.name, func(t *testing.T) {
			if got := askFor(tt.column); got != tt.want {
				t.Errorf("askFor(%q, min %d) = %d, want %d",
					tt.column.Title, tt.column.MinWidth, got, tt.want)
			}
		})
	}
}
