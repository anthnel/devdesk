package theme

import "github.com/charmbracelet/lipgloss"

// UpdateCell is what an image update cell prints (§3.88): the arrow and what to
// move to, or nothing. Plain text, no ANSI sequence — UpdateStyle colours it
// (Rule 122).
//
// An image with no known update prints nothing rather than a dim "-": the
// column is read for the arrow, and a dash on every other row would be noise
// that says nothing — not checked, up to date and not checkable alike.
func UpdateCell(available bool, label string) string {
	if !available {
		return ""
	}
	return IconArrowDown + " " + label
}

// UpdateStyle colours the cell.
func UpdateStyle(available bool) lipgloss.Style {
	if !available {
		return DimStyle
	}
	return lipgloss.NewStyle().Foreground(ColorUpdateAvailable)
}
