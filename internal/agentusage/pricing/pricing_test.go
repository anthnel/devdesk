package pricing

import (
	"strings"
	"testing"

	"gopkg.in/yaml.v3"

	"github.com/anthnel/devdesk/internal/config"
)

// prices decodes a YAML mapping of model id -> rates the way the config
// loader does, since config.Rate can only be built by decoding.
func prices(t *testing.T, doc string) map[string]config.ModelPrice {
	t.Helper()
	var m map[string]config.ModelPrice
	if err := yaml.Unmarshal([]byte(doc), &m); err != nil {
		t.Fatalf("fixture YAML: %v", err)
	}
	return m
}

// entry renders a complete price entry with the given key, then lets the
// caller override single lines.
func entry(key string, lines ...string) string {
	if len(lines) == 0 {
		lines = []string{"input: 1", "output: 5", "cache_write_5m: 1.25", "cache_write_1h: 2", "cache_read: 0.1"}
	}
	return key + ":\n  " + strings.Join(lines, "\n  ") + "\n"
}

func problemByKey(ps []Problem) map[string][]string {
	out := map[string][]string{}
	for _, p := range ps {
		out[p.Key] = append(out[p.Key], p.Reason)
	}
	return out
}

func TestFromConfigAcceptsACompleteEntry(t *testing.T) {
	tbl, probs := FromConfig(prices(t, entry("claude-x")))
	if len(probs) != 0 {
		t.Fatalf("problems = %v, want none", probs)
	}
	if tbl.Len() != 1 {
		t.Fatalf("Len = %d, want 1", tbl.Len())
	}
	got, ok := tbl.Lookup("claude-x")
	if !ok {
		t.Fatal("claude-x not found")
	}
	want := Rates{Input: 1, Output: 5, CacheWrite5m: 1.25, CacheWrite1h: 2, CacheRead: 0.1}
	if got != want {
		t.Errorf("rates = %+v, want %+v", got, want)
	}
}

func TestFromConfigRejectsAnUnusableEntryWithoutAffectingOthers(t *testing.T) {
	tests := []struct {
		name       string
		lines      []string
		wantReason string // substring; "" = any non-empty reason
	}{
		{"missing input", []string{"output: 5", "cache_write_5m: 1", "cache_write_1h: 2", "cache_read: 0.1"}, "input"},
		{"missing output", []string{"input: 1", "cache_write_5m: 1", "cache_write_1h: 2", "cache_read: 0.1"}, "output"},
		{"missing cache_write_5m", []string{"input: 1", "output: 5", "cache_write_1h: 2", "cache_read: 0.1"}, "cache_write_5m"},
		{"missing cache_write_1h", []string{"input: 1", "output: 5", "cache_write_5m: 1", "cache_read: 0.1"}, "cache_write_1h"},
		{"missing cache_read", []string{"input: 1", "output: 5", "cache_write_5m: 1", "cache_write_1h: 2"}, "cache_read"},
		{"null rate counts as missing", []string{"input: 1", "output: 5", "cache_write_5m: 1", "cache_write_1h: 2", "cache_read: null"}, "cache_read"},
		{"text rate", []string{"input: \"5$\"", "output: 5", "cache_write_5m: 1", "cache_write_1h: 2", "cache_read: 0.1"}, "input"},
		{"list rate", []string{"input: 1", "output: [1, 2]", "cache_write_5m: 1", "cache_write_1h: 2", "cache_read: 0.1"}, "output"},
		{"negative", []string{"input: 1", "output: 5", "cache_write_5m: -1", "cache_write_1h: 2", "cache_read: 0.1"}, "cache_write_5m"},
		{"NaN", []string{"input: .nan", "output: 5", "cache_write_5m: 1", "cache_write_1h: 2", "cache_read: 0.1"}, "input"},
		{"+Inf", []string{"input: 1", "output: .inf", "cache_write_5m: 1", "cache_write_1h: 2", "cache_read: 0.1"}, "output"},
		{"-Inf", []string{"input: 1", "output: 5", "cache_write_5m: 1", "cache_write_1h: -.inf", "cache_read: 0.1"}, "cache_write_1h"},
	}
	for _, tc := range tests {
		t.Run(tc.name, func(t *testing.T) {
			doc := entry("bad", tc.lines...) + entry("good")
			tbl, probs := FromConfig(prices(t, doc))

			if _, ok := tbl.Lookup("bad"); ok {
				t.Error("unusable entry is in the table")
			}
			if _, ok := tbl.Lookup("good"); !ok {
				t.Error("a bad neighbour took the good entry down")
			}
			if tbl.Len() != 1 {
				t.Errorf("Len = %d, want 1", tbl.Len())
			}
			byKey := problemByKey(probs)
			if len(probs) != 1 || len(byKey["bad"]) != 1 {
				t.Fatalf("problems = %+v, want exactly one for key %q", probs, "bad")
			}
			reason := byKey["bad"][0]
			if reason == "" || !strings.Contains(reason, tc.wantReason) {
				t.Errorf("reason = %q, want it to name %q", reason, tc.wantReason)
			}
		})
	}
}

func TestFromConfigMissingRateReasonIsExact(t *testing.T) {
	_, probs := FromConfig(prices(t, entry("x", "input: 1", "output: 5", "cache_write_5m: 1", "cache_write_1h: 2")))
	if len(probs) != 1 || probs[0].Key != "x" || probs[0].Reason != "incomplete: missing cache_read" {
		t.Errorf("problems = %+v, want {x, \"incomplete: missing cache_read\"}", probs)
	}
}

func TestFromConfigZeroIsAValidExplicitRate(t *testing.T) {
	tbl, probs := FromConfig(prices(t, entry("free", "input: 0", "output: 0", "cache_write_5m: 0", "cache_write_1h: 0", "cache_read: 0")))
	if len(probs) != 0 {
		t.Fatalf("problems = %v, want none", probs)
	}
	got, ok := tbl.Lookup("free")
	if !ok || got != (Rates{}) {
		t.Errorf("Lookup(free) = %+v, %v; want zero rates, true", got, ok)
	}
}

func TestFromConfigEmptyAndNil(t *testing.T) {
	for name, in := range map[string]map[string]config.ModelPrice{"nil": nil, "empty": {}} {
		t.Run(name, func(t *testing.T) {
			tbl, probs := FromConfig(in)
			if tbl.Len() != 0 || len(probs) != 0 {
				t.Errorf("Len = %d, problems = %v; want 0, none", tbl.Len(), probs)
			}
			if _, ok := tbl.Lookup("anything"); ok {
				t.Error("empty table found a model")
			}
		})
	}
}

func TestFromConfigDropsBothKeysThatCollideAfterNormalisation(t *testing.T) {
	doc := entry("\"Claude-X \"") + entry("claude-x", "input: 9", "output: 9", "cache_write_5m: 9", "cache_write_1h: 9", "cache_read: 9") + entry("other")
	tbl, probs := FromConfig(prices(t, doc))

	if _, ok := tbl.Lookup("claude-x"); ok {
		t.Error("a colliding key survived: choosing one would be a guess")
	}
	if _, ok := tbl.Lookup("other"); !ok {
		t.Error("an unrelated entry was dropped")
	}
	if tbl.Len() != 1 {
		t.Errorf("Len = %d, want 1", tbl.Len())
	}
	byKey := problemByKey(probs)
	if len(probs) != 2 || len(byKey["Claude-X "]) != 1 || len(byKey["claude-x"]) != 1 {
		t.Fatalf("problems = %+v, want one per colliding key, with the user's own spelling", probs)
	}
	for k, rs := range byKey {
		if rs[0] == "" {
			t.Errorf("empty reason for %q", k)
		}
	}
}

func TestLookup(t *testing.T) {
	doc := entry("claude-sonnet-5") +
		entry("claude-sonnet-5-5", "input: 3", "output: 15", "cache_write_5m: 3.75", "cache_write_1h: 6", "cache_read: 0.3") +
		entry("Claude-Haiku-4-5") +
		entry("claude-opus-4-1-20250805", "input: 15", "output: 75", "cache_write_5m: 18.75", "cache_write_1h: 30", "cache_read: 1.5") +
		entry("claude-opus-4-1")
	tbl, probs := FromConfig(prices(t, doc))
	if len(probs) != 0 {
		t.Fatalf("problems = %v", probs)
	}
	sonnet5, _ := tbl.Lookup("claude-sonnet-5")
	sonnet55, _ := tbl.Lookup("claude-sonnet-5-5")
	datedOpus, _ := tbl.Lookup("claude-opus-4-1-20250805")
	plainOpus, _ := tbl.Lookup("claude-opus-4-1")

	tests := []struct {
		name  string
		model string
		found bool
		want  *Rates // nil = do not check the rates
	}{
		{"exact", "claude-sonnet-5", true, &sonnet5},
		{"exact, the longer sibling", "claude-sonnet-5-5", true, &sonnet55},
		{"case-insensitive", "CLAUDE-SONNET-5", true, &sonnet5},
		{"key written with capitals", "claude-haiku-4-5", true, nil},
		{"trimmed", "  claude-sonnet-5\t", true, &sonnet5},
		{"dated snapshot falls back to the undated key", "claude-haiku-4-5-20251001", true, nil},
		{"dated, upper case", "CLAUDE-HAIKU-4-5-20251001", true, nil},
		{"exact dated key wins over the fallback", "claude-opus-4-1-20250805", true, &datedOpus},
		{"another date uses the undated key", "claude-opus-4-1-20991231", true, &plainOpus},
		{"suffix that is not 8 digits", "claude-haiku-4-5-2025", false, nil},
		{"suffix of 9 digits", "claude-haiku-4-5-202510011", false, nil},
		{"suffix that is not digits", "claude-haiku-4-5-2025100a", false, nil},
		{"only one date is removed", "claude-haiku-4-5-20251001-20251002", false, nil},
		{"no prefix match: sonnet-5 does not price sonnet-5-5-x", "claude-sonnet-5-5-x", false, nil},
		{"no prefix match: undated key vs longer model", "claude-sonnet-5-6", false, nil},
		{"no prefix match: key is a prefix of the model", "claude-sonnet", false, nil},
		{"1m variant is not stripped", "claude-sonnet-5[1m]", false, nil},
		{"provider spelling is not stripped", "us.anthropic.claude-sonnet-5-v1:0", false, nil},
		{"provider @ date is not stripped", "claude-sonnet-5@20251001", false, nil},
		{"unknown", "gpt-9", false, nil},
		{"empty id", "", false, nil},
		{"blank id", "   ", false, nil},
	}
	for _, tc := range tests {
		t.Run(tc.name, func(t *testing.T) {
			got, ok := tbl.Lookup(tc.model)
			if ok != tc.found {
				t.Fatalf("Lookup(%q) found = %v, want %v", tc.model, ok, tc.found)
			}
			if !ok && got != (Rates{}) {
				t.Errorf("not found but rates = %+v", got)
			}
			if tc.want != nil && got != *tc.want {
				t.Errorf("Lookup(%q) = %+v, want %+v", tc.model, got, *tc.want)
			}
		})
	}
	if sonnet5 == sonnet55 {
		t.Fatal("fixture error: the two sonnet entries must differ for the prefix test to mean anything")
	}
}

func TestLookupKeyWrittenWithPaddingAndCaps(t *testing.T) {
	tbl, _ := FromConfig(prices(t, entry("\"  Claude-X \"")))
	if _, ok := tbl.Lookup("claude-x"); !ok {
		t.Error("config keys must be normalised the same way as the model id")
	}
}

func TestLookupZeroTable(t *testing.T) {
	var tbl Table
	if _, ok := tbl.Lookup("claude-x"); ok || tbl.Len() != 0 {
		t.Error("the zero Table must be a valid empty table")
	}
}
