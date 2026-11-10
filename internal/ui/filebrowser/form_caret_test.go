package filebrowser

import (
	"testing"

	tea "charm.land/bubbletea/v2"

	"github.com/anthnel/devdesk/internal/ui/testutil"
)

// TestCreateFormArrowsMoveTheCaretInTheName pins the name field's own ←→ before
// the form stops rebuilding a key from a tea.KeyType (DEVDESK-200): type "ab",
// left, type "X" gives "aXb"; right then moves the caret back to the end.
func TestCreateFormArrowsMoveTheCaretInTheName(t *testing.T) {
	f := newCreateForm(false, 80)
	for _, k := range testutil.Type("ab") {
		f, _ = f.Update(k.(tea.KeyMsg))
	}
	f, _ = f.Update(testutil.Key("left"))
	f, _ = f.Update(testutil.Key("X"))
	if got := f.name.Value(); got != "aXb" {
		t.Fatalf("after left then X, name = %q, want %q", got, "aXb")
	}
	f, _ = f.Update(testutil.Key("right"))
	f, _ = f.Update(testutil.Key("Y"))
	if got := f.name.Value(); got != "aXbY" {
		t.Errorf("after right then Y, name = %q, want %q", got, "aXbY")
	}
}
