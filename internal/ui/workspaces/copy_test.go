package workspaces

import (
	"errors"
	"strings"
	"testing"

	tea "github.com/charmbracelet/bubbletea"

	"github.com/anthnel/devdesk/internal/ui/keymap"
	"github.com/anthnel/devdesk/internal/ui/testutil"
)

// Y copies the row under the cursor, whatever it is. A file is the case worth
// pinning: every other action in this view acts on a directory, so a target
// resolver written from their shape would quietly skip one.
func TestCopyTakesTheSelectedRowWhateverItIs(t *testing.T) {
	tests := []struct {
		name   string
		cursor int
		want   string
	}{
		{"git repo", 0, "/tmp/workspaces/devdesk"},
		{"plain directory", 3, "/tmp/workspaces/empty-dir"},
		{"file", 4, "/tmp/workspaces/notes.md"},
	}

	for _, tc := range tests {
		t.Run(tc.name, func(t *testing.T) {
			m := loadedModel(t)
			m.table.SetCursor(tc.cursor)

			got, ok := m.copyTarget()
			if !ok {
				t.Fatalf("no copy target on a %s", tc.name)
			}
			if got != tc.want {
				t.Errorf("copyTarget() = %q, want %q", got, tc.want)
			}
		})
	}
}

// No row, nothing to copy — and in particular not the browsed directory, which
// T and O fall back to. Y answers "what is that row", so a silent fallback
// would be a wrong answer rather than a missing one.
func TestCopyingWithoutARowDoesNothing(t *testing.T) {
	m := newTestModel(t)

	if _, ok := m.copyTarget(); ok {
		t.Error("an empty listing offered a copy target")
	}
	refused(t, m, keymap.Copy, reasonNoRow)
}

func TestCopyingIssuesTheWrite(t *testing.T) {
	m := loadedModel(t)

	if _, cmd := step(t, m, testutil.Key(keymap.Copy)); cmd == nil {
		t.Error("Y issued no clipboard write")
	}
}

// Rule 128: both outcomes reach the footer, and a failure is an error rather
// than a notice — a clipboard write fails for reasons outside this application
// (no selection owner, no pbcopy), so a silent one leaves the user pasting
// whatever was there before.
func TestBothCopyOutcomesReachTheFooter(t *testing.T) {
	tests := []struct {
		name string
		msg  tea.Msg
		want string
	}{
		{"success", PathCopiedMsg{Path: "/tmp/workspaces/notes.md"}, "clipboard"},
		{"failure", PathCopiedMsg{Path: "/tmp/workspaces/notes.md", Error: errors.New("no clipboard on this system")}, "Failed"},
	}

	for _, tc := range tests {
		t.Run(tc.name, func(t *testing.T) {
			m, cmd := step(t, loadedModel(t), tc.msg)

			if cmd == nil {
				t.Error("the footer message carries no expiry timer, so it never clears")
			}
			if got := m.RenderFooter(160); !strings.Contains(got, tc.want) {
				t.Errorf("the footer does not report the %s:\n%s", tc.name, got)
			}
		})
	}
}

// Rule 130: the key keeps its place and is greyed where it does nothing.
func TestCopyIsGreyedWithoutARow(t *testing.T) {
	empty := newTestModel(t).GetShortcuts()
	if !hasShortcut(empty, keymap.Copy) {
		t.Error("Y disappeared from an empty listing instead of being greyed")
	}
	if !shortcutDisabled(empty, keymap.Copy) {
		t.Error("Y is offered on an empty listing, where it does nothing")
	}
	if shortcutDisabled(loadedModel(t).GetShortcuts(), keymap.Copy) {
		t.Error("Y is greyed on a row it can copy")
	}
}
