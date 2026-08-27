package viewer

import "strings"

// Range is a half-open byte range inside a line: [Start, End).
//
// Bytes rather than runes, because a token holds a string and slicing one by
// byte is exact and allocates nothing. The wrap counts runes, which is a
// different question and stays where it is.
type Range struct {
	Start int
	End   int
}

// MatchRanges is every place query occurs in text, folding case unless
// caseSensitive says not to.
//
// It is the *only* thing that decides a search matched, and it answers where at
// the same time. Nothing else may re-derive it: the filter that hides
// non-matching lines is `len(MatchRanges(...)) > 0`, so "a line that survived the
// search carries at least one highlighted occurrence" is true by construction
// rather than by two calculations agreeing. Two rules for one question is what
// scan.Categorize and Result.SecretVerdict each had to undo, and the symptom was
// the same both times — something counted in one place and absent from the other.
//
// The case is a parameter for that same reason. It decides which lines survive
// *and* which spans are highlighted, so a caller that filtered on its own reading
// of the flag would be exactly the second calculation this function exists to
// prevent.
//
// The ranges come back in order and never overlap, which is what lets MarkMatches
// walk the tokens once.
func MatchRanges(text, query string, caseSensitive bool) []Range {
	if text == "" || query == "" {
		return nil
	}

	haystack, needle := text, query
	if !caseSensitive {
		haystack, needle = strings.ToLower(text), strings.ToLower(query)

		// The exactness guard, and it belongs to this branch alone.
		// strings.ToLower can change a string's length in bytes — 'İ' folds to
		// two runes — and an offset into the folded text then names a different
		// byte of the original, so the highlight would land beside the match.
		// When the lengths disagree the search falls back to a case-sensitive
		// one: fewer matches, but every one of them in the right place.
		if len(haystack) != len(text) || len(needle) != len(query) {
			haystack, needle = text, query
		}
	}

	var ranges []Range
	for from := 0; from+len(needle) <= len(haystack); {
		i := strings.Index(haystack[from:], needle)
		if i < 0 {
			break
		}
		start := from + i
		ranges = append(ranges, Range{Start: start, End: start + len(needle)})
		from = start + len(needle)
	}
	return ranges
}

// MarkMatches splits tokens at the range boundaries and marks what falls inside.
//
// The split has to happen while the text is still plain, for the reason docLine
// exists at all: a styled run cannot be cut, because the measure counts an escape
// sequence's bytes as width and the cut lands inside the sequence (Rule 122, one
// layer up). A match is therefore one more span, never a colour laid over a
// finished line.
//
// A query crosses class boundaries freely — searching `"name":` spans a string
// and a punctuation token — which is why the ranges are computed on the line's
// flat text and applied to the tokens afterwards.
//
// The invariant Tokenize promises holds here too: concatenating the result
// reproduces the input exactly.
func MarkMatches(tokens []Token, ranges []Range) []Token {
	if len(ranges) == 0 {
		return tokens
	}

	out := make([]Token, 0, len(tokens)+2*len(ranges))
	offset, next := 0, 0

	for _, token := range tokens {
		start := offset
		end := start + len(token.Text)
		offset = end

		// Ranges that closed before this token can never apply to a later one:
		// they are ordered and disjoint.
		for next < len(ranges) && ranges[next].End <= start {
			next++
		}

		pos, i := start, next
		for pos < end {
			if i >= len(ranges) || ranges[i].Start >= end {
				out = appendSpan(out, token, pos-start, end-start, false)
				break
			}
			if plainUntil := ranges[i].Start; plainUntil > pos {
				out = appendSpan(out, token, pos-start, plainUntil-start, false)
				pos = plainUntil
			}
			// A range may run past this token and into the next; it is consumed
			// only once it has actually closed.
			stop := min(ranges[i].End, end)
			out = appendSpan(out, token, pos-start, stop-start, true)
			pos = stop
			if ranges[i].End <= end {
				i++
			}
		}
	}
	return out
}

// appendSpan adds a slice of a token, keeping everything the run said about
// itself and stamping whether the search found it. It copies rather than building
// a literal so that a field added to Token later cannot be dropped here in
// silence.
//
// An empty slice is dropped: it would render as nothing and concatenate to
// nothing.
func appendSpan(out []Token, token Token, from, to int, match bool) []Token {
	if from >= to {
		return out
	}
	token.Text = token.Text[from:to]
	token.Match = match
	return append(out, token)
}
