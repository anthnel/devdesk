package security

import (
	"testing"

	"github.com/anthnel/devdesk/internal/ui/testutil"
)

// Leaving a tab switches the Pod Security filter off, but only when it is on:
// switching off a filter that is already off rebuilds the rows and measures the
// columns again for nothing. With no result the tab change does not reload the
// table, so a rebuild here can only come from the token.
func TestSwitchTabDoesNotRebuildTheTableWhenTheFilterIsOff(t *testing.T) {
	m := onMisconfig(t, "baseline", pssFixtures())
	m.result = nil
	rows := m.findingsTable.Visible()
	if len(rows) == 0 {
		t.Fatal("no rows to observe")
	}
	first := &rows[0]

	m.switchTab(TabSecrets)

	if got := &m.findingsTable.Visible()[0]; got != first {
		t.Error("the table was rebuilt although the filter was already off")
	}
}

func TestSwitchTabStillSwitchesAnActiveFilterOff(t *testing.T) {
	m := feed(t, onMisconfig(t, "baseline", pssFixtures()), testutil.Key(pssFilterKey))
	if !m.findingsTable.IsTokenActive(pssToken) {
		t.Fatal("the filter is off before the test starts")
	}
	m.switchTab(TabSecrets)
	if m.findingsTable.IsTokenActive(pssToken) {
		t.Error("the filter is still on after the tab changed")
	}
}
