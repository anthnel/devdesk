package netcheck

import (
	"context"
	"errors"
	"fmt"
	"net/url"
	"strings"
	"testing"
	"time"

	"github.com/anthnel/devdesk/internal/netproxy"
)

// DEVDESK-582, acceptance criterion 5: the TCP row dials direct. When a proxy
// is configured for the target and that direct dial fails, "unreachable" would
// mislead: the conclusion says the direct path failed and that a proxy is
// configured for this target. Nothing else about the row changes.

func refusedDial(context.Context, string) (time.Duration, error) {
	return 0, errors.New("connection refused")
}

func acceptedDial(context.Context, string) (time.Duration, error) {
	return 8 * time.Millisecond, nil
}

// proxiedBoth is the common case: one proxy for both schemes.
var proxiedBoth = perScheme(
	decided(netproxy.DecisionProxied, "HTTPS_PROXY", "http://proxy.corp:3128"),
	decided(netproxy.DecisionProxied, "HTTP_PROXY", "http://proxy.corp:3128"))

func tcpRow(t *testing.T, env Env, tg Target) Check {
	t.Helper()
	return checkNamed(t, runWith(t, env, tg), CheckTCP)
}

func TestTheDirectFailureReasonIdentifierIsStable(t *testing.T) {
	if got := string(ReasonDirectFailedProxyConfigured); got != "direct-failed-proxy-configured" {
		t.Errorf("identifier = %q, want %q: MCP serializes it", got, "direct-failed-proxy-configured")
	}
}

// Every way the Proxy row reads "configured" ends the same way on a failed
// dial, and the Proxy fact is the displayed proxy only: no variable name, no
// scheme tag. When the two schemes differ the https value wins, the same
// order the Proxy row's Summary lists them in.
func TestAFailedDirectDialWithAProxyConfiguredSaysSo(t *testing.T) {
	for _, tc := range []struct {
		name  string
		proxy func(*url.URL) netproxy.Decision
		port  int
		want  string // the Proxy fact
	}{
		{"the same proxy for both schemes", proxiedBoth, 443, "http://proxy.corp:3128"},
		{"another port is named as dialed", proxiedBoth, 8443, "http://proxy.corp:3128"},
		{"a proxy for https only", perScheme(
			decided(netproxy.DecisionProxied, "HTTPS_PROXY", "http://proxy.corp:3128"), none()),
			443, "http://proxy.corp:3128"},
		{"a proxy for http only", perScheme(
			none(), decided(netproxy.DecisionProxied, "http_proxy", "http://proxy.corp:3128")),
			443, "http://proxy.corp:3128"},
		{"two different proxys: the https one", perScheme(
			decided(netproxy.DecisionProxied, "HTTPS_PROXY", "http://a:3128"),
			decided(netproxy.DecisionProxied, "HTTP_PROXY", "http://b:8080")),
			443, "http://a:3128"},
		{"a proxy outranks an exclusion on the other scheme", perScheme(
			decided(netproxy.DecisionExcluded, "HTTPS_PROXY", ""),
			decided(netproxy.DecisionProxied, "HTTP_PROXY", "http://b:8080")),
			443, "http://b:8080"},
		{"a proxy outranks loopback on the other scheme", perScheme(
			decided(netproxy.DecisionProxied, "HTTPS_PROXY", "http://a:3128"),
			decided(netproxy.DecisionLoopback, "HTTP_PROXY", "")),
			443, "http://a:3128"},
	} {
		t.Run(tc.name, func(t *testing.T) {
			tg := Target{Host: "example.com", Port: tc.port}
			checks := runWith(t, fakeEnv{dial: refusedDial, proxy: tc.proxy}, tg)

			// The premise: the Proxy row reads "configured".
			if p := checkNamed(t, checks, CheckProxy); p.Reason != ReasonProxyConfigured {
				t.Fatalf("premise: proxy row reason = %q, want %q", p.Reason, ReasonProxyConfigured)
			}

			c := checkNamed(t, checks, CheckTCP)
			wantSummary := fmt.Sprintf(
				"Port %d does not accept direct connections; a proxy is configured for this target", tc.port)
			if c.Summary != wantSummary {
				t.Errorf("summary = %q, want %q", c.Summary, wantSummary)
			}
			if c.Verdict != Fail {
				t.Errorf("verdict = %v, want Fail: the direct dial did fail", c.Verdict)
			}
			if c.Reason != ReasonDirectFailedProxyConfigured {
				t.Errorf("reason = %q, want %q", c.Reason, ReasonDirectFailedProxyConfigured)
			}
			if got := factValue(c, "Proxy"); got != tc.want {
				t.Errorf("Proxy fact = %q, want %q", got, tc.want)
			}
			if got := factValue(c, "Address"); got != tg.Addr() {
				t.Errorf("Address fact = %q, want %q: the row still describes the dial", got, tg.Addr())
			}
			if got := factValue(c, "Error"); got != "connection refused" {
				t.Errorf("Error fact = %q, want the dial error kept", got)
			}

			// The gate is unchanged: the failed direct dial still blocks what follows.
			for _, id := range []CheckID{CheckTLSHandshake, CheckHTTP} {
				if b := checkNamed(t, checks, id); b.Verdict != NotApplicable || b.Because != CheckTCP {
					t.Errorf("%s = %v because %q, want N/A because %q", id, b.Verdict, b.Because, CheckTCP)
				}
			}
		})
	}
}

// A dial that succeeds says what it always said: the proxy does not enter it.
func TestASuccessfulDirectDialWithAProxyConfiguredIsUnchanged(t *testing.T) {
	c := tcpRow(t, fakeEnv{dial: acceptedDial, proxy: proxiedBoth}, target())

	if c.Verdict != OK {
		t.Errorf("verdict = %v, want OK", c.Verdict)
	}
	if want := "Port 443 accepted the connection in 8 ms"; c.Summary != want {
		t.Errorf("summary = %q, want %q", c.Summary, want)
	}
	if c.Reason != "" {
		t.Errorf("reason = %q, want none on a passing row", c.Reason)
	}
	if got := factValue(c, "Proxy"); got != "" {
		t.Errorf("Proxy fact = %q, want none: the dial worked", got)
	}
}

// Without a proxy that applies, a refusal is what it was before: the conclusion
// only changes when a proxy is configured for this target.
func TestAFailedDirectDialWithoutAProxyIsUnchanged(t *testing.T) {
	for _, tc := range []struct {
		name  string
		proxy func(*url.URL) netproxy.Decision
	}{
		{"no variable", nil},
		{"no variable, explicitly", perScheme(none(), none())},
		{"NO_PROXY names the target", perScheme(
			decided(netproxy.DecisionExcluded, "HTTPS_PROXY", ""),
			decided(netproxy.DecisionExcluded, "HTTP_PROXY", ""))},
		{"a loopback target", perScheme(
			decided(netproxy.DecisionLoopback, "HTTPS_PROXY", ""),
			decided(netproxy.DecisionLoopback, "HTTP_PROXY", ""))},
		{"an invalid variable", perScheme(decided(netproxy.DecisionInvalid, "HTTPS_PROXY", ""), none())},
		{"both variables invalid", perScheme(
			decided(netproxy.DecisionInvalid, "HTTPS_PROXY", ""),
			decided(netproxy.DecisionInvalid, "HTTP_PROXY", ""))},
	} {
		t.Run(tc.name, func(t *testing.T) {
			c := tcpRow(t, fakeEnv{dial: refusedDial, proxy: tc.proxy}, target())

			if c.Verdict != Fail {
				t.Errorf("verdict = %v, want Fail", c.Verdict)
			}
			if want := "Port 443 does not accept connections"; c.Summary != want {
				t.Errorf("summary = %q, want %q", c.Summary, want)
			}
			if c.Reason != "" {
				t.Errorf("reason = %q, want none: no proxy applies", c.Reason)
			}
			if got := factValue(c, "Proxy"); got != "" {
				t.Errorf("Proxy fact = %q, want none: no proxy applies", got)
			}
			if got := factValue(c, "Error"); got != "connection refused" {
				t.Errorf("Error fact = %q, want the dial error", got)
			}
		})
	}
}

// The help for the new conclusion says the direct path failed and that a proxy
// is configured, and offers something to do that is not the generic advice
// about a service that is down.
func TestTheDirectFailureWithAProxyIsExplained(t *testing.T) {
	c := tcpRow(t, fakeEnv{dial: refusedDial, proxy: proxiedBoth}, target())
	e := Explain(c)

	wantMeans := "The direct path to this port is closed, and that is all this run tests. A proxy is " +
		"configured for this target: behind a mandatory proxy a closed direct path is expected, " +
		"and it says nothing about whether the target is up."
	if e.Means != wantMeans {
		t.Errorf("means = %q, want %q", e.Means, wantMeans)
	}
	for _, word := range []string{"direct path", "proxy"} {
		if !strings.Contains(e.Means, word) {
			t.Errorf("means %q does not say %q", e.Means, word)
		}
	}

	wantDo := "If this target is meant to be reached through the proxy, check it from a client that " +
		"uses it, such as an HTTPS monitor in the Status view. If it is meant to be reached " +
		"directly, add it to NO_PROXY and look for what drops the connection."
	if e.Do != wantDo {
		t.Errorf("do = %q, want %q", e.Do, wantDo)
	}

	generic := Explain(tcpRow(t, fakeEnv{dial: refusedDial}, target()))
	if strings.TrimSpace(e.Do) == "" || e.Do == generic.Do {
		t.Errorf("do = %q: it must be non-empty and differ from the generic TCP Fail advice", e.Do)
	}
	if e.Means == generic.Means {
		t.Error("the explanation is the generic TCP Fail one: it does not mention the proxy")
	}
	if e.Observed != c.Summary {
		t.Errorf("observed = %q, want the summary %q", e.Observed, c.Summary)
	}
}

// The guidance row is a row of the table, not a branch in code: it is reached
// by a battery scenario (TestNoGuidanceRowIsUnreachable fails otherwise), says
// something about the failed state, and leaves the generic row for the other
// failures.
func TestTheDirectFailureGuidanceRowIsInTheTableAndReachable(t *testing.T) {
	var rows int
	for _, g := range explanations {
		if g.check == CheckTCP && g.verdict == Fail && g.reason == ReasonDirectFailedProxyConfigured {
			rows++
			if strings.TrimSpace(g.do) == "" {
				t.Error("the guidance row has nothing to do about the failure")
			}
		}
	}
	if rows != 1 {
		t.Fatalf("explanations has %d rows for tcp/fail/%s, want 1", rows, ReasonDirectFailedProxyConfigured)
	}

	var reached bool
	for _, c := range produced(t) {
		if c.ID == CheckTCP && c.Verdict == Fail && c.Reason == ReasonDirectFailedProxyConfigured {
			reached = true
		}
	}
	if !reached {
		t.Error("no scenario of the battery produces the TCP direct-failed-proxy-configured row")
	}

	// The generic row still serves the failures with no proxy.
	generic := Explain(Check{ID: CheckTCP, Verdict: Fail, Summary: "x"})
	if strings.Contains(generic.Means, "proxy") {
		t.Errorf("the generic TCP Fail explanation mentions a proxy: %q", generic.Means)
	}
}

// The proxy is shown redacted by netproxy, and the TCP row must not put a
// credential back: no summary, fact or explanation carries "@" or a secret.
func TestTheDirectFailureRowCarriesNoCredential(t *testing.T) {
	// What netproxy hands over for http://user:pass@proxy.corp:3128 is scheme://host:port.
	redacted := perScheme(
		decided(netproxy.DecisionProxied, "HTTPS_PROXY", "http://proxy.corp:3128"),
		decided(netproxy.DecisionProxied, "HTTP_PROXY", "http://proxy.corp:3128"))

	c := tcpRow(t, fakeEnv{dial: refusedDial, proxy: redacted}, target())
	if c.Reason != ReasonDirectFailedProxyConfigured {
		t.Fatalf("premise: reason = %q, want %q", c.Reason, ReasonDirectFailedProxyConfigured)
	}
	e := Explain(c)

	texts := []string{c.Summary, e.Observed, e.Means, e.Do}
	for _, f := range c.Facts {
		texts = append(texts, f.Key, f.Value)
	}
	for _, s := range texts {
		for _, bad := range []string{"@", secretUser, secretPass} {
			if strings.Contains(s, bad) {
				t.Errorf("%q contains %q", s, bad)
			}
		}
	}
}

// An invalid variable for one scheme and a valid proxy for the other: the Proxy
// row merges to Invalid, but a proxy is configured all the same, so a failed
// direct dial still says so. The TCP stage reads the row's field, not its text.
func TestAFailedDirectDialWithOneInvalidAndOneProxiedSchemeNamesTheProxy(t *testing.T) {
	proxy := perScheme(
		decided(netproxy.DecisionInvalid, "HTTPS_PROXY", ""),
		decided(netproxy.DecisionProxied, "HTTP_PROXY", "http://proxy.corp:3128"))
	checks := runWith(t, fakeEnv{dial: refusedDial, proxy: proxy}, target())

	if p := checkNamed(t, checks, CheckProxy); p.Reason != ReasonProxyInvalid {
		t.Fatalf("premise: proxy row reason = %q, want %q", p.Reason, ReasonProxyInvalid)
	}
	c := checkNamed(t, checks, CheckTCP)
	if c.Reason != ReasonDirectFailedProxyConfigured {
		t.Errorf("reason = %q, want %q", c.Reason, ReasonDirectFailedProxyConfigured)
	}
	if want := "Port 443 does not accept direct connections; a proxy is configured for this target"; c.Summary != want {
		t.Errorf("summary = %q, want %q", c.Summary, want)
	}
	if got := factValue(c, "Proxy"); got != "http://proxy.corp:3128" {
		t.Errorf("Proxy fact = %q, want the proxied scheme's proxy", got)
	}
}
