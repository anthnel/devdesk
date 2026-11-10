package components

import (
	"testing"

	tea "charm.land/bubbletea/v2"

	"github.com/anthnel/devdesk/internal/forge"
)

// DEVDESK-201 (Bubbles v2): textinput.Model.Width became SetWidth(n) / Width().
// A textinput with a zero width shows one rune of its placeholder, so a form's
// fields must keep the width they were given across any terminal resize.
func TestTextFieldsKeepTheirWidthAfterResize(t *testing.T) {
	const nameWidth = 50 // the width NewCreationForm gives its name field

	for _, size := range []tea.WindowSizeMsg{
		{Width: 80, Height: 24},
		{Width: 200, Height: 50},
		{Width: 40, Height: 10},
	} {
		f := NewCreationForm(0, "", "", "private", []string{"private", "internal", "public"}, forge.VocabularyFor(""))
		if got := f.nameInput.Width(); got != nameWidth {
			t.Fatalf("a new form's name field is %d wide, want %d", got, nameWidth)
		}

		f, _ = f.Update(size)

		if got := f.nameInput.Width(); got != nameWidth {
			t.Errorf("after a %dx%d resize the name field is %d wide, want %d",
				size.Width, size.Height, got, nameWidth)
		}
	}
}
