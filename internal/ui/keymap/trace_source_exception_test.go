package keymap

import (
	"strings"
	"testing"
)

// DEVDESK-471: ctrl+o opens the "Trace from" selector on the Diagnostics trace
// sub-level. It is a declared exception on its own surface, netdiag/trace:
// IsException matches by key only, so without this declaration the next survey
// could not tell the uses of ctrl+o apart. Step 2 removed the Path tab, so
// netdiag/path is gone and the reason no longer cites the tab.
func TestTraceSourceKeyIsADeclaredException(t *testing.T) {
	for _, e := range DeclaredExceptions() {
		if e.Key == "ctrl+o" && e.Surface == "netdiag/path" {
			t.Error("the netdiag/path exception outlived the Path tab")
		}
	}
	for _, e := range DeclaredExceptions() {
		if e.Key == "ctrl+o" && e.Surface == "netdiag/trace" {
			if e.Why == "" {
				t.Error("the ctrl+o exception on netdiag/trace has no reason")
			}
			if strings.Contains(e.Why, "Path tab") {
				t.Errorf("the reason still cites the Path tab: %q", e.Why)
			}
			return
		}
	}
	t.Errorf("no declared exception for ctrl+o on netdiag/trace; declared: %v", exceptionKeys())
}
