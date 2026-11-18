package mcp

import (
	"encoding/json"
	"net/url"
	"strings"
	"testing"

	"github.com/anthnel/devdesk/internal/netproxy"
)

// DEVDESK-582, criteria 4 and 6 over MCP: the proxy row is reported with the
// INFO verdict and the proxy as displayed, and no credential is serialized.

const (
	mcpSecretUser = "s3cretUser"
	mcpSecretPass = "s3cretPass"
)

// This proves the wiring only: the Decision the fake env hands over is already
// redacted, so the absence of a token here shows that the MCP output adds none
// of its own. The redaction itself is proven in internal/netproxy (Describe) and,
// from the real environment variable to the results, in
// TestTheProxyRowNeverCarriesACredentialEndToEnd (internal/netcheck/proxy_e2e_test.go).
func TestNetCheckReportsTheConfiguredProxyFromItsDecisionOnly(t *testing.T) {
	fakeCacheHome(t)
	env := reachableHost()
	env.proxy = func(*url.URL) netproxy.Decision {
		return netproxy.Decision{Kind: netproxy.DecisionProxied, Variable: "HTTPS_PROXY", Proxy: "http://proxy.test:3128"}
	}
	stubNetwork(t, env)

	var out netCheckOut
	callTool(t, connect(t, testEnv(nil)), "net_check", map[string]any{"host": "example.test"}, &out)

	raw, err := json.Marshal(out)
	if err != nil {
		t.Fatalf("marshal the output: %v", err)
	}
	text := string(raw)
	for _, want := range []string{
		`"id":"proxy"`,
		`"verdict":"INFO"`,
		`"summary":"Configured for this target: http://proxy.test:3128"`,
	} {
		if !strings.Contains(text, want) {
			t.Errorf("the JSON output has no %s:\n%s", want, text)
		}
	}
	for _, bad := range []string{mcpSecretUser, mcpSecretPass} {
		if strings.Contains(text, bad) {
			t.Errorf("the JSON output carries %q:\n%s", bad, text)
		}
	}

	// An INFO row never colours the headline: the same run without a proxy has
	// the same verdict.
	baseline := reachableHost()
	stubNetwork(t, baseline)
	var plain netCheckOut
	callTool(t, connect(t, testEnv(nil)), "net_check", map[string]any{"host": "example.test"}, &plain)
	if out.Verdict != plain.Verdict {
		t.Errorf("headline verdict = %q with a proxy, %q without: an observation must not move it", out.Verdict, plain.Verdict)
	}
}

// An invalid value is named by its variable only.
func TestNetCheckReportsAnInvalidProxyValueWithoutIt(t *testing.T) {
	fakeCacheHome(t)
	env := reachableHost()
	env.proxy = func(u *url.URL) netproxy.Decision {
		if u.Scheme == "https" {
			return netproxy.Decision{Kind: netproxy.DecisionInvalid, Variable: "HTTPS_PROXY"}
		}
		return netproxy.Decision{}
	}
	stubNetwork(t, env)

	var out netCheckOut
	callTool(t, connect(t, testEnv(nil)), "net_check", map[string]any{"host": "example.test"}, &out)

	raw, err := json.Marshal(out)
	if err != nil {
		t.Fatalf("marshal the output: %v", err)
	}
	for _, want := range []string{`"id":"proxy"`, `"verdict":"WARN"`, `Invalid HTTPS_PROXY value`} {
		if !strings.Contains(string(raw), want) {
			t.Errorf("the JSON output has no %s:\n%s", want, raw)
		}
	}
	for _, bad := range []string{mcpSecretUser, mcpSecretPass, "@"} {
		if strings.Contains(string(raw), bad) {
			t.Errorf("the JSON output carries %q:\n%s", bad, raw)
		}
	}
}

// The stage field's schema is what an agent reads to learn the stage names. It
// listed five of the seven; route and proxy are real StageIDs too.
func TestTheStageSchemaListsEveryStage(t *testing.T) {
	cs := connect(t, testEnv(nil))

	res, err := cs.ListTools(t.Context(), nil)
	if err != nil {
		t.Fatalf("ListTools: %v", err)
	}
	for _, tool := range res.Tools {
		if tool.Name != "net_check" {
			continue
		}
		schema, err := json.Marshal(tool.OutputSchema)
		if err != nil {
			t.Fatalf("marshal output schema: %v", err)
		}
		var doc struct {
			Properties struct {
				Checks struct {
					Items struct {
						Properties struct {
							Stage struct {
								Description string `json:"description"`
							} `json:"stage"`
						} `json:"properties"`
					} `json:"items"`
				} `json:"checks"`
			} `json:"properties"`
		}
		if err := json.Unmarshal(schema, &doc); err != nil {
			t.Fatalf("decode output schema: %v", err)
		}
		got := doc.Properties.Checks.Items.Properties.Stage.Description
		for _, want := range []string{"resolve", "route", "reach", "proxy", "connect", "tls", "http"} {
			if !strings.Contains(got, want) {
				t.Errorf("the checks[].stage description %q does not list %q", got, want)
			}
		}
		return
	}
	t.Fatal("net_check is not registered")
}

// Criterion 3 over MCP, on the wire: no proxy variable means the proxy row is
// serialized as N/A with the summary None, never as INFO. The verdict field's
// description naming INFO is checked by TestTheVerdictSchemaListsINFO
// (netcheck_tools_test.go).
func TestNetCheckReportsNoProxyAsNotApplicableNone(t *testing.T) {
	fakeCacheHome(t)
	stubNetwork(t, reachableHost())

	var out netCheckOut
	callTool(t, connect(t, testEnv(nil)), "net_check", map[string]any{"host": "example.test"}, &out)

	raw, err := json.Marshal(out)
	if err != nil {
		t.Fatalf("marshal the output: %v", err)
	}
	var doc struct {
		Checks []map[string]any `json:"checks"`
	}
	if err := json.Unmarshal(raw, &doc); err != nil {
		t.Fatalf("decode the output: %v", err)
	}
	for _, c := range doc.Checks {
		if c["id"] != "proxy" {
			continue
		}
		if c["verdict"] != "N/A" || c["summary"] != "None" {
			t.Errorf("proxy row = verdict %v summary %v, want N/A / None:\n%s", c["verdict"], c["summary"], raw)
		}
		if _, has := c["because"]; has && c["because"] != "" {
			t.Errorf("proxy row carries because=%v: None is not an upstream failure", c["because"])
		}
		return
	}
	t.Fatalf("net_check reported no proxy check:\n%s", raw)
}
