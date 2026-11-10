package fuzzy

import (
	"testing"

	"github.com/anthnel/devdesk/internal/ui/testutil"
)

// DEVDESK-201 D3: the prompt hands the whole message to its input, so a paste
// lands in the query (newlines become spaces) and neither confirms nor cancels.
func TestPasteReachesTheQuery(t *testing.T) {
	tests := []struct{ name, typed, paste, want string }{
		{"word", "", "devdesk", "devdesk"},
		{"appended", "dev", "desk", "devdesk"},
		{"multi-line", "", "a\nb", "a b"},
		{"action letters", "", "Dq?", "Dq?"},
	}
	for _, tc := range tests {
		t.Run(tc.name, func(t *testing.T) {
			f := New("Loading")
			for _, k := range testutil.Type(tc.typed) {
				f, _ = f.Update(k)
			}
			f, cmd := f.Update(testutil.Paste(tc.paste))
			if got := f.query.Value(); got != tc.want {
				t.Errorf("query = %q after pasting %q, want %q", got, tc.paste, tc.want)
			}
			for _, msg := range testutil.Msgs(cmd) {
				switch msg.(type) {
				case ConfirmMsg, CancelMsg:
					t.Errorf("the paste produced %T", msg)
				}
			}
		})
	}
}

// A Windows-style paste ("a\r\nb") arrives with CRLF line ends. The input
// turns each of \r and \n into a space, so the query holds two spaces. This
// documents the current behavior; a single space would need the paste to be
// normalized (\r\n -> \n) before it reaches the input.
func TestPasteCRLFBecomesTwoSpaces(t *testing.T) {
	f := New("Loading")
	f, _ = f.Update(testutil.Paste("a\r\nb"))
	if got := f.query.Value(); got != "a  b" {
		t.Errorf("query = %q after pasting CRLF text, want %q (current behavior)", got, "a  b")
	}
}
