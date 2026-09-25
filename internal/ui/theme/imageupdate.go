package theme

import "github.com/charmbracelet/lipgloss"

// UpdateStyle colours the cell: an update in the structural blue, everything
// else dim — up to date is the nominal state, and the others are explanations,
// not findings (Rule 122).
func UpdateStyle(available bool) lipgloss.Style {
	if !available {
		return DimStyle
	}
	return lipgloss.NewStyle().Foreground(ColorUpdateAvailable)
}
