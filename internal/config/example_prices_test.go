package config

import (
	"os"
	"path/filepath"
	"regexp"
	"strings"
	"testing"

	"gopkg.in/yaml.v3"
)

// pinnedMarker is the frozen first line of the example block. It is repeated
// here on purpose: files on disk are recognized by it, so changing the
// constant must break this test.
const pinnedMarker = "# Optional: price table for the :agent-usage cost estimate."

func blockLines() []string {
	return strings.Split(strings.TrimSuffix(ExamplePricesBlock, "\n"), "\n")
}

func TestExamplePricesBlock_Shape(t *testing.T) {
	if !strings.HasSuffix(ExamplePricesBlock, "\n") {
		t.Error("block must end with a newline")
	}
	active := regexp.MustCompile(`^\s*(agent_usage|prices):`)
	for i, line := range blockLines() {
		if !strings.HasPrefix(line, "#") {
			t.Errorf("line %d is not a comment: %q", i+1, line)
		}
		if active.MatchString(line) {
			t.Errorf("line %d is an active key: %q", i+1, line)
		}
	}
	for _, want := range []string{
		"claude-example-model",
		"input: 3", "output: 15", "cache_write_5m: 3.75", "cache_write_1h: 6", "cache_read: 0.3",
		"USD per million tokens",
		"price page",
	} {
		if !strings.Contains(ExamplePricesBlock, want) {
			t.Errorf("block lacks %q", want)
		}
	}
}

func TestExamplePricesMarker_IsFrozenAndFirstLine(t *testing.T) {
	if examplePricesMarker != pinnedMarker {
		t.Errorf("marker changed: %q", examplePricesMarker)
	}
	if got := blockLines()[0]; got != pinnedMarker {
		t.Errorf("first line of the block = %q, want the marker", got)
	}
}

func TestExamplePricesBlock_UncommentedLoads(t *testing.T) {
	setupTmpHome(t)

	var out []string
	started := false
	for _, line := range blockLines() {
		if line == "# agent_usage:" {
			started = true
		}
		if !started {
			continue
		}
		line = strings.TrimPrefix(line, "#")
		line = strings.TrimPrefix(line, " ")
		out = append(out, line)
	}
	if !started {
		t.Fatal("block has no '# agent_usage:' line")
	}

	path, err := GetContextPath("default")
	if err != nil {
		t.Fatal(err)
	}
	data, err := yaml.Marshal(Default())
	if err != nil {
		t.Fatal(err)
	}
	if err := os.MkdirAll(filepath.Dir(path), 0o700); err != nil {
		t.Fatal(err)
	}
	if err := os.WriteFile(path, append(data, []byte("\n"+strings.Join(out, "\n")+"\n")...), 0o600); err != nil {
		t.Fatal(err)
	}

	cfg, err := LoadContext("default")
	if err != nil {
		t.Fatalf("LoadContext: %v", err)
	}
	if len(cfg.AgentUsage.Prices) != 1 {
		t.Fatalf("want 1 price entry, got %d", len(cfg.AgentUsage.Prices))
	}
	p, ok := cfg.AgentUsage.Prices["claude-example-model"]
	if !ok {
		t.Fatal("entry claude-example-model missing")
	}
	for name, c := range map[string]struct {
		r    Rate
		want float64
	}{
		"input": {p.Input, 3}, "output": {p.Output, 15}, "cache_write_5m": {p.CacheWrite5m, 3.75},
		"cache_write_1h": {p.CacheWrite1h, 6}, "cache_read": {p.CacheRead, 0.3},
	} {
		v, st := c.r.Value()
		if st != RateSet || v != c.want {
			t.Errorf("%s = %v (state %v), want %v set", name, v, st, c.want)
		}
	}
}

func TestCarriesExampleBlock(t *testing.T) {
	cases := []struct {
		name string
		data string
		want bool
	}{
		{"block at the end", "version: 1\n\n" + ExamplePricesBlock, true},
		{"block alone", ExamplePricesBlock, true},
		{"marker with CRLF", "a: 1\r\n" + pinnedMarker + "\r\n# more\r\n", true},
		{"marker with trailing spaces", "a: 1\n" + pinnedMarker + "  \t\n", true},
		{"marker without final newline", "a: 1\n" + pinnedMarker, true},
		{"marker preceded by spaces", "a: 1\n  " + pinnedMarker + "\n", false},
		{"marker as substring of another line", "a: 1\n# note: " + pinnedMarker + " yes\n", false},
		{"marker as prefix of a longer line", "a: 1\n" + pinnedMarker + " more\n", false},
		{"first line modified", "a: 1\n# Optional: price table for the :agent-usage cost estimate!\n", false},
		{"empty", "", false},
	}
	for _, c := range cases {
		t.Run(c.name, func(t *testing.T) {
			if got := carriesExampleBlock([]byte(c.data)); got != c.want {
				t.Errorf("carriesExampleBlock = %v, want %v", got, c.want)
			}
		})
	}
}

func TestMarshalContext_Example(t *testing.T) {
	plain, err := yaml.Marshal(Default())
	if err != nil {
		t.Fatal(err)
	}

	withEntry := Default()
	withEntry.AgentUsage.Prices = map[string]ModelPrice{"m": {}} // incomplete entry, still an entry
	withEntryYAML, err := yaml.Marshal(withEntry)
	if err != nil {
		t.Fatal(err)
	}

	invalid := Default()
	invalid.AgentUsage.Prices = map[string]ModelPrice{"m": {Input: Rate{state: RateInvalid, raw: "5$"}}}
	invalidYAML, err := yaml.Marshal(invalid)
	if err != nil {
		t.Fatal(err)
	}

	cases := []struct {
		name    string
		cfg     *Config
		with    bool
		want    string
		markers int
	}{
		{"empty prices, example on", Default(), true, string(plain) + "\n" + ExamplePricesBlock, 1},
		{"empty prices, example off", Default(), false, string(plain), 0},
		{"one entry, example on", withEntry, true, string(withEntryYAML), 0},
		{"invalid rate, example on", invalid, true, string(invalidYAML), 0},
	}
	for _, c := range cases {
		t.Run(c.name, func(t *testing.T) {
			got, err := marshalContext(c.cfg, c.with)
			if err != nil {
				t.Fatal(err)
			}
			if string(got) != c.want {
				t.Errorf("output differs from expectation:\n%s", got)
			}
			if n := strings.Count(string(got), pinnedMarker); n != c.markers {
				t.Errorf("marker count = %d, want %d", n, c.markers)
			}
		})
	}
}
