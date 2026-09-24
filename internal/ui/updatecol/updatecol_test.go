package updatecol

import (
	"testing"

	"github.com/charmbracelet/lipgloss"

	"github.com/anthnel/devdesk/internal/imageupdate"
)

// The column shrinks to its MinWidth when the table is short of room, and at
// that width "new build" must still be whole — it read "new…" when the floor
// was the title's width.
func TestTheNewBuildLabelFitsTheNarrowestColumn(t *testing.T) {
	col := Column(false, func(s imageupdate.Status) imageupdate.Status { return s })
	cell := col.Cell(imageupdate.Status{Kind: imageupdate.NewBuild})
	if w := lipgloss.Width(cell); w > col.MinWidth {
		t.Errorf("%q is %d cells, the column may shrink to %d", cell, w, col.MinWidth)
	}
	if col.MinWidth < len(Title) {
		t.Errorf("MinWidth %d is narrower than the title", col.MinWidth)
	}
}
