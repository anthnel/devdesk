package filebrowser

import (
	"os"
	"testing"

	"github.com/anthnel/devdesk/internal/ui/keymap"
	"github.com/anthnel/devdesk/internal/ui/testutil"
)

// DEVDESK-201 D3: a paste reaches the focused field of the browser (the filter
// bar, the create form's name) and fires no action (Rule 111).
func TestPasteReachesTheCreateFormName(t *testing.T) {
	tests := []struct{ name, paste, want string }{
		{"word", "new-dir", "new-dir"},
		{"with space", "x y", "x y"},
		{"multi-line", "a\nb", "a b"},
		{"action letters", "Dq?", "Dq?"},
	}
	for _, tc := range tests {
		t.Run(tc.name, func(t *testing.T) {
			dir := tree(t)
			m := openAt(t, dir)
			m, _ = press(t, m, keymap.New)
			if m.mode != modeCreating {
				t.Fatal("N must open the form")
			}
			m, cmd := step(t, m, testutil.Paste(tc.paste))

			if got := m.form.name.Value(); got != tc.want {
				t.Errorf("name = %q after pasting %q, want %q", got, tc.paste, tc.want)
			}
			if m.mode != modeCreating {
				t.Error("the paste left the creating mode")
			}
			if cmd != nil {
				for _, msg := range testutil.Msgs(cmd) {
					if _, ok := msg.(createFormSubmitMsg); ok {
						t.Error("the paste submitted the form")
					}
				}
			}
			if entries, _ := os.ReadDir(dir); len(entries) != 4 {
				t.Errorf("the directory holds %d entries, want the 4 it started with", len(entries))
			}
		})
	}
}

func TestPasteReachesTheFilterBar(t *testing.T) {
	tests := []struct{ name, paste, want string }{
		{"word", "notes", "notes"},
		{"multi-line", "a\nb", "a b"},
		{"action letters", "Dq", "Dq"},
	}
	for _, tc := range tests {
		t.Run(tc.name, func(t *testing.T) {
			m := openAt(t, tree(t))
			m, _ = press(t, m, "/")
			m, _ = step(t, m, testutil.Paste(tc.paste))
			if got := m.table.FilterBar().SearchQuery(); got != tc.want {
				t.Errorf("search query = %q after pasting %q, want %q", got, tc.paste, tc.want)
			}
			if m.mode != modeNormal {
				t.Error("the paste changed the mode")
			}
		})
	}
}
