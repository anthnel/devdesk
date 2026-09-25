package updatecol

import (
	"testing"

	"github.com/charmbracelet/lipgloss"

	"github.com/anthnel/devdesk/internal/imageupdate"
	"github.com/anthnel/devdesk/internal/ui/theme"
)

// The column shrinks to its MinWidth when the table is short of room, and at
// that width every cell but a patch tag must still be whole — "new build" read
// "new…" when the floor was the title's width and the cell was a word.
func TestEveryGlyphFitsTheNarrowestColumn(t *testing.T) {
	col := Column(false, func(s imageupdate.Status) imageupdate.Status { return s })
	for k := imageupdate.None; k <= imageupdate.NewBuild; k++ {
		if k == imageupdate.NewPatch {
			continue
		}
		cell := col.Cell(imageupdate.Status{Kind: k})
		if w := lipgloss.Width(cell); w > col.MinWidth {
			t.Errorf("kind %d: %q is %d cells, the column may shrink to %d", k, cell, w, col.MinWidth)
		}
	}
	if col.MinWidth < len(Title) {
		t.Errorf("MinWidth %d is narrower than the title", col.MinWidth)
	}
}

// Every state but None says which it is, and no two say it the same way: a
// Kind added without a glyph would render blank, the answer None keeps for
// "the question does not apply".
func TestEveryStateHasItsOwnGlyph(t *testing.T) {
	seen := map[string]imageupdate.Kind{}
	for k := imageupdate.Pending; k <= imageupdate.NewBuild; k++ {
		cell := Cell(imageupdate.Status{Kind: k, Tag: "1.2.4"})
		if cell == "" {
			t.Errorf("kind %d renders blank", k)
			continue
		}
		if prev, dup := seen[cell]; dup {
			t.Errorf("kinds %d and %d both render %q", prev, k, cell)
		}
		seen[cell] = k
	}
	if got := Cell(imageupdate.Status{}); got != "" {
		t.Errorf("None renders %q, want blank", got)
	}
}

// A newer patch keeps its tag — the version is what the cell is for — and a
// new build is the arrow alone.
func TestAPatchNamesItsTagAndANewBuildIsTheArrow(t *testing.T) {
	if got, want := Cell(imageupdate.Status{Kind: imageupdate.NewPatch, Tag: "20.11.4"}), theme.IconArrowDown+" 20.11.4"; got != want {
		t.Errorf("patch = %q, want %q", got, want)
	}
	if got := Cell(imageupdate.Status{Kind: imageupdate.NewBuild}); got != theme.IconArrowDown {
		t.Errorf("new build = %q, want the arrow alone", got)
	}
}
