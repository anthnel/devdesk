package components

import (
	"strings"

	"github.com/charmbracelet/x/ansi"
)

// ClampField cuts the rendered line of a one-line field (a textinput's View)
// to at most width cells, counting grapheme clusters the way the renderer does
// (ansi.GraphemeWidth). Style sequences are kept whole; it never pads, and a
// line that already fits comes back unchanged. When it cuts, it closes a style
// the cut left open, so the colour does not bleed onto what follows the field.
// What follows the cut is dropped, the cursor cell of a field scrolled to its
// end included: see the limit in docs/architecture/ui-components.md.
//
// Why: bubbles v2.2.1 decides whether a textinput scrolls with uniseg (clusters)
// but builds its scroll window with runewidth per rune, where "❤️" (U+2764
// U+FE0F) is 1 cell and its U+FE0F 0, while the renderer draws it in 2. A field
// that holds such emoji overflows its box by up to twice its width, and its
// window can start on a lone U+FE0F. Upstream ticket: annex 1 of the
// DEVDESK-785 plan.
//
// Removal signal: TestTextInputWindowIsMeasuredWithRunewidth fails the day
// bubbles measures its window in clusters. Then drop this file, its tests and
// its two call sites (paletteInputView, FilterBar.View).
func ClampField(rendered string, width int) string {
	width = max(width, 0)
	if ansi.StringWidth(rendered) <= width {
		return rendered
	}
	var b strings.Builder
	used, styled := 0, false
	for rest := rendered; rest != ""; {
		if rest[0] == '\x1b' {
			// An escape sequence takes no cell and is kept whole.
			_, _, n, _ := ansi.DecodeSequence(rest, 0, nil)
			if seq := rest[:n]; strings.HasPrefix(seq, "\x1b[") && strings.HasSuffix(seq, "m") {
				styled = true
			}
			b.WriteString(rest[:n])
			rest = rest[n:]
			continue
		}
		// DecodeSequence would split a keycap, so the text is walked by cluster.
		cluster, w := ansi.FirstGraphemeCluster(rest, ansi.GraphemeWidth)
		if cluster == "" || used+w > width {
			break
		}
		used += w
		b.WriteString(cluster)
		rest = rest[len(cluster):]
	}
	if styled {
		b.WriteString("\x1b[0m")
	}
	return b.String()
}
