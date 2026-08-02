package workspaces

import (
	"strings"
	"testing"

	tea "github.com/charmbracelet/bubbletea"

	"github.com/anthnel/devdesk/internal/ui/testutil"
)

// feedInput applies messages in order.
func feedInput(w *WorkspaceInput, msgs ...tea.Msg) *WorkspaceInput {
	for _, msg := range msgs {
		w, _ = w.Update(msg)
	}
	return w
}

// ── Construction ─────────────────────────────────────────────────────────────

func TestNewWorkspaceInputStartsEmptyOnTheField(t *testing.T) {
	w := NewWorkspaceInput()

	if w.input.Value() != "" {
		t.Errorf("the create input is prefilled with %q", w.input.Value())
	}
	if w.focusIndex != focusInput {
		t.Errorf("focusIndex = %d on a new input, want the text field", w.focusIndex)
	}
	if w.isRename {
		t.Error("the create input reports itself as a rename")
	}
}

// The rename input starts prefilled, so a small correction does not mean
// retyping the whole name.
func TestNewRenameInputPrefillsTheCurrentName(t *testing.T) {
	w := NewRenameInput("old-name")

	if w.input.Value() != "old-name" {
		t.Errorf("the rename input = %q, want it prefilled with the current name", w.input.Value())
	}
	if !w.isRename {
		t.Error("the rename input does not report itself as a rename")
	}
}

// ── Navigation ───────────────────────────────────────────────────────────────

func TestInputFocusCyclesAndWraps(t *testing.T) {
	w := NewWorkspaceInput()

	w = feedInput(w, testutil.Key("down"))
	if w.focusIndex != focusCreate {
		t.Errorf("focusIndex = %d after down, want the confirm button", w.focusIndex)
	}
	if w.input.Focused() {
		t.Error("the text field kept focus while a button was selected")
	}

	w = feedInput(w, testutil.Key("down"))
	if w.focusIndex != focusCancel {
		t.Errorf("focusIndex = %d after a second down, want the cancel button", w.focusIndex)
	}

	w = feedInput(w, testutil.Key("down"))
	if w.focusIndex != focusInput {
		t.Errorf("focusIndex = %d after wrapping, want the text field", w.focusIndex)
	}
	if !w.input.Focused() {
		t.Error("the text field did not regain focus on wrap")
	}

	w = feedInput(w, testutil.Key("up"))
	if w.focusIndex != focusCancel {
		t.Errorf("focusIndex = %d after up from the field, want the cancel button", w.focusIndex)
	}
}

func TestTypingOnlyReachesTheFocusedField(t *testing.T) {
	w := feedInput(NewWorkspaceInput(), testutil.Type("my-project")...)
	if w.input.Value() != "my-project" {
		t.Errorf("input = %q after typing, want the typed value", w.input.Value())
	}

	w = feedInput(w, testutil.Key("down"))
	w = feedInput(w, testutil.Type("xyz")...)
	if w.input.Value() != "my-project" {
		t.Errorf("input = %q after typing on a button, want it unchanged", w.input.Value())
	}
}

// ── Submission ───────────────────────────────────────────────────────────────

// Enter submits from the text field as well as from the confirm button, so the
// common case is one keystroke.
func TestEnterSubmitsFromTheFieldAndTheButton(t *testing.T) {
	for _, focus := range []int{focusInput, focusCreate} {
		w := feedInput(NewWorkspaceInput(), testutil.Type("my-project")...)
		w.focusIndex = focus

		_, cmd := w.Update(testutil.Key("enter"))

		msg, ok := testutil.MsgOf[WorkspaceInputSubmitMsg](cmd)
		if !ok {
			t.Fatalf("enter at focus %d did not submit, got %T", focus, testutil.Msg(cmd))
		}
		if msg.Name != "my-project" {
			t.Errorf("submitted %q, want \"my-project\"", msg.Name)
		}
	}
}

func TestRenameSubmitsItsOwnMessage(t *testing.T) {
	w := NewRenameInput("old-name")

	_, cmd := w.Update(testutil.Key("enter"))

	msg, ok := testutil.MsgOf[RenameInputSubmitMsg](cmd)
	if !ok {
		t.Fatalf("enter on a rename input did not emit RenameInputSubmitMsg, got %T", testutil.Msg(cmd))
	}
	if msg.Name != "old-name" {
		t.Errorf("submitted %q, want the prefilled name", msg.Name)
	}
}

func TestSubmissionTrimsAndRejectsBlankNames(t *testing.T) {
	t.Run("trims", func(t *testing.T) {
		w := feedInput(NewWorkspaceInput(), testutil.Type("  spaced  ")...)

		_, cmd := w.Update(testutil.Key("enter"))

		msg, _ := testutil.MsgOf[WorkspaceInputSubmitMsg](cmd)
		if msg.Name != "spaced" {
			t.Errorf("submitted %q, want it trimmed", msg.Name)
		}
	})

	for _, name := range []string{"", "   "} {
		t.Run("rejects "+name, func(t *testing.T) {
			w := NewWorkspaceInput()
			w.input.SetValue(name)

			_, cmd := w.Update(testutil.Key("enter"))

			if cmd != nil {
				t.Errorf("a blank name was submitted as %T", testutil.Msg(cmd))
			}
		})
	}
}

func TestCancelling(t *testing.T) {
	t.Run("esc from anywhere", func(t *testing.T) {
		_, cmd := NewWorkspaceInput().Update(testutil.Key("esc"))

		if _, ok := testutil.MsgOf[WorkspaceInputCancelMsg](cmd); !ok {
			t.Errorf("esc did not cancel, got %T", testutil.Msg(cmd))
		}
	})

	t.Run("enter on the cancel button", func(t *testing.T) {
		w := NewWorkspaceInput()
		w.focusIndex = focusCancel

		_, cmd := w.Update(testutil.Key("enter"))

		if _, ok := testutil.MsgOf[WorkspaceInputCancelMsg](cmd); !ok {
			t.Errorf("enter on Cancel did not cancel, got %T", testutil.Msg(cmd))
		}
	})
}

// ── Rendering ────────────────────────────────────────────────────────────────

func TestInputViewShowsItsPurpose(t *testing.T) {
	create := NewWorkspaceInput().View()
	if !strings.Contains(create, "Create") {
		t.Errorf("the create form does not offer a Create action:\n%s", create)
	}

	rename := NewRenameInput("old-name").View()
	if !strings.Contains(rename, "Rename") {
		t.Errorf("the rename form does not offer a Rename action:\n%s", rename)
	}
	if !strings.Contains(rename, "old-name") {
		t.Error("the rename form does not show the current name")
	}
}

func TestInputViewMarksTheFocusedElement(t *testing.T) {
	w := NewWorkspaceInput()
	onField := w.View()

	w = feedInput(w, testutil.Key("down"))

	if w.View() == onField {
		t.Error("moving focus to the button changed nothing in the render")
	}
}
