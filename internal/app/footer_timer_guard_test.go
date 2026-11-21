package app

import (
	"testing"
	"time"

	"github.com/anthnel/devdesk/internal/ui/components"
)

// footerWaitBudget is how long a test that only builds and drains footer
// commands may take. The production footer timer is 3 s
// (components.FooterMsgDuration), so a test that really waits for it cannot fit
// under this bound, while a test that ran in milliseconds has a wide margin even
// on a loaded -race runner.
const footerWaitBudget = 1500 * time.Millisecond

// requireNoRealFooterWait fails the test, when it ends, if it took longer than
// footerWaitBudget (DEVDESK-644).
//
// The expiry of a footer message is a tea.Tick whose timer starts when the Cmd
// is built, not when it is run. testutil.FastTimers must therefore be called
// before the Update that builds the Cmd: calling it afterwards (the old drive
// helper did) leaves a 3 s timer already armed, and testutil.Msgs then sleeps
// for the rest of it.
//
// Call it first in a test that builds a footer Cmd and drains it. The guard only
// catches a real footer wait, and checks that FooterMsgDuration is back to the
// value it had at the start of the test once the cleanups have run. The 3 s
// value itself is pinned by TestAMessageGetsThreeSeconds in the components
// package, not here.
func requireNoRealFooterWait(t *testing.T) {
	t.Helper()
	production := components.FooterMsgDuration
	start := time.Now()
	t.Cleanup(func() {
		t.Helper()
		if elapsed := time.Since(start); elapsed > footerWaitBudget {
			t.Errorf("test took %v, over the %v budget: a footer timer really slept; "+
				"call testutil.FastTimers(t, &components.FooterMsgDuration) before the Update that builds the Cmd "+
				"(the timer starts when the Cmd is built, not when it is run)", elapsed.Round(time.Millisecond), footerWaitBudget)
		}
		if components.FooterMsgDuration != production {
			t.Errorf("FooterMsgDuration = %v after cleanup, want %v restored", components.FooterMsgDuration, production)
		}
	})
}
