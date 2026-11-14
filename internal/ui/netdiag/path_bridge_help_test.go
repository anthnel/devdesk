package netdiag

import (
	"strings"
	"testing"
)

// DEVDESK-331, Rule 114: the help says when the bridge row appears, where it
// is read, and what happens when it cannot be shown.
func TestPathHelpExplainsTheBridge(t *testing.T) {
	body := pathHelpContainerParagraphs(t)
	cases := []struct{ what, want string }{
		{"the row", "bridge row"},
		{"only with the interface", "only when this machine has the interface of the container's network bridge"},
		{"read on this machine", "read on this machine"},
		{"no bridge otherwise", "no bridge is shown"},
		{"remote engine, same docker0", "docker0"},
	}
	for _, tc := range cases {
		if !strings.Contains(body, tc.want) {
			t.Errorf("Path help lacks %q (%s)", tc.want, tc.what)
		}
	}
}
