package security

import (
	"strings"
	"testing"

	"github.com/anthnel/devdesk/internal/scan"
	"github.com/anthnel/devdesk/internal/ui/theme"
)

// misconfigColumnOf returns the CFG column, which exists only with the
// category on.
func misconfigColumnOf(t *testing.T) func(scanTarget) string {
	t.Helper()
	for _, col := range inventoryColumns(false, true) {
		if col.Title == "CFG" {
			return col.Cell
		}
	}
	t.Fatal("the inventory has no CFG column when the category is on")
	return nil
}

func misconfigTitles(withMisconfig bool) []string {
	titles := make([]string, 0)
	for _, col := range inventoryColumns(false, withMisconfig) {
		titles = append(titles, col.Title)
	}
	return titles
}

// Off, the column would be a dash on every row for the life of the view.
func TestTheMisconfigColumnExistsOnlyWhenTheCategoryIsOn(t *testing.T) {
	if got := strings.Join(misconfigTitles(false), " "); strings.Contains(got, "CFG") {
		t.Errorf("the CFG column is present with the category off: %q", got)
	}
	if got := strings.Join(misconfigTitles(true), " "); !strings.Contains(got, "CFG") {
		t.Errorf("the CFG column is absent with the category on: %q", got)
	}
}

// The three states a row can be in, and the pair a bare counter conflates.
func TestWhatEachRowShowsForMisconfigurations(t *testing.T) {
	cell := misconfigColumnOf(t)

	tests := []struct {
		name   string
		target scanTarget
		want   string
		why    string
	}{
		{
			"never scanned",
			scanTarget{Name: "a", Scanned: false, Misconfig: &scan.MisconfigSummary{Count: 9}},
			"-", "a purged row has looked at nothing, whatever an entry carries",
		},
		{
			"scanned without the category",
			scanTarget{Name: "b", Scanned: true},
			"-", "no stage read this target for misconfigurations",
		},
		{
			"scanned and clean",
			scanTarget{Name: "c", Scanned: true, Misconfig: &scan.MisconfigSummary{}},
			"0", "a stage looked and found nothing",
		},
		{
			"scanned, with findings",
			scanTarget{Name: "d", Scanned: true, Misconfig: &scan.MisconfigSummary{Count: 12, Worst: scan.SeverityHigh}},
			"12", "twelve misconfigurations",
		},
		{
			"clean, but nothing rendered",
			scanTarget{Name: "e", Scanned: true, Misconfig: &scan.MisconfigSummary{Unrendered: 3}},
			"0?", "a chart nobody rendered is not a clean chart",
		},
	}
	for _, tt := range tests {
		if got := cell(tt.target); got != tt.want {
			t.Errorf("%s: CFG cell = %q, want %q — %s", tt.name, got, tt.want, tt.why)
		}
	}
}

// Rule 122: the cell is measured before it is styled. The worst severity
// decides the colour, and a clean count carries none.
func TestTheMisconfigCellIsPlainAndOnlyAFindingIsColoured(t *testing.T) {
	var col = misconfigColumn()

	found := scanTarget{Scanned: true, Misconfig: &scan.MisconfigSummary{Count: 2, Worst: scan.SeverityCritical}}
	if cell := col.Cell(found); cell != "2" {
		t.Errorf("Cell = %q, want the bare count", cell)
	}
	want := theme.SeverityTextStyle(string(scan.SeverityCritical))
	if got := col.Style(found); got.GetForeground() != want.GetForeground() {
		t.Error("Style does not carry the worst severity's colour")
	}

	clean := scanTarget{Scanned: true, Misconfig: &scan.MisconfigSummary{}}
	if got := col.Style(clean); got.GetForeground() != theme.DimStyle.GetForeground() {
		t.Error("a clean count is coloured, want dim")
	}
}

// inventoryColumnCritical is an index, so the column the table opens sorted by
// must not move when the misconfiguration category is switched on.
func TestTheMisconfigColumnDoesNotMoveTheSortedColumn(t *testing.T) {
	for _, withMisconfig := range []bool{false, true} {
		cols := inventoryColumns(false, withMisconfig)
		if got := cols[inventoryColumnCritical].Title; got != "CRIT" {
			t.Errorf("with misconfig=%v, the sorted column is %q, want CRIT", withMisconfig, got)
		}
	}
}
