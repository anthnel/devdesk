package viewer

import (
	"strings"
	"testing"

	"github.com/anthnel/devdesk/internal/ui/testutil"
)

// `f` switches between the tree and the text of one document, so the keys the
// text pane owns are greyed in the tree rather than dropped (Rule 130).
// Hiding them made three entries appear and disappear on every press of a key
// whose whole job is to switch back and forth.
func TestSwitchingDisplayDoesNotReshapeTheColumn(t *testing.T) {
	m := jsonModel(t) // opens on the tree

	tree := testutil.ShortcutKeys(m.GetShortcuts())
	text := testutil.ShortcutKeys(feed(t, m, testutil.Key("f")).GetShortcuts())

	if strings.Join(tree, " ") != strings.Join(text, " ") {
		t.Errorf("the tree advertises %v and the text %v; want the same keys", tree, text)
	}
}

func TestTheTextPaneKeysAreGreyedInTheTree(t *testing.T) {
	tree := jsonModel(t)
	text := feed(t, tree, testutil.Key("f"))

	for _, key := range []string{"w", "/"} {
		if !testutil.ShortcutDisabled(tree.GetShortcuts(), key) {
			t.Errorf("%q is offered in the tree, where the text pane is not on screen", key)
		}
		if !testutil.ShortcutEnabled(text.GetShortcuts(), key) {
			t.Errorf("%q is greyed in the text pane, where it works", key)
		}
	}

	// And the other way round: ←→ collapses a node, which only the tree has.
	if !testutil.ShortcutEnabled(tree.GetShortcuts(), "←→") {
		t.Error("←→ is greyed in the tree, where it collapses a node")
	}
	if !testutil.ShortcutDisabled(text.GetShortcuts(), "←→") {
		t.Error("←→ is offered in the text pane, which has no nodes")
	}
}
