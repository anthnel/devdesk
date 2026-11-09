package security

import (
	"slices"
	"strings"
	"testing"
	"time"

	"github.com/anthnel/devdesk/internal/command"
	"github.com/anthnel/devdesk/internal/jobs"
	"github.com/anthnel/devdesk/internal/ui/indicator"
)

// DEVDESK-145 step 1: what one inventory row says about its own activity. The
// state is read from the registry snapshot (runs + remembered failures) the
// router hands the view, against the context the snapshot carries
// (ChangedMsg.Context, decision Q1=B).

const (
	secCtx   = "default"
	secRepo  = "/w/api"
	secImage = "nginx:1.27"
)

func secRun(kind jobs.Kind, ctx string, state jobs.ItemState, target string) jobs.Run {
	run := jobs.NewRun(kind, command.ViewSecurity, ctx, "inventory", target)
	run.Items[0].State = state
	return run
}

func secFailure(kind jobs.Kind, ctx, target string, at time.Time) jobs.Failure {
	return jobs.Failure{
		FailureKey: jobs.FailureKey{Kind: kind, Target: jobs.TargetKey(target), Context: ctx},
		Display:    target, Detail: "scan failed — check logs", At: at,
	}
}

func secSnapshot(t *testing.T, ctx string, runs []jobs.Run, failures []jobs.Failure) Model {
	t.Helper()
	m := inventoryModel(t)
	return feed(t, m, jobs.ChangedMsg{Runs: runs, Failures: failures, Context: ctx})
}

func TestRowActivity(t *testing.T) {
	t0 := time.Date(2026, 11, 1, 10, 0, 0, 0, time.UTC)
	repo := scanTarget{Kind: kindRepo, Name: secRepo}
	tests := []struct {
		name      string
		target    scanTarget
		runs      []jobs.Run
		failures  []jobs.Failure
		wantState indicator.State
		wantInfo  []string
		wantZero  bool
	}{
		{
			name: "running scan", target: repo,
			runs:      []jobs.Run{secRun(jobs.KindScan, secCtx, jobs.ItemRunning, secRepo)},
			wantState: indicator.Running, wantInfo: []string{"Scanning", secRepo},
		},
		{
			name: "queued scan", target: repo,
			runs:      []jobs.Run{secRun(jobs.KindScan, secCtx, jobs.ItemQueued, secRepo)},
			wantState: indicator.Queued, wantInfo: []string{"Queued", "scan", secRepo},
		},
		{
			name: "scan failure alone", target: repo,
			failures:  []jobs.Failure{secFailure(jobs.KindScan, secCtx, secRepo, t0)},
			wantState: indicator.Failed, wantInfo: []string{": last scan failed — check logs"},
		},
		{
			name: "failure and running scan: running wins, failure is masked", target: repo,
			runs:      []jobs.Run{secRun(jobs.KindScan, secCtx, jobs.ItemRunning, secRepo)},
			failures:  []jobs.Failure{secFailure(jobs.KindScan, secCtx, secRepo, t0)},
			wantState: indicator.Running, wantInfo: []string{"· earlier failure: scan (see :jobs)"},
		},
		{
			name: "failure in another context is invisible", target: repo,
			failures:  []jobs.Failure{secFailure(jobs.KindScan, "other", secRepo, t0)},
			wantState: indicator.Idle, wantZero: true,
		},
		{
			name: "sync failure is not a scan failure (D5)", target: repo,
			failures:  []jobs.Failure{secFailure(jobs.KindSync, secCtx, secRepo, t0)},
			wantState: indicator.Idle, wantZero: true,
		},
		{
			name: "pull failure is not a scan failure (D5)", target: repo,
			failures:  []jobs.Failure{secFailure(jobs.KindPull, secCtx, secRepo, t0)},
			wantState: indicator.Idle, wantZero: true,
		},
		{
			name: "image failure on its exact reference", target: scanTarget{Kind: kindImage, Name: secImage},
			failures:  []jobs.Failure{secFailure(jobs.KindScan, secCtx, secImage, t0)},
			wantState: indicator.Failed,
		},
		{
			name: "image failure on the bare name does not bridge", target: scanTarget{Kind: kindImage, Name: secImage},
			failures:  []jobs.Failure{secFailure(jobs.KindScan, secCtx, "nginx", t0)},
			wantState: indicator.Idle, wantZero: true,
		},
		{
			name: "trailing slash is normalized", target: scanTarget{Kind: kindRepo, Name: secRepo + "/"},
			failures:  []jobs.Failure{secFailure(jobs.KindScan, secCtx, secRepo, t0)},
			wantState: indicator.Failed,
		},
		{
			name: "done and skipped items are idle", target: repo,
			runs: []jobs.Run{
				secRun(jobs.KindScan, secCtx, jobs.ItemDone, secRepo),
				secRun(jobs.KindScan, secCtx, jobs.ItemSkipped, secRepo),
			},
			wantState: indicator.Idle, wantZero: true,
		},
	}
	for _, tt := range tests {
		t.Run(tt.name, func(t *testing.T) {
			m := secSnapshot(t, secCtx, tt.runs, tt.failures)
			got := m.rowActivity(tt.target)
			if got.State != tt.wantState {
				t.Fatalf("State = %v, want %v (detail %q)", got.State, tt.wantState, got.Detail.String())
			}
			if tt.wantZero && !got.Detail.IsZero() {
				t.Errorf("Detail = %q, want none", got.Detail.String())
			}
			for _, s := range tt.wantInfo {
				if !strings.Contains(got.Detail.String(), s) {
					t.Errorf("Detail %q lacks %q", got.Detail.String(), s)
				}
			}
		})
	}
}

func TestScanRunning(t *testing.T) {
	tests := []struct {
		name string
		runs []jobs.Run
		want bool
	}{
		{"running scan", []jobs.Run{secRun(jobs.KindScan, secCtx, jobs.ItemRunning, secRepo)}, true},
		{"queued scan is not running (H1)", []jobs.Run{secRun(jobs.KindScan, secCtx, jobs.ItemQueued, secRepo)}, false},
		{"running scan in another context", []jobs.Run{secRun(jobs.KindScan, "other", jobs.ItemRunning, secRepo)}, false},
		{"running sync", []jobs.Run{secRun(jobs.KindSync, secCtx, jobs.ItemRunning, secRepo)}, false},
		{"done scan", []jobs.Run{secRun(jobs.KindScan, secCtx, jobs.ItemDone, secRepo)}, false},
		{"no run", nil, false},
	}
	for _, tt := range tests {
		t.Run(tt.name, func(t *testing.T) {
			m := secSnapshot(t, secCtx, tt.runs, nil)
			if got := m.scanRunning(secRepo); got != tt.want {
				t.Errorf("scanRunning = %v, want %v", got, tt.want)
			}
		})
	}
}

func TestSecKindsAreScanOnly(t *testing.T) {
	if want := []jobs.Kind{jobs.KindScan}; !slices.Equal(secKinds, want) {
		t.Errorf("secKinds = %v, want %v", secKinds, want)
	}
}

// A view built and not yet fed has no context, which matches no run.
func TestAViewNotYetFedMatchesNoRun(t *testing.T) {
	m := inventoryModel(t)
	if got := m.contextName; got != "" {
		t.Errorf("contextName before any snapshot = %q, want empty", got)
	}
	if m.scanRunning(secRepo) {
		t.Error("scanRunning is true on a view that was never fed")
	}
}

func TestJobsChangedStoresTheContext(t *testing.T) {
	m := secSnapshot(t, "prod", nil, nil)
	if m.contextName != "prod" {
		t.Errorf("contextName = %q, want prod", m.contextName)
	}
	m = feed(t, m, jobs.ChangedMsg{Context: "dev"})
	if m.contextName != "dev" {
		t.Errorf("contextName after a second snapshot = %q, want dev", m.contextName)
	}
}

func TestJobsChangedStoresACopyOfTheFailures(t *testing.T) {
	t0 := time.Date(2026, 11, 1, 10, 0, 0, 0, time.UTC)
	failures := []jobs.Failure{secFailure(jobs.KindScan, secCtx, secRepo, t0)}
	m := secSnapshot(t, secCtx, nil, failures)

	failures[0].Kind = jobs.KindSync // the router's slice is reused after the send
	failures[0].Target = "/elsewhere"

	if got := m.rowActivity(scanTarget{Kind: kindRepo, Name: secRepo}).State; got != indicator.Failed {
		t.Errorf("State after the message slice was mutated = %v, want Failed", got)
	}
}

func TestJobsChangedReplacesFailuresRatherThanMerging(t *testing.T) {
	t0 := time.Date(2026, 11, 1, 10, 0, 0, 0, time.UTC)
	m := secSnapshot(t, secCtx, nil, []jobs.Failure{secFailure(jobs.KindScan, secCtx, secRepo, t0)})
	m = feed(t, m, jobs.ChangedMsg{Context: secCtx})
	if got := m.rowActivity(scanTarget{Kind: kindRepo, Name: secRepo}).State; got != indicator.Idle {
		t.Errorf("State after an empty snapshot = %v, want Idle", got)
	}
}
