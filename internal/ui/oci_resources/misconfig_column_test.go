package ociresources

import (
	"testing"

	"github.com/anthnel/devdesk/internal/cache"
	"github.com/anthnel/devdesk/internal/scan"
	"github.com/anthnel/devdesk/internal/ui/theme"
)

func misconfigTitles(withMisconfig bool) []string {
	titles := make([]string, 0)
	for _, col := range imageColumns(withMisconfig) {
		titles = append(titles, col.Title)
	}
	return titles
}

// Off, the column would be a dash on every row for the life of the view.
func TestTheImagesMisconfigColumnFollowsTheCategory(t *testing.T) {
	for _, title := range misconfigTitles(false) {
		if title == "CFG" {
			t.Fatal("the CFG column is present with the misconfig category off")
		}
	}
	found := false
	for _, title := range misconfigTitles(true) {
		found = found || title == "CFG"
	}
	if !found {
		t.Fatal("the CFG column is absent with the misconfig category on")
	}
}

// An image does have misconfigurations to find — Trivy reads the Dockerfile
// instructions baked into its layers — so the three states are the repository's
// three states, not a repository-only column.
func TestWhatAnImageRowShowsForMisconfigurations(t *testing.T) {
	col := misconfigColumn()

	tests := []struct {
		name string
		row  imageRow
		want string
	}{
		{"never scanned", imageRow{}, "-"},
		{"scanned before the category existed", imageRow{Scanned: true}, "-"},
		{"scanned and clean", imageRow{Scanned: true, Entry: cache.ImageScanEntry{Misconfig: &scan.MisconfigSummary{}}}, "0"},
		{
			"scanned, with findings",
			imageRow{Scanned: true, Entry: cache.ImageScanEntry{
				Misconfig: &scan.MisconfigSummary{Count: 4, Worst: scan.SeverityMedium},
			}},
			"4",
		},
	}
	for _, tt := range tests {
		if got := col.Cell(tt.row); got != tt.want {
			t.Errorf("%s: CFG cell = %q, want %q", tt.name, got, tt.want)
		}
	}
}

// Rule 122: plain text in the cell, the colour through Style — and only a count
// that found something is coloured.
func TestTheImagesMisconfigCellIsPlainAndOnlyAFindingIsColoured(t *testing.T) {
	col := misconfigColumn()

	found := imageRow{Scanned: true, Entry: cache.ImageScanEntry{
		Misconfig: &scan.MisconfigSummary{Count: 4, Worst: scan.SeverityMedium},
	}}
	want := theme.SeverityTextStyle(string(scan.SeverityMedium))
	if got := col.Style(found); got.GetForeground() != want.GetForeground() {
		t.Error("Style does not carry the worst severity's colour")
	}

	clean := imageRow{Scanned: true, Entry: cache.ImageScanEntry{Misconfig: &scan.MisconfigSummary{}}}
	if got := col.Style(clean); got.GetForeground() != theme.DimStyle.GetForeground() {
		t.Error("a clean count is coloured, want dim")
	}
}
