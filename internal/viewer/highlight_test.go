package viewer

import (
	"strings"
	"testing"
)

// The invariant every caller depends on. The rendered lines are matched against
// the document's real ones, so a lexer that dropped or added a byte would
// desynchronise the two — and the failure would show up as a viewport scrolled
// to the wrong place, a long way from its cause.
func TestHighlightingReproducesTheInputExactly(t *testing.T) {
	cases := []struct {
		kind Kind
		text string
	}{
		{KindJSON, "{\n  \"name\": \"nginx\",\n  \"n\": 42,\n  \"ok\": true,\n  \"z\": null\n}"},
		{KindXML, "<?xml version=\"1.0\"?>\n<!-- c -->\n<root a=\"1\">text</root>\n"},
		{KindPlain, "just\nsome\ttext\n"},
		{KindLog, "2026-01-01 [ERROR] boom\n"},
		{KindJSON, ""},
		{KindJSON, "{not really json"},
	}

	for _, tc := range cases {
		var rebuilt strings.Builder
		for _, token := range Tokenize(tc.kind, tc.text) {
			rebuilt.WriteString(token.Text)
		}
		if rebuilt.String() != tc.text {
			t.Errorf("%s: tokens rebuild %q, want %q", tc.kind, rebuilt.String(), tc.text)
		}
	}
}

func TestJSONTokensAreClassified(t *testing.T) {
	tokens := Tokenize(KindJSON, `{"name": "nginx", "n": 42, "ok": true}`)

	seen := map[TokenClass]string{}
	for _, token := range tokens {
		if _, ok := seen[token.Class]; !ok {
			seen[token.Class] = token.Text
		}
	}

	for class, want := range map[TokenClass]string{
		ClassKey:     `"name"`,
		ClassString:  `"nginx"`,
		ClassNumber:  `42`,
		ClassLiteral: `true`,
		ClassPunct:   `{`,
	} {
		if seen[class] != want {
			t.Errorf("class %d first matched %q, want %q", class, seen[class], want)
		}
	}
}

func TestXMLTagsAndAttributesAreClassifiedApart(t *testing.T) {
	tokens := Tokenize(KindXML, `<!-- c --><root a="1">text</root>`)

	classes := map[TokenClass]bool{}
	for _, token := range tokens {
		classes[token.Class] = true
	}
	for _, want := range []TokenClass{ClassComment, ClassTag, ClassAttr, ClassString} {
		if !classes[want] {
			t.Errorf("class %d never appeared in an XML document that has one", want)
		}
	}
	if classes[ClassKey] {
		t.Error("an XML tag was classified as a JSON key")
	}
}

// A log's colour comes from its level, one whole line at a time, which is a
// different question from what a token is. Asking chroma would answer the wrong
// one.
func TestALogIsNotTokenized(t *testing.T) {
	tokens := Tokenize(KindLog, "2026-01-01 [ERROR] boom\nsecond line\n")
	if len(tokens) != 1 || tokens[0].Class != ClassText {
		t.Errorf("Tokenize returned %d tokens, want one plain run", len(tokens))
	}
}
