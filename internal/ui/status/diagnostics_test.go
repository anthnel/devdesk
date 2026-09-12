package status

import (
	"testing"

	"github.com/anthnel/devdesk/internal/status"
	"github.com/anthnel/devdesk/internal/ui/keymap"
	"github.com/anthnel/devdesk/internal/ui/netdiag"
	"github.com/anthnel/devdesk/internal/ui/testutil"
)

func TestDiagnosticsTargetForAnHTTPSMonitor(t *testing.T) {
	tg, autoRun := diagnosticsTarget(status.ComponentStatus{Type: status.TypeHTTPS, Target: "example.com"})
	if tg.Host != "example.com" || tg.Port != 443 {
		t.Errorf("target = %+v, want example.com:443", tg)
	}
	if !autoRun {
		t.Error("autoRun = false, want true — an https monitor's port is certain")
	}
}

func TestDiagnosticsTargetForAnHTTPMonitorWithAnExplicitPort(t *testing.T) {
	tg, autoRun := diagnosticsTarget(status.ComponentStatus{Type: status.TypeHTTP, Target: "http://example.com:8080/health"})
	if tg.Host != "example.com" || tg.Port != 8080 {
		t.Errorf("target = %+v, want example.com:8080", tg)
	}
	if !autoRun {
		t.Error("autoRun = false, want true")
	}
}

func TestDiagnosticsTargetForAnSSLMonitor(t *testing.T) {
	tg, autoRun := diagnosticsTarget(status.ComponentStatus{Type: status.TypeSSL, Target: "example.com:8443"})
	if tg.Host != "example.com" || tg.Port != 8443 {
		t.Errorf("target = %+v, want example.com:8443", tg)
	}
	if !autoRun {
		t.Error("autoRun = false, want true — an SSL monitor's port is certain")
	}

	tg, _ = diagnosticsTarget(status.ComponentStatus{Type: status.TypeSSL, Target: "example.com"})
	if tg.Host != "example.com" || tg.Port != 443 {
		t.Errorf("target = %+v, want the default 443 when the monitor named no port", tg)
	}
}

func TestDiagnosticsTargetForAnICMPOrDNSMonitorDoesNotAutoRun(t *testing.T) {
	for _, typ := range []status.ComponentType{status.TypeICMP, status.TypeDNS} {
		tg, autoRun := diagnosticsTarget(status.ComponentStatus{Type: typ, Target: "example.com"})
		if tg.Host != "example.com" {
			t.Errorf("%s: host = %q, want example.com", typ, tg.Host)
		}
		if autoRun {
			t.Errorf("%s: autoRun = true, want false — the port is a guess, not read from the monitor", typ)
		}
	}
}

// TestHOpensDiagnosticsOnTheSelectedMonitor covers §3.66: pressing H on a
// monitor asks the router to open netdiag on its target.
func TestHOpensDiagnosticsOnTheSelectedMonitor(t *testing.T) {
	m := loadedModel(t)
	// Sorted display order is api, dns-primary, web (see
	// TestSelectedIndexResolvesThroughTheSortOrder); cursor 2 is web (https).
	m.monitorTable.SetCursor(2)

	_, cmd := step(t, m, testutil.Key(keymap.Diagnose))
	msg, ok := testutil.MsgOf[netdiag.OpenRequestMsg](cmd)
	if !ok {
		t.Fatalf("H emitted %T, want netdiag.OpenRequestMsg", testutil.Msg(cmd))
	}
	if msg.Target.Host != "example.com" || msg.Target.Port != 443 {
		t.Errorf("target = %+v, want example.com:443", msg.Target)
	}
	if !msg.AutoRun {
		t.Error("AutoRun = false, want true for an https monitor")
	}
}

// TestHOnTheCertificateTabUsesTheCertsHost covers the other declared tab.
func TestHOnTheCertificateTabUsesTheCertsHost(t *testing.T) {
	m := loadedModel(t)
	m = feed(t, m, testutil.Key("tab")) // Certificates tab — one row, "cert"

	_, cmd := step(t, m, testutil.Key(keymap.Diagnose))
	msg, ok := testutil.MsgOf[netdiag.OpenRequestMsg](cmd)
	if !ok {
		t.Fatalf("H emitted %T, want netdiag.OpenRequestMsg", testutil.Msg(cmd))
	}
	if msg.Target.Host != "example.com" || msg.Target.Port != 443 {
		t.Errorf("target = %+v, want example.com:443", msg.Target)
	}
}

func TestHOnADNSMonitorDoesNotAutoRun(t *testing.T) {
	m := loadedModel(t)
	// dns-primary is the second row in sorted display order (api,
	// dns-primary, web).
	m.monitorTable.SetCursor(1)

	_, cmd := step(t, m, testutil.Key(keymap.Diagnose))
	msg, ok := testutil.MsgOf[netdiag.OpenRequestMsg](cmd)
	if !ok {
		t.Fatalf("H emitted %T, want netdiag.OpenRequestMsg", testutil.Msg(cmd))
	}
	if msg.AutoRun {
		t.Error("AutoRun = true, want false — a DNS monitor's port is a guess")
	}
}

func TestHWithNoSelectionDoesNothing(t *testing.T) {
	m := newTestModel(t) // no components at all
	_, cmd := step(t, m, testutil.Key(keymap.Diagnose))
	if cmd != nil {
		t.Error("H with nothing selected returned a command, want none")
	}
}
