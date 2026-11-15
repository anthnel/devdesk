package components

import (
	"strings"

	"charm.land/lipgloss/v2"
	"github.com/charmbracelet/x/ansi"

	"github.com/anthnel/devdesk/internal/ui/theme"
)

// BarFrame draws one line of content inside the rectangle that closes the
// viewport, and the rectangle's bottom edge under it.
//
// The viewport's own bottom border serves as the top of the rectangle, so the
// two lines here are the sides and the floor. The router detects the bar from
// the shape of the footer's first line (OpensWithBarFrame; no view declares
// it) and turns the viewport's last line from └…┘ into ├…┤. A footer therefore
// starts with a │…│ line only through BarFrame:
//
//	├────────────────────────────────────┤  (viewport's last line, joined)
//	│  / cursor...                    Aa │
//	└────────────────────────────────────┘
//
// It is shared rather than copied because two things now occupy that one slot —
// the filter bar and the viewer's go-to-line prompt — and a second
// implementation of the frame would be free to disagree with the first about
// where the corners are. `inner` is already styled; it is padded to the width
// between the sides, so a caller measures nothing.
func BarFrame(width int, inner string) string {
	if width == 0 {
		width = 80
	}

	borderStyle := lipgloss.NewStyle().Foreground(theme.ColorViewportBorder).Background(theme.ColorBackground)

	// width-2 to leave room for │ on each side.
	padded := theme.PadWithBg(inner, width-2)
	content := borderStyle.Render("│") + padded + borderStyle.Render("│")
	bottom := borderStyle.Render("└" + strings.Repeat("─", width-2) + "┘")
	return content + "\n" + bottom
}

// OpensWithBarFrame reports whether a rendered footer starts with a BarFrame:
// a first line framed by │ on both sides and a second line closed by └…┘. The
// router reads it to turn the viewport's bottom corners into ├ ┤, so the join
// follows what the footer really draws rather than what a view claims.
func OpensWithBarFrame(footer string) bool {
	lines := strings.SplitN(ansi.Strip(footer), "\n", 3)
	if len(lines) < 2 {
		return false
	}
	return strings.HasPrefix(lines[0], "│") && strings.HasSuffix(lines[0], "│") &&
		strings.HasPrefix(lines[1], "└") && strings.HasSuffix(lines[1], "┘")
}
