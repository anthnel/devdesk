// Package updatecol is the "Update" column three tables share (§3.88): oci's
// Images tab, the containers list and the Remediation tab of a scan result.
//
// It lives apart from theme because it builds a datatable column, which theme
// cannot import, and apart from each view so the three read one definition.
package updatecol

import (
	"github.com/charmbracelet/lipgloss"

	"github.com/anthnel/devdesk/internal/imageupdate"
	"github.com/anthnel/devdesk/internal/ui/datatable"
	"github.com/anthnel/devdesk/internal/ui/theme"
)

// Title is the column's header.
const Title = "Update"

// minWidth is what the column never shrinks under: the title. Every cell but
// a newer patch is a single glyph (Cell), so only a patch tag can be wider,
// and it is cut from its end — the version stays readable.
const minWidth = len(Title)

// glyphs is the icon each state that is not a newer patch prints. A glyph
// rather than its word: the column sat in the widest table there is, and
// "local build" spent eleven cells saying what one says. The word is still
// Label, which the filter and the help read.
var glyphs = map[imageupdate.Kind]string{
	imageupdate.Pending:    theme.IconHourglass,
	imageupdate.Failed:     theme.IconHelpCircle,
	imageupdate.LocalBuild: theme.IconHammer,
	imageupdate.NotLocal:   theme.IconCloud,
	imageupdate.Pinned:     theme.IconPin,
	imageupdate.UpToDate:   theme.IconOK,
	imageupdate.NewBuild:   theme.IconArrowDown,
}

// Cell is what an Update cell prints (§3.88): a newer patch is the arrow and
// the tag to move to — the version is the information — and every other state
// its glyph, so no two answers share a blank. None stays blank: the question
// does not apply. Plain text, no ANSI sequence: UpdateStyle colours it
// (Rule 122).
func Cell(s imageupdate.Status) string {
	if s.Kind == imageupdate.NewPatch {
		return theme.IconArrowDown + " " + s.Tag
	}
	return glyphs[s.Kind]
}

// Column is the column for rows whose status get returns. optional lets it
// give way first when the terminal is narrow, for a table where other columns
// matter more.
func Column[T any](optional bool, get func(T) imageupdate.Status) datatable.Column[T] {
	return datatable.Column[T]{
		Title: Title, Sizing: datatable.SizingContent, MinWidth: minWidth, MaxWidth: 20, Optional: optional,
		Cell: func(r T) string {
			return Cell(get(r))
		},
		Style: func(r T) lipgloss.Style { return theme.UpdateStyle(get(r).Available()) },
		// No comparator: `datatable` reserves two cells for a sortable column's
		// arrow, and the containers table is the widest there is. A filter on
		// the label does what a sort would — an update's label only: "local
		// build" would otherwise answer a search for "ca" on every built image.
		// The label, not the cell: a glyph is nothing anyone types.
		Search: func(r T) string {
			if s := get(r); s.Available() {
				return s.Label()
			}
			return ""
		},
	}
}
