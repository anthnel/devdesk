package datatable

import (
	"testing"
)

// The invariant this package exists to hold: the widths sum to exactly what is
// available, at every terminal width, so the selected row reaches the right
// viewport border (Rule 116). Testing it once here is what replaces checking
// twelve hand-written copies in review.

// col is a column with only the fields the solver reads.
func col(minWidth, flex int) Column[string] {
	return Column[string]{MinWidth: minWidth, Flex: flex}
}

func sum(widths []int) int {
	total := 0
	for _, w := range widths {
		total += w
	}
	return total
}

func TestTheWidthsAlwaysSumToWhatIsAvailable(t *testing.T) {
	layouts := map[string][]Column[string]{
		"one flexible column":     {col(20, 1)},
		"fixed and flexible":      {col(16, 0), col(20, 1), col(10, 0)},
		"two flexible columns":    {col(10, 1), col(10, 3)},
		"all fixed":               {col(12, 0), col(8, 0), col(30, 0)},
		"the workspaces shape":    workspacesShape(),
		"a column asking for one": {col(1, 0), col(60, 1)},
	}

	// Every width from unusable to generous, including the ones the hand-written
	// clamps get wrong.
	for name, columns := range layouts {
		for width := 0; width <= 200; width++ {
			widths := solveWidths(columns, availableFor(width, len(columns)))
			want := max(availableFor(width, len(columns)), 0)

			if got := sum(widths); got != want {
				t.Fatalf("%s at width %d: the widths sum to %d, want %d (%v)", name, width, got, want, widths)
			}
			for i, w := range widths {
				if w < 0 {
					t.Fatalf("%s at width %d: column %d is %d wide", name, width, i, w)
				}
			}
		}
	}
}

// workspacesShape reproduces the layout that overflows today: eleven columns,
// 105 of fixed width, and two of them clamping after the remainder is computed.
// Below a 154-column terminal the hand-written version sums to 130 against a
// smaller available — 96 at width 120, an overflow of 34.
func workspacesShape() []Column[string] {
	return []Column[string]{
		col(24, 3), col(10, 0), col(15, 0), col(8, 0), col(8, 0),
		col(8, 0), col(8, 0), col(8, 0), col(8, 0), col(8, 0), col(8, 0),
	}
}

func TestTheWorkspacesShapeNoLongerOverflows(t *testing.T) {
	columns := workspacesShape()
	const width = 120

	widths := solveWidths(columns, availableFor(width, len(columns)))

	available := availableFor(width, len(columns))
	if got := sum(widths); got != available {
		t.Errorf("the widths sum to %d against an available of %d — the overflow is back", got, available)
	}
}

// A terminal too narrow for the borders and the padding alone has no room to
// give, and the answer has to be zeros rather than negative widths.
func TestNoRoomAtAllGivesZeroWidths(t *testing.T) {
	widths := solveWidths([]Column[string]{col(10, 1), col(10, 0)}, -4)

	if sum(widths) != 0 {
		t.Errorf("widths = %v, want all zero", widths)
	}
}

func TestNoColumnsIsNotACrash(t *testing.T) {
	if got := solveWidths[string](nil, 80); len(got) != 0 {
		t.Errorf("solveWidths(nil) = %v", got)
	}
}

// The leftover goes to the flexible columns in proportion to their weight.
func TestTheSurplusIsSharedByWeight(t *testing.T) {
	columns := []Column[string]{col(10, 1), col(10, 3), col(10, 0)}

	widths := solveWidths(columns, 70) // 30 asked for, 40 to share

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
	widths := solveWidths([]Column[string]{col(10, 0), col(10, 0)}, 35)

	if widths[0] != 10 || widths[1] != 25 {
		t.Errorf("widths = %v, want the remainder on the last column", widths)
	}
}

// A fixed column is fixed because its content has a known size, so it is the
// last thing worth truncating: the shortfall comes out of the flexible ones
// first, and only then out of the fixed.
func TestTheShortfallComesOutOfTheFlexibleColumnsFirst(t *testing.T) {
	columns := []Column[string]{col(30, 1), col(10, 0)}

	widths := solveWidths(columns, 25) // 15 short

	if widths[1] != 10 {
		t.Errorf("the fixed column lost %d while a flexible one still had width", 10-widths[1])
	}
	if widths[0] != 15 {
		t.Errorf("widths = %v, want the flexible column to have given up the shortfall", widths)
	}
}

// Once the flexible columns are exhausted the fixed ones have to give way too,
// or the sum cannot hold.
func TestTheFixedColumnsGiveWayWhenNothingElseIsLeft(t *testing.T) {
	columns := []Column[string]{col(4, 1), col(20, 0), col(20, 0)}

	widths := solveWidths(columns, 10)

	if got := sum(widths); got != 10 {
		t.Fatalf("the widths sum to %d, want 10", got)
	}
	if widths[0] != 0 {
		t.Errorf("widths = %v, want the flexible column emptied before the fixed ones", widths)
	}
}

// Reclaiming from the widest first is what stops one column being emptied while
// another stays long.
func TestShrinkingLevelsTheColumnsRatherThanEmptyingOne(t *testing.T) {
	columns := []Column[string]{col(40, 1), col(10, 1)}

	widths := solveWidths(columns, 20) // 30 short

	if widths[1] == 0 {
		t.Errorf("widths = %v, want the long column to give way before the short one is emptied", widths)
	}
	if widths[0] < widths[1] {
		t.Errorf("widths = %v, want the wider column to still be the wider one", widths)
	}
}

// A negative MinWidth is a typo, not an instruction to take width away from the
// other columns.
func TestANegativeMinimumIsReadAsZero(t *testing.T) {
	widths := solveWidths([]Column[string]{col(-5, 0), col(10, 1)}, 30)

	if widths[0] != 0 {
		t.Errorf("widths = %v, want the negative minimum read as none", widths)
	}
	if sum(widths) != 30 {
		t.Errorf("the widths sum to %d, want 30", sum(widths))
	}
}
