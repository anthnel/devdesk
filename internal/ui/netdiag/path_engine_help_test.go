package netdiag

import (
	"strings"
	"testing"
)

// DEVDESK-332, Rule 114: the help says what the engine row reports when the
// container's bridge is not on this machine, and where the hops start.
func TestPathHelpExplainsTheEngineRow(t *testing.T) {
	body := pathHelpContainerParagraphs(t)
	cases := []struct{ what, want string }{
		{"the row", "engine row"},
		{"bridge absent", "internal path not visible (location unknown)"},
		{"why", "in a VM, remote or rootless"},
		{"no guessing", "DevDesk does not guess"},
		{"bridge not verified", "undetermined (bridge not verified)"},
		{"hops origin", "hops are traced from this machine"},
		{"WSL", "WSL"},
		{"no view inside the VM", "DevDesk cannot see inside the engine's VM"},
	}
	for _, tc := range cases {
		if !strings.Contains(body, tc.want) {
			t.Errorf("Path help lacks %q (%s)", tc.want, tc.what)
		}
	}
}
