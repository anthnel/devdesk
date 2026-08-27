package components

import (
	"strings"

	"github.com/charmbracelet/lipgloss"

	"github.com/anthnel/devdesk/internal/ui/theme"
)

// BarFrame draws one line of content inside the rectangle that closes the
// viewport, and the rectangle's bottom edge under it.
//
// The viewport's own bottom border serves as the top of the rectangle, so the
// two lines here are the sides and the floor:
//
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
