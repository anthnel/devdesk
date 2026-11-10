package components

import (
	"testing"

	"github.com/anthnel/devdesk/internal/ui/testutil"
)

// DEVDESK-201 D3: FilterBar.Update accepts a paste while the bar is searching
// (the other users of the bar depend on it), newlines become spaces, and a
// paste into a bar that is not searching is ignored. TestTypedTextReachesTheFieldUnchanged
// and TestWrappedInputPaste cover the plain-text rows for the other fields.
func TestPasteReachesTheFilterBar(t *testing.T) {
	tests := []struct {
		name      string
		searching bool
		paste     string
		want      string
	}{
		{"word", true, "alpha", "alpha"},
		{"with space", true, "x y", "x y"},
		{"multi-line", true, "a\nb", "a b"},
		{"action letters", true, "DSCqK/", "DSCqK/"},
		{"not searching: ignored", false, "alpha", ""},
	}
	for _, tc := range tests {
		t.Run(tc.name, func(t *testing.T) {
			fb := NewFilterBar()
			if tc.searching {
				fb.ActivateSearch()
			}
			fb, _ = fb.Update(testutil.Paste(tc.paste))
			if got := fb.SearchQuery(); got != tc.want {
				t.Errorf("search query = %q after pasting %q, want %q", got, tc.paste, tc.want)
			}
			if fb.InEditMode() != tc.searching {
				t.Errorf("InEditMode() = %v, want %v: the paste changed the mode", fb.InEditMode(), tc.searching)
			}
		})
	}
}
