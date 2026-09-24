package theme

import "github.com/charmbracelet/lipgloss"

// UpdateCell is what an image update cell prints (§3.88). An update is the
// arrow and what to move to; an image up to date is the check mark; anything
// else is its label as given — checking, ?, local build, not local, pinned — so
// no two answers share a blank. Plain text, no ANSI sequence: UpdateStyle
// colours it (Rule 122).
func UpdateCell(available, upToDate bool, label string) string {
	switch {
	case available:
		return IconArrowDown + " " + label
	case upToDate:
		return IconOK
	}
	return label
}

// UpdateStyle colours the cell: an update in the structural blue, everything
// else dim — up to date is the nominal state, and the others are explanations,
// not findings (Rule 122).
func UpdateStyle(available bool) lipgloss.Style {
	if !available {
		return DimStyle
	}
	return lipgloss.NewStyle().Foreground(ColorUpdateAvailable)
}
