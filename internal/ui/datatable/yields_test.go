package datatable

import (
	"slices"
	"testing"
)

// DEVDESK-128 (DEVDESK-98 step 4): an Optional column that carries
// YieldsToContent is removed before a required content column is cut below its
// widest value, but only while it is the column drop() would remove next.

// yields marks a column as Optional and willing to give way to content.
func yields(c Column[string]) Column[string] {
	c.Optional = true
	c.YieldsToContent = true
	return c
}

// dropFirst marks a column DropFirst (it must already be Optional).
func dropFirst(c Column[string]) Column[string] {
	c.DropFirst = true
	return c
}

func TestAYieldingColumnGoesBeforeRequiredContentIsCut(t *testing.T) {
	columns := []Column[string]{content(10, 1), yields(fixed(8)), fixed(5)}

	got := solveWidths(columns, 47, []int{30})

	if want := []int{36, 0, 5}; !slices.Equal(got, want) {
		t.Errorf("widths = %v, want %v (the content keeps its 30 cells, the surplus goes to it)", got, want)
	}
}

func TestWithoutYieldsToContentTheContentIsCutFirst(t *testing.T) {
	columns := []Column[string]{content(10, 1), optional(fixed(8)), fixed(5)}

	got := solveWidths(columns, 47, []int{30})

	if want := []int{26, 8, 5}; !slices.Equal(got, want) {
		t.Errorf("widths = %v, want %v", got, want)
	}
}

func TestOptionalContentIsCutBeforeAYieldingColumnGoes(t *testing.T) {
	columns := []Column[string]{
		content(10, 0), optional(content(12, 1)), yields(fixed(8)), fixed(5),
	}
	tests := []struct {
		name  string
		width int
		want  []int
	}{
		{"only the optional content is cut", 57, []int{20, 14, 8, 5}},
		{"optional content at its floor, the yielding column goes, then the ordinary rule", 51, []int{19, 19, 0, 5}},
	}
	for _, tt := range tests {
		t.Run(tt.name, func(t *testing.T) {
			got := solveWidths(columns, tt.width, []int{20, 20})
			if !slices.Equal(got, tt.want) {
				t.Errorf("widths at %d = %v, want %v", tt.width, got, tt.want)
			}
		})
	}
}

func TestYieldingStopsAtTheFirstColumnThatDoesNotYield(t *testing.T) {
	columns := []Column[string]{content(10, 1), optional(fixed(6)), yields(fixed(8)), fixed(5)}
	tests := []struct {
		width int
		want  []int
	}{
		{55, []int{36, 6, 0, 5}}, // the yielding column goes, the surplus feeds the content
		{47, []int{28, 6, 0, 5}}, // then the plain Optional is next: the content is cut as today
	}
	checked := 0
	for _, tt := range tests {
		got := solveWidths(columns, tt.width, []int{30})
		if !slices.Equal(got, tt.want) {
			t.Errorf("widths at %d = %v, want %v", tt.width, got, tt.want)
		}
		checked++
	}
	if checked != 2 {
		t.Fatalf("checked %d widths, want 2", checked)
	}
}

func TestAYieldingColumnThatIsNotNextHasNoEffect(t *testing.T) {
	// The rightmost Optional is the plain one, so it is the next to go: the flag
	// on the earlier column is not reached (prefix rule) and the content is cut.
	columns := []Column[string]{content(10, 1), yields(fixed(8)), optional(fixed(6)), fixed(5)}

	got := solveWidths(columns, 55, []int{30})

	if want := []int{26, 8, 6, 5}; !slices.Equal(got, want) {
		t.Errorf("widths = %v, want %v", got, want)
	}
}

func TestYieldsToContentWithoutOptionalIsIgnored(t *testing.T) {
	flagged := fixed(8)
	flagged.YieldsToContent = true
	columns := []Column[string]{content(10, 1), flagged, fixed(5)}

	got := solveWidths(columns, 47, []int{30})

	if want := []int{26, 8, 5}; !slices.Equal(got, want) {
		t.Errorf("widths = %v, want %v", got, want)
	}
}

func TestDropFirstOrderDecidesWhichColumnYields(t *testing.T) {
	// The DropFirst column sits left of the plain yielding one but goes first.
	columns := []Column[string]{content(10, 1), dropFirst(yields(fixed(8))), yields(fixed(6)), fixed(5)}

	got := solveWidths(columns, 55, []int{30})

	// avail 45, wants 49: the DropFirst column (index 1) yields; 3 columns have
	// 47, wants 41, surplus 6 to the content.
	if want := []int{36, 0, 6, 5}; !slices.Equal(got, want) {
		t.Errorf("widths = %v, want %v", got, want)
	}
}

// flagless behaviour, pinned to the values the solver gives today.
func TestFlagOffKeepsTodaysLayouts(t *testing.T) {
	tests := []struct {
		name    string
		columns []Column[string]
		width   int
		meas    []int
		want    []int
	}{
		{"optional kept, content cut", []Column[string]{content(10, 1), optional(fixed(8)), fixed(5)}, 47, []int{30}, []int{26, 8, 5}},
		{"optional content then yielding-less drop", []Column[string]{content(10, 0), optional(content(12, 1)), optional(fixed(8)), fixed(5)}, 57, []int{20, 20}, []int{17, 17, 8, 5}},
		{"narrower", []Column[string]{content(10, 0), optional(content(12, 1)), optional(fixed(8)), fixed(5)}, 51, []int{20, 20}, []int{14, 14, 8, 5}},
		{"plain optional stops", []Column[string]{content(10, 1), optional(fixed(6)), optional(fixed(8)), fixed(5)}, 55, []int{30}, []int{26, 6, 8, 5}},
		{"plain optional stops, narrower", []Column[string]{content(10, 1), optional(fixed(6)), optional(fixed(8)), fixed(5)}, 47, []int{30}, []int{18, 6, 8, 5}},
	}
	for _, tt := range tests {
		t.Run(tt.name, func(t *testing.T) {
			if got := solveWidths(tt.columns, tt.width, tt.meas); !slices.Equal(got, tt.want) {
				t.Errorf("widths = %v, want %v", got, tt.want)
			}
		})
	}
}

// Setting the flag where it cannot act (no Optional) never changes a layout.
func TestFlagWithoutOptionalChangesNothingAtAnyWidth(t *testing.T) {
	compared := 0
	for name, columns := range allLayouts() {
		flagged := slices.Clone(columns)
		for i := range flagged {
			if !flagged[i].Optional {
				flagged[i].YieldsToContent = true
			}
		}
		for width := 0; width <= 200; width++ {
			for _, meas := range [][]int{nil, {40, 40, 40, 40, 40, 40, 40, 40}} {
				if a, b := solveWidths(columns, width, meas), solveWidths(flagged, width, meas); !slices.Equal(a, b) {
					t.Fatalf("%s at %d: %v with the flag, %v without", name, width, b, a)
				}
				compared++
			}
		}
	}
	if compared == 0 {
		t.Fatal("nothing compared")
	}
}

// agentsLikeShape mirrors the agents table's drop order: the first five of the
// queue (indices 8, 2, 7, 6, 5 here) carry the flag, the rest of the Optional
// columns do not. Drop order: 8 (DropFirst), 2 (DropFirst), 7, 6, 5, then 4, 3.
func agentsLikeShape(flag bool) []Column[string] {
	mark := func(c Column[string]) Column[string] {
		c.YieldsToContent = flag
		return c
	}
	return []Column[string]{
		content(14, 0),                     // 0 Agent
		content(4, 0),                      // 1 ID
		dropFirst(mark(yields(fixed(9)))),  // 2 Est. cost
		optional(fixed(7)),                 // 3 State
		optional(fixed(5)),                 // 4 Tools
		mark(yields(fixed(10))),            // 5 Breakdown
		mark(yields(fixed(6))),             // 6 In
		mark(yields(fixed(10))),            // 7 Cache read
		dropFirst(mark(yields(fixed(11)))), // 8 Cache write
		fixed(6),                           // 9 Total
	}
}

// dropOrder is the documented order for agentsLikeShape (index 0 and 9 stay):
// DropFirst rightmost first, then Optional rightmost first.
var dropOrder = []int{8, 2, 7, 6, 5, 4, 3}

// removedOnly reports whether the zero-width columns are exactly a prefix of
// dropOrder, in that order.
func removedOnly(widths []int) bool {
	gone := 0
	for _, i := range dropOrder {
		if widths[i] != 0 {
			break
		}
		gone++
	}
	for k, i := range dropOrder {
		if (widths[i] == 0) != (k < gone) {
			return false
		}
	}
	return widths[0] > 0 && widths[1] > 0 && widths[9] > 0
}

func TestTheDropOrderIsTheSameWithAndWithoutTheFlag(t *testing.T) {
	for _, flag := range []bool{false, true} {
		checked := 0
		for width := 50; width <= 230; width++ {
			widths := solveWidths(agentsLikeShape(flag), width, []int{33, 8})
			if !removedOnly(widths) {
				t.Fatalf("flag=%v width %d: removed columns are not a prefix of the drop order: %v", flag, width, widths)
			}
			checked++
		}
		if checked != 181 {
			t.Fatalf("checked %d widths, want 181", checked)
		}
	}
}

// goneAt returns, for each column, the widest viewport at which it is absent.
func goneAt(flag bool) map[int]int {
	out := map[int]int{}
	for width := 50; width <= 230; width++ {
		for i, w := range solveWidths(agentsLikeShape(flag), width, []int{33, 8}) {
			if w == 0 {
				out[i] = width
			}
		}
	}
	return out
}

// The flag changes when a column goes, never whether it goes before another one:
// each column is gone at least as wide as before, and some strictly wider.
func TestTheFlagMovesTheMomentNotTheOrder(t *testing.T) {
	without, with := goneAt(false), goneAt(true)
	later := 0
	for _, i := range dropOrder {
		if with[i] < without[i] {
			t.Errorf("column %d is gone up to %d with the flag, up to %d without: it must not go later", i, with[i], without[i])
		}
		if with[i] > without[i] {
			later++
		}
	}
	if later == 0 {
		t.Error("the flag moved no column's removal: nothing yielded")
	}
	// The unflagged tail of the queue is untouched.
	for _, i := range []int{4, 3} {
		if with[i] != without[i] {
			t.Errorf("unflagged column %d moved from %d to %d", i, without[i], with[i])
		}
	}
}

// Rule 116 with the flag on, every width from 50 to 230 and several widths of
// content: the rendered span is exactly the interior.
func TestRule116HoldsWithYieldingColumns(t *testing.T) {
	checked, kept9 := 0, 0
	for _, meas := range [][]int{nil, {14, 4}, {18, 8}, {24, 8}, {33, 8}, {60, 20}} {
		for width := 50; width <= 230; width++ {
			widths := solveWidths(agentsLikeShape(true), width, meas)
			span, kept := renderedSpan(widths)
			if span != width-borderWidth {
				t.Fatalf("measured %v, width %d: the line spans %d, want %d (%v)", meas, width, span, width-borderWidth, widths)
			}
			if kept == 10 {
				kept9++
			}
			checked++
		}
	}
	if checked != 6*181 {
		t.Fatalf("checked %d, want %d", checked, 6*181)
	}
	if kept9 == 0 {
		t.Fatal("the full table never fits between 50 and 230: the sweep proves little")
	}
}

// The agent text stays whole while a yielding column is still there to go.
func TestTheContentIsNotCutWhileAYieldingColumnRemains(t *testing.T) {
	checked := 0
	for width := 50; width <= 230; width++ {
		widths := solveWidths(agentsLikeShape(true), width, []int{33, 8})
		yieldingLeft := false
		for _, i := range []int{8, 2, 7, 6, 5} {
			if widths[i] > 0 {
				yieldingLeft = true
			}
		}
		// Only meaningful while a yielding column is still shown.
		if yieldingLeft && widths[0] < 33 {
			t.Errorf("width %d: Agent cut to %d while a yielding column is still shown: %v", width, widths[0], widths)
		}
		if yieldingLeft {
			checked++
		}
	}
	if checked == 0 {
		t.Fatal("no width showed a yielding column")
	}
}
