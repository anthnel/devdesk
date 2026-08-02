package components

import (
	"strings"
	"testing"

	tea "github.com/charmbracelet/bubbletea"

	"github.com/anthnel/devdesk/internal/ui/testutil"
)

// Rule 104 requires the safe choice to be preselected on destructive
// confirmations. A regression here silently arms "Yes" under the cursor.
func TestNewConfirmModalDefaultsToNo(t *testing.T) {
	m := NewConfirmModal("Delete", "Delete this monitor?")
	if m.focused {
		t.Error("NewConfirmModal() focused Yes; the safe default is No")
	}
}

func TestConfirmModalArrowsToggleSelection(t *testing.T) {
	for _, key := range []string{"left", "right"} {
		t.Run(key, func(t *testing.T) {
			m := NewConfirmModal("Delete", "Sure?")

			m, cmd := m.Update(testutil.Key(key))
			if !m.focused {
				t.Errorf("after %q, selection did not move to Yes", key)
			}
			if cmd != nil {
				t.Errorf("after %q, expected no command, got one", key)
			}

			m, _ = m.Update(testutil.Key(key))
			if m.focused {
				t.Errorf("after a second %q, selection did not move back to No", key)
			}
		})
	}
}

func TestConfirmModalEnterAndSpaceConfirmSelection(t *testing.T) {
	tests := []struct {
		name    string
		key     string
		focused bool
		wantYes bool
	}{
		{"enter on No", "enter", false, false},
		{"enter on Yes", "enter", true, true},
		{"space on No", " ", false, false},
		{"space on Yes", " ", true, true},
	}

	for _, tt := range tests {
		t.Run(tt.name, func(t *testing.T) {
			m := NewConfirmModal("Delete", "Sure?")
			m.focused = tt.focused

			_, cmd := m.Update(testutil.Key(tt.key))
			assertConfirmAnswer(t, cmd, tt.wantYes)
		})
	}
}

func TestConfirmModalLetterShortcuts(t *testing.T) {
	tests := []struct {
		key     string
		wantYes bool
	}{
		{"y", true},
		{"Y", true},
		{"n", false},
		{"N", false},
		{"esc", false},
	}

	for _, tt := range tests {
		t.Run(tt.key, func(t *testing.T) {
			m := NewConfirmModal("Delete", "Sure?")

			_, cmd := m.Update(testutil.Key(tt.key))
			assertConfirmAnswer(t, cmd, tt.wantYes)
		})
	}
}

// The letter shortcuts must answer regardless of which button is highlighted,
// otherwise "n" on a focused Yes would confirm a deletion.
func TestConfirmModalLetterShortcutsIgnoreSelection(t *testing.T) {
	m := NewConfirmModal("Delete", "Sure?")
	m.focused = true // Yes is highlighted

	_, cmd := m.Update(testutil.Key("n"))
	assertConfirmAnswer(t, cmd, false)
}

func TestConfirmModalIgnoresUnhandledKeys(t *testing.T) {
	m := NewConfirmModal("Delete", "Sure?")

	got, cmd := m.Update(testutil.Key("z"))
	if cmd != nil {
		t.Error("an unhandled key produced a command")
	}
	if got.focused {
		t.Error("an unhandled key changed the selection")
	}
}

func TestConfirmModalStoresWindowSize(t *testing.T) {
	m := NewConfirmModal("Delete", "Sure?")

	m, _ = m.Update(testutil.Resize(120, 40))
	if m.width != 120 || m.height != 40 {
		t.Errorf("window size = %dx%d, want 120x40", m.width, m.height)
	}
}

func TestConfirmModalViewShowsTitleAndMessage(t *testing.T) {
	m := NewConfirmModal("Delete monitor", "This cannot be undone")

	view := m.View()
	for _, want := range []string{"Delete monitor", "This cannot be undone", "Yes", "No"} {
		if !strings.Contains(view, want) {
			t.Errorf("View() does not contain %q", want)
		}
	}
}

// assertConfirmAnswer drains cmd and checks which answer message it carries.
func assertConfirmAnswer(t *testing.T, cmd tea.Cmd, wantYes bool) {
	t.Helper()

	if cmd == nil {
		t.Fatal("expected an answer command, got nil")
	}
	_, gotYes := testutil.MsgOf[ConfirmModalYesMsg](cmd)
	_, gotNo := testutil.MsgOf[ConfirmModalNoMsg](cmd)

	switch {
	case wantYes && !gotYes:
		t.Errorf("expected ConfirmModalYesMsg, got %T", testutil.Msg(cmd))
	case !wantYes && !gotNo:
		t.Errorf("expected ConfirmModalNoMsg, got %T", testutil.Msg(cmd))
	}
}
