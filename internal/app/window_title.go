package app

import (
	"strings"
	"unicode"

	"github.com/charmbracelet/x/ansi"

	"github.com/anthnel/devdesk/internal/command"
)

const (
	// maxWindowTitleWidth is the widest window title, in terminal cells.
	maxWindowTitleWidth = 60

	windowTitleApp       = "dk"
	windowTitleSeparator = " · "
	windowTitleTail      = "…"
	// windowTitleDefaultContext is shown when the context name is empty.
	windowTitleDefaultContext = "default"
)

// windowTitle builds the terminal window title "dk · <view> · <context>".
// The view is its canonical command name. It is a pure function: the same
// inputs always give the same title, so the renderer re-emits it only when the
// view or the context changes. Both inputs are sanitized because a context
// name comes from a file on disk and must not inject terminal sequences.
func windowTitle(view command.ViewType, context string) string {
	parts := []string{windowTitleApp}
	if v := titleSegment(string(view)); v != "" {
		parts = append(parts, v)
	}
	c := titleSegment(context)
	if c == "" {
		c = windowTitleDefaultContext
	}
	parts = append(parts, c)
	return ansi.Truncate(strings.Join(parts, windowTitleSeparator), maxWindowTitleWidth, windowTitleTail)
}

// titleSegment removes escape sequences, control characters, invisible format
// characters (bidi overrides, zero-width, BOM) and line or paragraph separators
// from s, then trims blanks. An empty result means the segment is skipped.
func titleSegment(s string) string {
	s = ansi.Strip(strings.ToValidUTF8(s, ""))
	s = strings.Map(func(r rune) rune {
		if unicode.IsControl(r) || unicode.In(r, unicode.Cf, unicode.Zl, unicode.Zp) {
			return -1
		}
		return r
	}, s)
	return strings.TrimSpace(s)
}
