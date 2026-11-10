package components

import (
	"testing"

	tea "github.com/charmbracelet/bubbletea"

	"github.com/anthnel/devdesk/internal/forge"
	"github.com/anthnel/devdesk/internal/ui/testutil"
)

// DEVDESK-200 characterization: text typed or pasted reaches a field exactly,
// accents, dashes and spaces included, whatever way key messages are read.
func TestTypedTextReachesTheFieldUnchanged(t *testing.T) {
	const typed = "café au lait — naïve ü"
	const pasted = "élan vital"

	feed := func(update func(tea.Msg), msgs []tea.Msg) {
		for _, m := range msgs {
			update(m)
		}
	}

	tests := []struct {
		name  string
		setup func() (update func(tea.Msg), value func() string)
	}{
		{"WrappedInput", func() (func(tea.Msg), func() string) {
			w := NewWrappedInput(80, 0, "")
			w.Focus()
			return func(m tea.Msg) { w, _ = w.Update(m) }, func() string { return w.Value() }
		}},
		{"FilterBar", func() (func(tea.Msg), func() string) {
			fb := NewFilterBar()
			fb.ActivateSearch()
			return func(m tea.Msg) { fb, _ = fb.Update(m) }, func() string { return fb.SearchQuery() }
		}},
		{"CreationForm name", func() (func(tea.Msg), func() string) {
			f := NewCreationForm(0, "", "", "private", []string{"private", "internal", "public"}, forge.VocabularyFor(""))
			f, _ = f.Update(testutil.Key("down")) // name field
			return func(m tea.Msg) { f, _ = f.Update(m) }, func() string { return f.nameInput.Value() }
		}},
	}
	for _, tt := range tests {
		t.Run(tt.name, func(t *testing.T) {
			update, value := tt.setup()

			feed(update, testutil.Type(typed))
			if got := value(); got != typed {
				t.Errorf("after typing %q the field holds %q", typed, got)
			}

			feed(update, []tea.Msg{testutil.Paste(pasted)})
			if got, want := value(), typed+pasted; got != want {
				t.Errorf("after the paste the field holds %q, want %q", got, want)
			}
		})
	}
}
