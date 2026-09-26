package workspaces

import (
	"testing"

	"github.com/anthnel/devdesk/internal/ui/theme"
)

func TestTheGitStatusCellFollowsStarship(t *testing.T) {
	tests := []struct {
		name  string
		entry Entry
		want  string
	}{
		{"clean and level", Entry{GitBranch: "main"}, "main"},
		{"working tree", Entry{GitBranch: "main", GitModified: 3, GitUntracked: 2}, "main !3 ?2"},
		{"ahead", Entry{GitBranch: "main", GitUnpushed: 1}, "main ⇡1"},
		{"behind", Entry{GitBranch: "main", GitUnpulled: 4}, "main ⇣4"},
		{"diverged", Entry{GitBranch: "main", GitUnpushed: 2, GitUnpulled: 5}, "main ⇕⇡2⇣5"},
		{"no upstream", Entry{GitBranch: "feature", GitNoUpstream: true}, "feature ⊘"},
		{"everything", Entry{GitBranch: "main", GitModified: 1, GitUntracked: 1, GitUnpulled: 2}, "main !1 ?1 ⇣2"},
		{"not a repository", Entry{}, ""},
	}
	for _, tt := range tests {
		t.Run(tt.name, func(t *testing.T) {
			if got := formatGitStatus(tt.entry); got != tt.want {
				t.Errorf("formatGitStatus() = %q, want %q", got, tt.want)
			}
		})
	}
}

// The split is at a rune index, which only holds when every marker is one cell
// wide — a Nerd Font glyph renders double on some terminals and would shift the
// colour boundary into the branch name.
func TestEveryGitMarkerIsOneCellWide(t *testing.T) {
	for _, mark := range []string{gitMarkModified, gitMarkUntracked, gitMarkAhead,
		gitMarkBehind, gitMarkDiverged, gitMarkNoUpstream} {
		if r := []rune(mark); len(r) != 1 || r[0] >= 0xE000 {
			t.Errorf("marker %q is not a single non-private-use rune", mark)
		}
	}
}

// Only the markers carry the warning, and only for what F would refuse.
func TestTheBranchStaysPlainAndTheMarkersWarnOnlyWhenSyncWouldRefuse(t *testing.T) {
	m := loadedModel(t)
	rows := m.rowsFor([]Entry{
		{Path: "/a", IsGitRepo: true, GitBranch: "main", GitModified: 1},
		{Path: "/b", IsGitRepo: true, GitBranch: "main", GitUnpulled: 3},
		{Path: "/c", IsGitRepo: true, GitBranch: "main", GitUnpushed: 1, GitUnpulled: 1},
	})
	warn := theme.StatusWarningStyle.GetForeground()

	if got := rows[0].branchCut; got != len("main") {
		t.Errorf("branchCut = %d, want the branch's %d runes", got, len("main"))
	}
	if gitBranchStyle(rows[0]).GetForeground() == warn {
		t.Error("the branch name is coloured as a warning")
	}
	for i, wantWarn := range []bool{true, false, true} {
		if got := gitMarkersStyle(rows[i]).GetForeground() == warn; got != wantWarn {
			t.Errorf("row %s: markers warn = %v, want %v", rows[i].Entry.Path, got, wantWarn)
		}
	}
}
