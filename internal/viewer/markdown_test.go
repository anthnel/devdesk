package viewer

import (
	"strings"
	"testing"
)

const sampleMarkdown = "# Title\n\n" +
	"Some **bold** and *emph* and ~~gone~~ text.\n\n" +
	"- item one\n- item two\n\n" +
	"> quoted\n\n" +
	"Inline `code` here.\n\n" +
	"```go\nfunc main() { s := \"hi\" }\n```\n"

func rendered(t *testing.T, text string) []Token {
	t.Helper()
	return RenderMarkdown(Tokenize(KindMarkdown, text))
}

func joined(tokens []Token) string {
	var out strings.Builder
	for _, token := range tokens {
		out.WriteString(token.Text)
	}
	return out.String()
}

// The whole point of the rendered display: the markers go, and the classes stay
// to say what they meant.
func TestRenderedMarkdownDropsTheMarkers(t *testing.T) {
	text := joined(rendered(t, sampleMarkdown))

	for _, marker := range []string{"# ", "**", "~~", "```", "`code`"} {
		if strings.Contains(text, marker) {
			t.Errorf("the rendered document still shows %q:\n%s", marker, text)
		}
	}
	for _, want := range []string{"Title", "bold", "emph", "gone", "code", "• item one", "│ quoted"} {
		if !strings.Contains(text, want) {
			t.Errorf("the rendered document lost %q:\n%s", want, text)
		}
	}
}

// Removing the marks is only worth doing if something is left saying the run was
// ever different. Three of the classes carry a text attribute for exactly this.
func TestRenderedMarkdownKeepsWhatTheMarkersMeant(t *testing.T) {
	// The heading keeps the newline that ends its line: the marker is the "# ",
	// and the line break is content.
	want := map[TokenClass]string{
		ClassHeading: "Title\n",
		ClassStrong:  "bold",
		ClassEmph:    "emph",
		ClassStrike:  "gone",
	}

	seen := map[TokenClass]string{}
	for _, token := range rendered(t, sampleMarkdown) {
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

// chroma hands a fenced block to the language's own lexer, so the code inside is
// already coloured as Go. The rendering must take the fence off without taking
// that with it — which is what the by-the-line delimiter rule is for, since
// coalescing joins the backticks to the language name and sometimes to the first
// run of the body.
func TestRenderedMarkdownKeepsFencedCodeColored(t *testing.T) {
	tokens := rendered(t, sampleMarkdown)

	text := joined(tokens)
	if !strings.Contains(text, "func main()") {
		t.Fatalf("the fenced code did not survive the rendering:\n%s", text)
	}
	if strings.Contains(text, "```go") || strings.Contains(text, "go\nfunc") {
		t.Errorf("the fence delimiter or its language name is still on screen:\n%s", text)
	}

	var keyword, str bool
	for _, token := range tokens {
		if token.Text == "func" && token.Class == ClassKeyword {
			keyword = true
		}
		if token.Text == `"hi"` && token.Class == ClassString {
			str = true
		}
	}
	if !keyword || !str {
		t.Error("the fenced Go lost its own coloring when the fence was removed")
	}
}

// A body that opens on a string is the case the by-the-line rule exists for:
// coalescing joins it onto the delimiter run, so a whole-token match would drop
// the first line of the code with the fence.
func TestAFencedBodyStartingWithAStringSurvives(t *testing.T) {
	text := joined(rendered(t, "```go\n\"first\"\nsecond\n```\n"))

	if !strings.Contains(text, `"first"`) {
		t.Errorf("the first run of the fenced body went with the delimiter:\n%q", text)
	}
	if strings.Contains(text, "```") {
		t.Errorf("the delimiter survived:\n%q", text)
	}
}

// Rendering is a display, not a parser, and the line is drawn where the lexer
// stops telling us things. A link's brackets arrive as ordinary text — the same
// class as a bracket in a sentence — so reconstructing one would be guesswork.
func TestRenderedMarkdownLeavesLinksAlone(t *testing.T) {
	tokens := rendered(t, "See [the docs](http://x.y) for more.\n")
	text := joined(tokens)

	if !strings.Contains(text, "[the docs](http://x.y)") {
		t.Errorf("the link was rewritten:\n%q", text)
	}

	var linkText, url bool
	for _, token := range tokens {
		if token.Text == "the docs" && token.Class == ClassKey {
			linkText = true
		}
		if token.Text == "http://x.y" && token.Class == ClassAttr {
			url = true
		}
	}
	if !linkText || !url {
		t.Error("a link's text and its URL are not colored apart, which is what stands in for rendering it")
	}
}

// Markdown is the only kind with a rendered display, and the view asks this
// before offering `f` (Rule 130) — a wrong answer advertises a key that would
// show an empty pane.
func TestOnlyMarkdownIsRenderable(t *testing.T) {
	if !KindMarkdown.Renderable() {
		t.Error("Markdown does not report the rendered display it has")
	}
	for _, kind := range []Kind{KindPlain, KindJSON, KindXML, KindLog, KindYAML, KindTOML, KindDockerfile, KindShell} {
		if kind.Renderable() {
			t.Errorf("%q reports a rendered display it has no renderer for", kind)
		}
	}
}

// No kind may claim both: `f` is one key with one meaning, and a kind offering a
// tree and a rendering would make it ambiguous.
func TestNoKindHasTwoDerivedDisplays(t *testing.T) {
	for _, kind := range []Kind{
		KindPlain, KindJSON, KindXML, KindLog,
		KindYAML, KindTOML, KindMarkdown, KindDockerfile, KindShell,
	} {
		if kind.Structured() && kind.Renderable() {
			t.Errorf("%q claims both a tree and a rendered form", kind)
		}
	}
}
