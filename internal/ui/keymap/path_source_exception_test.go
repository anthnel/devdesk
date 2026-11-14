package keymap

import "testing"

// DEVDESK-330: ctrl+o opens the "Trace from" selector on the Path tab. It is a
// declared exception on its own surface, not a free ride on the remediation
// tab's ctrl+o: IsException matches by key only, so without this declaration
// the next survey could not tell the two uses apart.
func TestPathSourceKeyIsADeclaredException(t *testing.T) {
	for _, e := range DeclaredExceptions() {
		if e.Key == "ctrl+o" && e.Surface == "netdiag/path" {
			if e.Why == "" {
				t.Error("the ctrl+o exception on netdiag/path has no reason")
			}
			return
		}
	}
	t.Errorf("no declared exception for ctrl+o on netdiag/path; declared: %v", exceptionKeys())
}
