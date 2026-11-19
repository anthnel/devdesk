package netcheck

import (
	"context"
	"errors"
	"net"
	"net/url"
	"reflect"
	"slices"
	"strings"
	"testing"

	"github.com/anthnel/devdesk/internal/netproxy"
)

// DEVDESK-596: when the direct lookup of a named target fails and the Proxy row
// says a proxy applies, the DNS row is rewritten in place once the Proxy stage
// has run. Behind a mandatory proxy the name is often resolved by the proxy, so
// "does not resolve" alone would mislead. The Proxy stage keeps its place in the
// pipeline; it concludes on the earlier DNS row through stage.conclude.

const (
	resolveGenericSummary = "example.com does not resolve"
	resolveProxySummary   = "example.com does not resolve directly; a proxy is configured for this target"
	proxyDisplay          = "http://proxy.corp:3128"
)

func resolveFailing(err error) func(context.Context, string, string) ([]net.IP, error) {
	return func(context.Context, string, string) ([]net.IP, error) { return nil, err }
}

func resolveEmpty(context.Context, string, string) ([]net.IP, error) { return nil, nil }

var errNoSuchHost = errors.New("no such host")

// misbehavingErr is the DNS error that keeps its own reason.
func misbehavingErr() *net.DNSError {
	return &net.DNSError{Err: "server misbehaving", Name: "example.com", IsTemporary: true}
}

// resolveRowOf returns the DNS row of a finished run.
func resolveRowOf(t *testing.T, res Results) Check {
	t.Helper()
	return checkNamed(t, res.All(), CheckResolve)
}

func runAll(t *testing.T, env Env, tg Target) Results {
	t.Helper()
	res, err := Run(context.Background(), tg, env, DefaultSettings())
	if err != nil {
		t.Fatalf("Run: %v", err)
	}
	return res
}

// chain runs every stage through RunStep, the way a view does.
func chain(env Env, tg Target) Results {
	var res Results
	for _, id := range Steps() {
		res = RunStep(context.Background(), tg, env, DefaultSettings(), id, res)
	}
	return res
}

func TestTheDirectResolveReasonIdentifierIsStable(t *testing.T) {
	if got := string(ReasonDirectResolveFailedProxyConfigured); got != "direct-resolve-failed-proxy-configured" {
		t.Errorf("identifier = %q, want %q: MCP serializes it", got, "direct-resolve-failed-proxy-configured")
	}
}

// Every way the Proxy row reads "configured" rewrites a failed lookup the same
// way. The Proxy fact is the displayed proxy only (https first), the Resolver and
// Error facts are kept, and the verdict stays Fail: the direct lookup did fail.
func TestAFailedResolutionWithAProxyConfiguredSaysSo(t *testing.T) {
	for _, tc := range []struct {
		name     string
		resolve  func(context.Context, string, string) ([]net.IP, error)
		resolver string
		proxy    func(*url.URL) netproxy.Decision
		want     []Fact
	}{
		{"an unknown name", resolveFailing(errNoSuchHost), "", proxiedBoth,
			[]Fact{{"Resolver", "System resolver"}, {"Proxy", proxyDisplay}, {"Error", "no such host"}}},
		{"an empty answer carries no error fact", resolveEmpty, "", proxiedBoth,
			[]Fact{{"Resolver", "System resolver"}, {"Proxy", proxyDisplay}}},
		{"a proxy for https only", resolveFailing(errNoSuchHost), "", perScheme(
			decided(netproxy.DecisionProxied, "HTTPS_PROXY", proxyDisplay), none()),
			[]Fact{{"Resolver", "System resolver"}, {"Proxy", proxyDisplay}, {"Error", "no such host"}}},
		{"a proxy for http only", resolveFailing(errNoSuchHost), "", perScheme(
			none(), decided(netproxy.DecisionProxied, "http_proxy", proxyDisplay)),
			[]Fact{{"Resolver", "System resolver"}, {"Proxy", proxyDisplay}, {"Error", "no such host"}}},
		{"two different proxys: the https one", resolveFailing(errNoSuchHost), "", perScheme(
			decided(netproxy.DecisionProxied, "HTTPS_PROXY", "http://a:3128"),
			decided(netproxy.DecisionProxied, "HTTP_PROXY", "http://b:8080")),
			[]Fact{{"Resolver", "System resolver"}, {"Proxy", "http://a:3128"}, {"Error", "no such host"}}},
		{"a proxy outranks an exclusion on the other scheme", resolveFailing(errNoSuchHost), "", perScheme(
			decided(netproxy.DecisionExcluded, "HTTPS_PROXY", ""),
			decided(netproxy.DecisionProxied, "HTTP_PROXY", "http://b:8080")),
			[]Fact{{"Resolver", "System resolver"}, {"Proxy", "http://b:8080"}, {"Error", "no such host"}}},
		{"a proxy outranks loopback on the other scheme", resolveFailing(errNoSuchHost), "", perScheme(
			decided(netproxy.DecisionProxied, "HTTPS_PROXY", "http://a:3128"),
			decided(netproxy.DecisionLoopback, "HTTP_PROXY", "")),
			[]Fact{{"Resolver", "System resolver"}, {"Proxy", "http://a:3128"}, {"Error", "no such host"}}},
		{"an invalid variable and a proxied scheme still name the proxy", resolveFailing(errNoSuchHost), "", perScheme(
			decided(netproxy.DecisionInvalid, "HTTPS_PROXY", ""),
			decided(netproxy.DecisionProxied, "HTTP_PROXY", proxyDisplay)),
			[]Fact{{"Resolver", "System resolver"}, {"Proxy", proxyDisplay}, {"Error", "no such host"}}},
		{"a named resolver is kept", resolveFailing(errNoSuchHost), "9.9.9.9", proxiedBoth,
			[]Fact{{"Resolver", "9.9.9.9"}, {"Proxy", proxyDisplay}, {"Error", "no such host"}}},
	} {
		t.Run(tc.name, func(t *testing.T) {
			tg := Target{Host: "example.com", Port: 443, Resolver: tc.resolver}
			res := runAll(t, fakeEnv{resolve: tc.resolve, proxy: tc.proxy}, tg)

			// The premise: the Proxy row reads that a proxy applies.
			if p := checkNamed(t, res.All(), CheckProxy); p.Proxy == "" {
				t.Fatalf("premise: the proxy row names no proxy (reason %q)", p.Reason)
			}

			c := resolveRowOf(t, res)
			if c.Summary != resolveProxySummary {
				t.Errorf("summary = %q, want %q", c.Summary, resolveProxySummary)
			}
			if c.Verdict != Fail {
				t.Errorf("verdict = %v, want Fail: the direct lookup did fail", c.Verdict)
			}
			if c.Reason != ReasonDirectResolveFailedProxyConfigured {
				t.Errorf("reason = %q, want %q", c.Reason, ReasonDirectResolveFailedProxyConfigured)
			}
			if !slices.Equal(c.Facts, tc.want) {
				t.Errorf("facts = %v, want %v", c.Facts, tc.want)
			}
			if c.ID != CheckResolve || c.Stage != StageResolve || c.Title != "DNS resolution" {
				t.Errorf("identity = %s/%s/%q, want the row's own", c.ID, c.Stage, c.Title)
			}
		})
	}
}

// Without a proxy that applies, the DNS row is what it was before: strictly the
// row of a run in which no proxy variable exists.
func TestAFailedResolutionWithoutAProxyIsUnchanged(t *testing.T) {
	failures := []struct {
		name    string
		resolve func(context.Context, string, string) ([]net.IP, error)
	}{
		{"an unknown name", resolveFailing(errNoSuchHost)},
		{"an empty answer", resolveEmpty},
		{"a misbehaving resolver", resolveFailing(misbehavingErr())},
	}
	for _, tc := range []struct {
		name  string
		proxy func(*url.URL) netproxy.Decision
	}{
		{"no variable", perScheme(none(), none())},
		{"NO_PROXY names the target on both schemes", perScheme(
			decided(netproxy.DecisionExcluded, "HTTPS_PROXY", ""),
			decided(netproxy.DecisionExcluded, "HTTP_PROXY", ""))},
		{"a loopback target", perScheme(
			decided(netproxy.DecisionLoopback, "HTTPS_PROXY", ""),
			decided(netproxy.DecisionLoopback, "HTTP_PROXY", ""))},
		{"an invalid variable on https only", perScheme(decided(netproxy.DecisionInvalid, "HTTPS_PROXY", ""), none())},
		{"an invalid variable on http only", perScheme(none(), decided(netproxy.DecisionInvalid, "HTTP_PROXY", ""))},
		{"both variables invalid", perScheme(
			decided(netproxy.DecisionInvalid, "HTTPS_PROXY", ""),
			decided(netproxy.DecisionInvalid, "HTTP_PROXY", ""))},
	} {
		for _, f := range failures {
			t.Run(tc.name+", "+f.name, func(t *testing.T) {
				reference := resolveRowOf(t, runAll(t, fakeEnv{resolve: f.resolve}, target()))
				got := resolveRowOf(t, runAll(t, fakeEnv{resolve: f.resolve, proxy: tc.proxy}, target()))

				if !reflect.DeepEqual(got, reference) {
					t.Errorf("row = %+v, want the row of a run without a proxy: %+v", got, reference)
				}
				if got.Reason == ReasonDirectResolveFailedProxyConfigured || factValue(got, "Proxy") != "" {
					t.Errorf("row %+v mentions a proxy that does not apply", got)
				}
			})
		}
	}
}

// Rewriting the DNS row changes nothing else: same checks in the same order, the
// Proxy row says what it said, and everything below the failed resolution is
// still blocked by it.
func TestTheRestOfTheRunIsUnchangedBehindAProxy(t *testing.T) {
	plain := runAll(t, fakeEnv{resolve: resolveFailing(errNoSuchHost)}, target())
	behind := runAll(t, fakeEnv{resolve: resolveFailing(errNoSuchHost), proxy: proxiedBoth}, target())

	if got, want := idsOf(behind.All()), idsOf(plain.All()); !slices.Equal(got, want) {
		t.Fatalf("checks = %v, want %v, the same order as without a proxy", got, want)
	}
	if got := resolveRowOf(t, behind); got.Reason != ReasonDirectResolveFailedProxyConfigured {
		t.Fatalf("premise: the DNS row reason = %q, want %q", got.Reason, ReasonDirectResolveFailedProxyConfigured)
	}

	p := checkNamed(t, behind.All(), CheckProxy)
	if p.Verdict != Info || p.Summary != "Configured for this target: "+proxyDisplay {
		t.Errorf("proxy row = %v %q, want INFO and the configured summary", p.Verdict, p.Summary)
	}
	if got := factValue(p, "HTTPS"); got != proxyDisplay+" (HTTPS_PROXY)" {
		t.Errorf("HTTPS fact = %q", got)
	}
	if got := factValue(p, "HTTP"); got != proxyDisplay+" (HTTP_PROXY)" {
		t.Errorf("HTTP fact = %q", got)
	}

	for _, id := range []CheckID{
		CheckRoute, CheckICMP, CheckTCP,
		CheckTLSHandshake, CheckTLSChain, CheckTLSHostname, CheckTLSExpiry, CheckTLSVersion, CheckHTTP,
	} {
		c := checkNamed(t, behind.All(), id)
		if c.Verdict != NotApplicable || c.Because != CheckResolve {
			t.Errorf("%s = %v because %q, want N/A because %q", id, c.Verdict, c.Because, CheckResolve)
		}
		if want := "Skipped — DNS resolution did not succeed"; c.Summary != want {
			t.Errorf("%s summary = %q, want %q", id, c.Summary, want)
		}
		if ref := checkNamed(t, plain.All(), id); !reflect.DeepEqual(c, ref) {
			t.Errorf("%s = %+v, want the row of a run without a proxy: %+v", id, c, ref)
		}
	}

	rev := checkNamed(t, behind.All(), CheckReverseDNS)
	if rev.Verdict != NotApplicable || rev.Because != "" || rev.Summary != "Target is a name, not an address" {
		t.Errorf("reverse DNS = %v because %q %q, want N/A with its own summary", rev.Verdict, rev.Because, rev.Summary)
	}

	if got := Summarize(behind.All()); got != Fail {
		t.Errorf("Summarize = %v, want Fail", got)
	}
}

// A lookup that worked has nothing to conclude, proxy or not.
func TestASuccessfulResolutionBehindAProxyIsUnchanged(t *testing.T) {
	plain := runAll(t, fakeEnv{}, target())
	behind := runAll(t, fakeEnv{proxy: proxiedBoth}, target())

	for _, id := range []CheckID{CheckResolve, CheckReverseDNS} {
		got, want := checkNamed(t, behind.All(), id), checkNamed(t, plain.All(), id)
		if !reflect.DeepEqual(got, want) {
			t.Errorf("%s = %+v, want %+v", id, got, want)
		}
	}
	if c := resolveRowOf(t, behind); c.Verdict != OK || c.Reason != "" {
		t.Errorf("DNS row = %v (reason %q), want OK with no reason", c.Verdict, c.Reason)
	}
}

// A literal address is not resolved at all: the row says so, proxy or not.
func TestALiteralAddressBehindAProxyIsUnchanged(t *testing.T) {
	tg := Target{Host: "10.2.3.4", Port: 443}
	plain := runAll(t, fakeEnv{}, tg)
	behind := runAll(t, fakeEnv{proxy: proxiedBoth}, tg)

	for _, id := range []CheckID{CheckResolve, CheckReverseDNS} {
		got, want := checkNamed(t, behind.All(), id), checkNamed(t, plain.All(), id)
		if !reflect.DeepEqual(got, want) {
			t.Errorf("%s = %+v, want %+v", id, got, want)
		}
	}
	if c := resolveRowOf(t, behind); c.Verdict != NotApplicable || c.Reason != "" {
		t.Errorf("DNS row = %v (reason %q), want N/A with no reason", c.Verdict, c.Reason)
	}
}

// "Server misbehaving" keeps its Summary and its Reason, which say something
// more precise than the proxy does; the proxy is added as one fact after the
// resolver, so the facts read Resolver, Proxy, Error, Temporary.
func TestAMisbehavingResolverKeepsItsReasonBehindAProxy(t *testing.T) {
	err := misbehavingErr()
	res := runAll(t, fakeEnv{resolve: resolveFailing(err), proxy: proxiedBoth}, target())
	c := resolveRowOf(t, res)

	if c.Reason != ReasonServerMisbehaving {
		t.Errorf("reason = %q, want %q", c.Reason, ReasonServerMisbehaving)
	}
	if c.Summary != resolveGenericSummary {
		t.Errorf("summary = %q, want %q unchanged", c.Summary, resolveGenericSummary)
	}
	if c.Verdict != Fail {
		t.Errorf("verdict = %v, want Fail", c.Verdict)
	}
	want := []Fact{
		{"Resolver", "System resolver"},
		{"Proxy", proxyDisplay},
		{"Error", err.Error()},
		{"Temporary", "Yes — a SERVFAIL response, usually transient"},
	}
	if !slices.Equal(c.Facts, want) {
		t.Errorf("facts = %v, want %v", c.Facts, want)
	}
}

// Run and a chain of RunStep give the same rows, and the conclusion arrives with
// the Proxy stage and not before: after the resolve stage alone the row is the
// generic one. The DNS row never changes position.
func TestRunStepChainMatchesRunBehindAProxy(t *testing.T) {
	env := fakeEnv{resolve: resolveFailing(errNoSuchHost), proxy: proxiedBoth}
	whole := runAll(t, env, target())
	piecewise := chain(env, target())

	if !reflect.DeepEqual(piecewise.All(), whole.All()) {
		t.Errorf("chain = %+v\nRun   = %+v", piecewise.All(), whole.All())
	}

	afterResolve := RunStep(context.Background(), target(), env, DefaultSettings(), StageResolve, Results{})
	if c := resolveRowOf(t, afterResolve); c.Summary != resolveGenericSummary || c.Reason != "" {
		t.Errorf("after the resolve stage: %q (reason %q), want the generic conclusion", c.Summary, c.Reason)
	}

	afterProxy := RunStep(context.Background(), target(), env, DefaultSettings(), StageProxy, afterResolve)
	if c := resolveRowOf(t, afterProxy); c.Summary != resolveProxySummary || c.Reason != ReasonDirectResolveFailedProxyConfigured {
		t.Errorf("after the proxy stage: %q (reason %q), want the rewritten conclusion", c.Summary, c.Reason)
	}
	if ids := idsOf(afterProxy.All()); !slices.Equal(ids, []CheckID{CheckResolve, CheckReverseDNS, CheckProxy}) {
		t.Errorf("checks = %v, want the DNS row still first, in place", ids)
	}
	if i, ok := afterProxy.index[CheckResolve]; !ok || i != 0 {
		t.Errorf("the DNS row is at index %d (found %v), want 0", i, ok)
	}
}

// The conclusion builds a new slice of facts. The previous Results shares
// backing arrays with the copy RunStep works on (clone is shallow), and a caller
// may hold it across a goroutine boundary: an append into a slice with spare
// capacity would write into it.
func TestTheConclusionDoesNotWriteIntoPrior(t *testing.T) {
	env := fakeEnv{resolve: resolveFailing(errNoSuchHost), proxy: proxiedBoth}
	generic := RunStep(context.Background(), target(), env, DefaultSettings(), StageResolve, Results{})

	checks := slices.Clone(generic.All())
	spare := make([]Fact, 2, 8)
	copy(spare, checks[0].Facts)
	checks[0].Facts = spare
	prior := ResultsOf(checks...)

	wantFacts := slices.Clone(spare)
	wantSummary, wantReason := checks[0].Summary, checks[0].Reason

	next := RunStep(context.Background(), target(), env, DefaultSettings(), StageProxy, prior)

	// The premise: the step did rewrite its own copy.
	if c := resolveRowOf(t, next); c.Reason != ReasonDirectResolveFailedProxyConfigured {
		t.Fatalf("premise: the new row reason = %q, want %q", c.Reason, ReasonDirectResolveFailedProxyConfigured)
	}

	got := resolveRowOf(t, prior)
	if !slices.Equal(got.Facts, wantFacts) || got.Summary != wantSummary || got.Reason != wantReason {
		t.Errorf("prior row = %q (reason %q) %v, want %q (reason %q) %v untouched",
			got.Summary, got.Reason, got.Facts, wantSummary, wantReason, wantFacts)
	}
	if spare := got.Facts[:cap(got.Facts)]; spare[2] != (Fact{}) {
		t.Errorf("the spare capacity of the prior facts was written: %v", spare)
	}
}

// Running the Proxy stage again does not stack a second Proxy fact, in either
// shape of the conclusion.
func TestTheConclusionIsIdempotent(t *testing.T) {
	for _, tc := range []struct {
		name string
		err  error
	}{
		{"the generic rewrite", errNoSuchHost},
		{"a misbehaving resolver", misbehavingErr()},
	} {
		t.Run(tc.name, func(t *testing.T) {
			env := fakeEnv{resolve: resolveFailing(tc.err), proxy: proxiedBoth}
			first := chain(env, target())
			second := RunStep(context.Background(), target(), env, DefaultSettings(), StageProxy, first)

			c := resolveRowOf(t, second)
			var proxies int
			for _, f := range c.Facts {
				if f.Key == "Proxy" {
					proxies++
				}
			}
			if proxies != 1 {
				t.Errorf("DNS row has %d Proxy facts after two runs, want 1: %v", proxies, c.Facts)
			}
			if once := resolveRowOf(t, first); !reflect.DeepEqual(c, once) {
				t.Errorf("second run = %+v, want the first run's row %+v", c, once)
			}
		})
	}
}

// A cancelled Proxy stage did not run, so it concludes nothing: the DNS row stays
// the generic one. The control shows the same step with a live context does
// rewrite it.
func TestACancelledProxyStageLeavesTheGenericResolveConclusion(t *testing.T) {
	env := fakeEnv{resolve: resolveFailing(errNoSuchHost), proxy: proxiedBoth}
	generic := RunStep(context.Background(), target(), env, DefaultSettings(), StageResolve, Results{})

	live := RunStep(context.Background(), target(), env, DefaultSettings(), StageProxy, generic)
	if c := resolveRowOf(t, live); c.Reason != ReasonDirectResolveFailedProxyConfigured {
		t.Fatalf("control: a live proxy stage left reason %q, want %q", c.Reason, ReasonDirectResolveFailedProxyConfigured)
	}

	ctx, cancel := context.WithCancel(context.Background())
	cancel()
	res := RunStep(ctx, target(), env, DefaultSettings(), StageProxy, generic)

	c := resolveRowOf(t, res)
	if c.Summary != resolveGenericSummary || c.Reason != "" || factValue(c, "Proxy") != "" {
		t.Errorf("DNS row = %q (reason %q) %v, want the generic conclusion", c.Summary, c.Reason, c.Facts)
	}
	if p := checkNamed(t, res.All(), CheckProxy); p.Verdict != Unknown {
		t.Errorf("proxy row = %v, want the Unknown placeholder", p.Verdict)
	}
}

// Without a Proxy row nothing says a proxy applies, and the DNS phase never reads
// the environment for it: the resolve stage alone leaves the generic conclusion
// and ProxyFor is not called.
func TestAResolveRowWithoutAProxyRowKeepsTheGenericConclusion(t *testing.T) {
	var asked int
	env := fakeEnv{
		resolve: resolveFailing(errNoSuchHost),
		proxy: func(u *url.URL) netproxy.Decision {
			asked++
			return proxiedBoth(u)
		},
	}
	res := RunStep(context.Background(), target(), env, DefaultSettings(), StageResolve, Results{})

	c := resolveRowOf(t, res)
	if c.Summary != resolveGenericSummary || c.Reason != "" || factValue(c, "Proxy") != "" {
		t.Errorf("DNS row = %q (reason %q) %v, want the generic conclusion", c.Summary, c.Reason, c.Facts)
	}
	if asked != 0 {
		t.Errorf("ProxyFor was called %d times by the DNS phase, want 0", asked)
	}
}

// The conclusion acts only when all its premises hold. Called directly, it
// leaves the results alone when the DNS row is missing or did not fail, or when
// the Proxy row names no proxy; the control shows it rewrites when they hold.
func TestTheConclusionNeedsItsPremises(t *testing.T) {
	dns := func(v Verdict) Check {
		return Check{ID: CheckResolve, Stage: StageResolve, Title: "DNS resolution", Verdict: v,
			Summary: resolveGenericSummary, Facts: []Fact{{"Resolver", "System resolver"}, {"Error", "no such host"}}}
	}
	proxied := Check{ID: CheckProxy, Stage: StageProxy, Verdict: Info, Reason: ReasonProxyConfigured, Proxy: proxyDisplay}
	unproxied := Check{ID: CheckProxy, Stage: StageProxy, Verdict: NotApplicable, Reason: ReasonProxyNone}

	for _, tc := range []struct {
		name   string
		checks []Check
		want   bool // rewritten
	}{
		{"every premise holds", []Check{dns(Fail), proxied}, true},
		{"no DNS row", []Check{proxied}, false},
		{"a DNS row that passed", []Check{dns(OK), proxied}, false},
		{"a DNS row that does not apply", []Check{dns(NotApplicable), proxied}, false},
		{"no proxy row", []Check{dns(Fail)}, false},
		{"a proxy row without a proxy", []Check{dns(Fail), unproxied}, false},
	} {
		t.Run(tc.name, func(t *testing.T) {
			res := ResultsOf(tc.checks...)
			before := slices.Clone(res.All())

			concludeResolveBehindProxy(target(), &res)

			changed := !reflect.DeepEqual(res.All(), before)
			if changed != tc.want {
				t.Fatalf("changed = %v, want %v; rows now %+v", changed, tc.want, res.All())
			}
			if tc.want {
				if c := resolveRowOf(t, res); c.Reason != ReasonDirectResolveFailedProxyConfigured {
					t.Errorf("reason = %q, want %q", c.Reason, ReasonDirectResolveFailedProxyConfigured)
				}
			}
		})
	}
}

// DEVDESK-596, Explain: the rewritten DNS row has a guidance row of its own. The
// generic DNS Fail advice says the name is wrong or the zone does not publish
// it, which is the wrong lead behind a mandatory proxy, where the proxy resolves
// the name and this machine often cannot.
const (
	resolveProxyMeans = "Every check below needs an address, so none of them ran. Clients that go " +
		"through the configured proxy usually hand it the name and let it resolve it, so behind " +
		"a mandatory proxy a name this machine cannot resolve is expected and says nothing about " +
		"whether the target exists or is up."
	resolveProxyDo = "If this target is meant to be reached through the proxy, check it from a " +
		"client that uses it, such as an HTTPS monitor in the Status view. If it is meant to be " +
		"reached directly, add it to NO_PROXY, then check the spelling and try the name against " +
		"another resolver."
)

func TestTheFailedResolutionWithAProxyIsExplained(t *testing.T) {
	c := resolveRowOf(t, runAll(t, fakeEnv{resolve: resolveFailing(errNoSuchHost), proxy: proxiedBoth}, target()))
	if c.Reason != ReasonDirectResolveFailedProxyConfigured {
		t.Fatalf("premise: reason = %q, want %q", c.Reason, ReasonDirectResolveFailedProxyConfigured)
	}

	e := Explain(c)
	if e.Means != resolveProxyMeans {
		t.Errorf("means = %q, want %q", e.Means, resolveProxyMeans)
	}
	if e.Do != resolveProxyDo {
		t.Errorf("do = %q, want %q", e.Do, resolveProxyDo)
	}
	if e.Observed != c.Summary || e.Observed != resolveProxySummary {
		t.Errorf("observed = %q, want the summary %q", e.Observed, resolveProxySummary)
	}

	// It is not the generic DNS Fail guidance, which the run without a proxy gets.
	generic := Explain(resolveRowOf(t, runAll(t, fakeEnv{resolve: resolveFailing(errNoSuchHost)}, target())))
	if e.Means == generic.Means {
		t.Error("the means is the generic DNS Fail one: it does not mention the proxy")
	}
	if e.Do == generic.Do {
		t.Error("the do is the generic DNS Fail one: it does not mention the proxy")
	}
}

// The guidance row is a row of the table, not a branch in code: exactly one,
// with something to do, reached by a battery scenario (TestNoGuidanceRowIsUnreachable
// fails otherwise); the generic row stays for the failures with no proxy.
func TestTheResolveProxyGuidanceRowIsInTheTableAndReachable(t *testing.T) {
	var rows int
	for _, g := range explanations {
		if g.check == CheckResolve && g.verdict == Fail && g.reason == ReasonDirectResolveFailedProxyConfigured {
			rows++
			if strings.TrimSpace(g.do) == "" {
				t.Error("the guidance row has nothing to do about the failure")
			}
		}
	}
	if rows != 1 {
		t.Fatalf("explanations has %d rows for resolve/fail/%s, want 1", rows, ReasonDirectResolveFailedProxyConfigured)
	}

	var reached bool
	for _, c := range produced(t) {
		if c.ID == CheckResolve && c.Verdict == Fail && c.Reason == ReasonDirectResolveFailedProxyConfigured {
			reached = true
		}
	}
	if !reached {
		t.Error("no scenario of the battery produces the DNS direct-resolve-failed-proxy-configured row")
	}

	generic := Explain(Check{ID: CheckResolve, Verdict: Fail, Summary: "x"})
	if strings.Contains(strings.ToLower(generic.Means), "proxy") {
		t.Errorf("the generic DNS Fail explanation mentions a proxy: %q", generic.Means)
	}
}

// The proxy reaches the row through netproxy.Display, which drops userinfo; the
// DNS row and its explanation must not put a credential back: no summary, fact
// or explanation carries "@" or a secret.
func TestTheFailedResolutionRowCarriesNoCredential(t *testing.T) {
	u, err := url.Parse("http://" + secretUser + ":" + secretPass + "@proxy.corp:3128")
	if err != nil {
		t.Fatal(err)
	}
	shown := netproxy.Display(u)
	proxy := perScheme(
		decided(netproxy.DecisionProxied, "HTTPS_PROXY", shown),
		decided(netproxy.DecisionProxied, "HTTP_PROXY", shown))

	c := resolveRowOf(t, runAll(t, fakeEnv{resolve: resolveFailing(errNoSuchHost), proxy: proxy}, target()))
	if c.Reason != ReasonDirectResolveFailedProxyConfigured {
		t.Fatalf("premise: reason = %q, want %q", c.Reason, ReasonDirectResolveFailedProxyConfigured)
	}
	if got := factValue(c, "Proxy"); got != proxyDisplay {
		t.Fatalf("premise: Proxy fact = %q, want %q", got, proxyDisplay)
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

// A misbehaving resolver keeps the guidance it had: it says something more
// precise than the proxy does (the resolver answered, with something unusable).
func TestAMisbehavingResolverBehindAProxyKeepsItsGuidance(t *testing.T) {
	c := resolveRowOf(t, runAll(t, fakeEnv{resolve: resolveFailing(misbehavingErr()), proxy: proxiedBoth}, target()))
	if c.Reason != ReasonServerMisbehaving {
		t.Fatalf("premise: reason = %q, want %q", c.Reason, ReasonServerMisbehaving)
	}

	g, ok := guidanceFor(CheckResolve, Fail, ReasonServerMisbehaving)
	if !ok || g.reason != ReasonServerMisbehaving {
		t.Fatalf("no guidance row for resolve/fail/%s", ReasonServerMisbehaving)
	}
	e := Explain(c)
	if e.Means != g.means || e.Do != g.do {
		t.Errorf("explanation = %q / %q, want the server-misbehaving row %q / %q", e.Means, e.Do, g.means, g.do)
	}
	if e.Means == resolveProxyMeans || e.Do == resolveProxyDo {
		t.Error("the misbehaving resolver got the proxy guidance, which is less precise")
	}
	if e.Observed != c.Summary {
		t.Errorf("observed = %q, want the summary %q", e.Observed, c.Summary)
	}

	noProxy := Explain(resolveRowOf(t, runAll(t, fakeEnv{resolve: resolveFailing(misbehavingErr())}, target())))
	if e.Means != noProxy.Means || e.Do != noProxy.Do {
		t.Errorf("behind a proxy: %q / %q, without: %q / %q, want the same guidance", e.Means, e.Do, noProxy.Means, noProxy.Do)
	}
}
