package netdiag

import (
	"context"
	"net/netip"
	"testing"

	"github.com/anthnel/devdesk/internal/command"
	"github.com/anthnel/devdesk/internal/netcheck"
	"github.com/anthnel/devdesk/internal/ui/keymap"
	"github.com/anthnel/devdesk/internal/ui/testutil"
)

// DEVDESK-471 AC6: a view opened from Status (status's H) has four tabs, a
// prefilled Diagnostics form, and esc from the results goes back to Status,
// also after a visit to the trace sub-level.
func statusOpened(t *testing.T) *Model {
	t.Helper()
	m := NewWithTarget(testConfig(), netcheck.Target{Host: "example.com", Port: 443})
	m.OriginView = command.ViewStatus
	m = feed(t, m, testutil.Resize(120, 40))
	fe := &fakeEngine{}
	fe.resolveFn = func(context.Context, string) ([]netip.Addr, error) {
		return []netip.Addr{netip.MustParseAddr("93.184.216.34")}, nil
	}
	m.pathModel.engine = fe.engine()
	if m.activeTab != tabDiagnostics || m.state != StateInput ||
		m.targetInput.Value() != "example.com" || m.portInput.Value() != "443" {
		t.Fatalf("tab=%d state=%v target=%q port=%q, want the prefilled Diagnostics form",
			m.activeTab, m.state, m.targetInput.Value(), m.portInput.Value())
	}
	return m
}

func TestOpenedFromStatusHasFourTabs(t *testing.T) {
	m := statusOpened(t)
	// The model is a pointer: cycle it itself and expect it back on Diagnostics.
	for _, want := range []int{tabPorts, tabInterfaces, tabForward, tabDiagnostics} {
		m = feed(t, m, testutil.Key("tab"))
		if m.activeTab != want {
			t.Fatalf("tab reached %d, want %d (four tabs)", m.activeTab, want)
		}
	}
}

func TestOpenedFromStatusEscReturnsToStatusAfterATraceVisit(t *testing.T) {
	m := statusOpened(t)
	m = feed(t, m, testutil.Key("enter"))
	route := check(netcheck.CheckRoute, netcheck.OK, "eth0 via 192.168.1.1")
	route.Title = "Local route"
	m = deliver(t, m, route, check(netcheck.CheckResolve, netcheck.OK, "example.com resolves"))
	m = selectCheck(t, m, netcheck.CheckRoute)

	m = feed(t, m, testutil.Key(keymap.Diagnose), testutil.Key("enter"))
	m = traceHops(t, m, 2)
	m = feed(t, m, testutil.Key("esc")) // cancels the trace
	m = feed(t, m, testutil.Key("esc")) // back to the results
	if m.state != StateResults {
		t.Fatalf("state = %v, want StateResults after leaving the sub-level", m.state)
	}

	_, cmd := step(t, m, testutil.Key("esc"))
	msg, ok := testutil.MsgOf[BackToOriginMsg](cmd)
	if !ok || msg.Origin != command.ViewStatus {
		t.Errorf("esc from the results gave %T %+v, want BackToOriginMsg to Status", testutil.Msg(cmd), msg)
	}
}
