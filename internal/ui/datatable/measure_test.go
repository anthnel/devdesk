package datatable

import (
	"strings"
	"testing"

	"github.com/anthnel/devdesk/internal/ui/testutil"
)

// The measurement, and above all *when* it happens. The width a column takes is
// easy; the hard half is that datatable cannot tell a two-second refresh from a
// change of population — both arrive through SetItems — so a table that
// measured on every one of them would rearrange its own columns under a reader
// who had not touched anything.

// measuredConfig is one content column and one fixed one, with room to spare so
// nothing is being squeezed: the widths below are what the columns *want*.
func measuredConfig() Config[row] {
	return Config[row]{
		SortColumn: -1,
		Columns: []Column[row]{
			{
				Title: "Name", Sizing: SizingContent, MinWidth: 6,
				Cell:   func(r row) string { return r.Name },
				Search: func(r row) string { return r.Name },
			},
			{
				Title: "State", Sizing: SizingFixed, MinWidth: 12, Flex: 1,
				Cell: func(r row) string { return r.State },
			},
		},
	}
}

func measured(t *testing.T, names ...string) Model[row] {
	t.Helper()
	m := New(measuredConfig())
	m.Resize(120, 10)
	m.SetItems(rowsNamed(names...))
	return m
}

func rowsNamed(names ...string) []row {
	out := make([]row, len(names))
	for i, name := range names {
		out[i] = row{Name: name, State: "running"}
	}
	return out
}

func nameWidth(m *Model[row]) int { return m.Table().Columns()[0].Width }

// The first non-empty population measures itself. Without that a table sits at
// its MinWidths for the life of the view, and nothing on screen says why.
func TestTheFirstPopulationMeasuresItself(t *testing.T) {
	m := measured(t, "api", "a-rather-long-service-name")

	if got := nameWidth(&m); got != len("a-rather-long-service-name") {
		t.Errorf("the Name column is %d wide, want its widest value (%d)",
			got, len("a-rather-long-service-name"))
	}
}

// And every population after it does not. This is the periodic refresh: the
// same table, reloaded, with one longer value in it — the columns stay where
// the user last saw them.
func TestARefreshDoesNotMoveTheColumns(t *testing.T) {
	m := measured(t, "api", "web")
	before := nameWidth(&m)

	m.SetItems(rowsNamed("api", "web", strings.Repeat("x", 40)))

	if got := nameWidth(&m); got != before {
		t.Errorf("the Name column moved from %d to %d on a reload nobody asked for", before, got)
	}
}

// Remeasure is how the view says the population changed for a reason — a tab, a
// drill-down, an explicit refresh. It is the only thing that promotes what the
// rebuild has been measuring all along.
func TestRemeasureFollowsTheNewPopulation(t *testing.T) {
	m := measured(t, "api", "web")

	m.SetItems(rowsNamed("api", strings.Repeat("x", 40)))
	m.Remeasure()

	if got := nameWidth(&m); got != 40 {
		t.Errorf("the Name column is %d wide after Remeasure, want the new widest value (40)", got)
	}
}

// A resize is a measurement moment in its own right: the user has just changed
// how much room there is, and the content has not moved.
func TestAResizeTakesTheCurrentMeasurement(t *testing.T) {
	m := measured(t, "api", "web")

	m.SetItems(rowsNamed("api", strings.Repeat("x", 30)))
	m.Resize(120, 10)

	if got := nameWidth(&m); got != 30 {
		t.Errorf("the Name column is %d wide after a resize, want 30", got)
	}
}

// A search that keeps only short values gives the room back. Without it the
// filter narrows the list and buys nothing visually — the column stays sized
// for rows that are no longer on screen.
func TestASearchGivesTheRoomBack(t *testing.T) {
	m := measured(t, "api", "a-rather-long-service-name")
	wide := nameWidth(&m)

	m.Update(testutil.Key("/"))
	for _, msg := range testutil.Type("api") {
		m.Update(msg)
	}

	got := nameWidth(&m)
	if got >= wide {
		t.Errorf("the Name column is still %d wide with only %q visible", got, "api")
	}
	// Down to its floor, not to the three cells "api" needs: MinWidth is a
	// minimum again, and this column declared 6.
	if got != 6 {
		t.Errorf("the Name column is %d wide, want its declared floor of 6", got)
	}
}

// The header counts as content. A column narrower than its own title truncates
// the one cell that says what the column is, and "the width the content wants"
// plainly includes being able to name itself.
func TestTheHeaderIsPartOfWhatAColumnWants(t *testing.T) {
	cfg := measuredConfig()
	cfg.Columns[0].Title = "A very wide header"
	cfg.Columns[0].MinWidth = 3

	m := New(cfg)
	m.Resize(120, 10)
	m.SetItems(rowsNamed("api"))

	if got := nameWidth(&m); got != len("A very wide header") {
		t.Errorf("the column is %d wide, want room for its own header (%d)",
			got, len("A very wide header"))
	}
}

// A fixed column is not measured at all, whatever it holds.
func TestAFixedColumnIsNotMeasured(t *testing.T) {
	cfg := measuredConfig()
	cfg.Columns[0].Sizing = SizingFixed

	m := New(cfg)
	m.Resize(120, 10)
	m.SetItems(rowsNamed(strings.Repeat("x", 40)))

	if got := nameWidth(&m); got != 6 {
		t.Errorf("the fixed column is %d wide, want the 6 it declared", got)
	}
}

// ── Truncation ───────────────────────────────────────────────────────────────

// The default keeps the start, which is what an address, a name or a state
// wants: they are recognised by how they begin.
func TestTheDefaultTruncationKeepsTheStart(t *testing.T) {
	if got := fit("192.168.1.240", 8, false); !strings.HasPrefix(got, "192.168") {
		t.Errorf("fit(...) = %q, want the start kept", got)
	}
}

// TruncateHead keeps the end. A column of image references shares its prefix on
// every row, so cutting the tail renders identical cells that identify nothing
// — precisely when the column is squeezed and hardest to read.
func TestTruncateHeadKeepsTheEnd(t *testing.T) {
	const ref = "registry.example.com/team/app:1.2.3"

	got := strings.TrimSpace(fit(ref, 14, true))

	if !strings.HasSuffix(got, "app:1.2.3") {
		t.Errorf("fit(%q, 14, head) = %q, want the end kept", ref, got)
	}
	if !strings.HasPrefix(got, truncationMarker) {
		t.Errorf("fit(...) = %q, want it to say something was cut", got)
	}
}

// Two references differing only in their tail come out different, which is the
// whole reason the flag exists.
func TestTruncateHeadTellsTwoSimilarValuesApart(t *testing.T) {
	a := fit("registry.example.com/team/api:1.0", 14, true)
	b := fit("registry.example.com/team/web:1.0", 14, true)

	if a == b {
		t.Errorf("both references render as %q", strings.TrimSpace(a))
	}
}

// A value that fits is left alone, in either direction.
func TestNothingIsCutWhenItFits(t *testing.T) {
	for _, head := range []bool{false, true} {
		if got := strings.TrimSpace(fit("short", 20, head)); got != "short" {
			t.Errorf("fit(%q, 20, %v) = %q", "short", head, got)
		}
	}
}

// A column with no room for the marker and anything after it says only that
// there is more, rather than showing one arbitrary character of it.
func TestAColumnTooNarrowForTheMarkerSaysOnlyThat(t *testing.T) {
	if got := strings.TrimSpace(fit("a-long-value", 1, true)); got != truncationMarker {
		t.Errorf("fit(..., 1, head) = %q, want just the marker", got)
	}
}
