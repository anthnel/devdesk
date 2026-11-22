package keymap

import "testing"

// DEVDESK-678: tab flips the "g" prompt (ws and ge) between fuzzy and exact
// matching. Rule 135 reserves tab for switching tabs, so using it here is a
// declared exception on its own surface, fuzzy/finder. IsException matches by
// key only, so the surface is what tells this tab apart from the command
// line's (app/command-line).
func TestFinderTabKeyIsADeclaredException(t *testing.T) {
	for _, e := range DeclaredExceptions() {
		if e.Key == "tab" && e.Surface == "fuzzy/finder" {
			if e.Why == "" {
				t.Error("the tab exception on fuzzy/finder has no reason")
			}
			return
		}
	}
	t.Errorf("no declared exception for tab on fuzzy/finder; declared: %v", exceptionKeys())
}
