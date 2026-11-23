package datatable

import (
	"fmt"
	"slices"
	"strings"
	"testing"

	"github.com/charmbracelet/x/ansi"
)

// DEVDESK-750 (step 6): SetColumnHidden takes a column out of the layout
// without removing it from the configuration. It exists so the security view
// can own one File column and show it on one tab only.

const hiddenCell = "PATHCELL"

// hiddenColumn is the index of the column the tests hide.
const hiddenColumn = 1

// hiddenConfig is Name | Path | State: Path is optional, sorts, and is the one
// the tests hide. Name absorbs the surplus (Flex), so RenderedWidth must equal
// the viewport interior whenever the layout is right.
func hiddenConfig(sortColumn int) Config[row] {
	return Config[row]{
		SortColumn: sortColumn,
		Columns: []Column[row]{
			{Title: "Name", Sizing: SizingContent, MinWidth: 20, Flex: 1,
				Cell: func(r row) string { return r.Name },
				Less: func(a, b row) bool { return a.Name < b.Name }},
			{Title: "Path", Sizing: SizingContent, MinWidth: 16, MaxWidth: 40, Optional: true, TruncateHead: true,
				Cell: func(row) string { return hiddenCell },
				Less: func(a, b row) bool { return a.Size < b.Size }},
			{Title: "State", Sizing: SizingFixed, MinWidth: 12,
				Cell: func(r row) string { return r.State },
				Less: func(a, b row) bool { return a.State < b.State }},
		},
	}
}

func hiddenModel(width, sortColumn int) Model[row] {
	m := New(hiddenConfig(sortColumn))
	m.SetItems(fixtures())
	m.Resize(width, 10)
	return m
}

func header(m *Model[row]) string { return ansi.Strip(m.headerLine(m.table.Columns())) }

func widthsOf(m *Model[row]) []int {
	cols := m.table.Columns()
	out := make([]int, len(cols))
	for i, c := range cols {
		out[i] = c.Width
	}
	return out
}

func TestAHiddenColumnTakesNoSpace(t *testing.T) {
	for _, width := range []int{50, 80, 120, 175} {
		t.Run(fmt.Sprintf("width %d", width), func(t *testing.T) {
			m := hiddenModel(width, -1)
			m.SetColumnHidden(hiddenColumn, true)
			m.Resize(width, 10)

			if got := widthsOf(&m)[hiddenColumn]; got != 0 {
				t.Errorf("hidden column width = %d, want 0", got)
			}
			if h := header(&m); strings.Contains(h, "Path") {
				t.Errorf("the header still names the hidden column: %q", h)
			}
			if view := ansi.Strip(m.View()); strings.Contains(view, hiddenCell) {
				t.Errorf("a cell of the hidden column is rendered:\n%s", view)
			}
			if got, want := m.RenderedWidth(), width-borderWidth; got != want {
				t.Errorf("RenderedWidth = %d, want %d (the padding of the hidden column must be handed back)", got, want)
			}
			for i, line := range strings.Split(m.View(), "\n") {
				if got := ansi.StringWidth(line); got != m.RenderedWidth() {
					t.Errorf("line %d is %d cells, want %d", i, got, m.RenderedWidth())
				}
			}
		})
	}
}

func TestShowingAHiddenColumnAgainRestoresIt(t *testing.T) {
	for _, width := range []int{80, 120, 175} {
		t.Run(fmt.Sprintf("width %d", width), func(t *testing.T) {
			m := hiddenModel(width, -1)
			before := slices.Clone(widthsOf(&m))
			if before[hiddenColumn] == 0 {
				t.Fatalf("setup: the column is already absent at %d: %v", width, before)
			}

			m.SetColumnHidden(hiddenColumn, true)
			m.Resize(width, 10)
			m.SetColumnHidden(hiddenColumn, false)
			m.Resize(width, 10)

			if after := widthsOf(&m); !slices.Equal(before, after) {
				t.Errorf("widths = %v after hide and show, want %v", after, before)
			}
			if h := header(&m); !strings.Contains(h, "Path") {
				t.Errorf("the title did not come back: %q", h)
			}
			if !strings.Contains(ansi.Strip(m.View()), hiddenCell) {
				t.Error("the cells did not come back")
			}
			if got, want := m.RenderedWidth(), width-borderWidth; got != want {
				t.Errorf("RenderedWidth = %d, want %d", got, want)
			}
		})
	}
}

// Hiding twice, or showing a column never hidden, changes nothing.
func TestHidingIsIdempotent(t *testing.T) {
	m := hiddenModel(120, -1)
	want := slices.Clone(widthsOf(&m))

	m.SetColumnHidden(hiddenColumn, false)
	m.Resize(120, 10)
	if got := widthsOf(&m); !slices.Equal(got, want) {
		t.Errorf("showing a shown column: widths = %v, want %v", got, want)
	}

	m.SetColumnHidden(hiddenColumn, true)
	m.SetColumnHidden(hiddenColumn, true)
	m.Resize(120, 10)
	m.SetColumnHidden(hiddenColumn, false)
	m.Resize(120, 10)
	if got := widthsOf(&m); !slices.Equal(got, want) {
		t.Errorf("hiding twice then showing once: widths = %v, want %v", got, want)
	}
}

func TestTheFirstColumnCannotBeHidden(t *testing.T) {
	m := hiddenModel(120, -1)
	want := slices.Clone(widthsOf(&m))
	m.SetColumnHidden(0, true)
	m.Resize(120, 10)
	if got := widthsOf(&m); !slices.Equal(got, want) {
		t.Errorf("widths = %v after hiding column 0, want %v (a no-op)", got, want)
	}
	if h := header(&m); !strings.Contains(h, "Name") {
		t.Errorf("the first column left the header: %q", h)
	}
}

func TestHidingAnOutOfRangeColumnIsHarmless(t *testing.T) {
	m := hiddenModel(120, -1)
	want := slices.Clone(widthsOf(&m))
	for _, i := range []int{-1, 3, 99} {
		m.SetColumnHidden(i, true)
	}
	m.Resize(120, 10)
	if got := widthsOf(&m); !slices.Equal(got, want) {
		t.Errorf("widths = %v, want %v", got, want)
	}
}

// ── Sorting ──────────────────────────────────────────────────────────────────

func TestCycleSortSkipsAHiddenColumn(t *testing.T) {
	m := hiddenModel(120, -1)
	m.SetColumnHidden(hiddenColumn, true)
	m.Resize(120, 10)

	var visited []int
	for range 12 {
		m.CycleSort()
		column, _ := m.SortState()
		if column == hiddenColumn {
			t.Fatalf("CycleSort landed on the hidden column; visited %v", visited)
		}
		visited = append(visited, column)
	}
	for _, want := range []int{0, 2, -1} {
		if !slices.Contains(visited, want) {
			t.Errorf("the cycle never reached %d: %v", want, visited)
		}
	}
	if h := header(&m); strings.Contains(h, "Path") {
		t.Errorf("the hidden column is back in the header: %q", h)
	}
}

func TestCycleSortReachesTheColumnAgainWhenShown(t *testing.T) {
	m := hiddenModel(120, -1)
	m.SetColumnHidden(hiddenColumn, true)
	m.SetColumnHidden(hiddenColumn, false)
	m.Resize(120, 10)

	var visited []int
	for range 12 {
		m.CycleSort()
		column, _ := m.SortState()
		visited = append(visited, column)
	}
	if !slices.Contains(visited, hiddenColumn) {
		t.Errorf("a shown column must be sortable again: %v", visited)
	}
}

func TestHidingTheSortedColumnResetsTheSort(t *testing.T) {
	m := hiddenModel(120, -1)
	m.SetSort(hiddenColumn, true)
	if column, _ := m.SortState(); column != hiddenColumn {
		t.Fatalf("setup: sorted by %d, want %d", column, hiddenColumn)
	}

	m.SetColumnHidden(hiddenColumn, true)

	if column, desc := m.SortState(); column != -1 || desc {
		t.Errorf("SortState = (%d, %v), want (-1, false)", column, desc)
	}
	m.Resize(120, 10)
	if h := header(&m); strings.Contains(h, "▲") || strings.Contains(h, "▼") {
		t.Errorf("a sort arrow is left in the header: %q", h)
	}
}

func TestHidingAnotherColumnKeepsTheSort(t *testing.T) {
	m := hiddenModel(120, -1)
	m.SetSort(2, true)
	m.SetColumnHidden(hiddenColumn, true)
	if column, desc := m.SortState(); column != 2 || !desc {
		t.Errorf("SortState = (%d, %v), want (2, true)", column, desc)
	}
}
