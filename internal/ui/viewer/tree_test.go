package viewer

import (
	"strings"
	"testing"

	"github.com/anthnel/devdesk/internal/command"
	"github.com/anthnel/devdesk/internal/config"
	"github.com/anthnel/devdesk/internal/ui/testutil"
	viewerpkg "github.com/anthnel/devdesk/internal/viewer"
)

// A .json opens as json, which is what the request asked for.
func TestAStructuredDocumentOpensOnItsTree(t *testing.T) {
	m := jsonModel(t)

	if m.display != displayTree {
		t.Error("a JSON document did not open on its tree")
	}
	if got := m.formatLabel(); got != "json · tree" {
		t.Errorf("formatLabel = %q, want \"json · tree\"", got)
	}
}

func TestAnXMLDocumentOpensOnItsTree(t *testing.T) {
	m := open(t, fakeSource{name: "pod.xml", content: `<pod name="web"><image>nginx</image></pod>`})

	if m.display != displayTree {
		t.Error("an XML document did not open on its tree")
	}
	if m.doc.Kind != viewerpkg.KindXML {
		t.Errorf("Kind = %q, want xml", m.doc.Kind)
	}
}

func TestATextDocumentHasNoTree(t *testing.T) {
	m := open(t, fakeSource{name: "notes.md", content: "# hello"})

	if m.display != displayText {
		t.Error("a text document opened on a tree it does not have")
	}
	if m.structured() {
		t.Error("structured() is true for a Markdown file")
	}
}

func TestCollapsingANodeHidesItsDescendants(t *testing.T) {
	m := jsonModel(t)
	before := len(m.tree.Items())

	// The root is the first row; collapsing it leaves one.
	m = feed(t, m, testutil.Key("left"))

	if got := len(m.tree.Items()); got != 1 {
		t.Errorf("%d rows with the root collapsed, want 1 (was %d)", got, before)
	}

	m = feed(t, m, testutil.Key("right"))
	if got := len(m.tree.Items()); got != before {
		t.Errorf("%d rows after expanding again, want %d", got, before)
	}
}

// datatable clamps the cursor, so a collapse that removes the rows under it
// leaves it on a row that still exists rather than past the end.
func TestTheCursorStaysOnARowThatSurvivesACollapse(t *testing.T) {
	m := jsonModel(t)
	m = feed(t, m, testutil.Key("G")) // last row

	m = feed(t, m, testutil.Key("left")) // collapse whatever is under it
	m = feed(t, m, testutil.Key("g"), testutil.Key("left"))

	if _, ok := m.tree.Selected(); !ok {
		t.Error("nothing is selected after collapsing to a single row")
	}
	if cursor := m.tree.Cursor(); cursor >= len(m.tree.Items()) {
		t.Errorf("cursor = %d with %d rows — it is past the end", cursor, len(m.tree.Items()))
	}
}

// A leaf has nothing to expand into, so the keys do nothing rather than
// swallowing a keypress that looked like it worked.
func TestExpandingALeafDoesNothing(t *testing.T) {
	m := open(t, fakeSource{name: "s.json", content: `"just a string"`})

	before := len(m.tree.Items())
	m = feed(t, m, testutil.Key("right"), testutil.Key("left"))

	if got := len(m.tree.Items()); got != before {
		t.Errorf("%d rows after expanding a leaf, want %d", got, before)
	}
}

// Rule 122: a cell is measured before it is styled, so what Cell returns must
// carry no escape sequence.
func TestEveryTreeCellIsPlainText(t *testing.T) {
	m := jsonModel(t)

	for _, row := range m.tree.Table().Rows() {
		for _, cell := range row {
			if strings.Contains(cell, "\x1b") {
				t.Fatalf("cell %q carries an escape sequence", cell)
			}
		}
	}
}

// A closed container says how many children it holds — the one thing it can
// usefully say while shut.
func TestAClosedContainerShowsItsChildCount(t *testing.T) {
	m := jsonModel(t)
	m = feed(t, m, testutil.Key("left"))

	row, ok := m.tree.Selected()
	if !ok {
		t.Fatal("nothing selected")
	}
	if row.Node.Value != "{3}" {
		t.Errorf("the collapsed root reads %q, want {3}", row.Node.Value)
	}
}

// `f` moves between the tree and the document's own text.
func TestFSwitchesBetweenTreeAndText(t *testing.T) {
	m := jsonModel(t)

	m = feed(t, m, testutil.Key("f"))
	if m.display != displayText {
		t.Fatal("f did not switch to the text display")
	}
	if got := m.formatLabel(); got != "json · text" {
		t.Errorf("formatLabel = %q, want \"json · text\"", got)
	}
	if !strings.Contains(m.View(), "web") {
		t.Error("the text display does not render the document")
	}

	m = feed(t, m, testutil.Key("f"))
	if m.display != displayTree {
		t.Error("f did not switch back to the tree")
	}
}

// Rule 130: a document with no tree does not advertise a key that would do
// nothing.
func TestAPlainDocumentOffersNoDisplayToggle(t *testing.T) {
	m := open(t, fakeSource{name: "notes.md", content: "hello"})

	if hasShortcut(m, "f") {
		t.Error("a document with no tree advertises f")
	}

	m = feed(t, m, testutil.Key("f"))
	if m.display != displayText {
		t.Error("f moved a document that has no tree")
	}
}

// `esc` goes back to whoever opened the document.
func TestEscReturnsToTheOrigin(t *testing.T) {
	m := jsonModel(t)
	m.OriginView = command.ViewWorkspaces

	_, cmd := step(t, m, testutil.Key("esc"))
	if cmd == nil {
		t.Fatal("esc issued no command")
	}
	msg, ok := cmd().(BackToOriginMsg)
	if !ok {
		t.Fatalf("esc produced %T, want BackToOriginMsg", cmd())
	}
	if msg.Origin != command.ViewWorkspaces {
		t.Errorf("Origin = %q, want workspaces", msg.Origin)
	}
}

// The empty viewer the router builds for its own contract test has nowhere to
// go, and swallowing esc beats switching to a view nobody asked for.
func TestEscWithNoOriginIsInert(t *testing.T) {
	m := New(config.Default())

	if _, cmd := step(t, m, testutil.Key("esc")); cmd != nil {
		t.Error("esc issued a command with no origin recorded")
	}
}

func hasShortcut(m Model, key string) bool {
	for _, s := range m.GetShortcuts() {
		if s.Key == key {
			return true
		}
	}
	return false
}
