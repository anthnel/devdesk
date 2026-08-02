package components

import (
	"strings"
	"testing"

	"github.com/anthnel/devdesk/internal/ui/testutil"
)

// Focus indices in this modal: 0 = checkbox, 1 = Yes, 2 = No.
const (
	deleteFocusCheckbox = 0
	deleteFocusYes      = 1
	deleteFocusNo       = 2
)

func TestNewDeleteConfirmModalDefaultsToNo(t *testing.T) {
	m := NewDeleteConfirmModal("Delete project", "This cannot be undone")

	if m.focused != deleteFocusNo {
		t.Errorf("focus = %d on a new modal, want %d (No)", m.focused, deleteFocusNo)
	}
	if m.permanentlyRemove {
		t.Error("immediate deletion is pre-checked on the standard modal")
	}
}

func TestNewDeleteConfirmModalPermanentPreChecksImmediateDeletion(t *testing.T) {
	m := NewDeleteConfirmModalPermanent("Delete project", "Already marked for deletion")

	if !m.permanentlyRemove {
		t.Error("the permanent variant did not pre-check immediate deletion")
	}
	if m.focused != deleteFocusNo {
		t.Errorf("focus = %d, want %d (No) even on the permanent variant", m.focused, deleteFocusNo)
	}
}

func TestDeleteConfirmModalVerticalNavigationClamps(t *testing.T) {
	m := NewDeleteConfirmModal("Delete", "Sure?")

	for i := 0; i < 5; i++ {
		m, _ = m.Update(testutil.Key("up"))
	}
	if m.focused != deleteFocusCheckbox {
		t.Errorf("focus = %d after repeated up, want %d (clamped at the checkbox)", m.focused, deleteFocusCheckbox)
	}

	for i := 0; i < 5; i++ {
		m, _ = m.Update(testutil.Key("down"))
	}
	if m.focused != deleteFocusNo {
		t.Errorf("focus = %d after repeated down, want %d (clamped at No)", m.focused, deleteFocusNo)
	}
}

func TestDeleteConfirmModalVimNavigation(t *testing.T) {
	m := NewDeleteConfirmModal("Delete", "Sure?")

	m, _ = m.Update(testutil.Key("k"))
	if m.focused != deleteFocusYes {
		t.Errorf("focus = %d after k, want %d", m.focused, deleteFocusYes)
	}
	m, _ = m.Update(testutil.Key("j"))
	if m.focused != deleteFocusNo {
		t.Errorf("focus = %d after j, want %d", m.focused, deleteFocusNo)
	}
}

func TestDeleteConfirmModalHorizontalMovesBetweenButtonsOnly(t *testing.T) {
	m := NewDeleteConfirmModal("Delete", "Sure?")

	m, _ = m.Update(testutil.Key("left"))
	if m.focused != deleteFocusYes {
		t.Errorf("focus = %d after left from No, want %d (Yes)", m.focused, deleteFocusYes)
	}
	m, _ = m.Update(testutil.Key("right"))
	if m.focused != deleteFocusNo {
		t.Errorf("focus = %d after right from Yes, want %d (No)", m.focused, deleteFocusNo)
	}

	// From the checkbox, horizontal keys must not jump into the buttons.
	m.focused = deleteFocusCheckbox
	m, _ = m.Update(testutil.Key("left"))
	if m.focused != deleteFocusCheckbox {
		t.Errorf("focus = %d after left from the checkbox, want it unchanged", m.focused)
	}
}

func TestDeleteConfirmModalTabCycles(t *testing.T) {
	m := NewDeleteConfirmModal("Delete", "Sure?") // starts on No (2)

	m, _ = m.Update(testutil.Key("tab"))
	if m.focused != deleteFocusCheckbox {
		t.Errorf("focus = %d after tab from No, want %d (wraps to the checkbox)", m.focused, deleteFocusCheckbox)
	}
	m, _ = m.Update(testutil.Key("shift+tab"))
	if m.focused != deleteFocusNo {
		t.Errorf("focus = %d after shift+tab, want %d", m.focused, deleteFocusNo)
	}
}

func TestDeleteConfirmModalSpaceTogglesCheckboxWhenFocused(t *testing.T) {
	m := NewDeleteConfirmModal("Delete", "Sure?")
	m.focused = deleteFocusCheckbox

	m, cmd := m.Update(testutil.Key(" "))
	if !m.permanentlyRemove {
		t.Error("space on the checkbox did not check it")
	}
	if cmd != nil {
		t.Error("toggling the checkbox emitted an answer command")
	}

	m, _ = m.Update(testutil.Key(" "))
	if m.permanentlyRemove {
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
			m.permanentlyRemove = tt.permanent
			m.focused = deleteFocusYes

			_, cmd := m.Update(testutil.Key("enter"))
			msg, ok := testutil.MsgOf[DeleteConfirmModalYesMsg](cmd)
			if !ok {
				t.Fatalf("enter on Yes did not emit DeleteConfirmModalYesMsg, got %T", testutil.Msg(cmd))
			}
			if msg.PermanentlyRemove != tt.wantPermanent {
				t.Errorf("PermanentlyRemove = %v, want %v", msg.PermanentlyRemove, tt.wantPermanent)
			}
		})
	}
}

func TestDeleteConfirmModalEnterOnNoCancels(t *testing.T) {
	m := NewDeleteConfirmModal("Delete", "Sure?") // focus starts on No

	_, cmd := m.Update(testutil.Key("enter"))
	if _, ok := testutil.MsgOf[DeleteConfirmModalNoMsg](cmd); !ok {
		t.Errorf("enter on No did not emit DeleteConfirmModalNoMsg, got %T", testutil.Msg(cmd))
	}
}

func TestDeleteConfirmModalEnterOnCheckboxTogglesInsteadOfConfirming(t *testing.T) {
	m := NewDeleteConfirmModal("Delete", "Sure?")
	m.focused = deleteFocusCheckbox

	m, cmd := m.Update(testutil.Key("enter"))
	if cmd != nil {
		t.Error("enter on the checkbox emitted an answer command instead of toggling")
	}
	if !m.permanentlyRemove {
		t.Error("enter on the checkbox did not toggle it")
	}
}

func TestDeleteConfirmModalLetterShortcuts(t *testing.T) {
	t.Run("y confirms regardless of focus", func(t *testing.T) {
		m := NewDeleteConfirmModal("Delete", "Sure?")
		m.permanentlyRemove = true

		_, cmd := m.Update(testutil.Key("y"))
		msg, ok := testutil.MsgOf[DeleteConfirmModalYesMsg](cmd)
		if !ok {
			t.Fatalf("y did not confirm, got %T", testutil.Msg(cmd))
		}
		if !msg.PermanentlyRemove {
			t.Error("y dropped the checkbox state")
		}
	})

	for _, key := range []string{"n", "N", "esc"} {
		t.Run(key+" cancels", func(t *testing.T) {
			m := NewDeleteConfirmModal("Delete", "Sure?")
			m.focused = deleteFocusYes

			_, cmd := m.Update(testutil.Key(key))
			if _, ok := testutil.MsgOf[DeleteConfirmModalNoMsg](cmd); !ok {
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
			m := NewDeleteConfirmModalPermanent("Delete", "Already marked for deletion")
			m.focused = deleteFocusCheckbox

			m, _ = m.Update(testutil.Key(key))

			if !m.permanentlyRemove {
				t.Errorf("%q unchecked the locked checkbox", key)
			}
		})
	}
}

// A focusable control that ignores every key is more confusing than an absent
// one, so navigation skips the locked checkbox entirely.
func TestDeleteConfirmModalPermanentSkipsCheckboxWhenNavigating(t *testing.T) {
	t.Run("up clamps at Yes", func(t *testing.T) {
		m := NewDeleteConfirmModalPermanent("Delete", "Already marked for deletion")

		for i := 0; i < 5; i++ {
			m, _ = m.Update(testutil.Key("up"))
		}
		if m.focused != deleteFocusYes {
			t.Errorf("focus = %d after repeated up, want %d (Yes)", m.focused, deleteFocusYes)
		}
	})

	t.Run("tab cycles between the buttons only", func(t *testing.T) {
		m := NewDeleteConfirmModalPermanent("Delete", "Already marked for deletion") // starts on No

		m, _ = m.Update(testutil.Key("tab"))
		if m.focused != deleteFocusYes {
			t.Errorf("focus = %d after tab from No, want %d (Yes)", m.focused, deleteFocusYes)
		}
		m, _ = m.Update(testutil.Key("tab"))
		if m.focused != deleteFocusNo {
			t.Errorf("focus = %d after a second tab, want %d (No)", m.focused, deleteFocusNo)
		}
		m, _ = m.Update(testutil.Key("shift+tab"))
		if m.focused != deleteFocusYes {
			t.Errorf("focus = %d after shift+tab, want %d (Yes)", m.focused, deleteFocusYes)
		}
	})
}

// Confirming the permanent variant must carry the checked state through.
func TestDeleteConfirmModalPermanentConfirmsAsImmediate(t *testing.T) {
	m := NewDeleteConfirmModalPermanent("Delete", "Already marked for deletion")
	m.focused = deleteFocusYes

	_, cmd := m.Update(testutil.Key("enter"))
	msg, ok := testutil.MsgOf[DeleteConfirmModalYesMsg](cmd)
	if !ok {
		t.Fatalf("enter on Yes did not emit DeleteConfirmModalYesMsg, got %T", testutil.Msg(cmd))
	}
	if !msg.PermanentlyRemove {
		t.Error("the permanent variant confirmed with PermanentlyRemove = false")
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

	m.permanentlyRemove = true
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
