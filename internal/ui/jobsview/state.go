package jobsview

import (
	"github.com/charmbracelet/lipgloss"

	"github.com/anthnel/devdesk/internal/jobs"
	sharedcomponents "github.com/anthnel/devdesk/internal/ui/components"
	"github.com/anthnel/devdesk/internal/ui/indicator"
)

// runStateStyle colours the State word of a run with the colour of its dot, so
// the row has one colour source (Rule 125). Done is neutral, not green.
func runStateStyle(state jobs.RunState) lipgloss.Style {
	return indicator.Style(sharedcomponents.RunIndicator(state))
}

// itemStateStyle is the same for one target.
func itemStateStyle(state jobs.ItemState) lipgloss.Style {
	return indicator.Style(sharedcomponents.ItemIndicator(state))
}
