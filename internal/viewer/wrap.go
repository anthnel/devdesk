package viewer

import "strings"

// WrapLines soft-wraps content so no line exceeds width runes.
//
// It breaks at the width and not at a word boundary, deliberately. The content
// here is a log line, a stack trace or a minified document, where the columns
// carry meaning and a word-wrap would shuffle them; a paragraph of prose is not
// what this pane is for.
func WrapLines(content string, width int) string {
	if width <= 0 {
		return content
	}
	lines := strings.Split(content, "\n")
	out := make([]string, 0, len(lines))
	for _, line := range lines {
		runes := []rune(line)
		if len(runes) <= width {
			out = append(out, line)
			continue
		}
		for len(runes) > width {
			out = append(out, string(runes[:width]))
			runes = runes[width:]
		}
		if len(runes) > 0 {
			out = append(out, string(runes))
		}
	}
	return strings.Join(out, "\n")
}
