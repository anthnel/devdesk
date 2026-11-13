package ociresources

import (
	"slices"
	"strings"
	"testing"

	"github.com/charmbracelet/x/ansi"

	"github.com/anthnel/devdesk/internal/imagesearch"
	"github.com/anthnel/devdesk/internal/ui/testutil"
)

// DEVDESK-309 (IS-05): the level 1 table sorts by Name, Registry and Stars
// through the shared datatable cycle (`.`).

func sortRow(name, label, key string, stars int, hasStars bool) searchRow {
	return searchRow{
		img:   imagesearch.Image{Name: name, Repo: name, EntryKey: key, Stars: stars, HasStars: hasStars},
		label: label,
	}
}

func sortNames(rows []searchRow) []string {
	out := make([]string, len(rows))
	for i, r := range rows {
		out[i] = r.img.Name
	}
	return out
}

func TestSearchSortColumnIndexes(t *testing.T) {
	cols := searchColumns()
	for idx, title := range map[int]string{searchColName: "Name", searchColRegistry: "Registry", searchColStars: "Stars"} {
		if idx >= len(cols) || cols[idx].Title != title {
			t.Errorf("column %d is not %q", idx, title)
			continue
		}
		if cols[idx].Less == nil {
			t.Errorf("column %q has no Less: it cannot be sorted by", title)
		}
	}
	if !cols[searchColStars].DescFirst {
		t.Error("Stars must open in descending order (DescFirst)")
	}
	for _, i := range []int{searchColName, searchColRegistry} {
		if cols[i].DescFirst {
			t.Errorf("column %q must open ascending", cols[i].Title)
		}
	}
}

func TestStarsKey(t *testing.T) {
	cases := []struct {
		name string
		row  searchRow
		want int
	}{
		{"no stars reported", sortRow("a", "x", "k", 0, false), -1},
		{"reported zero", sortRow("a", "x", "k", 0, true), 0},
		{"some", sortRow("a", "x", "k", 42, true), 42},
	}
	for _, c := range cases {
		if got := starsKey(c.row); got != c.want {
			t.Errorf("%s: starsKey = %d, want %d", c.name, got, c.want)
		}
	}
}

func TestLessSearchName(t *testing.T) {
	a, b := sortRow("Alpine", "r", "k", 0, false), sortRow("busybox", "r", "k", 0, false)
	if !lessSearchName(a, b) || lessSearchName(b, a) {
		t.Error("Alpine must sort before busybox, whatever the case")
	}
	lo, up := sortRow("nginx", "r", "k", 0, false), sortRow("NGINX", "r", "k", 0, false)
	if lessSearchName(lo, up) || lessSearchName(up, lo) {
		t.Error("names equal ignoring case must not be less than each other")
	}
}

func TestLessSearchRegistry(t *testing.T) {
	a, b := sortRow("n", "Hub", "k", 0, false), sortRow("n", "nexus", "k", 0, false)
	if !lessSearchRegistry(a, b) || lessSearchRegistry(b, a) {
		t.Error("Hub must sort before nexus on the lower-cased label")
	}
	x, y := sortRow("n", "hub", "k", 0, false), sortRow("m", "HUB", "k", 0, false)
	if lessSearchRegistry(x, y) || lessSearchRegistry(y, x) {
		t.Error("labels equal ignoring case must not be less than each other")
	}
}

func TestLessSearchStars(t *testing.T) {
	none := sortRow("a", "r", "k", 0, false)
	zero := sortRow("b", "r", "k", 0, true)
	many := sortRow("c", "r", "k", 9, true)
	if !lessSearchStars(none, zero) || !lessSearchStars(zero, many) || lessSearchStars(many, none) {
		t.Error("order must be: no stars < zero stars < some stars")
	}
	if lessSearchStars(none, none) {
		t.Error("equal keys must not be less")
	}
}

// sortFixture: names, labels and stars chosen so that each column orders the
// rows differently and ties exist on every column.
func sortFixture() []searchRow {
	return []searchRow{
		sortRow("beta", "hub", "1", 10, true),
		sortRow("Alpha", "nexus", "2", 0, false),
		sortRow("gamma", "hub", "1", 0, true),
		sortRow("alpha2", "Nexus", "2", 10, true),
		sortRow("delta", "hub", "1", 0, false),
	}
}

func sortedTable(t *testing.T, width int) (get func() []string, press func()) {
	t.Helper()
	tbl := newSearchTable()
	tbl.Resize(width, 30)
	rows := sortFixture()
	sortSearchRows("", rows) // the order rebuildSearchTable hands over
	tbl.SetItems(rows)
	return func() []string { return sortNames(tbl.Visible()) },
		func() { tbl.Update(testutil.Key(".")) }
}

func TestSearchSortCycleOverSevenPresses(t *testing.T) {
	tbl := newSearchTable()
	tbl.Resize(120, 30)
	rows := sortFixture()
	sortSearchRows("", rows)
	tbl.SetItems(rows)

	type state struct {
		col  int
		desc bool
	}
	want := []state{
		{searchColName, false}, {searchColName, true},
		{searchColRegistry, false}, {searchColRegistry, true},
		{searchColStars, true}, {searchColStars, false},
		{-1, false},
	}
	for i, w := range want {
		tbl.Update(testutil.Key("."))
		if col, desc := tbl.SortState(); col != w.col || desc != w.desc {
			t.Fatalf("press %d: SortState = (%d,%v), want (%d,%v)", i+1, col, desc, w.col, w.desc)
		}
	}
}

func TestSearchSortByNameIsCaseInsensitive(t *testing.T) {
	get, press := sortedTable(t, 120)
	press() // Name asc
	if got, want := get(), []string{"Alpha", "alpha2", "beta", "delta", "gamma"}; !slices.Equal(got, want) {
		t.Errorf("Name asc = %q, want %q", got, want)
	}
	press() // Name desc
	if got, want := get(), []string{"gamma", "delta", "beta", "alpha2", "Alpha"}; !slices.Equal(got, want) {
		t.Errorf("Name desc = %q, want %q", got, want)
	}
}

func TestSearchSortByRegistryTiesKeepTheDefaultOrder(t *testing.T) {
	get, press := sortedTable(t, 120)
	press()
	press()
	press() // Registry asc: hub x3, then Nexus/nexus x2
	// Default order (name, lower-cased): Alpha, alpha2, beta, delta, gamma.
	if got, want := get(), []string{"beta", "delta", "gamma", "Alpha", "alpha2"}; !slices.Equal(got, want) {
		t.Errorf("Registry asc = %q, want %q", got, want)
	}
	press() // Registry desc: ties still in default order, not reversed
	if got, want := get(), []string{"Alpha", "alpha2", "beta", "delta", "gamma"}; !slices.Equal(got, want) {
		t.Errorf("Registry desc = %q, want %q", got, want)
	}
}

func TestSearchSortByStarsKeepsTheStarlessBlockContiguous(t *testing.T) {
	get, press := sortedTable(t, 120)
	for range 4 {
		press()
	}
	press() // Stars desc (first direction): starred first, starless block last
	// 10 stars: alpha2, beta; reported 0: gamma; no stars: Alpha, delta.
	if got, want := get(), []string{"alpha2", "beta", "gamma", "Alpha", "delta"}; !slices.Equal(got, want) {
		t.Errorf("Stars desc = %q, want %q", got, want)
	}
	press() // Stars asc: starless block first, ties in default order
	if got, want := get(), []string{"Alpha", "delta", "gamma", "alpha2", "beta"}; !slices.Equal(got, want) {
		t.Errorf("Stars asc = %q, want %q", got, want)
	}
	press() // back to the default order
	if got, want := get(), []string{"Alpha", "alpha2", "beta", "delta", "gamma"}; !slices.Equal(got, want) {
		t.Errorf("no sort = %q, want the default order %q", got, want)
	}
}

func TestSearchHeaderShowsTheStarsArrowUntruncated(t *testing.T) {
	tbl := newSearchTable()
	tbl.Resize(120, 30)
	tbl.SetItems(hubRows())
	for range 5 {
		tbl.Update(testutil.Key("."))
	}
	if col, desc := tbl.SortState(); col != searchColStars || !desc {
		t.Fatalf("SortState = (%d,%v), want Stars descending", col, desc)
	}
	if got := ansi.Strip(tbl.Table().View()); !strings.Contains(got, "Stars ▼") {
		t.Errorf("the header at 120 columns lacks %q:\n%s", "Stars ▼", got)
	}
	if got, want := tbl.RenderedWidth(), 120-2; got != want {
		t.Errorf("the line spans %d, want %d (Rule 116)", got, want)
	}
}
