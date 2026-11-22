package fuzzy

import (
	"slices"
	"testing"
)

// rank is the pure ranking rematch delegates to: no table, no query field.
// It must not reorder or modify its input.
func TestRank(t *testing.T) {
	in := cands("a/zdevz", "b/devops", "devdesk", "xdev/dev", "docs/data-env")
	snapshot := slices.Clone(in)

	labelsOf := func(cs []Candidate) []string {
		var out []string
		for _, c := range cs {
			out = append(out, c.Label)
		}
		return out
	}

	tests := []struct {
		name  string
		query string
		mode  Mode
		want  []string
	}{
		{"exact", "dev", ModeExact, []string{"devdesk", "b/devops", "xdev/dev", "a/zdevz"}},
		{"exact no match", "wsd", ModeExact, nil},
		{"fuzzy", "dev", ModeFuzzy, []string{"devdesk", "b/devops", "xdev/dev", "a/zdevz", "docs/data-env"}},
		{"fuzzy no match", "qqq", ModeFuzzy, nil},
	}
	for _, tc := range tests {
		t.Run(tc.name, func(t *testing.T) {
			got := labelsOf(rank(in, tc.query, tc.mode))
			if !slices.Equal(got, tc.want) {
				t.Errorf("rank(%q, %v) = %q, want %q", tc.query, tc.mode, got, tc.want)
			}
		})
	}
	if !slices.Equal(in, snapshot) {
		t.Errorf("rank changed its input: %v, want %v", in, snapshot)
	}
}
