package viewer

import (
	"testing"

	"github.com/anthnel/devdesk/internal/ui/testutil"
)

// DEVDESK-201 D3: a paste reaches the search bar and the go-to-line prompt,
// newlines become spaces in these one-line fields, and no viewer action fires
// (Rule 111: f, c, w, n, g, s, V... are all text while a field has focus).
func TestPasteReachesTheSearchBar(t *testing.T) {
	tests := []struct{ name, paste, want string }{
		{"word", "error", "error"},
		{"with space", "x y", "x y"},
		{"multi-line", "a\nb", "a b"},
		{"action letters", "fcwngsV", "fcwngsV"},
	}
	for _, tc := range tests {
		t.Run(tc.name, func(t *testing.T) {
			m := feed(t, longLog(t), testutil.Key("/"))
			if !m.bar.InEditMode() {
				t.Fatal("/ did not open the search bar")
			}
			before := m.gotoActive
			m = feed(t, m, testutil.Paste(tc.paste))

			if got := m.bar.SearchQuery(); got != tc.want {
				t.Errorf("search query = %q after pasting %q, want %q", got, tc.paste, tc.want)
			}
			if m.gotoActive != before {
				t.Error("the paste opened the go-to-line prompt")
			}
		})
	}
}

func TestPasteReachesTheGoToLinePrompt(t *testing.T) {
	tests := []struct{ name, paste, want string }{
		{"number", "120", "120"},
		{"multi-line", "1\n2", "1 2"},
		{"action letters", "fcwn", "fcwn"},
	}
	for _, tc := range tests {
		t.Run(tc.name, func(t *testing.T) {
			m := feed(t, longLog(t), testutil.Key("g"))
			if !m.gotoActive {
				t.Fatal("g did not open the prompt")
			}
			m = feed(t, m, testutil.Paste(tc.paste))

			if got := m.gotoInput.Value(); got != tc.want {
				t.Errorf("prompt = %q after pasting %q, want %q", got, tc.paste, tc.want)
			}
			if !m.gotoActive {
				t.Error("the paste closed the prompt")
			}
			if m.textViewport.YOffset() != 0 {
				t.Errorf("YOffset = %d: the paste scrolled the pane", m.textViewport.YOffset())
			}
		})
	}
}
