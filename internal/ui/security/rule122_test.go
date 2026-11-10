package security

import (
	"strings"
	"testing"

	"charm.land/bubbles/v2/spinner"

	"github.com/anthnel/devdesk/internal/ui/keymap"
	"github.com/anthnel/devdesk/internal/ui/testutil"
)

// Rule 122: a cell is measured while plain and coloured afterwards. DEVDESK-145
// removed the spinner from the Scanned cell altogether: a scan that is running
// reads `scanning` (static, dim), and the blinking dot lives in column 0.
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
			names := make([]string, 0, len(m.inventory.Items()))
			for _, target := range m.inventory.Items() {
				names = append(names, target.Name)
			}
			m = scanning(t, m, names...)

			var running int
			for _, row := range m.inventory.Table().Rows() {
				for i, cell := range row {
					if strings.Contains(cell, "\x1b") {
						t.Errorf("cell %d carries an escape sequence: %q", i, cell)
					}
				}
				if row[len(row)-1] == scanningText {
					running++
				}
			}
			if running == 0 {
				t.Error("no row says it is being scanned, so nothing tells the user work started")
			}
			if !strings.Contains(m.View(), scanningText) {
				t.Error("the rendered view shows no scan in progress")
			}
		})
	}
}

// No cell ever holds a spinner frame: the Scanned cell is the same text on
// every tick, so a tick cannot move it.
func TestTheScannedCellIsStaticWhileAScanRuns(t *testing.T) {
	m := inventoryModel(t, verdictFixtures()...)
	name := m.inventory.Items()[0].Name
	m = scanning(t, m, name)

	first := scanningCell(t, m)
	if first != scanningText {
		t.Fatalf("the scanning cell = %q, want exactly %q", first, scanningText)
	}
	for _, f := range spinner.Dot.Frames {
		if strings.Contains(first, f) {
			t.Errorf("the cell holds the spinner frame %q", f)
		}
	}
	m = withJobs(t, m, scanningRun(name))
	if second := scanningCell(t, m); second != first {
		t.Errorf("the cell moved from %q to %q across a snapshot", first, second)
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
