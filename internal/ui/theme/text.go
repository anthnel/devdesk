package theme

import (
	"github.com/mattn/go-runewidth"
)

// Ellipsis is the marker appended (or prepended) by the truncation helpers.
const Ellipsis = "..."

// StringWidth returns the number of terminal columns s occupies.
//
// Use this instead of len() whenever a value is measured for display: len()
// counts bytes, so "é" reads as 2 columns and any CJK glyph as 3, which makes
// text wrap early and truncation cut mid-rune.
func StringWidth(s string) int {
	return runewidth.StringWidth(s)
}

// TruncateWidth shortens s to at most maxWidth terminal columns, appending
// Ellipsis when it does not fit. Truncation happens on rune boundaries, so the
// result is always valid UTF-8.
//
// Use it for values whose head carries the meaning (titles, messages). For
// filesystem paths prefer TruncateTailWidth, which keeps the end.
func TruncateWidth(s string, maxWidth int) string {
	if maxWidth <= 0 {
		return ""
	}
	if runewidth.StringWidth(s) <= maxWidth {
		return s
	}
	if maxWidth <= runewidth.StringWidth(Ellipsis) {
		// runewidth.Truncate never shortens its tail marker, so it would return
		// a result wider than the budget here. Emit a partial marker instead.
		return runewidth.Truncate(Ellipsis, maxWidth, "")
	}
	return runewidth.Truncate(s, maxWidth, Ellipsis)
}

// TruncateTailWidth shortens s to at most maxWidth terminal columns, keeping the
// tail and prefixing Ellipsis. Truncation happens on rune boundaries, so the
// result is always valid UTF-8.
//
// Use it for filesystem paths and repository references, where the trailing
// segments identify the item and the leading ones are shared noise.
func TruncateTailWidth(s string, maxWidth int) string {
	if maxWidth <= 0 {
		return ""
	}
	if runewidth.StringWidth(s) <= maxWidth {
		return s
	}

	budget := maxWidth - runewidth.StringWidth(Ellipsis)
	if budget <= 0 {
		// No room for content beside the marker: emit as much of it as fits.
		return runewidth.Truncate(Ellipsis, maxWidth, "")
	}

	// Walk backwards from the end, accumulating columns until the budget is
	// spent. Stopping before the rune that would overflow keeps double-width
	// glyphs whole rather than splitting them.
	runes := []rune(s)
	width := 0
	cut := len(runes)
	for cut > 0 {
		w := runewidth.RuneWidth(runes[cut-1])
		if width+w > budget {
			break
		}
		width += w
		cut--
	}
	return Ellipsis + string(runes[cut:])
}
