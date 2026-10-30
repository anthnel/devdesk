package config

import (
	"math"
	"os"
	"path/filepath"
	"strings"
	"testing"
)

// loadAgentUsageYAML writes raw YAML as the "default" context under an isolated HOME
// and returns the loaded result.
func loadAgentUsageYAML(t *testing.T, body string) (*Config, error) {
	t.Helper()
	home := setupTmpHome(t)
	dir := filepath.Join(home, ".devdesk")
	if err := os.MkdirAll(dir, 0o700); err != nil {
		t.Fatal(err)
	}
	if err := os.WriteFile(filepath.Join(dir, "config.yaml"), []byte(body), 0o600); err != nil {
		t.Fatal(err)
	}
	return LoadContext("default")
}

func wantRate(t *testing.T, name string, r Rate, want float64, wantState RateState) {
	t.Helper()
	got, st := r.Value()
	if st != wantState {
		t.Errorf("%s: state = %v, want %v", name, st, wantState)
	}
	if wantState == RateSet && got != want {
		t.Errorf("%s: value = %v, want %v", name, got, want)
	}
}

func TestAgentUsagePrices_FullEntry(t *testing.T) {
	cfg, err := loadAgentUsageYAML(t, `
agent_usage:
  prices:
    claude-x:
      input: 5
      output: 25.5
      cache_write_5m: 6.25
      cache_write_1h: 10
      cache_read: 0
`)
	if err != nil {
		t.Fatalf("LoadContext: %v", err)
	}
	p, ok := cfg.AgentUsage.Prices["claude-x"]
	if !ok {
		t.Fatalf("entry missing: %#v", cfg.AgentUsage.Prices)
	}
	wantRate(t, "input (int)", p.Input, 5, RateSet)
	wantRate(t, "output", p.Output, 25.5, RateSet)
	wantRate(t, "cache_write_5m", p.CacheWrite5m, 6.25, RateSet)
	wantRate(t, "cache_write_1h", p.CacheWrite1h, 10, RateSet)
	wantRate(t, "cache_read explicit 0", p.CacheRead, 0, RateSet)
}

func TestAgentUsagePrices_MissingRate(t *testing.T) {
	cfg, err := loadAgentUsageYAML(t, "agent_usage:\n  prices:\n    m:\n      input: 1\n")
	if err != nil {
		t.Fatal(err)
	}
	p := cfg.AgentUsage.Prices["m"]
	wantRate(t, "input", p.Input, 1, RateSet)
	wantRate(t, "cache_read", p.CacheRead, 0, RateMissing)
	wantRate(t, "output", p.Output, 0, RateMissing)
}

// A badly typed or invalid rate costs its own entry, never the context load.
func TestAgentUsagePrices_InvalidRateDoesNotFailLoad(t *testing.T) {
	cases := map[string]string{
		"string":      `"5$"`,
		"bare string": `abc`,
		"list":        `[1, 2]`,
		"map":         `{a: 1}`,
		"bool":        `true`,
		"null":        `~`,
	}
	for name, v := range cases {
		t.Run(name, func(t *testing.T) {
			cfg, err := loadAgentUsageYAML(t, "app:\n  secret_backend: memory\nagent_usage:\n  prices:\n    bad:\n      input: "+v+"\n      output: 3\n    good:\n      input: 1\n")
			if err != nil {
				t.Fatalf("LoadContext failed for %s: %v", name, err)
			}
			if cfg.App.SecretBackend != "memory" {
				t.Errorf("other sections lost: secret_backend = %q", cfg.App.SecretBackend)
			}
			bad := cfg.AgentUsage.Prices["bad"]
			if name != "null" {
				wantRate(t, "bad.input", bad.Input, 0, RateInvalid)
			}
			wantRate(t, "bad.output untouched", bad.Output, 3, RateSet)
			wantRate(t, "good.input", cfg.AgentUsage.Prices["good"].Input, 1, RateSet)
		})
	}
}

// Validation (negative, NaN, Inf) belongs to the pricing package: here they are values.
func TestAgentUsagePrices_NumericOdditiesDecodeAsValues(t *testing.T) {
	cfg, err := loadAgentUsageYAML(t, "agent_usage:\n  prices:\n    m:\n      input: .nan\n      output: .inf\n      cache_read: -1\n")
	if err != nil {
		t.Fatal(err)
	}
	p := cfg.AgentUsage.Prices["m"]
	if v, st := p.Input.Value(); st != RateSet || !math.IsNaN(v) {
		t.Errorf("input = %v, %v; want NaN, RateSet", v, st)
	}
	if v, st := p.Output.Value(); st != RateSet || !math.IsInf(v, 1) {
		t.Errorf("output = %v, %v; want +Inf, RateSet", v, st)
	}
	wantRate(t, "cache_read", p.CacheRead, -1, RateSet)
}

func TestAgentUsagePrices_ModelKeysKeptAsWritten(t *testing.T) {
	cfg, err := loadAgentUsageYAML(t, "agent_usage:\n  prices:\n    \"Claude-X \":\n      input: 1\n    claude-x:\n      input: 2\n")
	if err != nil {
		t.Fatal(err)
	}
	for _, k := range []string{"Claude-X ", "claude-x"} {
		if _, ok := cfg.AgentUsage.Prices[k]; !ok {
			t.Errorf("key %q not kept verbatim: %v", k, cfg.AgentUsage.Prices)
		}
	}
	if len(cfg.AgentUsage.Prices) != 2 {
		t.Errorf("len = %d, want 2", len(cfg.AgentUsage.Prices))
	}
}

func TestAgentUsagePrices_RoundTrip(t *testing.T) {
	// Build the table through the loader so the test only needs the decoding API.
	loaded, err := loadAgentUsageYAML(t, "agent_usage:\n  prices:\n    Claude-X:\n      input: 5\n      output: 25\n      cache_write_5m: 6.25\n      cache_write_1h: 10\n      cache_read: 0.5\n    typo:\n      input: \"5$\"\n      output: 1\n")
	if err != nil {
		t.Fatal(err)
	}
	if err := SaveContext(loaded, "default"); err != nil {
		t.Fatal(err)
	}
	path, _ := GetContextPath("default")
	raw, _ := os.ReadFile(path)
	if !strings.Contains(string(raw), "5$") {
		t.Errorf("invalid raw text not written back:\n%s", raw)
	}
	again, err := LoadContext("default")
	if err != nil {
		t.Fatalf("reload: %v", err)
	}
	if len(again.AgentUsage.Prices) != 2 {
		t.Fatalf("prices after round trip: %v", again.AgentUsage.Prices)
	}
	a, b := loaded.AgentUsage.Prices, again.AgentUsage.Prices
	for k, want := range a {
		got, ok := b[k]
		if !ok {
			t.Errorf("key %q lost", k)
			continue
		}
		wantR := [5]Rate{want.Input, want.Output, want.CacheWrite5m, want.CacheWrite1h, want.CacheRead}
		gotR := [5]Rate{got.Input, got.Output, got.CacheWrite5m, got.CacheWrite1h, got.CacheRead}
		for i := range wantR {
			wv, ws := wantR[i].Value()
			gv, gs := gotR[i].Value()
			if ws != gs || (ws == RateSet && wv != gv) {
				t.Errorf("%s rate #%d: (%v,%v) -> (%v,%v)", k, i, wv, ws, gv, gs)
			}
		}
	}
	wantRate(t, "typo.input", b["typo"].Input, 0, RateInvalid)
	wantRate(t, "Claude-X.cache_read", b["Claude-X"].CacheRead, 0.5, RateSet)
}

// A missing rate must stay missing after a save (omitempty), not become an explicit 0.
func TestAgentUsagePrices_MissingStaysMissingOnSave(t *testing.T) {
	cfg, err := loadAgentUsageYAML(t, "agent_usage:\n  prices:\n    m:\n      input: 1\n")
	if err != nil {
		t.Fatal(err)
	}
	if err := SaveContext(cfg, "default"); err != nil {
		t.Fatal(err)
	}
	path, _ := GetContextPath("default")
	raw, _ := os.ReadFile(path)
	if strings.Contains(string(raw), "cache_read") || strings.Contains(string(raw), "output") {
		t.Errorf("missing rates written:\n%s", raw)
	}
}

func TestAgentUsage_NoDefaultAndNoKeyWhenEmpty(t *testing.T) {
	if Default().AgentUsage.Prices != nil {
		t.Errorf("Default().AgentUsage.Prices = %v, want nil", Default().AgentUsage.Prices)
	}
	cfg, err := loadAgentUsageYAML(t, "app:\n  secret_backend: memory\n")
	if err != nil {
		t.Fatal(err)
	}
	if cfg.AgentUsage.Prices != nil {
		t.Errorf("loaded prices = %v, want nil (no default, no migration)", cfg.AgentUsage.Prices)
	}
	for name, c := range map[string]*Config{"Default": Default(), "loaded": cfg} {
		if err := SaveContext(c, "default"); err != nil {
			t.Fatal(err)
		}
		path, _ := GetContextPath("default")
		raw, _ := os.ReadFile(path)
		if strings.Contains(string(raw), "agent_usage") {
			t.Errorf("%s: agent_usage key written for an empty section:\n%s", name, raw)
		}
	}
}

// A non-scalar invalid rate is written back as it was read, and stays invalid
// on the next load.
func TestAgentUsagePrices_NonScalarInvalidRateRoundTrips(t *testing.T) {
	cases := map[string]struct{ yaml, want string }{
		"list": {"input: [1, 2]", "[1, 2]"},
		"map":  {"input: {a: 1}", "{a: 1}"},
	}
	for name, tc := range cases {
		t.Run(name, func(t *testing.T) {
			cfg, err := loadAgentUsageYAML(t, "agent_usage:\n  prices:\n    m:\n      "+tc.yaml+"\n      output: 3\n")
			if err != nil {
				t.Fatal(err)
			}
			if err := SaveContext(cfg, "default"); err != nil {
				t.Fatal(err)
			}
			path, _ := GetContextPath("default")
			raw, _ := os.ReadFile(path)
			if !strings.Contains(string(raw), tc.want) {
				t.Errorf("original content lost:\n%s", raw)
			}
			again, err := LoadContext("default")
			if err != nil {
				t.Fatalf("reload: %v", err)
			}
			wantRate(t, "input", again.AgentUsage.Prices["m"].Input, 0, RateInvalid)
			wantRate(t, "output", again.AgentUsage.Prices["m"].Output, 3, RateSet)
		})
	}
}
