package fuzzy

import (
	"slices"
	"testing"

	"github.com/anthnel/devdesk/internal/ui/testutil"
)

// frozenLabels is a fixed candidate set for TestFuzzyRankingIsFrozen.
var frozenLabels = []string{
	"workspaces/devdesk", "docs/data-env", "a/zdevz", "b/devops", "devdesk",
	"xdev/dev", "tools/dev-env/dk", "Work/DevDesk", "src/d/e/v", "dev",
	"docs/devdesk", "dd/ee/vv",
}

// DEVDESK-678 criterion 7: the fuzzy results and their order are the ones the
// prompt gave before the exact mode existed. The expectations below were read
// by running the code as it was before the story; this test must stay green
// through the refactoring of rematch and must never be edited to follow it.
func TestFuzzyRankingIsFrozen(t *testing.T) {
	tests := []struct {
		query string
		want  []string
	}{
		{"dev", []string{"devdesk", "dev", "dd/ee/vv", "b/devops", "src/d/e/v", "Work/DevDesk", "xdev/dev", "tools/dev-env/dk", "a/zdevz", "docs/devdesk", "workspaces/devdesk", "docs/data-env"}},
		{"DEV", []string{"devdesk", "dev", "dd/ee/vv", "b/devops", "src/d/e/v", "Work/DevDesk", "xdev/dev", "tools/dev-env/dk", "a/zdevz", "docs/devdesk", "workspaces/devdesk", "docs/data-env"}},
		{"wsd", []string{"workspaces/devdesk"}},
		{"ddk", []string{"devdesk", "docs/devdesk", "tools/dev-env/dk", "Work/DevDesk", "workspaces/devdesk"}},
		{"devdesk", []string{"devdesk", "Work/DevDesk", "docs/devdesk", "workspaces/devdesk"}},
		{"dvd", []string{"devdesk", "xdev/dev", "Work/DevDesk", "docs/devdesk", "tools/dev-env/dk", "workspaces/devdesk"}},
		{"d/e", []string{"src/d/e/v", "dd/ee/vv", "docs/devdesk", "docs/data-env", "xdev/dev"}},
		{"xy", nil}, // below MinQueryLen: no query runs
	}
	cands := make([]Candidate, len(frozenLabels))
	for i, l := range frozenLabels {
		cands[i] = Candidate{Key: "/k/" + l, Label: l}
	}
	for _, tc := range tests {
		t.Run(tc.query, func(t *testing.T) {
			f := New("Loading")
			f.SetCandidates(cands, 0)
			for _, k := range testutil.Type(tc.query) {
				f, _ = f.Update(k)
			}
			var got []string
			for _, c := range f.results.Items() {
				got = append(got, c.Label)
			}
			if !slices.Equal(got, tc.want) {
				t.Errorf("fuzzy results for %q = %q, want %q", tc.query, got, tc.want)
			}
		})
	}
}
