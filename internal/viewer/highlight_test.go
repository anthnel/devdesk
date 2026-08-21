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

		// The three kinds added with the rendered display. The invariant is what
		// makes the raw display of a Markdown its source to the character, so it
		// is exactly what `f` promises.
		{KindMarkdown, "# T\n\nSome **bold** and `code`.\n\n- one\n\n```go\nfunc f() {}\n```\n"},
		{KindMarkdown, "---\ntitle: hi\n---\n\n# After the front matter\n"},
		{KindDockerfile, "# c\nFROM alpine:3.19 AS build\nENV FOO=bar\nCMD [\"sh\"]\n"},
		{KindShell, "#!/usr/bin/env bash\nset -e\nN=\"world\"\nif [ -n \"$N\" ]; then\n  echo \"hi ${N}\"\nfi\n"},
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

// The mapping for the three kinds added with the rendered display, read off the
// lexers rather than guessed — which is what caught each of the three surprises
// below.
func TestMarkdownTokensAreClassified(t *testing.T) {
	tokens := Tokenize(KindMarkdown, sampleMarkdown)

	assertFirstPerClass(t, tokens, map[TokenClass]string{
		ClassHeading: "# Title\n",
		ClassStrong:  "**bold**",
		ClassEmph:    "*emph*",
		ClassStrike:  "~~gone~~",
		ClassKeyword: "-",
	})
}

// A Dockerfile instruction is the one thing worth colouring in the file, and it
// arrives as a bare Keyword — a type no earlier kind ever emitted.
func TestDockerfileInstructionsAreKeywords(t *testing.T) {
	tokens := Tokenize(KindDockerfile, "# note\nFROM alpine:3.19\nENV FOO=bar\n")

	assertFirstPerClass(t, tokens, map[TokenClass]string{
		ClassComment: "# note",
		ClassKeyword: "FROM",
		ClassString:  "alpine:3.19",
		ClassKey:     "FOO",
	})
}

// A shell variable arrives as NameVariable, which fell through to ordinary text
// before it was mapped — leaving a script coloured everywhere except the names.
func TestShellKeywordsAndVariablesAreClassified(t *testing.T) {
	tokens := Tokenize(KindShell, "#!/bin/bash\nN=1\nif [ -n \"$N\" ]; then\n  echo hi\nfi\n")

	classes := map[TokenClass]bool{}
	var variable, keyword bool
	for _, token := range tokens {
		classes[token.Class] = true
		if token.Text == "$N" && token.Class == ClassKey {
			variable = true
		}
		if token.Text == "if" && token.Class == ClassKeyword {
			keyword = true
		}
	}
	if !keyword {
		t.Error("a shell keyword is not classified as one")
	}
	if !variable {
		t.Error("a shell variable fell through to ordinary text")
	}
	if !classes[ClassComment] {
		t.Error("the shebang line is not a comment")
	}
}

// The guard on the Keyword split. Every kind that worked before this change
// emits KeywordConstant and never a bare Keyword, so routing the two apart could
// not disturb them — but only for as long as that stays true, and only if the
// test is equality: chroma implements a sub-category as `t/100 == other/100`, so
// InSubCategory(KeywordConstant) answers true for every keyword there is and
// would quietly put `if` and `FROM` back in the literal colour.
func TestAKeywordConstantIsStillALiteral(t *testing.T) {
	cases := []struct {
		kind Kind
		text string
		word string
	}{
		{KindJSON, `{"ok": true, "z": null}`, "true"},
		{KindYAML, "debug: true\nempty: null\n", "true"},
		{KindTOML, "debug = true\n", "true"},
	}

	for _, tc := range cases {
		var found bool
		for _, token := range Tokenize(tc.kind, tc.text) {
			if token.Text != tc.word {
				continue
			}
			found = true
			if token.Class != ClassLiteral {
				t.Errorf("%s: %q is class %d, want ClassLiteral", tc.kind, tc.word, token.Class)
			}
		}
		if !found {
			t.Errorf("%s: never saw %q", tc.kind, tc.word)
		}
	}
}

// The guard that makes Markdown affordable. chroma's markdown lexer ends its
// inline rules with a catch-all single-character alternative, so prose comes back
// one token per character — at the 5 MiB ceiling that is millions of Token values
// for a paragraph. Without coalescing this test counts one token per letter.
func TestAdjacentRunsOfOneClassAreCoalesced(t *testing.T) {
	prose := "a plain sentence of ordinary words with nothing marked up in it at all"
	tokens := Tokenize(KindMarkdown, prose)

	if len(tokens) != 1 {
		t.Errorf("a paragraph of prose came back as %d tokens, want 1", len(tokens))
	}
	if joined(tokens) != prose {
		t.Errorf("coalescing changed the text: %q", joined(tokens))
	}

	for _, token := range Tokenize(KindMarkdown, sampleMarkdown) {
		if token.Text == "" {
			t.Error("an empty run survived coalescing")
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
