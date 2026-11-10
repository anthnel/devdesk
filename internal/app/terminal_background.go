package app

import (
	"image/color"

	"charm.land/lipgloss/v2"
	"github.com/charmbracelet/colorprofile"
)

// terminalBackground returns the colour to set as the terminal's own
// background (tea.View.BackgroundColor), or nil when it should be left alone.
//
// It must match what painted cells get on the same profile, otherwise a seam
// shows between them and the terminal background:
//   - TrueColor: the exact colour.
//   - ANSI256: the colour converted to the palette, as the renderer does for cells.
//   - anything else (16 colours, NO_COLOR, not a TTY, unknown): nil, because the
//     approximation would not match the cells.
//
// The renderer compares the result with !=, so it must be a comparable dynamic
// type; both branches return a plain colour value.
func terminalBackground(profile colorprofile.Profile, c color.Color) color.Color {
	if c == nil {
		return nil
	}
	if _, none := c.(lipgloss.NoColor); none {
		return nil
	}
	switch profile {
	case colorprofile.TrueColor:
		return c
	case colorprofile.ANSI256:
		return colorprofile.ANSI256.Convert(c)
	}
	return nil
}
