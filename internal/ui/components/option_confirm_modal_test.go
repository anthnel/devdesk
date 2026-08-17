package components

import (
	"strings"
	"testing"

	"github.com/anthnel/devdesk/internal/ui/testutil"
)

// Focus indices in this modal: 0 = checkbox, 1 = Yes, 2 = No.
const (
	focusCheckbox = 0
	focusYes      = 1
	focusNo       = 2
)

func TestNewDeleteConfirmModalDefaultsToNo(t *testing.T) {
	m := NewDeleteConfirmModal("Delete project", "This cannot be undone")

	if m.focused != focusNo {
		t.Errorf("focus = %d on a new modal, want %d (No)", m.focused, focusNo)
	}
	if m.option {
		t.Error("immediate deletion is pre-checked on the standard modal")
	}
}

func TestNewDeleteConfirmModalLockedPreChecksImmediateDeletion(t *testing.T) {
	m := NewDeleteConfirmModalLocked("Delete project", "Already marked for deletion")

	if !m.option {
		t.Error("the permanent variant did not pre-check immediate deletion")
	}
	if m.focused != focusNo {
		t.Errorf("focus = %d, want %d (No) even on the permanent variant", m.focused, focusNo)
	}
}

// Rule 135: ↑/↓ are the only field navigation, and they cycle so every control
// stays reachable in one direction.
func TestDeleteConfirmModalVerticalNavigationCycles(t *testing.T) {
	m := NewDeleteConfirmModal("Delete", "Sure?") // starts on No

	m, _ = m.Update(testutil.Key("down"))
	if m.focused != focusCheckbox {
		t.Errorf("focus = %d after down from No, want %d (wraps to the checkbox)", m.focused, focusCheckbox)
	}

	m, _ = m.Update(testutil.Key("up"))
	if m.focused != focusNo {
		t.Errorf("focus = %d after up from the checkbox, want %d (wraps back to No)", m.focused, focusNo)
	}

	// A full cycle in either direction returns where it started.
	for range 3 {
		m, _ = m.Update(testutil.Key("down"))
	}
	if m.focused != focusNo {
		t.Errorf("focus = %d after a full cycle", m.focused)
	}
}

// Rule 135 reserves tab for switching tabs. The modal has none, so it does
// nothing here — it used to cycle the focus, which is what ↑/↓ now do.
func TestDeleteConfirmModalIgnoresTab(t *testing.T) {
	m := NewDeleteConfirmModal("Delete", "Sure?")
	before := m.focused

	for _, key := range []string{"tab", "shift+tab"} {
		m, _ = m.Update(testutil.Key(key))
		if m.focused != before {
			t.Errorf("%q moved the focus to %d", key, m.focused)
		}
	}
}

// Only y/n are letters here, and they confirm rather than move (§3.26: a modal
// is a mode — it claims every key before the view sees it, which is why its
// Y/N cannot collide with the action vocabulary).
func TestDeleteConfirmModalVimKeysDoNotMoveTheFocus(t *testing.T) {
	for _, key := range []string{"k", "j", "h", "l"} {
		m := NewDeleteConfirmModal("Delete", "Sure?")
		before := m.focused

		m, _ = m.Update(testutil.Key(key))
		if m.focused != before {
			t.Errorf("%q moved the focus to %d; arrows are the only navigation", key, m.focused)
		}
	}
}

func TestDeleteConfirmModalHorizontalMovesBetweenButtonsOnly(t *testing.T) {
	m := NewDeleteConfirmModal("Delete", "Sure?")

	m, _ = m.Update(testutil.Key("left"))
	if m.focused != focusYes {
		t.Errorf("focus = %d after left from No, want %d (Yes)", m.focused, focusYes)
	}
	m, _ = m.Update(testutil.Key("right"))
	if m.focused != focusNo {
		t.Errorf("focus = %d after right from Yes, want %d (No)", m.focused, focusNo)
	}

	// From the checkbox, horizontal keys must not jump into the buttons.
	m.focused = focusCheckbox
	m, _ = m.Update(testutil.Key("left"))
	if m.focused != focusCheckbox {
		t.Errorf("focus = %d after left from the checkbox, want it unchanged", m.focused)
	}
}

func TestDeleteConfirmModalSpaceTogglesCheckboxWhenFocused(t *testing.T) {
	m := NewDeleteConfirmModal("Delete", "Sure?")
	m.focused = focusCheckbox

	m, cmd := m.Update(testutil.Key(" "))
	if !m.option {
		t.Error("space on the checkbox did not check it")
	}
	if cmd != nil {
		t.Error("toggling the checkbox emitted an answer command")
	}

	m, _ = m.Update(testutil.Key(" "))
	if m.option {
		t.Error("space on the checkbox did not uncheck it")
	}
}

func TestDeleteConfirmModalConfirmCarriesCheckboxState(t *testing.T) {
	tests := []struct {
		name          string
		permanent     bool
		wantPermanent bool
	}{
		{"unchecked yields a grace-period delete", false, false},
		{"checked yields an immediate delete", true, true},
	}

	for _, tt := range tests {
		t.Run(tt.name, func(t *testing.T) {
			m := NewDeleteConfirmModal("Delete", "Sure?")
			m.option = tt.permanent
			m.focused = focusYes

			_, cmd := m.Update(testutil.Key("enter"))
			msg, ok := testutil.MsgOf[OptionConfirmModalYesMsg](cmd)
			if !ok {
				t.Fatalf("enter on Yes did not emit OptionConfirmModalYesMsg, got %T", testutil.Msg(cmd))
			}
			if msg.Option != tt.wantPermanent {
				t.Errorf("Option = %v, want %v", msg.Option, tt.wantPermanent)
			}
		})
	}
}

func TestDeleteConfirmModalEnterOnNoCancels(t *testing.T) {
	m := NewDeleteConfirmModal("Delete", "Sure?") // focus starts on No

	_, cmd := m.Update(testutil.Key("enter"))
	if _, ok := testutil.MsgOf[OptionConfirmModalNoMsg](cmd); !ok {
		t.Errorf("enter on No did not emit OptionConfirmModalNoMsg, got %T", testutil.Msg(cmd))
	}
}

func TestDeleteConfirmModalEnterOnCheckboxTogglesInsteadOfConfirming(t *testing.T) {
	m := NewDeleteConfirmModal("Delete", "Sure?")
	m.focused = focusCheckbox

	m, cmd := m.Update(testutil.Key("enter"))
	if cmd != nil {
		t.Error("enter on the checkbox emitted an answer command instead of toggling")
	}
	if !m.option {
		t.Error("enter on the checkbox did not toggle it")
	}
}

func TestDeleteConfirmModalLetterShortcuts(t *testing.T) {
	t.Run("y confirms regardless of focus", func(t *testing.T) {
		m := NewDeleteConfirmModal("Delete", "Sure?")
		m.option = true

		_, cmd := m.Update(testutil.Key("y"))
		msg, ok := testutil.MsgOf[OptionConfirmModalYesMsg](cmd)
		if !ok {
			t.Fatalf("y did not confirm, got %T", testutil.Msg(cmd))
		}
		if !msg.Option {
			t.Error("y dropped the checkbox state")
		}
	})

	for _, key := range []string{"n", "N", "esc"} {
		t.Run(key+" cancels", func(t *testing.T) {
			m := NewDeleteConfirmModal("Delete", "Sure?")
			m.focused = focusYes

			_, cmd := m.Update(testutil.Key(key))
			if _, ok := testutil.MsgOf[OptionConfirmModalNoMsg](cmd); !ok {
				t.Errorf("%q did not cancel, got %T", key, testutil.Msg(cmd))
			}
		})
	}
}

// The permanent variant is used when the project is already scheduled for
// deletion, so a grace-period delete is not on offer: no key may uncheck the box.
func TestDeleteConfirmModalPermanentCheckboxIsLocked(t *testing.T) {
	for _, key := range []string{" ", "enter"} {
		t.Run(key+" leaves it checked", func(t *testing.T) {
			m := NewDeleteConfirmModalLocked("Delete", "Already marked for deletion")
			m.focused = focusCheckbox

			m, _ = m.Update(testutil.Key(key))

			if !m.option {
				t.Errorf("%q unchecked the locked checkbox", key)
			}
		})
	}
}

// A focusable control that ignores every key is more confusing than an absent
// one, so navigation skips the locked checkbox entirely.
func TestDeleteConfirmModalPermanentSkipsCheckboxWhenNavigating(t *testing.T) {
	m := NewDeleteConfirmModalLocked("Delete", "Already marked for deletion") // starts on No

	// Cycling never lands on the checkbox: it toggles between the two buttons.
	for i, want := range []int{focusYes, focusNo, focusYes, focusNo} {
		m, _ = m.Update(testutil.Key("down"))
		if m.focused != want {
			t.Fatalf("down #%d landed on %d, want %d", i+1, m.focused, want)
		}
	}

	for i, want := range []int{focusYes, focusNo, focusYes} {
		m, _ = m.Update(testutil.Key("up"))
		if m.focused != want {
			t.Fatalf("up #%d landed on %d, want %d", i+1, m.focused, want)
		}
	}
}

// Confirming the permanent variant must carry the checked state through.
func TestDeleteConfirmModalPermanentConfirmsAsImmediate(t *testing.T) {
	m := NewDeleteConfirmModalLocked("Delete", "Already marked for deletion")
	m.focused = focusYes

	_, cmd := m.Update(testutil.Key("enter"))
	msg, ok := testutil.MsgOf[OptionConfirmModalYesMsg](cmd)
	if !ok {
		t.Fatalf("enter on Yes did not emit OptionConfirmModalYesMsg, got %T", testutil.Msg(cmd))
	}
	if !msg.Option {
		t.Error("the permanent variant confirmed with Option = false")
	}
}

func TestDeleteConfirmModalStoresWindowSize(t *testing.T) {
	m := NewDeleteConfirmModal("Delete", "Sure?")

	m, _ = m.Update(testutil.Resize(100, 30))
	if m.width != 100 || m.height != 30 {
		t.Errorf("window size = %dx%d, want 100x30", m.width, m.height)
	}
}

func TestDeleteConfirmModalViewShowsWarningOnlyWhenChecked(t *testing.T) {
	m := NewDeleteConfirmModal("Delete project", "This cannot be undone")

	if strings.Contains(m.View(), "irreversible") {
		t.Error("the irreversibility warning shows while immediate deletion is unchecked")
	}

	m.option = true
	view := m.View()
	if !strings.Contains(view, "irreversible") {
		t.Error("the irreversibility warning is missing while immediate deletion is checked")
	}
	for _, want := range []string{"Delete project", "This cannot be undone", "Yes", "No"} {
		if !strings.Contains(view, want) {
			t.Errorf("View() does not contain %q", want)
		}
	}
}
