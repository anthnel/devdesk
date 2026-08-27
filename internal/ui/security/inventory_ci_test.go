package security

import (
	"strings"
	"testing"

	tea "github.com/charmbracelet/bubbletea"

	"github.com/anthnel/devdesk/internal/ui/theme"
)

// ciCellOf renders the CI column for one row, the way the table would.
func ciCellOf(t *testing.T, target scanTarget) string {
	t.Helper()
	for _, col := range inventoryColumns(true) {
		if col.Title == "CI" {
			return col.Cell(target)
		}
	}
	t.Fatal("the inventory has no CI column when the setting is on")
	return ""
}

func ciTitles(withCI bool) []string {
	titles := make([]string, 0)
	for _, col := range inventoryColumns(withCI) {
		titles = append(titles, col.Title)
	}
	return titles
}

// Off, the column would be four cells of nothing on every row for the life of
// the view — which says less than no column at all.
func TestTheCIColumnExistsOnlyWhenTheSettingIsOn(t *testing.T) {
	if got := strings.Join(ciTitles(false), " "); strings.Contains(got, "CI") {
		t.Errorf("the CI column is present with the setting off: %q", got)
	}
	if got := strings.Join(ciTitles(true), " "); !strings.Contains(got, "CI") {
		t.Errorf("the CI column is absent with the setting on: %q", got)
	}
}

// It sits beside the severity counters and before Scanned, which is where a
// reader looks for what a scan concluded — and it leaves CRIT where the sort
// index expects it.
func TestTheCIColumnSitsBeforeScannedAndLeavesTheSortIndexAlone(t *testing.T) {
	titles := ciTitles(true)
	if titles[inventoryColumnCritical] != "CRIT" {
		t.Errorf("column %d is %q, want CRIT", inventoryColumnCritical, titles[inventoryColumnCritical])
	}
	joined := strings.Join(titles, " ")
	if !strings.Contains(joined, "LOW CI Scanned") {
		t.Errorf("the CI column is not between LOW and Scanned: %q", joined)
	}
}

// The three absences do not mean the same thing, and the cell is what tells
// them apart. An image is never gradeable — it has no pipeline — so it shows
// nothing rather than a dash.
func TestTheCICellSeparatesTheThreeAbsences(t *testing.T) {
	grade := "B"
	for _, tc := range []struct {
		name   string
		target scanTarget
		want   string
	}{
		{"graded repository", scanTarget{Kind: kindRepo, Scanned: true, CIGradeable: true, CIScore: &grade}, "B"},
		{"scanned, no letter", scanTarget{Kind: kindRepo, Scanned: true, CIGradeable: true}, "?"},
		{"purged by ctrl+a", scanTarget{Kind: kindRepo, CIGradeable: true}, "-"},
		{"another forge", scanTarget{Kind: kindRepo, Scanned: true}, ""},
		{"an image", scanTarget{Kind: kindImage, Scanned: true}, ""},
	} {
		t.Run(tc.name, func(t *testing.T) {
			if got := ciCellOf(t, tc.target); got != tc.want {
				t.Errorf("cell = %q, want %q", got, tc.want)
			}
		})
	}
}

// Rule 122: the cell is measured before it is styled, so it carries no escape
// sequence. The colour comes from the column's Style.
func TestTheCICellIsPlainText(t *testing.T) {
	grade := "E"
	cell := ciCellOf(t, scanTarget{Kind: kindRepo, Scanned: true, CIGradeable: true, CIScore: &grade})
	if strings.Contains(cell, "\x1b") {
		t.Errorf("the CI cell carries an escape sequence: %q", cell)
	}
}

// A rescan replaces the letter. Without the verdict on the message the row
// would keep the grade of its previous scan beside counters that are new —
// which is the reason the secret verdict travels the same way.
func TestARescanReplacesTheGradeOnTheRow(t *testing.T) {
	old, fresh := "E", "A"
	m := feed(t, New(testConfig(), nil), tea.WindowSizeMsg{Width: 160, Height: 30})
	m.setInventory([]scanTarget{{
		Kind: kindRepo, Name: "/repos/devdesk", Scanned: true,
		CIGradeable: true, CIScore: &old, Scanning: true,
	}})

	m = feed(t, m, InventoryScanFinishedMsg{Name: "/repos/devdesk", CIScore: &fresh})

	row := m.inventory.Items()[0]
	if row.CIScore == nil || *row.CIScore != fresh {
		t.Fatalf("the row kept %v, want %q", row.CIScore, fresh)
	}
	if got := theme.CIScoreCell(row.ci(), row.CIScore); got != fresh {
		t.Errorf("the cell shows %q, want %q", got, fresh)
	}
}
