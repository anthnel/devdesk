package viewer

import (
	"strings"
	"testing"

	"github.com/charmbracelet/x/ansi"

	"github.com/anthnel/devdesk/internal/ui/testutil"
	"github.com/anthnel/devdesk/internal/ui/theme"
	viewerpkg "github.com/anthnel/devdesk/internal/viewer"
)

const markdownDoc = "# Title\n\n" +
	"Some **bold** text and a `snippet`.\n\n" +
	"- item one\n- item two\n\n" +
	"> quoted\n"

func markdownModel(t *testing.T) Model {
	t.Helper()
	return open(t, fakeSource{name: "README.md", content: markdownDoc})
}

// A Markdown document opens on the view it is meant to be read as, the way a
// JSON opens on its tree. `f` is what the source is for.
func TestMarkdownOpensRendered(t *testing.T) {
	m := markdownModel(t)

	if m.display != displayRendered {
		t.Fatalf("display = %d, want displayRendered", m.display)
	}
	if got := m.formatLabel(); got != "markdown · rendered" {
		t.Errorf("formatLabel = %q, want %q", got, "markdown · rendered")
	}

	// Stripped: a bullet and the text after it are two runs with different
	// styles, so the escape sequence between them breaks a substring match.
	view := ansi.Strip(m.View())
	for _, marker := range []string{"# Title", "**bold**", "`snippet`"} {
		if strings.Contains(view, marker) {
			t.Errorf("the rendered pane still shows %q:\n%s", marker, view)
		}
	}
	if !strings.Contains(view, "• item one") {
		t.Errorf("a list bullet was not rendered:\n%s", view)
	}
	if !strings.Contains(view, "│ quoted") {
		t.Errorf("a blockquote marker was not rendered:\n%s", view)
	}
}

// One key, one axis: the document as it is against the one view its kind
// derives. The wording of the shortcut says which way it goes.
func TestFSwitchesBetweenRenderedAndRaw(t *testing.T) {
	m := markdownModel(t)

	if !hasShortcut(m, "f") {
		t.Fatal("a Markdown document does not offer f")
	}

	m = feed(t, m, testutil.Key("f"))
	if m.display != displayText {
		t.Fatalf("f left the display at %d, want displayText", m.display)
	}
	if got := m.formatLabel(); got != "markdown · raw" {
		t.Errorf("formatLabel = %q, want %q", got, "markdown · raw")
	}

	// The raw display is the source, to the character — which is what makes the
	// rendering's omissions acceptable rather than losses.
	view := ansi.Strip(m.View())
	for _, marker := range []string{"# Title", "**bold**", "- item one", "> quoted", "`snippet`"} {
		if !strings.Contains(view, marker) {
			t.Errorf("the raw display does not show %q:\n%s", marker, view)
		}
	}

	m = feed(t, m, testutil.Key("f"))
	if m.display != displayRendered {
		t.Errorf("f did not come back to the rendered display, landed on %d", m.display)
	}
}

// `c` is coloring and `f` is the display, and the two do not overlap. Turning
// the color off in a rendered document must not put the asterisks back — that
// would make one screen reachable two ways, which is the shape §3.9 removed from
// the secret backend and the deleted `:theme` command took with it.
func TestColoringOffKeepsTheMarkersHidden(t *testing.T) {
	m := feed(t, markdownModel(t), testutil.Key("c"))

	if m.display != displayRendered {
		t.Fatal("c moved the display")
	}
	view := ansi.Strip(m.View())
	if strings.Contains(view, "**bold**") || strings.Contains(view, "# Title") {
		t.Errorf("c brought the markers back:\n%s", view)
	}
	if !strings.Contains(view, "bold") {
		t.Error("c lost the text along with the color")
	}
	if strings.Contains(m.View(), syntaxStyle(viewerpkg.ClassHeading).Render("Title")) {
		t.Error("the heading is still styled with coloring off")
	}
}

// The search reads what is on the screen. In a rendered document that is the
// rendered text, which is the only answer that can agree with what the user is
// looking at.
func TestSearchWorksInRenderedMarkdown(t *testing.T) {
	m := markdownModel(t)

	m = feed(t, m, testutil.Key("/"))
	m = feed(t, m, testutil.Type("bold")...)
	m = feed(t, m, testutil.Key("enter"))

	if !m.FilterBarVisible() {
		t.Error("the filter bar is not offered in a rendered document")
	}
	view := ansi.Strip(m.View())
	if !strings.Contains(view, "bold") {
		t.Errorf("the matching line is missing:\n%s", view)
	}
	if strings.Contains(view, "item one") {
		t.Errorf("a line with no match survived the filter:\n%s", view)
	}
	if m.matchedLines != 1 {
		t.Errorf("matchedLines = %d, want 1", m.matchedLines)
	}
}

// The same chain as the YAML and TOML assertion: the name decides the kind, the
// kind picks the lexer, the class picks the style. Each link is unit-tested
// apart; this is what fails when one is not wired to the next.
func TestDockerfilesAndShellScriptsReachTheScreenColored(t *testing.T) {
	cases := []struct {
		name    string
		file    string
		content string
		format  string
		keyword string
	}{
		{"a Dockerfile by name", "Dockerfile", "FROM alpine:3.19\n", "dockerfile", "FROM"},
		{"a Dockerfile with a suffix", "Dockerfile.dev", "FROM alpine:3.19\n", "dockerfile", "FROM"},
		{"a shell script", "deploy.sh", "if [ -n \"$HOME\" ]; then\n  echo hi\nfi\n", "shell", "if"},
	}

	for _, tc := range cases {
		t.Run(tc.name, func(t *testing.T) {
			m := open(t, fakeSource{name: tc.file, content: tc.content})

			if got := m.formatLabel(); got != tc.format {
				t.Errorf("formatLabel = %q, want %q", got, tc.format)
			}
			if got := m.View(); !strings.Contains(got, syntaxStyle(viewerpkg.ClassKeyword).Render(tc.keyword)) {
				t.Errorf("%q is not colored as a keyword:\n%s", tc.keyword, got)
			}
		})
	}
}

// Neither derives a tree, so `f` is not offered and does nothing (Rule 130).
// Markdown is the counter-case and is deliberately in the list: it *does* offer
// f, and it must still never open on a tree.
func TestNoNewKindOffersTheTree(t *testing.T) {
	cases := []struct {
		file    string
		content string
		display display
		offersF bool
	}{
		{"Dockerfile", "FROM alpine\n", displayText, false},
		{"deploy.sh", "echo hi\n", displayText, false},
		{"README.md", "# hi\n", displayRendered, true},
	}

	for _, tc := range cases {
		t.Run(tc.file, func(t *testing.T) {
			m := open(t, fakeSource{name: tc.file, content: tc.content})

			if m.display != tc.display {
				t.Errorf("display = %d, want %d", m.display, tc.display)
			}
			if m.structured() {
				t.Error("the document claims a tree it has no parser for")
			}
			if hasShortcut(m, "f") != tc.offersF {
				t.Errorf("offers f = %v, want %v", hasShortcut(m, "f"), tc.offersF)
			}
		})
	}
}

// Rule 115, for the styles added here. Three of them carry an attribute rather
// than a color — bold, italic, strikethrough — which is exactly the shape that
// invites a lipgloss style written without a background; a styled run closes
// with a reset, which would then take the pane's background with it to the right
// margin.
func TestEveryRenderedStyleCarriesTheAppBackground(t *testing.T) {
	for _, class := range []viewerpkg.TokenClass{
		viewerpkg.ClassHeading, viewerpkg.ClassStrong,
		viewerpkg.ClassEmph, viewerpkg.ClassStrike, viewerpkg.ClassKeyword,
	} {
		if got := syntaxStyle(class).GetBackground(); got != theme.ColorBackground {
			t.Errorf("class %d has background %v, want the app background %v",
				class, got, theme.ColorBackground)
		}
	}

	for _, line := range strings.Split(markdownModel(t).textViewport.View(), "\n") {
		if strings.TrimSpace(ansi.Strip(line)) == "" {
			continue
		}
		if !strings.Contains(line, "\x1b[") {
			t.Fatalf("line %q carries no styling at all, so it has no background", line)
		}
	}
}
