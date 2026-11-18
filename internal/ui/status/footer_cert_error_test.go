package status

import (
	"strings"
	"testing"
	"time"

	"github.com/charmbracelet/x/ansi"

	"github.com/anthnel/devdesk/internal/config"
	"github.com/anthnel/devdesk/internal/status"
	"github.com/anthnel/devdesk/internal/ui/components"
	"github.com/anthnel/devdesk/internal/ui/keymap"
	"github.com/anthnel/devdesk/internal/ui/testutil"
	"github.com/anthnel/devdesk/internal/ui/theme"
)

// DEVDESK-581 step 4 (Q2 = b): on the Certificates tab, any DOWN failure of the
// selected row (proxy, x509, dial) is carried by the footer at level Error. The
// message is derived on every render, like Rule 140's, so it has no timer.

const (
	proxyRefused = "proxy: CONNECT refused (403 Forbidden)"
	x509Failure  = "x509: certificate signed by unknown authority"
)

func certFooterConfigs() []config.ComponentConfig {
	return append(monitorConfigs(),
		config.ComponentConfig{Name: "cert-proxy", Type: "ssl", Target: "proxied.example.com:443"},
		config.ComponentConfig{Name: "cert-x509", Type: "ssl", Target: "selfsigned.example.com:443"},
		config.ComponentConfig{Name: "cert-ok", Type: "ssl", Target: "ok.example.com:443"},
		config.ComponentConfig{Name: "cert-soon", Type: "ssl", Target: "soon.example.com:443"},
		config.ComponentConfig{Name: "cert-expired", Type: "ssl", Target: "expired.example.com:443"},
	)
}

// certFooterResults is checkResults plus five certificates covering each way a
// row can end: DOWN by proxy, DOWN by x509, OK, close to expiry, expired.
func certFooterResults() []status.ComponentStatus {
	good, soon, expired := 90, 3, -2
	return append(checkResults(),
		status.ComponentStatus{Name: "cert-proxy", Type: status.TypeSSL, Target: "proxied.example.com:443", Status: status.StatusDown, Error: proxyRefused},
		status.ComponentStatus{Name: "cert-x509", Type: status.TypeSSL, Target: "selfsigned.example.com:443", Status: status.StatusDown, Error: x509Failure},
		status.ComponentStatus{Name: "cert-ok", Type: status.TypeSSL, Target: "ok.example.com:443", Status: status.StatusOK, SSLDaysLeft: &good},
		status.ComponentStatus{Name: "cert-soon", Type: status.TypeSSL, Target: "soon.example.com:443", Status: status.StatusError, Error: "expires in 3 days", SSLDaysLeft: &soon},
		status.ComponentStatus{Name: "cert-expired", Type: status.TypeSSL, Target: "expired.example.com:443", Status: status.StatusError, Error: "certificate expired", SSLDaysLeft: &expired},
	)
}

func certFooterModel(t *testing.T) Model {
	t.Helper()
	m := newTestModel(t, certFooterConfigs()...)
	return feed(t, m, CheckCompleteMsg{Components: certFooterResults(), Timestamp: time.Now()})
}

// onCertificate opens the Certificates tab and puts the cursor on a row by name.
// The cursor is moved with SetCursor because the display order (sorting) is not
// what these tests are about.
func onCertificate(t *testing.T, m Model, name string) Model {
	t.Helper()
	if m.activeTab != TabCertificates {
		m = feed(t, m, testutil.Key("tab"))
	}
	for i, c := range m.sslTable.Visible() {
		if c.Name == name {
			m.sslTable.SetCursor(i)
			if sel, ok := m.sslTable.Selected(); !ok || sel.Name != name {
				t.Fatalf("fixture error: cursor on row %d selects %+v, want %q", i, sel, name)
			}
			return m
		}
	}
	t.Fatalf("fixture error: no certificate row named %q", name)
	return m
}

// errorSGR is the opening sequence of the footer's Error level.
func errorSGR() string {
	return strings.SplitN(theme.FooterErrorStyle.Render("x"), "x", 2)[0]
}

func footerText(m Model) string { return ansi.Strip(m.RenderFooter(160)) }

func TestFooterCarriesTheSelectedCertificatesProxyFailure(t *testing.T) {
	m := onCertificate(t, certFooterModel(t), "cert-proxy")

	want := "cert-proxy " + theme.IconChevronRight + " " + proxyRefused
	st := m.status()
	if st.Text != want || st.Level != components.LevelError {
		t.Errorf("status() = {Text: %q, Level: %v}, want {%q, LevelError}", st.Text, st.Level, want)
	}
	if st.Activity {
		t.Error("status().Activity = true, a failed row is not a load")
	}

	footer := m.RenderFooter(160)
	if got := ansi.Strip(footer); !strings.Contains(got, want) {
		t.Errorf("footer = %q, want it to contain %q", got, want)
	}
	if !strings.Contains(footer, errorSGR()) {
		t.Error("the message is not rendered with the footer error style (ColorFooterError)")
	}
}

func TestFooterCarriesTheSelectedCertificatesX509Failure(t *testing.T) {
	m := onCertificate(t, certFooterModel(t), "cert-x509")

	want := "cert-x509 " + theme.IconChevronRight + " " + x509Failure
	if st := m.status(); st.Text != want || st.Level != components.LevelError {
		t.Errorf("status() = {Text: %q, Level: %v}, want {%q, LevelError}", st.Text, st.Level, want)
	}
	footer := m.RenderFooter(160)
	if !strings.Contains(ansi.Strip(footer), x509Failure) {
		t.Errorf("footer = %q, want the x509 text", ansi.Strip(footer))
	}
	if !strings.Contains(footer, errorSGR()) {
		t.Error("the x509 message is not at level Error")
	}
}

// The footer follows the cursor: it is derived from the selection on every
// render, not remembered from the last check.
func TestFooterFollowsTheCertificateCursor(t *testing.T) {
	m := onCertificate(t, certFooterModel(t), "cert-proxy")
	if !strings.Contains(footerText(m), proxyRefused) {
		t.Error("the proxy failure is not shown while its own row is selected")
	}

	m = onCertificate(t, m, "cert-x509")
	got := footerText(m)
	if strings.Contains(got, proxyRefused) || !strings.Contains(got, x509Failure) {
		t.Errorf("after moving to cert-x509 the footer = %q, want only the x509 text", got)
	}

	m = onCertificate(t, m, "cert-ok")
	if got := footerText(m); strings.Contains(got, x509Failure) || strings.Contains(got, proxyRefused) {
		t.Errorf("after moving to an OK row the footer = %q, want no failure text", got)
	}
}

func TestFooterHasNoFailureTextForARowThatIsNotDown(t *testing.T) {
	// "cert-soon" and "cert-expired" are StatusError with an Error string: the
	// expiry states stay carried by the columns (plan, step 4), only DOWN counts.
	// "cert" (the fixture's StatusWarning row) has no Error at all.
	for _, name := range []string{"cert-ok", "cert-soon", "cert-expired", "cert"} {
		t.Run(name, func(t *testing.T) {
			m := onCertificate(t, certFooterModel(t), name)

			if st := m.status(); st.Text != "" || st.Level != components.LevelInfo {
				t.Errorf("status() = %+v on %s, want the empty status", st, name)
			}
			got := footerText(m)
			for _, text := range []string{"expires in", "certificate expired", proxyRefused, x509Failure} {
				if strings.Contains(got, text) {
					t.Errorf("footer = %q on %s, must not contain %q", got, name, text)
				}
			}
			if strings.Contains(m.RenderFooter(160), errorSGR()) {
				t.Errorf("footer on %s carries the Error style, want none", name)
			}
		})
	}
}

func TestFooterIgnoresADownRowWithNoErrorText(t *testing.T) {
	results := append(certFooterResults(), status.ComponentStatus{
		Name: "cert-mute", Type: status.TypeSSL, Target: "mute.example.com:443", Status: status.StatusDown,
	})
	cfg := append(certFooterConfigs(), config.ComponentConfig{Name: "cert-mute", Type: "ssl", Target: "mute.example.com:443"})
	m := feed(t, newTestModel(t, cfg...), CheckCompleteMsg{Components: results, Timestamp: time.Now()})
	m = onCertificate(t, m, "cert-mute")

	if st := m.status(); st.Text != "" {
		t.Errorf("status().Text = %q for a DOWN row with an empty Error, want none", st.Text)
	}
}

// The Monitors tab is unchanged: its DOWN row ("api") is not announced.
func TestFooterIsUnchangedOnTheMonitorsTab(t *testing.T) {
	m := certFooterModel(t)
	if m.activeTab != TabMonitors {
		t.Fatal("fixture error: the model does not open on the Monitors tab")
	}
	// Put the cursor on the DOWN monitor.
	for i, c := range m.monitorTable.Visible() {
		if c.Name == "api" {
			m.monitorTable.SetCursor(i)
		}
	}
	if sel, _ := m.monitorTable.Selected(); sel.Name != "api" || sel.Status != status.StatusDown {
		t.Fatalf("fixture error: selected monitor = %+v, want the DOWN api row", sel)
	}

	if st := m.status(); st.Text != "" || st.Level != components.LevelInfo {
		t.Errorf("status() = %+v on a DOWN monitor, want the empty status", st)
	}
	if got := footerText(m); strings.Contains(got, "connection refused") {
		t.Errorf("footer = %q, a monitor failure must not reach the footer", got)
	}

	// A DOWN certificate is not announced while the Monitors tab is showing,
	// even though sslTable still holds the cursor.
	for i, c := range m.sslTable.Visible() {
		if c.Name == "cert-proxy" {
			m.sslTable.SetCursor(i)
		}
	}
	if got := footerText(m); strings.Contains(got, proxyRefused) {
		t.Errorf("footer = %q on the Monitors tab, want no certificate failure", got)
	}
}

func TestFooterLeavesTheCertificatesTabForTheMonitorsTab(t *testing.T) {
	m := onCertificate(t, certFooterModel(t), "cert-proxy")
	if !strings.Contains(footerText(m), proxyRefused) {
		t.Error("the failure is not shown while its row is selected on the Certificates tab")
	}
	m = feed(t, m, testutil.Key("tab"))
	if got := footerText(m); strings.Contains(got, proxyRefused) {
		t.Errorf("footer = %q after switching back to Monitors, want no certificate failure", got)
	}
}

// No timer: a ClearFooterMsg addressed to the footer's current id leaves the
// message in place, because it is a state derived on every render.
func TestFooterCertificateFailureSurvivesClearFooterMsg(t *testing.T) {
	m := onCertificate(t, certFooterModel(t), "cert-proxy")

	m = feed(t, m, components.ClearFooterMsg{ID: m.footer.ID()})

	if !strings.Contains(footerText(m), proxyRefused) {
		t.Errorf("footer = %q after ClearFooterMsg, want the failure still there", footerText(m))
	}
	if m.footer.IsSet() {
		t.Error("the failure was stored as a timed footer message; it must be derived, not set")
	}
}

// Derived, never set: nothing in the model keeps the text between two renders,
// so a fresh check that clears the failure clears the footer too.
func TestFooterFollowsTheNextCheck(t *testing.T) {
	m := onCertificate(t, certFooterModel(t), "cert-proxy")

	fixed := certFooterResults()
	for i := range fixed {
		if fixed[i].Name == "cert-proxy" {
			days := 60
			fixed[i].Status, fixed[i].Error, fixed[i].SSLDaysLeft = status.StatusOK, "", &days
		}
	}
	m = feed(t, m, CheckCompleteMsg{Components: fixed, Timestamp: time.Now()})
	m = onCertificate(t, m, "cert-proxy")

	if got := footerText(m); strings.Contains(got, proxyRefused) {
		t.Errorf("footer = %q after a check that fixed the row, want no failure text", got)
	}
}

// Rule 128 order, step 1: the check's own status comes first.
func TestFooterCheckingKeepsPriorityOverACertificateFailure(t *testing.T) {
	m := onCertificate(t, certFooterModel(t), "cert-proxy")
	m, _ = step(t, m, testutil.Key("ctrl+r"))
	if !m.checking {
		t.Fatal("fixture error: ctrl+r started no check")
	}

	wantChecking(t, m, "a check in progress over a DOWN certificate")
	got := footerText(m)
	if !strings.Contains(got, "Checking components...") || strings.Contains(got, proxyRefused) {
		t.Errorf("footer = %q during a check, want only \"Checking components...\"", got)
	}
}

// Rule 128 order, step 2: a timed message is shown before any derived status.
func TestFooterTimedMessageComesBeforeACertificateFailure(t *testing.T) {
	m := onCertificate(t, certFooterModel(t), "cert-proxy")
	_ = m.footer.Warn("Copied to clipboard") // the Cmd carries the 3 s timer; never run it

	got := footerText(m)
	if !strings.Contains(got, "Copied to clipboard") {
		t.Errorf("footer = %q, want the timed message", got)
	}
	if strings.Contains(got, proxyRefused) {
		t.Errorf("footer = %q, the certificate failure must not show over a timed message", got)
	}

	// Once the message is gone, the failure is still there. Clear is called on
	// the component directly because Update never offers it a ClearFooterMsg:
	// footer.Handle is reached only from handleMonitorOperations.
	m.footer.Clear()
	if !strings.Contains(footerText(m), proxyRefused) {
		t.Errorf("footer = %q after the timed message expired, want the failure back", footerText(m))
	}
}

// The form's validation status stays ahead of the failure, as it is today.
func TestFooterFormValidationComesBeforeACertificateFailure(t *testing.T) {
	m := onCertificate(t, certFooterModel(t), "cert-proxy")
	m = feed(t, m, testutil.Key(keymap.New))
	for i := 0; i < 4; i++ {
		m = feed(t, m, testutil.Key("down"))
	}
	m = feed(t, m, testutil.Key("enter"))

	got := footerText(m)
	if !strings.Contains(got, "Name is required") {
		t.Fatalf("fixture error: the refused submit shows %q, want the Name error", got)
	}
	if strings.Contains(got, proxyRefused) {
		t.Errorf("footer = %q with the form open, want the validation message only", got)
	}
}

// The line is one line: a long failure is truncated, never wrapped (Rule 124).
func TestFooterCertificateFailureStaysOnOneLine(t *testing.T) {
	long := "proxy: " + strings.Repeat("unreachable ", 40)
	results := certFooterResults()
	for i := range results {
		if results[i].Name == "cert-proxy" {
			results[i].Error = long
		}
	}
	m := feed(t, newTestModel(t, certFooterConfigs()...), CheckCompleteMsg{Components: results, Timestamp: time.Now()})
	m = onCertificate(t, m, "cert-proxy")

	if got, want := strings.Count(m.RenderFooter(80), "\n")+1, m.GetFooterHeight(); got != want {
		t.Errorf("RenderFooter emitted %d lines, GetFooterHeight() promised %d", got, want)
	}
}
