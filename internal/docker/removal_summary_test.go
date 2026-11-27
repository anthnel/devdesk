package docker

import "testing"

// RemovalReport.Summary is the one footer line of a batch (OC-22): a part with a zero
// count is left out, and a refusal says why.
func TestRemovalSummary(t *testing.T) {
	removed := RemovalResult{Status: RemovalRemoved}
	gone := RemovalResult{Status: RemovalGone}
	running := RemovalResult{Status: RemovalRefused, Reason: ReasonRunning}
	inUse := RemovalResult{Status: RemovalRefused, Reason: ReasonInUse}
	other := RemovalResult{Status: RemovalRefused, Reason: ReasonOther}
	tests := []struct {
		name   string
		report RemovalReport
		want   string
	}{
		{"everything removed", RemovalReport{removed, removed, removed}, "3 removed"},
		{"one refusal explained", RemovalReport{removed, removed, running}, "2 removed, 1 not removed (running)"},
		{"two reasons", RemovalReport{running, inUse, inUse}, "3 not removed (running, in use)"},
		{"an unexplained refusal points to the logs", RemovalReport{removed, other}, "1 removed, 1 not removed (see logs)"},
		{"already gone", RemovalReport{removed, gone}, "1 removed, 1 already gone"},
		{"all three", RemovalReport{removed, running, gone}, "1 removed, 1 not removed (running), 1 already gone"},
		{"nothing", nil, "Nothing removed"},
	}
	for _, tt := range tests {
		t.Run(tt.name, func(t *testing.T) {
			if got := tt.report.Summary(); got != tt.want {
				t.Errorf("Summary() = %q, want %q", got, tt.want)
			}
		})
	}
}
