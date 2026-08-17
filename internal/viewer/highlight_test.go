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
		{KindYAML, "# c\napp:\n  name: dk\n  port: 8080\n  on: true\n---\nlist:\n  - one\n"},
		{KindTOML, "# c\ntitle = \"dk\"\n\n[app]\nport = 8080\nratio = 1.5\n"},

		// A malformed document is exactly the one someone opens the viewer for. The
		// lexers do not refuse it, and the invariant has to hold anyway: the
		// rendered lines are matched against the document's real ones by index.
		{KindYAML, "app:\n\tname: [unclosed\n  : :\n"},
		{KindTOML, "[unclosed\nkey = = 1\n"},
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

func TestYAMLTokensAreClassified(t *testing.T) {
	tokens := Tokenize(KindYAML, "# note\nname: devdesk\nport: 8080\ndebug: true\n")

	assertFirstPerClass(t, tokens, map[TokenClass]string{
		ClassComment: "# note",
		ClassKey:     "name",
		ClassPunct:   ":",
		ClassString:  "devdesk",
		ClassNumber:  "8080",
		ClassLiteral: "true",
	})
}

// The test that earns its place: every TOML key, table headers included, comes
// out of chroma as NameOther. Without that mapping a TOML document is coloured
// everywhere except its keys.
func TestTOMLKeysAreClassifiedAsKeys(t *testing.T) {
	tokens := Tokenize(KindTOML, "# note\ntitle = \"devdesk\"\n\n[app]\nport = 8080\ndebug = true\n")

	assertFirstPerClass(t, tokens, map[TokenClass]string{
		ClassComment: "# note",
		ClassKey:     "title",
		ClassPunct:   "=",
		ClassString:  `"devdesk"`,
		ClassNumber:  "8080",
		ClassLiteral: "true",
	})

	var tableName bool
	for _, token := range tokens {
		if token.Text == "app" && token.Class == ClassKey {
			tableName = true
		}
	}
	if !tableName {
		t.Error("a [table] header is not classified as a key; it names a section, which is the same thing")
	}
}

// assertFirstPerClass checks the first token of each class, which is what pins a
// mapping without asserting on the whole stream — a lexer is free to split a run
// differently between versions.
func assertFirstPerClass(t *testing.T, tokens []Token, want map[TokenClass]string) {
	t.Helper()

	seen := map[TokenClass]string{}
	for _, token := range tokens {
		if _, ok := seen[token.Class]; !ok {
			seen[token.Class] = token.Text
		}
	}
	for class, text := range want {
		if seen[class] != text {
			t.Errorf("class %d first matched %q, want %q", class, seen[class], text)
		}
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
