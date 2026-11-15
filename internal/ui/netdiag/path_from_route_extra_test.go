package netdiag

import (
	"net/netip"
	"strings"
	"testing"

	"github.com/anthnel/devdesk/internal/netcheck"
	"github.com/anthnel/devdesk/internal/ui/components"
	"github.com/anthnel/devdesk/internal/ui/keymap"
	"github.com/anthnel/devdesk/internal/ui/testutil"
)

func TestHWithAnInvalidTargetReportsAnErrorWithoutSystemText(t *testing.T) {
	m, _ := routeResults(t, "example.com")
	m = selectCheck(t, m, netcheck.CheckRoute)
	// The target parses for the trace, but the form's port does not: H is
	// offered, then buildTarget refuses.
	m.portInput.SetValue("abc")
	if _, err := m.buildTarget(); err == nil {
		t.Skip("an invalid port is accepted here; no way to make buildTarget fail")
	}

	m = feed(t, m, testutil.Key(keymap.Diagnose))

	const want = "Cannot trace — the target is no longer valid"
	if m.footer.Level() != components.LevelError || m.footer.Text() != want {
		t.Errorf("footer = {%v %q}, want Error %q", m.footer.Level(), m.footer.Text(), want)
	}
	if m.activeTab != tabDiagnostics {
		t.Errorf("activeTab = %d, want Diagnostics unchanged", m.activeTab)
	}
}

func TestHReadyForgetsThePreviousPathTarget(t *testing.T) {
	m, _ := routeResults(t, "example.com")
	m = selectCheck(t, m, netcheck.CheckRoute)
	m.pathModel.target = pathTarget{Host: "old.example"}
	m.pathModel.addr = netip.MustParseAddr("192.0.2.1")

	m = feed(t, m, testutil.Key(keymap.Diagnose))

	if m.pathModel.target.Host != "example.com" || m.pathModel.addr.IsValid() {
		t.Errorf("target=%v addr=%v, want the results' host and no address", m.pathModel.target, m.pathModel.addr)
	}
	for _, h := range m.pathModel.headerInfo() {
		if strings.Contains(h.Value, "old.example") || strings.Contains(h.Value, "192.0.2.1") {
			t.Errorf("header still shows the old target: %q", h.Value)
		}
	}
}
