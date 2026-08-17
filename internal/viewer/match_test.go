package viewer

import (
	"strings"
	"testing"
)

func TestMatchRangesFindsEveryOccurrence(t *testing.T) {
	cases := []struct {
		name  string
		text  string
		query string
		want  []Range
	}{
		{"none", "hello world", "zzz", nil},
		{"one", "hello world", "world", []Range{{6, 11}}},
		{"twice", "port 80, port 443", "port", []Range{{0, 4}, {9, 13}}},
		{"case insensitive", "ERROR and error", "Error", []Range{{0, 5}, {10, 15}}},
		{"empty query", "hello", "", nil},
		{"empty text", "", "hello", nil},
		{"whole line", "abc", "abc", []Range{{0, 3}}},

		// Non-overlapping: the search advances past what it just found, so "aa" in
		// "aaa" is one match, not two. Overlapping ranges would break the single
		// walk MarkMatches does over the tokens.
		{"no overlap", "aaa", "aa", []Range{{0, 2}}},
	}

	for _, tc := range cases {
		t.Run(tc.name, func(t *testing.T) {
			got := MatchRanges(tc.text, tc.query)
			if len(got) != len(tc.want) {
				t.Fatalf("MatchRanges(%q, %q) = %v, want %v", tc.text, tc.query, got, tc.want)
			}
			for i := range got {
				if got[i] != tc.want[i] {
					t.Errorf("range %d = %v, want %v", i, got[i], tc.want[i])
				}
			}
		})
	}
}

// The offsets must name bytes of the *original* text, whatever the fold did to
// its length. A range that pointed into a longer lowercased copy would highlight
// beside the match, which looks like a bug in the highlight rather than in the
// fold.
func TestMatchRangesNameTheOriginalText(t *testing.T) {
	cases := []string{
		"hello world",
		"héllo wörld",        // multi-byte, same length folded
		"İstanbul and İzmir", // folds longer: the guard's case
		"日本語 テキスト",
	}

	for _, text := range cases {
		for _, query := range []string{"o", "l", "İ", "テ"} {
			for _, r := range MatchRanges(text, query) {
				if r.Start < 0 || r.End > len(text) || r.Start >= r.End {
					t.Fatalf("MatchRanges(%q, %q) returned %v, out of the text's bounds", text, query, r)
				}
				if !strings.EqualFold(text[r.Start:r.End], query) {
					t.Errorf("MatchRanges(%q, %q) points at %q", text, query, text[r.Start:r.End])
				}
			}
		}
	}
}

// The same invariant Tokenize promises. A marked line is rendered next to the
// document's own, so a byte gained or lost here desynchronises the two — and the
// failure would show up as a viewport scrolled to the wrong place, a long way
// from its cause.
func TestMarkingReproducesTheInputExactly(t *testing.T) {
	tokens := []Token{
		{Class: ClassPunct, Text: `{"`},
		{Class: ClassKey, Text: "name"},
		{Class: ClassPunct, Text: `": `},
		{Class: ClassString, Text: `"nginx-name"`},
		{Class: ClassPunct, Text: "}"},
	}

	var whole strings.Builder
	for _, token := range tokens {
		whole.WriteString(token.Text)
	}
	text := whole.String()

	for _, query := range []string{"name", "n", `"name"`, `me": "ng`, "{", "}", "zzz", ""} {
		marked := MarkMatches(tokens, MatchRanges(text, query))

		var rebuilt strings.Builder
		for _, token := range marked {
			rebuilt.WriteString(token.Text)
		}
		if rebuilt.String() != text {
			t.Errorf("query %q: marking rebuilds %q, want %q", query, rebuilt.String(), text)
		}
	}
}

// A query is free to cross class boundaries — `"name":` is a string and a
// punctuation token — so the ranges are computed on the flat text and the tokens
// are cut to fit, never the other way round.
func TestAMatchSpanningTwoTokensIsMarkedInBoth(t *testing.T) {
	tokens := []Token{
		{Class: ClassKey, Text: "name"},
		{Class: ClassPunct, Text: ": "},
		{Class: ClassString, Text: "dk"},
	}

	marked := MarkMatches(tokens, MatchRanges("name: dk", "me: d"))

	var matched []string
	for _, token := range marked {
		if token.Match {
			matched = append(matched, token.Text)
		}
	}
	if strings.Join(matched, "") != "me: d" {
		t.Errorf("the marked runs join to %q, want %q", strings.Join(matched, ""), "me: d")
	}
	if len(matched) != 3 {
		t.Errorf("the match was marked in %d runs, want 3 — one per token it crosses", len(matched))
	}
}

// The class survives the cut: an occurrence inside a key is still a key, which is
// why Match is a second field and not a ninth class.
func TestMarkingKeepsEachRunsClass(t *testing.T) {
	tokens := []Token{
		{Class: ClassKey, Text: "hostname"},
		{Class: ClassPunct, Text: "="},
		{Class: ClassString, Text: "hostile"},
	}

	want := []Token{
		{Class: ClassKey, Text: "host", Match: true},
		{Class: ClassKey, Text: "name"},
		{Class: ClassPunct, Text: "="},
		{Class: ClassString, Text: "host", Match: true},
		{Class: ClassString, Text: "ile"},
	}

	got := MarkMatches(tokens, MatchRanges("hostname=hostile", "host"))
	if len(got) != len(want) {
		t.Fatalf("the line was cut into %d runs, want %d: %v", len(got), len(want), got)
	}
	for i := range got {
		if got[i] != want[i] {
			t.Errorf("run %d = %+v, want %+v", i, got[i], want[i])
		}
	}
}

// Nothing found, nothing touched: the slice comes back as it went in, so the
// no-search path costs nothing.
func TestMarkingWithoutRangesIsANoOp(t *testing.T) {
	tokens := []Token{{Class: ClassText, Text: "unchanged"}}

	marked := MarkMatches(tokens, nil)
	if len(marked) != 1 || marked[0].Match || marked[0].Text != "unchanged" {
		t.Errorf("MarkMatches with no ranges returned %v", marked)
	}
}
