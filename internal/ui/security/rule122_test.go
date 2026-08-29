package security

import (
	"strings"
	"testing"

	"github.com/anthnel/devdesk/internal/ui/keymap"
	"github.com/anthnel/devdesk/internal/ui/testutil"
)

// Rule 122: a cell is measured while plain and coloured afterwards, so an escape
// sequence inside one is counted as width. The spinner stamped by setInventory
// was styled: 39 bytes of escape for a two-cell glyph in a column fourteen wide,
// so the cut landed inside the sequence and the Scanned column rendered as
// nothing — a scan running with no sign on screen that it was.
func TestAScanningRowCarriesNoEscapeSequence(t *testing.T) {
	withTrueColor(t)

	for _, tc := range []struct {
		name string
		run  func(*testing.T, Model) Model
	}{
		{"one target", func(t *testing.T, m Model) Model {
			next, _ := step(t, m, testutil.Key(keymap.Scan))
			return next
		}},
		{"every target", func(t *testing.T, m Model) Model {
			next, _ := scanAll(t, m, false)
			return next
		}},
	} {
		t.Run(tc.name, func(t *testing.T) {
			m := tc.run(t, inventoryModel(t, verdictFixtures()...))
			// The router marks the rows; here the snapshot stands in for it.
			names := make([]string, 0, len(m.inventory.Items()))
			for _, target := range m.inventory.Items() {
				names = append(names, target.Name)
			}
			m = scanning(t, m, names...)

			var scanning int
			for _, row := range m.inventory.Table().Rows() {
				for i, cell := range row {
					if strings.Contains(cell, "\x1b") {
						t.Errorf("cell %d carries an escape sequence: %q", i, cell)
					}
					if strings.Contains(cell, "scanning") {
						scanning++
					}
				}
			}
			if scanning == 0 {
				t.Error("no row says it is being scanned, so nothing tells the user work started")
			}
			if !strings.Contains(m.View(), "scanning") {
				t.Error("the rendered view shows no scan in progress")
			}
		})
	}
}

// The spinner has to advance, or a running scan is indistinguishable from a hung
// one. The frame is stamped onto the rows, so a tick that does not restamp them
// freezes it even while the chain keeps ticking.
func TestTheScanSpinnerAdvancesOnTheRows(t *testing.T) {
	m := inventoryModel(t, verdictFixtures()...)
	run := scanningRun(m.inventory.Items()[0].Name)
	m = withFrame(t, m, "one", run)

	first := scanningCell(t, m)
	m = withFrame(t, m, "two", run)

	if second := scanningCell(t, m); second == first {
		t.Errorf("the frame stayed at %q across a tick, so the row reads as hung", first)
	}
}

// scanningCell returns the Scanned cell of the row being scanned.
func scanningCell(t *testing.T, m Model) string {
	t.Helper()
	for _, row := range m.inventory.Table().Rows() {
		if strings.Contains(row[len(row)-1], "scanning") {
			return row[len(row)-1]
		}
	}
	t.Fatal("no row is being scanned")
	return ""
}
