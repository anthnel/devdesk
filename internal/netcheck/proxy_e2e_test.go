package netcheck

import (
	"bytes"
	"context"
	"encoding/json"
	"fmt"
	"os"
	"os/exec"
	"strings"
	"testing"
	"time"
)

// DEVDESK-582, criterion 6 end to end and without a network: the real
// systemEnv.ProxyFor reads the real process environment, so each case runs the
// test binary again with exactly the variables it wants. The fake env answers
// every other question; only the proxy one is delegated to the system env.

const (
	e2eHelperEnv = "NETCHECK_TEST_PROXY_E2E_HELPER"
	e2eRefuseEnv = "NETCHECK_TEST_PROXY_E2E_REFUSE_DIAL"
	// e2eFailResolveEnv makes the child's fake resolver fail every lookup
	// (DEVDESK-596).
	e2eFailResolveEnv = "NETCHECK_TEST_PROXY_E2E_FAIL_RESOLVE"
	e2eResultMark     = "NETCHECK_E2E_RESULT:"
	e2eSecretValue    = secretUser + ":" + secretPass
)

type e2eResult struct {
	Dump    string  `json:"dump"`
	Verdict Verdict `json:"verdict"`
	Summary string  `json:"summary"`
	Reason  Reason  `json:"reason"`
	// TCPProxy is the Proxy fact of the TCP row, empty when it carries none.
	TCPProxy string `json:"tcp_proxy"`
	// ResolveReason and ResolveProxy are the reason and the Proxy fact of the DNS
	// row, empty when it carries none.
	ResolveReason Reason `json:"resolve_reason"`
	ResolveProxy  string `json:"resolve_proxy"`
}

// TestProxyE2EHelper is the child half. It does nothing in a normal run.
func TestProxyE2EHelper(t *testing.T) {
	if os.Getenv(e2eHelperEnv) != "1" {
		t.Skip("child of TestTheProxyRowNeverCarriesACredentialEndToEnd")
	}
	set := DefaultSettings()
	env := fakeEnv{proxy: SystemEnv(set).ProxyFor}
	if os.Getenv(e2eRefuseEnv) == "1" {
		env.dial = refusedDial
	}
	if os.Getenv(e2eFailResolveEnv) == "1" {
		env.resolve = resolveFailing(errNoSuchHost)
	}
	res, err := Run(context.Background(), target(), env, set)
	if err != nil {
		t.Fatalf("Run: %v", err)
	}
	row, ok := res.Get(CheckProxy)
	if !ok {
		t.Fatal("the run left no proxy check")
	}
	tcp, ok := res.Get(CheckTCP)
	if !ok {
		t.Fatal("the run left no TCP check")
	}
	resolve, ok := res.Get(CheckResolve)
	if !ok {
		t.Fatal("the run left no DNS check")
	}
	out, err := json.Marshal(e2eResult{
		Dump:     fmt.Sprintf("%+v", res.All()),
		Verdict:  row.Verdict,
		Summary:  row.Summary,
		Reason:   row.Reason,
		TCPProxy: factValue(tcp, "Proxy"),

		ResolveReason: resolve.Reason,
		ResolveProxy:  factValue(resolve, "Proxy"),
	})
	if err != nil {
		t.Fatalf("marshal: %v", err)
	}
	fmt.Println(e2eResultMark + string(out))
}

// runE2EChild runs the child with the proxy variables of the parent removed and
// exactly vars set. With refuseDial the fake env refuses the direct TCP dial;
// with failResolve it fails every lookup.
func runE2EChild(t *testing.T, vars map[string]string, refuseDial, failResolve bool) e2eResult {
	t.Helper()
	drop := []string{"HTTPS_PROXY", "HTTP_PROXY", "NO_PROXY", "ALL_PROXY", "REQUEST_METHOD", e2eHelperEnv, e2eRefuseEnv, e2eFailResolveEnv}
	var env []string
	for _, kv := range os.Environ() {
		name, _, _ := strings.Cut(kv, "=")
		keep := true
		for _, d := range drop {
			if strings.EqualFold(name, d) {
				keep = false
			}
		}
		if keep {
			env = append(env, kv)
		}
	}
	// GORACE: under -race each child would otherwise sleep a second at exit.
	env = append(env, "GORACE=atexit_sleep_ms=0", e2eHelperEnv+"=1")
	if refuseDial {
		env = append(env, e2eRefuseEnv+"=1")
	}
	if failResolve {
		env = append(env, e2eFailResolveEnv+"=1")
	}
	for k, v := range vars {
		env = append(env, k+"="+v)
	}
	// A hung child must fail the test, not the whole suite's timeout.
	ctx, cancel := context.WithTimeout(context.Background(), 60*time.Second)
	defer cancel()
	cmd := exec.CommandContext(ctx, os.Args[0], "-test.run=^TestProxyE2EHelper$", "-test.count=1")
	cmd.Env = env
	var out bytes.Buffer
	cmd.Stdout, cmd.Stderr = &out, &out
	if err := cmd.Run(); err != nil {
		t.Fatalf("running the helper: %v\n%s", err, out.String())
	}
	for _, line := range strings.Split(out.String(), "\n") {
		rest, ok := strings.CutPrefix(strings.TrimSpace(line), e2eResultMark)
		if !ok {
			continue
		}
		var r e2eResult
		if err := json.Unmarshal([]byte(rest), &r); err != nil {
			t.Fatalf("decode the helper's answer %q: %v", rest, err)
		}
		return r
	}
	t.Fatalf("the helper printed no %q line:\n%s", e2eResultMark, out.String())
	return e2eResult{}
}

func TestTheProxyRowNeverCarriesACredentialEndToEnd(t *testing.T) {
	const proxyShown = "http://proxy.test:3128"
	tests := []struct {
		name         string
		vars         map[string]string
		refuseDial   bool
		failResolve  bool
		wantVerdict  Verdict
		wantReason   Reason
		wantSummary  string
		wantTCPProxy string
		// wantResolveReason and wantResolveProxy describe the DNS row, which is
		// only touched when failResolve is set and a proxy applies.
		wantResolveReason Reason
		wantResolveProxy  string
		// tokens that must appear nowhere in the results; defaults to the user
		// and the password.
		tokens []string
	}{
		{
			name:        "a proxy with credentials",
			vars:        map[string]string{"HTTPS_PROXY": "http://" + e2eSecretValue + "@proxy.test:3128"},
			wantVerdict: Info, wantReason: ReasonProxyConfigured,
			wantSummary: "Configured for this target: " + proxyShown + " (https only)",
		},
		{
			name:        "lowercase variables",
			vars:        map[string]string{"https_proxy": "http://" + e2eSecretValue + "@proxy.test:3128"},
			wantVerdict: Info, wantReason: ReasonProxyConfigured,
			wantSummary: "Configured for this target: " + proxyShown + " (https only)",
		},
		{
			name:        "HTTP_PROXY alone",
			vars:        map[string]string{"HTTP_PROXY": "http://" + e2eSecretValue + "@proxy.test:3128"},
			wantVerdict: Info, wantReason: ReasonProxyConfigured,
			wantSummary: "Configured for this target: " + proxyShown + " (http only)",
		},
		{
			name:        "a value whose credential is mangled",
			vars:        map[string]string{"HTTPS_PROXY": "http://" + e2eSecretValue + "/x@proxy.test:3128"},
			wantVerdict: Warn, wantReason: ReasonProxyInvalid,
			wantSummary: "Invalid HTTPS_PROXY value",
		},
		{
			name:        "a mangled value that still parses",
			vars:        map[string]string{"HTTPS_PROXY": "http://" + secretUser + ":1234/x@proxy.test:3128"},
			wantVerdict: Warn, wantReason: ReasonProxyInvalid,
			wantSummary: "Invalid HTTPS_PROXY value",
			tokens:      []string{secretUser, "1234"},
		},
		{
			name:        "a refused direct dial with a proxy configured",
			vars:        map[string]string{"HTTPS_PROXY": "http://" + e2eSecretValue + "@proxy.test:3128"},
			refuseDial:  true,
			wantVerdict: Info, wantReason: ReasonProxyConfigured,
			wantSummary:  "Configured for this target: " + proxyShown + " (https only)",
			wantTCPProxy: proxyShown,
		},
		{
			name:        "a failed resolution with a proxy with credentials",
			vars:        map[string]string{"HTTPS_PROXY": "http://" + e2eSecretValue + "@proxy.test:3128"},
			failResolve: true,
			wantVerdict: Info, wantReason: ReasonProxyConfigured,
			wantSummary:       "Configured for this target: " + proxyShown + " (https only)",
			wantResolveReason: ReasonDirectResolveFailedProxyConfigured,
			wantResolveProxy:  proxyShown,
		},
		{
			// The value is unusable, so no proxy applies: the DNS row keeps its
			// generic conclusion and the raw value shows up nowhere.
			name:        "a failed resolution with an invalid value only",
			vars:        map[string]string{"HTTPS_PROXY": "http://" + e2eSecretValue + "/x@proxy.test:3128"},
			failResolve: true,
			wantVerdict: Warn, wantReason: ReasonProxyInvalid,
			wantSummary: "Invalid HTTPS_PROXY value",
			tokens:      []string{secretUser, secretPass, "proxy.test", "/x@"},
		},
	}
	for _, tt := range tests {
		t.Run(tt.name, func(t *testing.T) {
			got := runE2EChild(t, tt.vars, tt.refuseDial, tt.failResolve)

			if got.Verdict != tt.wantVerdict || got.Reason != tt.wantReason || got.Summary != tt.wantSummary {
				t.Errorf("proxy row = %v %q (%q), want %v %q (%q)",
					got.Verdict, got.Summary, got.Reason, tt.wantVerdict, tt.wantSummary, tt.wantReason)
			}
			if got.TCPProxy != tt.wantTCPProxy {
				t.Errorf("TCP row Proxy fact = %q, want %q", got.TCPProxy, tt.wantTCPProxy)
			}
			if got.ResolveReason != tt.wantResolveReason || got.ResolveProxy != tt.wantResolveProxy {
				t.Errorf("DNS row = reason %q, Proxy fact %q, want %q, %q",
					got.ResolveReason, got.ResolveProxy, tt.wantResolveReason, tt.wantResolveProxy)
			}
			tokens := tt.tokens
			if tokens == nil {
				tokens = []string{secretUser, secretPass}
			}
			for _, bad := range tokens {
				if strings.Contains(got.Dump, bad) {
					t.Errorf("the results carry %q:\n%s", bad, got.Dump)
				}
			}
		})
	}
}

// RunStep(StageConnect) with no Proxy row among the results (the proxy stage
// was not run, or its row was never handed over) keeps the generic conclusion,
// whatever the env would answer: the connect stage reads the Proxy row, it does
// not ask the env itself.
func TestAFailedDialWithoutAProxyRowKeepsTheGenericConclusion(t *testing.T) {
	prior := ResultsOf(Check{ID: CheckResolve, Stage: StageResolve, Verdict: OK})
	env := fakeEnv{dial: refusedDial, proxy: proxiedBoth}

	res := RunStep(context.Background(), target(), env, DefaultSettings(), StageConnect, prior)

	c, ok := res.Get(CheckTCP)
	if !ok {
		t.Fatalf("the connect stage produced no TCP row; got %v", idsOf(res.All()))
	}
	if want := "Port 443 does not accept connections"; c.Summary != want {
		t.Errorf("summary = %q, want %q", c.Summary, want)
	}
	if c.Verdict != Fail {
		t.Errorf("verdict = %v, want Fail", c.Verdict)
	}
	if c.Reason != "" {
		t.Errorf("reason = %q, want none: no Proxy row was read", c.Reason)
	}
	if got := factValue(c, "Proxy"); got != "" {
		t.Errorf("Proxy fact = %q, want none", got)
	}
}
