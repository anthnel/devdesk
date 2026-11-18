package netdiag

import (
	"net/url"
	"slices"
	"strings"
	"testing"

	"github.com/charmbracelet/x/ansi"

	"github.com/anthnel/devdesk/internal/netcheck"
	"github.com/anthnel/devdesk/internal/netproxy"
	"github.com/anthnel/devdesk/internal/ui/testutil"
)

// DEVDESK-582, acceptance criteria 1, 2, 3, 4 and 6 at the screen level: the
// Proxy row says, in the table and in its detail, whether a proxy variable
// applies to the target, and no credential ever reaches a screen.

const (
	secretUser = "s3cretUser"
	secretPass = "s3cretPass"
)

// proxyEnv is the counting env with a scripted answer to the proxy question,
// so no test reads the proxy variables of the machine it runs on.
type proxyEnv struct {
	*countingEnv
	https, http netproxy.Decision
}

func (e proxyEnv) ProxyFor(u *url.URL) netproxy.Decision {
	e.countingEnv.ProxyFor(u)
	if u.Scheme == "https" {
		return e.https
	}
	return e.http
}

// runWithProxyAnswer runs the whole pipeline through the model's messages with
// the given proxy answer for both schemes.
func runWithProxyAnswer(t *testing.T, answer netproxy.Decision) *Model {
	t.Helper()
	return runWithProxyAnswers(t, answer, answer)
}

// runWithProxyAnswers answers the https and the http question separately.
func runWithProxyAnswers(t *testing.T, https, http netproxy.Decision) *Model {
	t.Helper()
	env := proxyEnv{countingEnv: newCountingEnv(), https: https, http: http}
	m := newTestModel(t)
	m.checkEnv = func(netcheck.Settings) netcheck.Env { return env }
	return runDiagnostics(t, m, "127.0.0.1")
}

// openProxyRow puts the cursor on the Proxy row and opens its detail.
func openProxyRow(t *testing.T, m *Model) *Model {
	t.Helper()
	for i, c := range m.checksTable.Visible() {
		if c.ID == netcheck.CheckProxy {
			m.checksTable.SetCursor(i)
			m = feed(t, m, testutil.Key("enter"))
			if m.state != StateDetails || m.selected.ID != netcheck.CheckProxy {
				t.Fatalf("state = %v, selected = %q, want the Proxy detail open", m.state, m.selected.ID)
			}
			return m
		}
	}
	t.Fatal("the table has no Proxy row")
	return m
}

// proxyLine is the table line that carries the Proxy row, stripped of styling.
func proxyLine(t *testing.T, rendered string) string {
	t.Helper()
	for _, line := range strings.Split(ansi.Strip(rendered), "\n") {
		if strings.Contains(line, "Proxy") {
			return line
		}
	}
	t.Fatalf("no table line carries the Proxy row:\n%s", ansi.Strip(rendered))
	return ""
}

func noSecret(t *testing.T, where, text string) {
	t.Helper()
	for _, bad := range []string{secretUser, secretPass} {
		if strings.Contains(text, bad) {
			t.Errorf("%s carries %q:\n%s", where, bad, ansi.Strip(text))
		}
	}
}

// Criterion 1 and 6, wiring only: a proxied target reads "Configured for this
// target: host:port" in the table and the proxy in the detail, and the view
// prints nothing beyond the Decision it is handed (table, footer, header,
// detail). The Decision here is already redacted, so this does not prove the
// redaction: that proof is in internal/netproxy (Describe) and, from the real
// environment variable to the results, in TestTheProxyRowNeverCarriesACredentialEndToEnd
// (internal/netcheck/proxy_e2e_test.go).
func TestAProxiedTargetShowsTheProxyFromItsDecisionOnly(t *testing.T) {
	const proxy = "http://proxy.test:3128"
	m := runWithProxyAnswer(t, netproxy.Decision{
		Kind: netproxy.DecisionProxied, Variable: "HTTPS_PROXY", Proxy: proxy,
	})

	table := m.Render()
	if want := "Configured for this target: " + proxy; !strings.Contains(proxyLine(t, table), want) {
		t.Errorf("the Proxy row does not say %q:\n%s", want, proxyLine(t, table))
	}
	if line := proxyLine(t, table); !strings.Contains(line, "INFO") {
		t.Errorf("the Proxy row is not an INFO row: %q", line)
	}
	noSecret(t, "the table", table)
	noSecret(t, "the footer", m.RenderFooter(120))
	for _, h := range m.GetHeaderInfo("ctx") {
		noSecret(t, "the header "+h.Key, h.Value)
	}

	m = openProxyRow(t, m)
	details := m.renderDetailsContent(100)
	if !strings.Contains(ansi.Strip(details), proxy) {
		t.Errorf("the Proxy detail does not name %q:\n%s", proxy, ansi.Strip(details))
	}
	noSecret(t, "the Proxy detail", details)
	noSecret(t, "the detail screen", m.Render())
	noSecret(t, "the detail footer", m.RenderFooter(120))
	noSecret(t, "the detail title", m.GetTitle())
}

// Criterion 2: a target NO_PROXY names says so.
func TestAnExcludedTargetSaysExcludedByNoProxy(t *testing.T) {
	m := runWithProxyAnswer(t, netproxy.Decision{Kind: netproxy.DecisionExcluded, Variable: "HTTPS_PROXY"})

	line := proxyLine(t, m.Render())
	if !strings.Contains(line, "Excluded by NO_PROXY") {
		t.Errorf("the Proxy row = %q, want \"Excluded by NO_PROXY\"", line)
	}
	if strings.Contains(line, "INFO") {
		t.Errorf("an excluded target is not a proxy observation: %q", line)
	}
}

// Criterion 6: an unusable value is named by its variable, never by its value.
func TestAnInvalidValueIsNamedWithoutItsValue(t *testing.T) {
	m := runWithProxyAnswers(t,
		netproxy.Decision{Kind: netproxy.DecisionInvalid, Variable: "HTTPS_PROXY"}, netproxy.Decision{})

	table := m.Render()
	line := proxyLine(t, table)
	if !strings.Contains(line, "Invalid HTTPS_PROXY value") {
		t.Errorf("the Proxy row = %q, want \"Invalid HTTPS_PROXY value\"", line)
	}
	if !strings.Contains(line, "WARN") {
		t.Errorf("an invalid value is not a WARN row: %q", line)
	}
	noSecret(t, "the table", table)

	m = openProxyRow(t, m)
	details := ansi.Strip(m.renderDetailsContent(100))
	noSecret(t, "the Proxy detail", details)
	if strings.Contains(details, "@") {
		t.Errorf("the Proxy detail carries an \"@\", a trace of the value:\n%s", details)
	}
}

// Criterion 3: with no proxy variable the row says None, and the rest of the
// screen is what it was without the stage: same rows, same order, same
// verdicts, same observations.
func TestWithoutAProxyTheRestOfTheScreenIsUnchanged(t *testing.T) {
	m := runWithProxyAnswer(t, netproxy.Decision{Kind: netproxy.DecisionNone})

	// The row, as it reads on screen: Check, verdict, observed.
	fields := strings.Fields(proxyLine(t, m.Render()))
	if n := len(fields); n < 3 || fields[0] != "Proxy" || fields[n-2] != "N/A" || fields[n-1] != "None" {
		t.Errorf("the Proxy row reads %q, want Proxy · N/A · None", fields)
	}

	// The same run, stage by stage, without the proxy stage.
	var without netcheck.Results
	env := newCountingEnv()
	for _, id := range netcheck.Steps() {
		if id == netcheck.StageProxy {
			continue
		}
		without = netcheck.RunStep(t.Context(), netcheck.Target{Host: "127.0.0.1", Port: 443}, env,
			netcheck.DefaultSettings(), id, without)
	}

	type row struct {
		ID      netcheck.CheckID
		Title   string
		Verdict netcheck.Verdict
		Summary string
	}
	rows := func(checks []netcheck.Check) []row {
		var out []row
		for _, c := range checks {
			if c.ID == netcheck.CheckProxy {
				continue
			}
			out = append(out, row{c.ID, c.Title, c.Verdict, c.Summary})
		}
		return out
	}
	got, want := rows(m.checksTable.Visible()), rows(without.All())
	if !slices.Equal(got, want) {
		t.Errorf("the other rows changed:\n got %v\nwant %v", got, want)
	}
	if len(want) == 0 {
		t.Fatal("the reference run produced no row")
	}
}

// Rule 114: the help says what the new row asks, where the other rows are
// listed, and that the checks themselves never go through the proxy.
func TestTheHelpDescribesTheProxyRow(t *testing.T) {
	sections := map[string]string{}
	for _, s := range New(testConfig()).GetHelpContent().Sections {
		sections[s.Title] = s.Body
	}

	asks, ok := sections["Diagnostics Tab — What it asks"]
	if !ok {
		t.Fatal("no \"What it asks\" section")
	}
	const line = "Proxy              - whether a proxy variable applies to this target; the checks below go direct"
	if !strings.Contains(asks, line) {
		t.Errorf("\"What it asks\" has no line %q:\n%s", line, asks)
	}
	icmp, proxy, tcp := strings.Index(asks, "ICMP echo"), strings.Index(asks, line), strings.Index(asks, "TCP connect")
	if icmp < 0 || tcp < 0 || icmp >= proxy || proxy >= tcp {
		t.Errorf("the Proxy line is not between ICMP echo and TCP connect (%d, %d, %d)", icmp, proxy, tcp)
	}

	where, ok := sections["Where the checks run"]
	if !ok {
		t.Fatal("no \"Where the checks run\" section")
	}
	found := false
	for _, sentence := range strings.Split(strings.ReplaceAll(where, "\n", " "), ". ") {
		low := strings.ToLower(sentence)
		if strings.Contains(low, "proxy") && strings.Contains(low, "never") {
			found = true
		}
	}
	if !found {
		t.Errorf("\"Where the checks run\" has no sentence saying the checks never go through the proxy:\n%s", where)
	}
}
