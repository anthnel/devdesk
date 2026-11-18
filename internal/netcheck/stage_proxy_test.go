package netcheck

import (
	"context"
	"crypto/tls"
	"errors"
	"net"
	"net/url"
	"slices"
	"strings"
	"testing"
	"time"

	"github.com/anthnel/devdesk/internal/netproxy"
)

// DEVDESK-582: the Proxy row says whether a proxy variable applies to the
// target. It reads the environment, sends nothing, and the checks around it stay
// direct.

// Env must carry the proxy question: a fake that answers it is not enough,
// the stage reaches the environment through the interface.
var _ interface {
	ProxyFor(u *url.URL) netproxy.Decision
} = Env(nil)

const (
	// Distinctive tokens: no output of the row may ever carry them.
	secretUser = "s3cretUser"
	secretPass = "s3cretPass"
)

func decided(kind netproxy.DecisionKind, variable, proxy string) netproxy.Decision {
	return netproxy.Decision{Kind: kind, Variable: variable, Proxy: proxy}
}

// perScheme answers the proxy question with one decision per scheme, like the
// real environment does with HTTPS_PROXY and HTTP_PROXY.
func perScheme(https, http netproxy.Decision) func(*url.URL) netproxy.Decision {
	return func(u *url.URL) netproxy.Decision {
		switch u.Scheme {
		case "https":
			return https
		case "http":
			return http
		}
		return netproxy.Decision{}
	}
}

func none() netproxy.Decision { return netproxy.Decision{Kind: netproxy.DecisionNone} }

// proxyRow runs the proxy stage alone and returns its one check.
func proxyRow(t *testing.T, env Env, tg Target) Check {
	t.Helper()
	var prior Results
	checks := runProxy(context.Background(), tg, env, DefaultSettings(), &prior)
	if len(checks) != 1 {
		t.Fatalf("runProxy returned %d checks, want exactly the one it declares", len(checks))
	}
	return checks[0]
}

// proxyCases is every state the row can be in, with the exact wording. The
// merge priority is Invalid > Proxied > Excluded > Loopback > None.
var proxyCases = []struct {
	name        string
	https, http netproxy.Decision
	verdict     Verdict
	reason      Reason
	summary     string
	httpsFact   string
	httpFact    string
}{
	{
		name:  "no variable for either scheme",
		https: none(), http: none(),
		verdict: NotApplicable, reason: ReasonProxyNone, summary: "None",
		httpsFact: "none", httpFact: "none",
	},
	{
		name:    "the same proxy for both schemes",
		https:   decided(netproxy.DecisionProxied, "HTTPS_PROXY", "http://proxy.corp:3128"),
		http:    decided(netproxy.DecisionProxied, "HTTP_PROXY", "http://proxy.corp:3128"),
		verdict: Info, reason: ReasonProxyConfigured,
		summary:   "Configured for this target: http://proxy.corp:3128",
		httpsFact: "http://proxy.corp:3128 (HTTPS_PROXY)", httpFact: "http://proxy.corp:3128 (HTTP_PROXY)",
	},
	{
		name:    "a proxy for https only",
		https:   decided(netproxy.DecisionProxied, "HTTPS_PROXY", "http://proxy.corp:3128"),
		http:    none(),
		verdict: Info, reason: ReasonProxyConfigured,
		summary:   "Configured for this target: http://proxy.corp:3128 (https only)",
		httpsFact: "http://proxy.corp:3128 (HTTPS_PROXY)", httpFact: "none",
	},
	{
		name:    "a proxy for http only",
		https:   none(),
		http:    decided(netproxy.DecisionProxied, "http_proxy", "http://proxy.corp:3128"),
		verdict: Info, reason: ReasonProxyConfigured,
		summary:   "Configured for this target: http://proxy.corp:3128 (http only)",
		httpsFact: "none", httpFact: "http://proxy.corp:3128 (http_proxy)",
	},
	{
		name:    "two different proxys",
		https:   decided(netproxy.DecisionProxied, "HTTPS_PROXY", "http://a:3128"),
		http:    decided(netproxy.DecisionProxied, "HTTP_PROXY", "http://b:8080"),
		verdict: Info, reason: ReasonProxyConfigured,
		summary:   "Configured for this target: http://a:3128 (https), http://b:8080 (http)",
		httpsFact: "http://a:3128 (HTTPS_PROXY)", httpFact: "http://b:8080 (HTTP_PROXY)",
	},
	{
		name:    "NO_PROXY names the target for both schemes",
		https:   decided(netproxy.DecisionExcluded, "HTTPS_PROXY", ""),
		http:    decided(netproxy.DecisionExcluded, "HTTP_PROXY", ""),
		verdict: NotApplicable, reason: ReasonProxyExcluded, summary: "Excluded by NO_PROXY",
		httpsFact: "excluded by NO_PROXY", httpFact: "excluded by NO_PROXY",
	},
	{
		name:    "excluded for one scheme, no variable for the other",
		https:   decided(netproxy.DecisionExcluded, "HTTPS_PROXY", ""),
		http:    none(),
		verdict: NotApplicable, reason: ReasonProxyExcluded, summary: "Excluded by NO_PROXY",
		httpsFact: "excluded by NO_PROXY", httpFact: "none",
	},
	{
		name:    "a loopback target",
		https:   decided(netproxy.DecisionLoopback, "HTTPS_PROXY", ""),
		http:    decided(netproxy.DecisionLoopback, "HTTP_PROXY", ""),
		verdict: NotApplicable, reason: ReasonProxyLoopback, summary: "Not used for a loopback target",
		httpsFact: "not used for a loopback target", httpFact: "not used for a loopback target",
	},
	{
		name:    "loopback for one scheme, no variable for the other",
		https:   none(),
		http:    decided(netproxy.DecisionLoopback, "HTTP_PROXY", ""),
		verdict: NotApplicable, reason: ReasonProxyLoopback, summary: "Not used for a loopback target",
		httpsFact: "none", httpFact: "not used for a loopback target",
	},
	{
		name:    "excluded outranks loopback",
		https:   decided(netproxy.DecisionExcluded, "HTTPS_PROXY", ""),
		http:    decided(netproxy.DecisionLoopback, "HTTP_PROXY", ""),
		verdict: NotApplicable, reason: ReasonProxyExcluded, summary: "Excluded by NO_PROXY",
		httpsFact: "excluded by NO_PROXY", httpFact: "not used for a loopback target",
	},
	{
		name:    "a proxy outranks an exclusion",
		https:   decided(netproxy.DecisionExcluded, "HTTPS_PROXY", ""),
		http:    decided(netproxy.DecisionProxied, "HTTP_PROXY", "http://proxy.corp:3128"),
		verdict: Info, reason: ReasonProxyConfigured,
		summary:   "Configured for this target: http://proxy.corp:3128 (http only)",
		httpsFact: "excluded by NO_PROXY", httpFact: "http://proxy.corp:3128 (HTTP_PROXY)",
	},
	{
		name:    "a proxy outranks loopback",
		https:   decided(netproxy.DecisionProxied, "HTTPS_PROXY", "http://proxy.corp:3128"),
		http:    decided(netproxy.DecisionLoopback, "HTTP_PROXY", ""),
		verdict: Info, reason: ReasonProxyConfigured,
		summary:   "Configured for this target: http://proxy.corp:3128 (https only)",
		httpsFact: "http://proxy.corp:3128 (HTTPS_PROXY)", httpFact: "not used for a loopback target",
	},
	{
		name:    "an invalid https variable, named as it was read",
		https:   decided(netproxy.DecisionInvalid, "https_proxy", ""),
		http:    none(),
		verdict: Warn, reason: ReasonProxyInvalid, summary: "Invalid https_proxy value",
		httpsFact: "invalid value in https_proxy", httpFact: "none",
	},
	{
		name:    "an invalid http variable",
		https:   none(),
		http:    decided(netproxy.DecisionInvalid, "HTTP_PROXY", ""),
		verdict: Warn, reason: ReasonProxyInvalid, summary: "Invalid HTTP_PROXY value",
		httpsFact: "none", httpFact: "invalid value in HTTP_PROXY",
	},
	{
		name:    "both variables invalid",
		https:   decided(netproxy.DecisionInvalid, "HTTPS_PROXY", ""),
		http:    decided(netproxy.DecisionInvalid, "HTTP_PROXY", ""),
		verdict: Warn, reason: ReasonProxyInvalid, summary: "Invalid HTTPS_PROXY and HTTP_PROXY values",
		httpsFact: "invalid value in HTTPS_PROXY", httpFact: "invalid value in HTTP_PROXY",
	},
	{
		name:    "invalid outranks a proxy",
		https:   decided(netproxy.DecisionProxied, "HTTPS_PROXY", "http://proxy.corp:3128"),
		http:    decided(netproxy.DecisionInvalid, "HTTP_PROXY", ""),
		verdict: Warn, reason: ReasonProxyInvalid, summary: "Invalid HTTP_PROXY value",
		httpsFact: "http://proxy.corp:3128 (HTTPS_PROXY)", httpFact: "invalid value in HTTP_PROXY",
	},
}

func TestTheProxyRowSaysWhatAppliesToTheTarget(t *testing.T) {
	for _, tc := range proxyCases {
		t.Run(tc.name, func(t *testing.T) {
			c := proxyRow(t, fakeEnv{proxy: perScheme(tc.https, tc.http)}, target())

			if c.ID != CheckProxy || c.Stage != StageProxy || c.Title != "Proxy" {
				t.Errorf("row = %s/%s/%q, want proxy/proxy/%q", c.ID, c.Stage, c.Title, "Proxy")
			}
			if c.Verdict != tc.verdict {
				t.Errorf("verdict = %v, want %v", c.Verdict, tc.verdict)
			}
			if c.Reason != tc.reason {
				t.Errorf("reason = %q, want %q", c.Reason, tc.reason)
			}
			if c.Summary != tc.summary {
				t.Errorf("summary = %q, want %q", c.Summary, tc.summary)
			}
			if c.Because != "" {
				t.Errorf("because = %q: nothing upstream blocks a read of the environment", c.Because)
			}
			if got := factValue(c, "HTTPS"); got != tc.httpsFact {
				t.Errorf("HTTPS fact = %q, want %q", got, tc.httpsFact)
			}
			if got := factValue(c, "HTTP"); got != tc.httpFact {
				t.Errorf("HTTP fact = %q, want %q", got, tc.httpFact)
			}
			keys := make([]string, 0, len(c.Facts))
			for _, f := range c.Facts {
				keys = append(keys, f.Key)
			}
			if !slices.Equal(keys, []string{"HTTPS", "HTTP"}) {
				t.Errorf("fact keys = %v, want HTTPS then HTTP", keys)
			}
		})
	}
}

// The wire names are stable and serialized by MCP: they are chosen once.
func TestTheProxyIdentifiersAreStable(t *testing.T) {
	for _, tc := range []struct{ got, want string }{
		{string(CheckProxy), "proxy"},
		{string(StageProxy), "proxy"},
		{string(ReasonProxyNone), "proxy-none"},
		{string(ReasonProxyConfigured), "proxy-configured"},
		{string(ReasonProxyExcluded), "proxy-excluded"},
		{string(ReasonProxyLoopback), "proxy-loopback"},
		{string(ReasonProxyInvalid), "proxy-invalid"},
	} {
		if tc.got != tc.want {
			t.Errorf("identifier = %q, want %q", tc.got, tc.want)
		}
	}
	if checkTitles[CheckProxy] != "Proxy" {
		t.Errorf("title = %q, want %q", checkTitles[CheckProxy], "Proxy")
	}
	if got := StageTitle(StageProxy); got != "Reading the proxy settings" {
		t.Errorf("progress label = %q, want %q", got, "Reading the proxy settings")
	}
}

// The scheme of the HEAD depends on a handshake that runs later, so the row
// asks about both, https first, on the address the user typed.
func TestTheProxyRowAsksAboutBothSchemesOnTheTarget(t *testing.T) {
	for _, tc := range []struct {
		name string
		tg   Target
		want []string
	}{
		{"a name", Target{Host: "example.com", Port: 443},
			[]string{"https://example.com:443", "http://example.com:443"}},
		{"a non-standard port", Target{Host: "example.com", Port: 8443},
			[]string{"https://example.com:8443", "http://example.com:8443"}},
		{"an IPv6 literal is bracketed", Target{Host: "2001:db8::1", Port: 8443},
			[]string{"https://[2001:db8::1]:8443", "http://[2001:db8::1]:8443"}},
	} {
		t.Run(tc.name, func(t *testing.T) {
			var asked []string
			proxyRow(t, fakeEnv{proxy: func(u *url.URL) netproxy.Decision {
				asked = append(asked, u.String())
				return none()
			}}, tc.tg)
			if !slices.Equal(asked, tc.want) {
				t.Errorf("ProxyFor was asked about %v, want %v", asked, tc.want)
			}
		})
	}
}

// The row sits between the ICMP echo and the TCP connect, depends on nothing
// and gates nothing: it is a read of the environment, never skipped.
func TestTheProxyStageSitsBetweenIcmpAndTcpWithoutDependencies(t *testing.T) {
	want := []StageID{StageResolve, StageRoute, StageReach, StageProxy, StageConnect, StageTLS, StageHTTP}
	if got := Steps(); !slices.Equal(got, want) {
		t.Fatalf("Steps() = %v, want %v", got, want)
	}

	var found bool
	for _, s := range stages() {
		if s.id != StageProxy {
			continue
		}
		found = true
		if s.dependsOn != "" {
			t.Errorf("the proxy stage depends on %q, want nothing", s.dependsOn)
		}
		if s.gate != "" {
			t.Errorf("the proxy stage gates on %q, want nothing", s.gate)
		}
		if !slices.Equal(s.produces, []CheckID{CheckProxy}) {
			t.Errorf("the proxy stage declares %v, want [proxy]", s.produces)
		}
	}
	if !found {
		t.Fatal("the pipeline has no proxy stage")
	}

	checks := runWith(t, fakeEnv{}, target())
	ids := idsOf(checks)
	icmp, proxy, tcp := slices.Index(ids, CheckICMP), slices.Index(ids, CheckProxy), slices.Index(ids, CheckTCP)
	if icmp < 0 || proxy != icmp+1 || tcp != proxy+1 {
		t.Errorf("order = %v, want proxy right after icmp and right before tcp", ids)
	}
}

// A mandatory proxy often means the DNS fails first. The row is still there,
// produced and not skipped, while the TCP row stays blocked by the resolution.
func TestTheProxyRowSurvivesAFailedResolution(t *testing.T) {
	failing := func(context.Context, string, string) ([]net.IP, error) {
		return nil, errors.New("no such host")
	}
	for _, tc := range []struct {
		name        string
		proxy       func(*url.URL) netproxy.Decision
		wantVerdict Verdict
		wantSummary string
	}{
		{"no proxy", nil, NotApplicable, "None"},
		{"a proxy", perScheme(
			decided(netproxy.DecisionProxied, "HTTPS_PROXY", "http://proxy.corp:3128"),
			decided(netproxy.DecisionProxied, "HTTP_PROXY", "http://proxy.corp:3128")),
			Info, "Configured for this target: http://proxy.corp:3128"},
	} {
		t.Run(tc.name, func(t *testing.T) {
			checks := runWith(t, fakeEnv{resolve: failing, proxy: tc.proxy}, target())

			row := checkNamed(t, checks, CheckProxy)
			if row.Verdict != tc.wantVerdict || row.Summary != tc.wantSummary {
				t.Errorf("proxy row = %v %q, want %v %q", row.Verdict, row.Summary, tc.wantVerdict, tc.wantSummary)
			}
			if row.Because != "" {
				t.Errorf("the proxy row was blocked by %q, it depends on nothing", row.Because)
			}
			if strings.HasPrefix(row.Summary, "Skipped") {
				t.Errorf("the proxy row was skipped: %q", row.Summary)
			}

			tcp := checkNamed(t, checks, CheckTCP)
			if tcp.Verdict != NotApplicable || tcp.Because != CheckResolve {
				t.Errorf("tcp = %v because %q, want N/A because %q", tcp.Verdict, tcp.Because, CheckResolve)
			}
		})
	}
}

// The row asks no question of the network, so it can neither fail the headline
// nor lift it: an observation or a non-answer leaves Summarize where it was.
func TestTheProxyRowNeverMovesTheHeadline(t *testing.T) {
	baseline := Summarize(runWith(t, fakeEnv{}, target()))
	if baseline != OK {
		t.Fatalf("baseline headline = %v, want OK", baseline)
	}
	for _, tc := range proxyCases {
		if tc.verdict == Warn {
			continue // an invalid variable is a WARN, and a WARN shows
		}
		t.Run(tc.name, func(t *testing.T) {
			got := Summarize(runWith(t, fakeEnv{proxy: perScheme(tc.https, tc.http)}, target()))
			if got != baseline {
				t.Errorf("headline = %v with this proxy row, %v without", got, baseline)
			}
		})
	}
}

// The stage is runnable alone, through RunStep, whatever came before.
func TestRunStepRunsTheProxyStageAlone(t *testing.T) {
	res := RunStep(context.Background(), target(), fakeEnv{}, DefaultSettings(), StageProxy, Results{})

	if got := idsOf(res.All()); !slices.Equal(got, []CheckID{CheckProxy}) {
		t.Fatalf("RunStep(StageProxy) produced %v, want [proxy]", got)
	}
	c, _ := res.Get(CheckProxy)
	if c.Verdict != NotApplicable || c.Reason != ReasonProxyNone {
		t.Errorf("row = %v (reason %q), want N/A proxy-none", c.Verdict, c.Reason)
	}
}

func TestACancelledRunLeavesTheProxyRowUnknown(t *testing.T) {
	ctx, cancel := context.WithCancel(context.Background())
	cancel()

	res, err := Run(ctx, target(), fakeEnv{}, DefaultSettings())
	if err != nil {
		t.Fatalf("Run: %v", err)
	}
	c, ok := res.Get(CheckProxy)
	if !ok {
		t.Fatalf("a cancelled run left no proxy row; got %v", idsOf(res.All()))
	}
	if c.Verdict != Unknown || !strings.HasPrefix(c.Summary, "Cancelled") {
		t.Errorf("proxy row = %v %q, want Unknown with the Cancelled placeholder", c.Verdict, c.Summary)
	}
	if c.Stage != StageProxy || c.Title != "Proxy" {
		t.Errorf("placeholder = %s/%q, want proxy/%q", c.Stage, c.Title, "Proxy")
	}
}

// The stage must not read the environment once cancelled: a cancelled context
// before the stage is the placeholder, not a late answer.
func TestACancelledStepDoesNotAskTheProxyQuestion(t *testing.T) {
	ctx, cancel := context.WithCancel(context.Background())
	cancel()

	asked := false
	env := fakeEnv{proxy: func(*url.URL) netproxy.Decision { asked = true; return none() }}
	res := RunStep(ctx, target(), env, DefaultSettings(), StageProxy, Results{})

	if asked {
		t.Error("the proxy question was asked after the context was cancelled")
	}
	if c, _ := res.Get(CheckProxy); c.Verdict != Unknown {
		t.Errorf("verdict = %v, want Unknown", c.Verdict)
	}
}

// Redaction is netproxy's job; this is the belt. A Decision already carries
// nothing but a redacted Display, and nothing downstream may put a credential
// back: no fact, summary or explanation carries "@" or a secret token.
func TestNoProxyRowTextCarriesACredential(t *testing.T) {
	for _, tc := range proxyCases {
		t.Run(tc.name, func(t *testing.T) {
			c := proxyRow(t, fakeEnv{proxy: perScheme(tc.https, tc.http)}, target())
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
		})
	}
}

// An Excluded, Loopback or Invalid row names no proxy, so it must not look like
// it does: no URL in a fact or in the summary. An Invalid row that also carries
// a proxy for the other scheme is the exception: its fact for that scheme names
// it, as the plan's per-scheme Facts do.
func TestARowWithoutAProxyNamesNoURL(t *testing.T) {
	for _, tc := range proxyCases {
		if tc.verdict == Info {
			continue
		}
		t.Run(tc.name, func(t *testing.T) {
			c := proxyRow(t, fakeEnv{proxy: perScheme(tc.https, tc.http)}, target())
			texts := []string{c.Summary}
			// An invalid row that also has a proxy for the other scheme names
			// it in that scheme's fact only: the summary never carries a URL.
			if tc.https.Kind != netproxy.DecisionProxied && tc.http.Kind != netproxy.DecisionProxied {
				for _, f := range c.Facts {
					texts = append(texts, f.Value)
				}
			}
			for _, s := range texts {
				if strings.Contains(s, "://") {
					t.Errorf("%q names a URL on a row where no proxy applies", s)
				}
			}
		})
	}
}

// The production Env answers from the process environment. A scheme the
// proxy variables do not cover is None whatever the machine sets, so this holds
// on any runner.
func TestTheSystemEnvAnswersTheProxyQuestion(t *testing.T) {
	d := SystemEnv(DefaultSettings()).ProxyFor(&url.URL{Scheme: "ftp", Host: "example.com:21"})
	if d.Kind != netproxy.DecisionNone {
		t.Errorf("ProxyFor(ftp) = %+v, want DecisionNone", d)
	}
}

// Explain answers the three non-answers structurally, like the non-TLS port:
// they carry no Because and a Reason, and the text says why nothing was
// checked and that the checks go direct.
func TestTheProxyNonAnswersAreExplained(t *testing.T) {
	for _, tc := range []struct {
		reason Reason
		means  string
	}{
		{ReasonProxyNone,
			"No proxy variable is set (HTTPS_PROXY, HTTP_PROXY or their lowercase forms), so clients on " +
				"this machine connect directly, the same path these checks test."},
		{ReasonProxyExcluded,
			"A proxy variable is set, but NO_PROXY names this target, so clients connect to it directly, " +
				"the same path these checks test."},
		{ReasonProxyLoopback,
			"Go programs, DevDesk included, never send localhost or a loopback address through a proxy, " +
				"whatever the variables say."},
	} {
		t.Run(string(tc.reason), func(t *testing.T) {
			e := Explain(Check{ID: CheckProxy, Verdict: NotApplicable, Reason: tc.reason, Summary: "x"})
			if e.Means != tc.means {
				t.Errorf("means = %q, want %q", e.Means, tc.means)
			}
			if e.Do != "" {
				t.Errorf("do = %q, want nothing: there is nothing to act on", e.Do)
			}
		})
	}
}

func TestTheProxyObservationsAreExplained(t *testing.T) {
	configured := Explain(Check{ID: CheckProxy, Verdict: Info, Reason: ReasonProxyConfigured, Summary: "x"})
	wantMeans := "Browsers and most HTTP clients on this machine, DevDesk's included, reach this target " +
		"through this proxy. These checks do not: they test the direct path, and nothing here was sent " +
		"to the proxy."
	if configured.Means != wantMeans {
		t.Errorf("configured means = %q, want %q", configured.Means, wantMeans)
	}
	if configured.Do != "" {
		t.Errorf("configured do = %q, want nothing: an observation has no action", configured.Do)
	}

	invalid := Explain(Check{ID: CheckProxy, Verdict: Warn, Reason: ReasonProxyInvalid, Summary: "x"})
	wantInvalid := "The variable is set but is not a usable proxy URL. Depending on the value, Go programs, " +
		"DevDesk included, either ignore it and connect directly, or fail every request they send " +
		"through it; other tools may read it differently."
	if invalid.Means != wantInvalid {
		t.Errorf("invalid means = %q, want %q", invalid.Means, wantInvalid)
	}
	wantDo := "Set it to scheme://host:port, for example http://proxy.example.com:3128, or unset it."
	if invalid.Do != wantDo {
		t.Errorf("invalid do = %q, want %q", invalid.Do, wantDo)
	}
}

// There is one cause per verdict on this row, so its guidance rows carry no
// Reason: they are reached by any Reason of that verdict, and
// TestNoGuidanceRowIsUnreachable cannot be tripped by a renamed one. A
// NotApplicable row is answered structurally and must not have a row at all.
func TestTheProxyGuidanceRowsAreReasonlessInfoAndWarn(t *testing.T) {
	got := map[Verdict]int{}
	for _, g := range explanations {
		if g.check != CheckProxy {
			continue
		}
		got[g.verdict]++
		if g.reason != "" {
			t.Errorf("proxy/%s guidance carries reason %q, want none", g.verdict, g.reason)
		}
	}
	if got[Info] != 1 || got[Warn] != 1 || len(got) != 2 {
		t.Errorf("proxy guidance rows per verdict = %v, want exactly one Info and one Warn", got)
	}
}

// The state a proxy row can be in is explained, whatever the wording.
func TestEveryProxyRowStateIsExplained(t *testing.T) {
	for _, tc := range proxyCases {
		t.Run(tc.name, func(t *testing.T) {
			c := proxyRow(t, fakeEnv{proxy: perScheme(tc.https, tc.http)}, target())
			e := Explain(c)
			if strings.Contains(e.Means, "No guidance is written") || strings.TrimSpace(e.Means) == "" {
				t.Errorf("the %v row (reason %q) is not explained: %q", c.Verdict, c.Reason, e.Means)
			}
			if e.Observed != c.Summary {
				t.Errorf("observed = %q, want the summary %q", e.Observed, c.Summary)
			}
			if c.Verdict == Warn && strings.TrimSpace(e.Do) == "" {
				t.Error("an invalid variable has something to do about it")
			}
		})
	}
}

// Nothing is sent to a proxy and nothing is sent to the network by this stage:
// it reads the environment and that is all.
func TestTheProxyStageTouchesNothingButTheProxyQuestion(t *testing.T) {
	forbid := func(name string) error {
		t.Errorf("%s was called by the proxy stage", name)
		return errors.New(name)
	}
	env := fakeEnv{
		resolve: func(context.Context, string, string) ([]net.IP, error) { return nil, forbid("Resolve") },
		reverse: func(context.Context, string, string) ([]string, error) { return nil, forbid("ReverseLookup") },
		ping:    func(context.Context, string, int) (PingStats, error) { return PingStats{}, forbid("Ping") },
		dial:    func(context.Context, string) (time.Duration, error) { return 0, forbid("DialTCP") },
		head:    func(context.Context, string) (HTTPResult, error) { return HTTPResult{}, forbid("Head") },
		route:   func(context.Context, net.IP) (RouteHop, error) { return RouteHop{}, forbid("Route") },
		handshake: func(context.Context, string, string) (*tls.ConnectionState, error) {
			return nil, forbid("Handshake")
		},
	}
	c := proxyRow(t, env, target())
	if c.Verdict != NotApplicable || c.Reason != ReasonProxyNone {
		t.Errorf("row = %v (reason %q), want N/A proxy-none", c.Verdict, c.Reason)
	}
}
