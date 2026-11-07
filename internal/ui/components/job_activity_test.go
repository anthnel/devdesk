package components

import (
	"strings"
	"testing"
	"time"

	"github.com/anthnel/devdesk/internal/jobs"
	"github.com/anthnel/devdesk/internal/ui/indicator"
)

func TestRunIndicator(t *testing.T) {
	want := map[jobs.RunState]indicator.State{
		jobs.RunQueued:    indicator.Queued,
		jobs.RunRunning:   indicator.Running,
		jobs.RunDone:      indicator.Done,
		jobs.RunFailed:    indicator.Failed,
		jobs.RunCancelled: indicator.Cancelled,
	}
	seen := map[indicator.State]bool{}
	for rs, st := range want {
		got := RunIndicator(rs)
		if got != st {
			t.Errorf("RunIndicator(%q) = %v, want %v", rs, got, st)
		}
		if got == indicator.Idle || seen[got] {
			t.Errorf("RunIndicator(%q) = %v: Idle or not distinct", rs, got)
		}
		seen[got] = true
	}
}

func TestItemIndicator(t *testing.T) {
	want := map[jobs.ItemState]indicator.State{
		jobs.ItemQueued:  indicator.Queued,
		jobs.ItemRunning: indicator.Running,
		jobs.ItemDone:    indicator.Done,
		jobs.ItemSkipped: indicator.Skipped,
		jobs.ItemFailed:  indicator.Failed,
	}
	seen := map[indicator.State]bool{}
	for is, st := range want {
		got := ItemIndicator(is)
		if got != st {
			t.Errorf("ItemIndicator(%q) = %v, want %v", is, got, st)
		}
		if got == indicator.Idle || seen[got] {
			t.Errorf("ItemIndicator(%q) = %v: Idle or not distinct", is, got)
		}
		seen[got] = true
	}
}

// jobRun builds a run of kind on label with one item per state given.
func jobRun(kind jobs.Kind, label string, states ...jobs.ItemState) jobs.Run {
	targets := make([]string, len(states))
	for i := range states {
		targets[i] = label + "/t" + string(rune('a'+i))
	}
	r := jobs.NewRun(kind, "", rowCtx, label, targets...)
	for i, s := range states {
		r.Items[i].State = s
	}
	return r
}

func withFailure(r jobs.Run, idx int, detail string) jobs.Run {
	r.Items[idx].State = jobs.ItemFailed
	r.Items[idx].Detail = detail
	return r
}

func TestDescribeRun(t *testing.T) {
	isolateRowHome(t)
	const app = "/home/u/w/app"
	now := time.Now()

	done2 := jobRun(jobs.KindScan, app, jobs.ItemDone)
	done2.EndedAt = now.Add(-2 * time.Minute)
	doneNow := jobRun(jobs.KindScan, app, jobs.ItemDone)
	doneNow.EndedAt = now.Add(-10 * time.Second)
	doneZero := jobRun(jobs.KindScan, app, jobs.ItemDone)
	failOne := withFailure(jobRun(jobs.KindScan, app, jobs.ItemDone, jobs.ItemDone), 1, "scan failed: db")
	failMany := withFailure(withFailure(jobRun(jobs.KindScan, app, jobs.ItemDone, jobs.ItemDone, jobs.ItemDone), 0, "a"), 1, "b")
	failNoDetail := withFailure(jobRun(jobs.KindScan, app, jobs.ItemDone), 0, "")
	bidi := withFailure(jobRun(jobs.KindScan, app, jobs.ItemDone), 0, "line1\nline2\u202e x")

	// A cancelled run can only be reached through the registry.
	reg := jobs.New()
	id := reg.Start(jobs.NewRun(jobs.KindScan, "", rowCtx, app, app+"/ta"))
	if !reg.Cancel(id) {
		t.Fatal("Cancel refused")
	}
	snap := reg.Snapshot()
	if len(snap) != 1 || snap[0].State() != jobs.RunCancelled {
		t.Fatalf("fixture is not a cancelled run: %+v", snap)
	}

	tests := []struct {
		name  string
		run   jobs.Run
		state indicator.State
		want  string
	}{
		{"running", jobRun(jobs.KindScan, app, jobs.ItemRunning, jobs.ItemQueued), indicator.Running, "● Scanning ~/w/app"},
		{"queued", jobRun(jobs.KindScan, app, jobs.ItemQueued), indicator.Queued, "◴ Queued: scan ~/w/app"},
		{"done 2 min ago", done2, indicator.Done, "◉ Scan of ~/w/app finished 2 min ago"},
		{"done under a minute", doneNow, indicator.Done, "◉ Scan of ~/w/app finished now"},
		{"done without end time", doneZero, indicator.Done, "◉ Scan of ~/w/app finished"},
		{"cancelled", snap[0], indicator.Cancelled, "⊘ Scan of ~/w/app was cancelled"},
		{"failed, one cause", failOne, indicator.Failed, "⊗ ~/w/app: last scan failed — db"},
		{"failed, several", failMany, indicator.Failed, "⊗ ~/w/app: last scan failed — 2 of 3 targets failed"},
		{"failed, no detail", failNoDetail, indicator.Failed, "⊗ ~/w/app: last scan failed"},
		{"failed, multi-line bidi", bidi, indicator.Failed, "⊗ ~/w/app: last scan failed — line1 line2 x"},
		{"pull capitalized", func() jobs.Run {
			r := jobRun(jobs.KindPull, "img", jobs.ItemDone)
			r.EndedAt = now.Add(-2 * time.Minute)
			return r
		}(), indicator.Done, "◉ Pull of img finished 2 min ago"},
	}
	for _, tt := range tests {
		t.Run(tt.name, func(t *testing.T) {
			got := DescribeRun(tt.run)
			if got.State != tt.state {
				t.Errorf("State = %v, want %v", got.State, tt.state)
			}
			if got.State != RunIndicator(tt.run.State()) {
				t.Errorf("State %v disagrees with RunIndicator(%v)", got.State, tt.run.State())
			}
			if s := got.Detail.String(); s != tt.want {
				t.Errorf("Detail = %q, want %q", s, tt.want)
			}
			if g := indicator.Glyph(tt.state, true); !strings.HasPrefix(got.Detail.String(), g) {
				t.Errorf("Detail does not start with glyph %q", g)
			}
		})
	}
}

func TestDescribeRunAgreesWithDescribeRow(t *testing.T) {
	isolateRowHome(t)
	for _, st := range []jobs.ItemState{jobs.ItemRunning, jobs.ItemQueued} {
		r := jobRun(jobs.KindScan, apiPath, st)
		r.Items[0].Target = apiPath
		row := DescribeRow(RowQuery{Runs: []jobs.Run{r}, Context: rowCtx, Target: apiPath})
		got := DescribeRun(r)
		if got.Detail.String() != row.Detail.String() {
			t.Errorf("%s: DescribeRun %q != DescribeRow %q", st, got.Detail.String(), row.Detail.String())
		}
	}
}

func TestDescribeItem(t *testing.T) {
	isolateRowHome(t)
	const a = "/home/u/w/a"
	item := func(s jobs.ItemState, detail string) jobs.Item {
		return jobs.Item{Target: a, State: s, Detail: detail}
	}
	tests := []struct {
		name  string
		kind  jobs.Kind
		it    jobs.Item
		state indicator.State
		want  string
	}{
		{"running", jobs.KindScan, item(jobs.ItemRunning, ""), indicator.Running, "● Scanning ~/w/a"},
		{"queued", jobs.KindScan, item(jobs.ItemQueued, ""), indicator.Queued, "◴ Queued: scan ~/w/a"},
		{"done", jobs.KindScan, item(jobs.ItemDone, ""), indicator.Done, "◉ Scan of ~/w/a finished"},
		{"skipped with detail", jobs.KindSync, item(jobs.ItemSkipped, "already there"), indicator.Skipped, "⊖ Sync of ~/w/a skipped — already there"},
		{"skipped without detail", jobs.KindSync, item(jobs.ItemSkipped, ""), indicator.Skipped, "⊖ Sync of ~/w/a skipped"},
		{"failed", jobs.KindScan, item(jobs.ItemFailed, "scan failed: db"), indicator.Failed, "⊗ ~/w/a: last scan failed — db"},
		{"failed multi-line", jobs.KindScan, item(jobs.ItemFailed, "x\ny\u202e"), indicator.Failed, "⊗ ~/w/a: last scan failed — x y"},
		{"skipped by a cancelled run", jobs.KindScan, item(jobs.ItemSkipped, "cancelled"), indicator.Skipped, "⊖ Scan of ~/w/a skipped — cancelled"},
		{"display name", jobs.KindPull, jobs.Item{Target: "reg/x:1", Display: "x:1", State: jobs.ItemDone}, indicator.Done, "◉ Pull of x:1 finished"},
	}
	for _, tt := range tests {
		t.Run(tt.name, func(t *testing.T) {
			got := DescribeItem(tt.kind, tt.it)
			if got.State != tt.state || got.State != ItemIndicator(tt.it.State) {
				t.Errorf("State = %v, want %v", got.State, tt.state)
			}
			if s := got.Detail.String(); s != tt.want {
				t.Errorf("Detail = %q, want %q", s, tt.want)
			}
		})
	}
}
