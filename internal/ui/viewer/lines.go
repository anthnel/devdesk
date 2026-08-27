package viewer

import (
	"strings"

	"github.com/charmbracelet/lipgloss"

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

	// Num is this line's place in the document, counting from 1, and it is
	// carried rather than derived. Everything downstream works on a *filtered*
	// slice — a search, a verbosity — so an index into what is on screen would
	// be a different number, and the one the gutter must not show: a line
	// number that renumbered itself under a filter would be worse than none.
	Num int
}

// buildLines splits a document into styled-per-line spans.
//
// It is called when the document loads, when `c` toggles and when `f` crosses
// the rendered boundary — and not otherwise: tokenising five megabytes per frame
// is not viable, and the filter and the wrap both work on the result rather than
// redoing it.
//
// `rendered` is not a second kind of highlighting, which is why it composes with
// `highlight` instead of replacing it. The markers have to be found before they
// can be taken away, so a rendered document is tokenised whatever `c` says; the
// colour is then dropped afterwards, and `c` off leaves clean prose rather than
// putting the asterisks back.
func buildLines(doc viewer.Document, highlight, rendered bool) []docLine {
	tokens := []viewer.Token{{Class: viewer.ClassText, Text: doc.Text}}
	switch {
	case rendered:
		tokens = viewer.RenderMarkdown(viewer.Tokenize(doc.Kind, doc.Text))
		if !highlight {
			tokens = flatten(tokens)
		}
	case highlight:
		tokens = viewer.Tokenize(doc.Kind, doc.Text)
	}

	lines := splitTokenLines(tokens)

	// A log's level rides on the line, not on a token. The two lists are the
	// same split of the same text — ParseLog and splitTokenLines both cut on
	// "\n" — so they line up index for index.
	for i := range lines {
		lines[i].Num = i + 1
		if i < len(doc.Lines) {
			lines[i].Level = doc.Lines[i].Level
		}
	}
	return lines
}

// flatten takes the classes off a stream, leaving the text exactly as it is.
//
// It is how `c` behaves in a rendered document: the markers stay gone — the
// rendering is a display, not a colouring — and what is left reads as ordinary
// prose. Classes are dropped rather than the tokens merged, because a merge
// would have to run before MarkMatches cuts them again for no gain.
func flatten(tokens []viewer.Token) []viewer.Token {
	out := make([]viewer.Token, 0, len(tokens))
	for _, token := range tokens {
		out = append(out, viewer.Token{Class: viewer.ClassText, Text: token.Text})
	}
	return out
}

// withText is a piece of a token: everything the run said about itself, over a
// shorter span of text.
//
// It is a copy rather than a fresh literal on purpose. Both of the functions
// below cut tokens up, and a literal has to name every field to keep it — which
// is how a highlight ends up working right until the line is long enough to be
// wrapped, the one case it exists for. Copying makes the omission unexpressible
// instead of something to remember for the next field anyone adds.
func withText(token viewer.Token, text string) viewer.Token {
	token.Text = text
	return token
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
			current = append(current, withText(token, part))
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
			current = append(current, withText(token, string(runes[:take])))
			runes = runes[take:]
			room -= take
		}
	}
	return append(segments, current)
}

// renderSegment paints a segment, one run at a time.
//
// A log line still reads as one piece — its level styles every run of it, so an
// ERROR is picked out at a glance, and a log has exactly one token per line
// unless a search cut it. That is the deliberate exception: an occurrence takes
// matchStyle even inside a log line, so the line comes out as level, match,
// level. A search that could not be seen in a log would be missing precisely
// where the lines are longest.
//
// Anything else is coloured by token class, with a match overriding it the same
// way.
func renderSegment(tokens []viewer.Token, level viewer.Level, isLog bool) string {
	var out strings.Builder
	for _, token := range tokens {
		out.WriteString(runStyle(token, level, isLog).Render(token.Text))
	}
	return out.String()
}

func runStyle(token viewer.Token, level viewer.Level, isLog bool) lipgloss.Style {
	switch {
	case token.Match:
		return matchStyle()
	case isLog:
		return levelStyle(level)
	default:
		return syntaxStyle(token.Class)
	}
}
