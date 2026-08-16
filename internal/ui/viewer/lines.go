package viewer

import (
	"strings"

	"github.com/anthnel/devdesk/internal/viewer"
)

// docLine is one source line, kept as the token spans it is made of.
//
// The spans are the point. Wrapping counts runes, and an ANSI escape's bytes
// count as width — the same hazard Rule 122 describes for table cells — so a
// line that has already been coloured cannot be wrapped without the cut landing
// inside an escape sequence. Keeping the line as tokens means it is wrapped
// while it is still plain and coloured afterwards, which is the only order that
// works.
type docLine struct {
	Tokens []viewer.Token
	Plain  string
	Level  viewer.Level
}

// buildLines splits a document into styled-per-line spans.
//
// It is called when the document loads and when `c` toggles, and not otherwise:
// tokenising five megabytes per frame is not viable, and the filter and the
// wrap both work on the result rather than redoing it.
func buildLines(doc viewer.Document, highlight bool) []docLine {
	tokens := []viewer.Token{{Class: viewer.ClassText, Text: doc.Text}}
	if highlight {
		tokens = viewer.Tokenize(doc.Kind, doc.Text)
	}

	lines := splitTokenLines(tokens)

	// A log's level rides on the line, not on a token. The two lists are the
	// same split of the same text — ParseLog and splitTokenLines both cut on
	// "\n" — so they line up index for index.
	for i := range lines {
		if i < len(doc.Lines) {
			lines[i].Level = doc.Lines[i].Level
		}
	}
	return lines
}

// splitTokenLines cuts a document-wide token stream into one slice of tokens per
// line, splitting any token that spans a newline.
func splitTokenLines(tokens []viewer.Token) []docLine {
	var lines []docLine
	var current []viewer.Token
	var plain strings.Builder

	flush := func() {
		lines = append(lines, docLine{Tokens: current, Plain: plain.String()})
		current = nil
		plain.Reset()
	}

	for _, token := range tokens {
		parts := strings.Split(token.Text, "\n")
		for i, part := range parts {
			if i > 0 {
				flush()
			}
			if part == "" {
				continue
			}
			current = append(current, viewer.Token{Class: token.Class, Text: part})
			plain.WriteString(part)
		}
	}
	flush()

	// A document ending in a newline produces a trailing empty line, which is
	// true of the text but renders as a blank row nobody asked for.
	if n := len(lines); n > 1 && lines[n-1].Plain == "" {
		lines = lines[:n-1]
	}
	return lines
}

// wrapTokens splits a line into segments no wider than width runes, cutting
// tokens where a segment ends.
//
// It breaks at the width rather than at a word boundary, for the same reason
// viewer.WrapLines does: the content here is a log line, a stack trace or a
// minified document, where the columns carry meaning.
func wrapTokens(tokens []viewer.Token, width int) [][]viewer.Token {
	if width <= 0 {
		return [][]viewer.Token{tokens}
	}

	var segments [][]viewer.Token
	var current []viewer.Token
	room := width

	for _, token := range tokens {
		runes := []rune(token.Text)
		for len(runes) > 0 {
			if room == 0 {
				segments = append(segments, current)
				current, room = nil, width
			}
			take := min(room, len(runes))
			current = append(current, viewer.Token{Class: token.Class, Text: string(runes[:take])})
			runes = runes[take:]
			room -= take
		}
	}
	return append(segments, current)
}

// render paints a segment. A log line takes one style for the whole of it, so
// that an ERROR is picked out at a glance; anything else is coloured token by
// token.
func renderSegment(tokens []viewer.Token, level viewer.Level, isLog bool) string {
	if isLog {
		var plain strings.Builder
		for _, token := range tokens {
			plain.WriteString(token.Text)
		}
		return levelStyle(level).Render(plain.String())
	}

	var out strings.Builder
	for _, token := range tokens {
		out.WriteString(syntaxStyle(token.Class).Render(token.Text))
	}
	return out.String()
}
