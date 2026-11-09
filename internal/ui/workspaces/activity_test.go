package workspaces

import (
	"slices"
	"strings"
	"testing"
	"time"

	"github.com/anthnel/devdesk/internal/command"
	"github.com/anthnel/devdesk/internal/jobs"
	"github.com/anthnel/devdesk/internal/ui/indicator"
)

// DEVDESK-144 step 1: what one row says about its own activity. The state is
// read from the registry snapshot (runs + remembered failures) the router
// hands the view, filtered on the context the view was built for.

const (
	actRepo = "/tmp/workspaces/api"
	actDir  = "/tmp/workspaces/dir"
	actSubA = "/tmp/workspaces/dir/a"
	actSubB = "/tmp/workspaces/dir/b"
)

func repoEntry(path string) Entry {
	return Entry{Name: pathBaseName(path), Path: path, IsDir: true, IsGitRepo: true}
}

func dirEntry(path string, subs ...string) Entry {
	return Entry{Name: pathBaseName(path), Path: path, IsDir: true, SubRepoPaths: subs}
}

// actRun builds a run of one kind whose single item is in the given state.
func actRun(kind jobs.Kind, ctx string, state jobs.ItemState, target string) jobs.Run {
	run := jobs.NewRun(kind, command.ViewWorkspaces, ctx, "~/work", target)
	run.Items[0].State = state
	return run
}

func actFailure(kind jobs.Kind, ctx, target, detail string, at time.Time) jobs.Failure {
	return jobs.Failure{
		FailureKey: jobs.FailureKey{Kind: kind, Target: jobs.TargetKey(target), Context: ctx},
		Display:    target, Detail: detail, At: at,
	}
}

func withSnapshot(t *testing.T, m Model, runs []jobs.Run, failures []jobs.Failure) Model {
	t.Helper()
	return feed(t, m, jobs.ChangedMsg{Runs: runs, Failures: failures})
}

func TestRowActivity(t *testing.T) {
	t0 := time.Date(2026, 11, 1, 10, 0, 0, 0, time.UTC)
	tests := []struct {
		name       string
		entry      Entry
		runs       []jobs.Run
		failures   []jobs.Failure
		wantState  indicator.State
		wantTarget string   // Detail.Target ("" = no target in the detail)
		wantInfo   []string // substrings of Detail.String()
		wantZero   bool     // Detail must be empty
	}{
		{
			name:      "running scan",
			entry:     repoEntry(actRepo),
			runs:      []jobs.Run{actRun(jobs.KindScan, testContext, jobs.ItemRunning, actRepo)},
			wantState: indicator.Running, wantTarget: actRepo,
			wantInfo: []string{"Scanning", actRepo},
		},
		{
			name:      "queued sync",
			entry:     repoEntry(actRepo),
			runs:      []jobs.Run{actRun(jobs.KindSync, testContext, jobs.ItemQueued, actRepo)},
			wantState: indicator.Queued, wantTarget: actRepo,
			wantInfo: []string{"Queued", "sync", actRepo},
		},
		{
			name:      "scan failure alone",
			entry:     repoEntry(actRepo),
			failures:  []jobs.Failure{actFailure(jobs.KindScan, testContext, actRepo, "scan failed — check logs", t0)},
			wantState: indicator.Failed, wantTarget: actRepo,
			wantInfo: []string{": last scan failed — check logs"},
		},
		{
			name:      "failure and running on the same repo: running wins, failure is masked",
			entry:     repoEntry(actRepo),
			runs:      []jobs.Run{actRun(jobs.KindSync, testContext, jobs.ItemRunning, actRepo)},
			failures:  []jobs.Failure{actFailure(jobs.KindScan, testContext, actRepo, "scan failed — check logs", t0)},
			wantState: indicator.Running, wantTarget: actRepo,
			wantInfo: []string{"· earlier failure: scan (see :jobs)"},
		},
		{
			name:      "failure in another context is invisible",
			entry:     repoEntry(actRepo),
			failures:  []jobs.Failure{actFailure(jobs.KindScan, "other", actRepo, "scan failed — check logs", t0)},
			wantState: indicator.Idle, wantZero: true,
		},
		{
			name:      "run in another context is invisible",
			entry:     repoEntry(actRepo),
			runs:      []jobs.Run{actRun(jobs.KindScan, "other", jobs.ItemRunning, actRepo)},
			wantState: indicator.Idle, wantZero: true,
		},
		{
			name:      "clone and pull failures are not this view's kinds",
			entry:     repoEntry(actRepo),
			failures:  []jobs.Failure{actFailure(jobs.KindClone, testContext, actRepo, "x", t0), actFailure(jobs.KindPull, testContext, actRepo, "y", t0)},
			wantState: indicator.Idle, wantZero: true,
		},
		{
			name:      "delete failure is a failure",
			entry:     repoEntry(actRepo),
			failures:  []jobs.Failure{actFailure(jobs.KindDelete, testContext, actRepo, "delete failed — check logs", t0)},
			wantState: indicator.Failed, wantTarget: actRepo,
			wantInfo: []string{"last delete failed"},
		},
		{
			name:  "done and skipped items are idle",
			entry: repoEntry(actRepo),
			runs: []jobs.Run{
				actRun(jobs.KindScan, testContext, jobs.ItemDone, actRepo),
				actRun(jobs.KindSync, testContext, jobs.ItemSkipped, actRepo),
			},
			wantState: indicator.Idle, wantZero: true,
		},
		{
			name:      "directory: running sub-repo beats a failed one",
			entry:     dirEntry(actDir, actSubA, actSubB),
			runs:      []jobs.Run{actRun(jobs.KindScan, testContext, jobs.ItemRunning, actSubA)},
			failures:  []jobs.Failure{actFailure(jobs.KindScan, testContext, actSubB, "scan failed — check logs", t0)},
			wantState: indicator.Running, wantTarget: actSubA,
		},
		{
			name:      "directory: queued sub-repo beats a failed one",
			entry:     dirEntry(actDir, actSubA, actSubB),
			runs:      []jobs.Run{actRun(jobs.KindScan, testContext, jobs.ItemQueued, actSubA)},
			failures:  []jobs.Failure{actFailure(jobs.KindScan, testContext, actSubB, "scan failed — check logs", t0)},
			wantState: indicator.Queued, wantTarget: actSubA,
		},
		{
			name:      "directory: only a failed sub-repo",
			entry:     dirEntry(actDir, actSubA, actSubB),
			failures:  []jobs.Failure{actFailure(jobs.KindScan, testContext, actSubB, "scan failed — check logs", t0)},
			wantState: indicator.Failed, wantTarget: actSubB,
		},
		{
			name:      "directory: its own delete target counts too",
			entry:     dirEntry(actDir, actSubA),
			runs:      []jobs.Run{actRun(jobs.KindDelete, testContext, jobs.ItemRunning, actDir)},
			wantState: indicator.Running, wantTarget: actDir,
		},
		{
			name:      "git repo: sub-repo paths are ignored",
			entry:     Entry{Name: "api", Path: actRepo, IsDir: true, IsGitRepo: true, SubRepoPaths: []string{actSubA}},
			runs:      []jobs.Run{actRun(jobs.KindScan, testContext, jobs.ItemRunning, actSubA)},
			wantState: indicator.Idle, wantZero: true,
		},
	}
	for _, tt := range tests {
		t.Run(tt.name, func(t *testing.T) {
			m := withSnapshot(t, newTestModel(t), tt.runs, tt.failures)
			got := m.rowActivity(tt.entry)
			if got.State != tt.wantState {
				t.Fatalf("State = %v, want %v (detail %q)", got.State, tt.wantState, got.Detail.String())
			}
			if tt.wantZero && !got.Detail.IsZero() {
				t.Errorf("Detail = %q, want none", got.Detail.String())
			}
			if tt.wantTarget != "" && got.Detail.Target != tt.wantTarget {
				t.Errorf("Detail.Target = %q, want %q", got.Detail.Target, tt.wantTarget)
			}
			for _, s := range tt.wantInfo {
				if !strings.Contains(got.Detail.String(), s) {
					t.Errorf("Detail %q lacks %q", got.Detail.String(), s)
				}
			}
		})
	}
}

func TestRowTargets(t *testing.T) {
	tests := []struct {
		name  string
		entry Entry
		want  []string
	}{
		{"git repo", repoEntry(actRepo), []string{actRepo}},
		{"git repo ignores sub-repos", Entry{Path: actRepo, IsGitRepo: true, SubRepoPaths: []string{actSubA}}, []string{actRepo}},
		{"directory aggregates its sub-repos", dirEntry(actDir, actSubA, actSubB), []string{actDir, actSubA, actSubB}},
		{"plain directory", dirEntry(actDir), []string{actDir}},
	}
	m := newTestModel(t)
	for _, tt := range tests {
		t.Run(tt.name, func(t *testing.T) {
			if got := m.rowTargets(tt.entry); !slices.Equal(got, tt.want) {
				t.Errorf("rowTargets = %v, want %v", got, tt.want)
			}
		})
	}
}

func TestWsKindsAreScanSyncDelete(t *testing.T) {
	want := []jobs.Kind{jobs.KindScan, jobs.KindSync, jobs.KindDelete}
	if !slices.Equal(wsKinds, want) {
		t.Errorf("wsKinds = %v, want %v", wsKinds, want)
	}
}

func TestJobsChangedStoresACopyOfTheFailures(t *testing.T) {
	t0 := time.Date(2026, 11, 1, 10, 0, 0, 0, time.UTC)
	failures := []jobs.Failure{actFailure(jobs.KindScan, testContext, actRepo, "scan failed — check logs", t0)}
	m := withSnapshot(t, newTestModel(t), nil, failures)

	failures[0].Kind = jobs.KindClone // the router's slice is reused after the send
	failures[0].Target = "/elsewhere"

	if got := m.rowActivity(repoEntry(actRepo)).State; got != indicator.Failed {
		t.Errorf("State after the message slice was mutated = %v, want Failed", got)
	}
}

func TestJobsChangedReplacesFailuresRatherThanMerging(t *testing.T) {
	t0 := time.Date(2026, 11, 1, 10, 0, 0, 0, time.UTC)
	m := withSnapshot(t, newTestModel(t), nil,
		[]jobs.Failure{actFailure(jobs.KindScan, testContext, actRepo, "scan failed — check logs", t0)})
	m = withSnapshot(t, m, nil, nil)
	if got := m.rowActivity(repoEntry(actRepo)).State; got != indicator.Idle {
		t.Errorf("State after an empty snapshot = %v, want Idle", got)
	}
}

func TestNewTakesTheContextName(t *testing.T) {
	t0 := time.Date(2026, 11, 1, 10, 0, 0, 0, time.UTC)
	f := []jobs.Failure{actFailure(jobs.KindScan, "prod", actRepo, "scan failed — check logs", t0)}

	prod := feed(t, New(testConfig(), nil, "prod"), jobs.ChangedMsg{Failures: f})
	if got := prod.rowActivity(repoEntry(actRepo)).State; got != indicator.Failed {
		t.Errorf("view built for prod: State = %v, want Failed", got)
	}
	dev := feed(t, New(testConfig(), nil, "dev"), jobs.ChangedMsg{Failures: f})
	if got := dev.rowActivity(repoEntry(actRepo)).State; got != indicator.Idle {
		t.Errorf("view built for dev: State = %v, want Idle", got)
	}
	sel := feed(t, NewForSelection(testConfig(), "prod", "pick"), jobs.ChangedMsg{Failures: f})
	if got := sel.rowActivity(repoEntry(actRepo)).State; got != indicator.Failed {
		t.Errorf("selection view built for prod: State = %v, want Failed", got)
	}
}
