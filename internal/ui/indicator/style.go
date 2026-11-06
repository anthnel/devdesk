package indicator

import (
	"github.com/charmbracelet/lipgloss"

	"github.com/anthnel/devdesk/internal/ui/theme"
)

// Color returns the theme colour that paints s. The theme globals are read at
// call time because ApplyTheme reassigns them.
func Color(s State) lipgloss.Color {
	switch s {
	case Running:
		return theme.ColorActivityRunning
	case Failed:
		return theme.ColorActivityFailed
	case Suspended, Restarting:
		return theme.ColorActivitySuspended
	case Queued, Done, Cancelled, Skipped, Unknown:
		return theme.ColorActivityNeutral
	default: // Idle
		return theme.ColorText
	}
}

// Style returns a foreground-only style for s; the datatable paints the
// background (Rule 122).
func Style(s State) lipgloss.Style {
	return lipgloss.NewStyle().Foreground(Color(s))
}
