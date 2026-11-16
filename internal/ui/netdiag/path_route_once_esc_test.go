package netdiag

import (
	"testing"

	"github.com/anthnel/devdesk/internal/netcheck"
	"github.com/anthnel/devdesk/internal/ui/testutil"
)

// An esc during the run, before the route stage ends, leaves results without a
// route lookup: there is nothing to reuse, so the trace reads the route itself
// (DEVDESK-473, plan step 4).
func TestRouteOnceAnEscDuringTheRunBeforeTheRouteStageReadsAgain(t *testing.T) {
	m, env := routeOnceModel(t, &fakeEngine{})
	m = typeInto(t, m, onceHostA)
	m, cmd := step(t, m, testutil.Key("enter"))
	done, ok := cmd().(stageDoneMsg) // the first stage only: the route stage has not run
	if !ok {
		t.Fatal("the first stage did not return a stageDoneMsg")
	}
	m, _ = step(t, m, done)
	if m.state != StateRunning {
		t.Fatalf("state = %v after the first stage, want StateRunning", m.state)
	}
	m = feed(t, m, testutil.Key("esc"))
	if m.state != StateResults {
		t.Fatalf("state = %v after esc, want StateResults", m.state)
	}
	if got := m.results.RouteLookups(); got != nil {
		t.Fatalf("route lookups = %v after an early esc, want none", got)
	}

	// The early stop leaves no Local route row, so H is refused: the trace level
	// is entered the way H does it, on the results the run left.
	for _, c := range m.results.All() {
		if c.ID == netcheck.CheckRoute {
			t.Fatal("the route stage ran before the esc; the test needs it unfinished")
		}
	}
	m.openTrace(StateResults, pathTarget{Host: onceHostA})
	m = startTrace(t, m)

	requireRouteCalls(t, env, onceHostA, 1)
	requireLocalRows(t, m, "eth1 (192.168.1.21)", "192.168.1.1")
}
