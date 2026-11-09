package ociresources

import (
	"slices"
	"strings"
	"testing"
	"time"

	"github.com/anthnel/devdesk/internal/command"
	"github.com/anthnel/devdesk/internal/jobs"
	"github.com/anthnel/devdesk/internal/ui/indicator"
)

// DEVDESK-146 step 1: what one Images row says about its own activity, read
// from the router snapshot against the context it carries. The identity is the
// raw reference (img.Name()), never a canonical form.

const (
	ociCtx = "default"
	ociRef = "nginx:1.27"
)

func ociRun(kind jobs.Kind, ctx string, state jobs.ItemState, target string) jobs.Run {
	run := jobs.NewRun(kind, command.ViewOCIResources, ctx, "images", target)
	run.Items[0].State = state
	return run
}

func ociFailure(kind jobs.Kind, ctx, target string, at time.Time) jobs.Failure {
	return jobs.Failure{
		FailureKey: jobs.FailureKey{Kind: kind, Target: jobs.TargetKey(target), Context: ctx},
		Display:    target, Detail: "failed — check logs", At: at,
	}
}

func ociSnapshot(t *testing.T, ctx string, runs []jobs.Run, failures []jobs.Failure) Model {
	t.Helper()
	return feed(t, loadedModel(t), jobs.ChangedMsg{Runs: runs, Failures: failures, Context: ctx})
}

func TestRowActivity(t *testing.T) {
	t0 := time.Date(2026, 11, 1, 10, 0, 0, 0, time.UTC)
	row := imageRow{RawName: ociRef, DisplayName: ociRef}
	tests := []struct {
		name      string
		row       imageRow
		runs      []jobs.Run
		failures  []jobs.Failure
		wantState indicator.State
		wantInfo  []string
		wantZero  bool
	}{
		{
			name: "running scan", row: row,
			runs:      []jobs.Run{ociRun(jobs.KindScan, ociCtx, jobs.ItemRunning, ociRef)},
			wantState: indicator.Running, wantInfo: []string{"Scanning", ociRef},
		},
		{
			name: "queued scan", row: row,
			runs:      []jobs.Run{ociRun(jobs.KindScan, ociCtx, jobs.ItemQueued, ociRef)},
			wantState: indicator.Queued, wantInfo: []string{"Queued", "scan", ociRef},
		},
		{
			name: "scan failure", row: row,
			failures:  []jobs.Failure{ociFailure(jobs.KindScan, ociCtx, ociRef, t0)},
			wantState: indicator.Failed,
		},
		{
			name: "pull failure", row: row,
			failures:  []jobs.Failure{ociFailure(jobs.KindPull, ociCtx, ociRef, t0)},
			wantState: indicator.Failed, wantInfo: []string{ociRef},
		},
		{
			name: "running pull", row: row,
			runs:      []jobs.Run{ociRun(jobs.KindPull, ociCtx, jobs.ItemRunning, ociRef)},
			wantState: indicator.Running, wantInfo: []string{"Pulling", ociRef},
		},
		{
			name: "sync failure is ignored (D5)", row: row,
			failures:  []jobs.Failure{ociFailure(jobs.KindSync, ociCtx, ociRef, t0)},
			wantState: indicator.Idle, wantZero: true,
		},
		{
			name: "failure in another context is invisible", row: row,
			failures:  []jobs.Failure{ociFailure(jobs.KindScan, "other", ociRef, t0)},
			wantState: indicator.Idle, wantZero: true,
		},
		{
			name: "a bare name does not bridge to the qualified reference", row: imageRow{RawName: "nginx"},
			failures:  []jobs.Failure{ociFailure(jobs.KindScan, ociCtx, "docker.io/library/nginx", t0)},
			wantState: indicator.Idle, wantZero: true,
		},
	}
	for _, tt := range tests {
		t.Run(tt.name, func(t *testing.T) {
			m := ociSnapshot(t, ociCtx, tt.runs, tt.failures)
			got := m.rowActivity(tt.row)
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

// A removal in flight is the view's own observation: Running, with its label.
func TestRowActivityBusyRemovalIsRunning(t *testing.T) {
	m := ociSnapshot(t, ociCtx, nil, nil)
	img := m.imageTable.Items()[0]
	m.imageTable.MarkBusy(img.Image.ID, "Removing "+img.RawName)

	got := m.rowActivity(img)
	if got.State != indicator.Running {
		t.Fatalf("State = %v, want Running", got.State)
	}
	if !strings.Contains(got.Detail.String(), "Removing "+img.RawName) {
		t.Errorf("Detail %q lacks the busy label", got.Detail.String())
	}
}

func TestScanAndPullRunning(t *testing.T) {
	tests := []struct {
		name string
		runs []jobs.Run
		scan bool
		pull bool
	}{
		{"running scan", []jobs.Run{ociRun(jobs.KindScan, ociCtx, jobs.ItemRunning, ociRef)}, true, false},
		{"queued scan is not running", []jobs.Run{ociRun(jobs.KindScan, ociCtx, jobs.ItemQueued, ociRef)}, false, false},
		{"running pull", []jobs.Run{ociRun(jobs.KindPull, ociCtx, jobs.ItemRunning, ociRef)}, false, true},
		{"other context", []jobs.Run{ociRun(jobs.KindScan, "other", jobs.ItemRunning, ociRef)}, false, false},
		{"running sync", []jobs.Run{ociRun(jobs.KindSync, ociCtx, jobs.ItemRunning, ociRef)}, false, false},
		{"no run", nil, false, false},
	}
	for _, tt := range tests {
		t.Run(tt.name, func(t *testing.T) {
			m := ociSnapshot(t, ociCtx, tt.runs, nil)
			if got := m.scanRunning(ociRef); got != tt.scan {
				t.Errorf("scanRunning = %v, want %v", got, tt.scan)
			}
			if got := m.pullRunning(ociRef); got != tt.pull {
				t.Errorf("pullRunning = %v, want %v", got, tt.pull)
			}
		})
	}
}

func TestOCIKindsAreScanAndPull(t *testing.T) {
	if want := []jobs.Kind{jobs.KindScan, jobs.KindPull}; !slices.Equal(ociKinds, want) {
		t.Errorf("ociKinds = %v, want %v", ociKinds, want)
	}
}

func TestJobsChangedStoresTheContext(t *testing.T) {
	m := ociSnapshot(t, "prod", nil, nil)
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
	failures := []jobs.Failure{ociFailure(jobs.KindScan, ociCtx, ociRef, t0)}
	m := ociSnapshot(t, ociCtx, nil, failures)

	failures[0].Kind = jobs.KindSync
	failures[0].Target = "elsewhere"

	if got := m.rowActivity(imageRow{RawName: ociRef}).State; got != indicator.Failed {
		t.Errorf("State after the message slice was mutated = %v, want Failed", got)
	}
}

func TestJobsChangedReplacesFailuresRatherThanMerging(t *testing.T) {
	t0 := time.Date(2026, 11, 1, 10, 0, 0, 0, time.UTC)
	m := ociSnapshot(t, ociCtx, nil, []jobs.Failure{ociFailure(jobs.KindScan, ociCtx, ociRef, t0)})
	m = feed(t, m, jobs.ChangedMsg{Context: ociCtx})
	if got := m.rowActivity(imageRow{RawName: ociRef}).State; got != indicator.Idle {
		t.Errorf("State after an empty snapshot = %v, want Idle", got)
	}
}
