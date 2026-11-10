package agentusageview

import (
	"testing"
	"time"

	"github.com/anthnel/devdesk/internal/ui/testutil"
)

// DEVDESK-201 D3: a paste reaches the filter bar once `/` has opened it,
// newlines become spaces, and no view action fires (Rule 111: the uppercase
// vocabulary is text while the bar has the keyboard).
func TestPasteReachesTheFilterBar(t *testing.T) {
	tests := []struct{ name, paste, want string }{
		{"word", "alpha", "alpha"},
		{"with space", "x y", "x y"},
		{"multi-line", "a\nb", "a b"},
		{"action letters", "DSCqK", "DSCqK"},
	}
	for _, tc := range tests {
		t.Run(tc.name, func(t *testing.T) {
			m := loaded(t, newModel(t), session("11111111-aaaa", "/srv/alpha", time.Hour))
			m = feed(t, m, testutil.Key("/"))
			m = feed(t, m, testutil.Paste(tc.paste))
			if got := m.table.FilterBar().SearchQuery(); got != tc.want {
				t.Errorf("search query = %q after pasting %q, want %q", got, tc.paste, tc.want)
			}
		})
	}
}
