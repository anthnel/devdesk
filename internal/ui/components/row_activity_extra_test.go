package components

import (
	"testing"

	"github.com/anthnel/devdesk/internal/jobs"
	"github.com/anthnel/devdesk/internal/ui/indicator"
)

// TestRowExtraTargets covers RowQuery.Extra: a row that answers for several
// registry identities (a template: its slug and its scan directory).
func TestRowExtraTargets(t *testing.T) {
	isolateRowHome(t)
	const slug = "tpl-slug"
	extra := []RowTarget{{Target: apiPath}}
	otherCtx := rowFailure(jobs.KindScan, apiPath, "boom", 1)
	otherCtx.Context = "elsewhere"

	tests := []struct {
		name  string
		q     RowQuery
		state indicator.State
		want  string
	}{
		{"nil Extra is inert",
			RowQuery{Runs: []jobs.Run{rowRun(1, jobs.KindSync, slug, jobs.ItemRunning)}, Target: slug},
			indicator.Running, "● Syncing tpl-slug"},
		{"nil Extra, nothing at all",
			RowQuery{Target: slug}, indicator.Idle, ""},
		{"main idle, extra running",
			RowQuery{Runs: []jobs.Run{rowRun(1, jobs.KindScan, apiPath, jobs.ItemRunning)}, Target: slug, Extra: extra},
			indicator.Running, "● Scanning ~/w/app/api"},
		{"main queued, extra running: running describes the row",
			RowQuery{
				Runs: []jobs.Run{
					rowRun(1, jobs.KindSync, slug, jobs.ItemQueued),
					rowRun(2, jobs.KindScan, apiPath, jobs.ItemRunning),
				},
				Target: slug, Extra: extra,
			},
			indicator.Running, "● Scanning ~/w/app/api"},
		{"main failed older, extra failed newer: newest wins",
			RowQuery{
				Failures: []jobs.Failure{
					rowFailure(jobs.KindScan, slug, "old", 1),
					rowFailure(jobs.KindSync, apiPath, "new", 2),
				},
				Target: slug, Extra: extra,
			},
			indicator.Failed, "⊗ ~/w/app/api: last sync failed — new · earlier failure: scan (see :jobs)"},
		{"main running, extra failed: failure is listed",
			RowQuery{
				Runs:     []jobs.Run{rowRun(1, jobs.KindSync, slug, jobs.ItemRunning)},
				Failures: []jobs.Failure{rowFailure(jobs.KindScan, apiPath, "boom", 1)},
				Target:   slug, Extra: extra,
			},
			indicator.Running, "● Syncing tpl-slug · earlier failure: scan (see :jobs)"},
		{"extra kinds filter ignores a sync failure",
			RowQuery{
				Failures: []jobs.Failure{rowFailure(jobs.KindSync, apiPath, "boom", 1)},
				Target:   slug, Extra: []RowTarget{{Target: apiPath, Kinds: []jobs.Kind{jobs.KindScan}}},
			},
			indicator.Idle, ""},
		{"extra target in another context is ignored",
			RowQuery{Failures: []jobs.Failure{otherCtx}, Target: slug, Extra: extra},
			indicator.Idle, ""},
	}
	for _, tt := range tests {
		t.Run(tt.name, func(t *testing.T) {
			tt.q.Context = rowCtx
			got := DescribeRow(tt.q)
			if got.State != tt.state {
				t.Errorf("State = %v, want %v", got.State, tt.state)
			}
			if s := got.Detail.String(); s != tt.want {
				t.Errorf("Detail = %q, want %q", s, tt.want)
			}
			if rs := RowState(tt.q); rs != got.State {
				t.Errorf("RowState = %v, DescribeRow.State = %v", rs, got.State)
			}
		})
	}
}
