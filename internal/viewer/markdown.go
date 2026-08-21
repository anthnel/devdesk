package viewer

import "strings"

// RenderMarkdown turns a Markdown token stream into its rendered form: the same
// document with the markers taken away and the classes left to say what they
// meant.
//
// It is not a Markdown implementation, and the distinction is the whole design.
// Every marker removed below is one the *lexer* already identified — a run
// chroma classified as a heading, a strong, an emphasis, a strikethrough, a list
// bullet, a quote prefix, a fence. Nothing here parses; nothing here decides
// from what a character looks like. That is why the link syntax survives
// untouched: `[` and `]` arrive as bare Text, indistinguishable from a bracket
// in prose, and rebuilding a link out of them would be exactly the guesswork
// this package refuses everywhere else. A link is already coloured — its text in
// the key colour, its URL in the attribute colour — which is most of what the
// rendering would have bought.
//
// Deliberately out of scope for the same reason: tables, thematic breaks and
// re-indenting nested lists. `f` shows the source, to the character, so nothing
// the rendering drops is out of reach.
//
// The one invariant this breaks is Tokenize's: concatenating the result does
// **not** reproduce the input. It is the point of the function. Everything
// downstream copes because it reads the tokens rather than the document —
// splitTokenLines rebuilds each line's plain text from them, so the search
// filters and highlights what is actually on screen.
func RenderMarkdown(tokens []Token) []Token {
	out := make([]Token, 0, len(tokens))
	inFence := false

	for _, token := range tokens {
		// The body between two fences is not ours to touch: chroma has already
		// handed it to the language's own lexer, so a ```go block arrives
		// coloured as Go and stays that way.
		if rest, ok := stripFenceLine(token); ok {
			inFence = !inFence
			out = appendRendered(out, token, rest)
			continue
		}
		if inFence {
			out = append(out, token)
			continue
		}

		out = appendRendered(out, token, renderText(token))
	}
	// Dropping runs leaves neighbours of the same class side by side — the two
	// ends of a fence close over the text between the blocks. Merging them again
	// costs one pass and keeps the stream as short as the screen it describes.
	return coalesce(out)
}

// appendRendered adds a token under a new text, dropping it when nothing is
// left. An empty run would render as nothing anyway; keeping it would only make
// the stream longer than the screen it describes.
func appendRendered(out []Token, token Token, text string) []Token {
	if text == "" {
		return out
	}
	token.Text = text
	return append(out, token)
}

// renderText is one token's markers taken off.
func renderText(token Token) string {
	switch token.Class {
	case ClassHeading:
		return strings.TrimPrefix(strings.TrimLeft(token.Text, "#"), " ")
	case ClassStrong:
		return trimMarker(token.Text, "**", "__")
	case ClassEmph:
		return trimMarker(token.Text, "*", "_")
	case ClassStrike:
		return trimMarker(token.Text, "~~")
	case ClassString:
		// Inline code: the backticks go, the string colour stays, which is what
		// says it was code at all once the delimiters are gone.
		return trimMarker(token.Text, "`")
	case ClassKeyword:
		return renderBlockMarker(token.Text)
	default:
		return token.Text
	}
}

// trimMarker removes a wrapping marker from a run, whichever of the given forms
// it used, and takes a lone marker down to nothing.
//
// Both ends are handled independently because the lexer does not always deliver
// a marked run whole: emphasis arrives as three tokens ("*", "emph", "*") while
// a strong arrives as two ("**bold", "**"). Trimming each side on its own merits
// covers every split without this function having to know which one it got.
func trimMarker(text string, markers ...string) string {
	for _, marker := range markers {
		if text == marker {
			return ""
		}
	}
	for _, marker := range markers {
		if strings.HasPrefix(text, marker) {
			text = text[len(marker):]
			break
		}
	}
	for _, marker := range markers {
		if strings.HasSuffix(text, marker) && len(text) >= len(marker) {
			text = text[:len(text)-len(marker)]
			break
		}
	}
	return text
}

// renderBlockMarker replaces the two block markers the markdown lexer emits as
// keywords: a list bullet and a blockquote prefix.
//
// A bullet becomes a bullet because that is what the character was standing in
// for; a quote becomes the rule a terminal draws down the margin. An ordered
// list's "1." is already what it means and is left alone, and so is a task
// list's "[x]" — a checkbox with its brackets removed reads as a stray letter.
func renderBlockMarker(text string) string {
	switch strings.TrimSpace(text) {
	case "-", "*", "+":
		return strings.Replace(text, strings.TrimSpace(text), "•", 1)
	case ">":
		return strings.Replace(text, ">", "│", 1)
	default:
		return text
	}
}

// stripFenceLine recognises a fenced block's boundary and returns whatever the
// run holds after it.
//
// The lexer gives the opening backticks, the language name and the closing
// backticks the string class, and coalescing then joins them: an opening fence
// arrives as a single run reading "```go" and a newline. So the delimiter is
// taken off by the *line* rather than matched whole — which is also what keeps a
// body starting with a string, joined onto the delimiter by that same
// coalescing, from disappearing with it.
//
// An inline `code` span is a string too, and is not caught: it opens with one
// backtick, not three.
func stripFenceLine(token Token) (string, bool) {
	if token.Class != ClassString || !strings.HasPrefix(token.Text, "```") {
		return "", false
	}
	_, rest, found := strings.Cut(token.Text, "\n")
	if !found {
		return "", true
	}
	return rest, true
}
