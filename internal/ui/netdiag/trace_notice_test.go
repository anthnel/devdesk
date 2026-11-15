package netdiag

import (
	"strings"
	"testing"

	tea "charm.land/bubbletea/v2"
	"github.com/charmbracelet/x/ansi"

	"github.com/anthnel/devdesk/internal/ui/components"
	"github.com/anthnel/devdesk/internal/ui/testutil"
)

// DEVDESK-470 task 470.5: the path trace probes the path to a host, so Port and
// DNS Server of the Diagnostics form do not apply to it. Trace Path says so in
// the sub-level's footer instead of ignoring them silently.

const (
	noticePortAndDNS = "Port and DNS Server are not used by the path trace"
	noticePortOnly   = "Port is not used by the path trace"
	noticeDNSOnly    = "DNS Server is not used by the path trace"
)

func TestTraceNoticeText(t *testing.T) {
	tests := []struct {
		name      string
		port, dns string
		want      string
	}{
		{"both empty", "", "", ""},
		{"both blank", "  ", "\t ", ""},
		{"port only", "8443", "", noticePortOnly},
		{"port only, dns blank", "8443", "   ", noticePortOnly},
		{"dns only", "", "9.9.9.9", noticeDNSOnly},
		{"dns only, port blank", " ", "9.9.9.9", noticeDNSOnly},
		{"both", "8443", "9.9.9.9", noticePortAndDNS},
	}
	for _, tt := range tests {
		t.Run(tt.name, func(t *testing.T) {
			if got := traceUnusedNotice(tt.port, tt.dns); got != tt.want {
				t.Errorf("traceUnusedNotice(%q, %q) = %q, want %q", tt.port, tt.dns, got, tt.want)
			}
		})
	}
}

// traceNoticeModel fills the form and presses Trace Path; it returns the Cmd
// that enter returned, not yet run.
func traceNoticeModel(t *testing.T, host, port, dns string) (*Model, *fakeEngine, tea.Cmd) {
	t.Helper()
	m, fe := traceFormModel(t, true)
	m.targetInput.SetValue(host)
	m.portInput.SetValue(port)
	m.dnsServerInput.SetValue(dns)
	m.focusedField = fieldTraceButton
	m, cmd := step(t, m, testutil.Key("enter"))
	return m, fe, cmd
}

func TestTraceNoticeShownInTheSubLevelFooterAndTheTraceIsLaunched(t *testing.T) {
	testutil.FastTimers(t, &components.FooterMsgDuration)
	m, fe, cmd := traceNoticeModel(t, traceLevelHost, "8443", "")

	if m.state != StateTrace || !m.pathModel.running() {
		t.Fatalf("state=%v running=%v, want the trace launched", m.state, m.pathModel.running())
	}
	f := &m.pathModel.footer
	if f.Level() != components.LevelInfo || f.Text() != noticePortOnly {
		t.Errorf("sub-level footer = {%v %q}, want Info %q", f.Level(), f.Text(), noticePortOnly)
	}
	if m.footer.Text() == noticePortOnly {
		t.Error("the notice is on the form's footer: it belongs to the sub-level's (m.pathModel.footer)")
	}

	// The Cmd carries the launch as well as the notice's timer.
	if fe.resolveCalls != 0 || fe.traceCalls != 0 {
		t.Fatalf("engine ran inside Update (resolve %d, trace %d, Rule 110)", fe.resolveCalls, fe.traceCalls)
	}
	// The Cmd starts with the resolution; the trace itself is chained on its
	// result message, which this test does not feed back.
	_ = testutil.Msgs(cmd)
	if fe.resolveCalls != 1 {
		t.Errorf("resolve calls = %d after the Cmd, want 1", fe.resolveCalls)
	}
}

func TestTraceNoticeTracesTheTargetAlone(t *testing.T) {
	testutil.FastTimers(t, &components.FooterMsgDuration)
	m, fe, cmd := traceNoticeModel(t, traceLevelHost, "8443", "9.9.9.9")

	if got := m.pathModel.target.Host; got != traceLevelHost {
		t.Errorf("traced host = %q, want %q alone", got, traceLevelHost)
	}
	if got := m.pathModel.target.String(); strings.Contains(got, "8443") || strings.Contains(got, "9.9.9.9") || strings.Contains(got, ":") {
		t.Errorf("traced target = %q, want no port and no DNS server in it", got)
	}
	_ = testutil.Msgs(cmd)
	if fe.resolveHost != traceLevelHost {
		t.Errorf("resolved host = %q, want %q", fe.resolveHost, traceLevelHost)
	}
	if got := m.pathModel.footer.Text(); got != noticePortAndDNS {
		t.Errorf("footer = %q, want %q", got, noticePortAndDNS)
	}
}

func TestTraceNoticeDNSOnly(t *testing.T) {
	m, _, _ := traceNoticeModel(t, traceLevelHost, "", "9.9.9.9")
	f := &m.pathModel.footer
	if f.Level() != components.LevelInfo || f.Text() != noticeDNSOnly {
		t.Errorf("footer = {%v %q}, want Info %q", f.Level(), f.Text(), noticeDNSOnly)
	}
}

func TestTraceNoticeAbsentWhenPortAndDNSAreEmpty(t *testing.T) {
	for _, blank := range []string{"", "   "} {
		m, _, _ := traceNoticeModel(t, traceLevelHost, blank, blank)
		if m.state != StateTrace || !m.pathModel.running() {
			t.Fatalf("blank %q: state=%v running=%v, want the trace launched", blank, m.state, m.pathModel.running())
		}
		if got := m.pathModel.footer.Text(); got != "" {
			t.Errorf("blank %q: sub-level footer = %q, want no notice", blank, got)
		}
		if got := m.footer.Text(); got != "" {
			t.Errorf("blank %q: form footer = %q, want no notice", blank, got)
		}
	}
}

// A refused target launches nothing and says nothing about Port or DNS.
func TestTraceNoticeAbsentWhenTheTargetIsRefused(t *testing.T) {
	m, fe, cmd := traceNoticeModel(t, "example.com:80", "8443", "9.9.9.9")

	if m.state != StateInput || m.pathModel.running() {
		t.Errorf("state=%v running=%v, a refused target must launch nothing", m.state, m.pathModel.running())
	}
	if cmd != nil {
		t.Errorf("a refused target returned a Cmd: %v", testutil.Msgs(cmd))
	}
	if fe.traceCalls != 0 || fe.resolveCalls != 0 {
		t.Errorf("trace=%d resolve=%d, want nothing emitted", fe.traceCalls, fe.resolveCalls)
	}
	for name, f := range map[string]*components.FooterMessage{"form": &m.footer, "sub-level": &m.pathModel.footer} {
		if strings.Contains(f.Text(), "not used by the path trace") {
			t.Errorf("%s footer = %q, want no notice on a refused target", name, f.Text())
		}
	}
}

// The notice must still be what the user reads once the sub-level renders and
// the trace progresses: neither the Path tab's derived status nor the hops may
// push it out before its own timer expires (Rule 128: info wins over status).
func TestTraceNoticeStaysVisibleWhileTheTraceRuns(t *testing.T) {
	phases := map[string]func(*Model) *Model{
		"first render": func(m *Model) *Model { return m },
		"after hops":   func(m *Model) *Model { return traceHops(t, m, 2) },
		"after the end": func(m *Model) *Model {
			return traceEnded(t, traceHops(t, m, 2))
		},
	}
	for name, advance := range phases {
		m, _, _ := traceNoticeModel(t, traceLevelHost, "8443", "")
		m = advance(m)
		if got := ansi.Strip(m.RenderFooter(120)); !strings.Contains(got, noticePortOnly) {
			t.Errorf("%s: footer = %q, want the notice %q visible", name, got, noticePortOnly)
		}
	}
}

// The notice is an event with its timer: its own expiry clears it, and the
// sub-level then shows the trace's progress again.
func TestTraceNoticeExpires(t *testing.T) {
	m, _, _ := traceNoticeModel(t, traceLevelHost, "8443", "")
	m = traceHops(t, m, 1)
	m = feed(t, m, components.ClearFooterMsg{ID: m.pathModel.footer.ID()})
	if got := m.pathModel.footer.Text(); got != "" {
		t.Errorf("footer = %q after the expiry, want the notice gone", got)
	}
	if got := ansi.Strip(m.RenderFooter(120)); !strings.Contains(got, "Tracing "+traceLevelHost) {
		t.Errorf("footer = %q, want the trace progress back once the notice expired", got)
	}
}
