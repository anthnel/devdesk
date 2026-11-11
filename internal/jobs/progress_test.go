package jobs

import (
	"testing"

	"github.com/anthnel/devdesk/internal/command"
)

// sealedRun builds a sealed run (not open) with the given item states.
func sealedRun(kind Kind, states ...ItemState) Run { return kindRun(kind, states...) }

// cancelledRun is a run asked to stop, whatever its items say.
func cancelledRun(kind Kind, states ...ItemState) Run {
	r := kindRun(kind, states...)
	r.cancelled = true
	return r
}

func failureSeq(seq uint64) Failure {
	return Failure{FailureKey: FailureKey{Kind: KindScan, Target: "/w/app", Context: "default"}, Seq: seq}
}

func TestSummarize(t *testing.T) {
	const (
		q = ItemQueued
		r = ItemRunning
		d = ItemDone
		f = ItemFailed
		s = ItemSkipped
	)
	for _, tc := range []struct {
		name        string
		runs        []Run
		failures    []Failure
		readThrough uint64
		want        Progress
	}{
		{name: "nothing at all", want: Progress{}},
		{name: "a settled run is idle", runs: []Run{sealedRun(KindScan, d, d)}, want: Progress{}},
		{
			name: "an open run with no target is indeterminate",
			runs: []Run{NewOpenRun(KindClone, command.ViewGitExplorer, "default", "g")},
			want: Progress{State: ProgressIndeterminate, Running: 1},
		},
		{
			name: "a one-target run is indeterminate",
			runs: []Run{NewImmediateRun(KindCreate, command.ViewWorkspaces, "default", "l", "t")},
			want: Progress{State: ProgressIndeterminate, Running: 1},
		},
		{
			name:     "four targets, one done, one failed, failure already read",
			runs:     []Run{sealedRun(KindScan, d, f, q, q)},
			failures: []Failure{failureSeq(1)}, readThrough: 1,
			want: Progress{State: ProgressPercent, Percent: 50, Measured: true, Running: 1},
		},
		{
			name: "weighted sum, not mean of percentages",
			runs: []Run{sealedRun(KindScan, d, q), sealedRun(KindScan, q, q, q, q, q, q, q, q)},
			want: Progress{State: ProgressPercent, Percent: 10, Measured: true, Running: 2},
		},
		{
			name: "one third rounds down",
			runs: []Run{sealedRun(KindScan, d, q, q)},
			want: Progress{State: ProgressPercent, Percent: 33, Measured: true, Running: 1},
		},
		{
			name: "three queued targets are zero percent",
			runs: []Run{sealedRun(KindScan, q, q, q)},
			want: Progress{State: ProgressPercent, Percent: 0, Measured: true, Running: 1},
		},
		{
			name: "skipped counts as done",
			runs: []Run{sealedRun(KindScan, s, r)},
			want: Progress{State: ProgressPercent, Percent: 50, Measured: true, Running: 1},
		},
		{
			name: "measured and open together are indeterminate",
			runs: []Run{sealedRun(KindScan, d, q), NewOpenRun(KindClone, command.ViewGitExplorer, "default", "g")},
			want: Progress{State: ProgressIndeterminate, Running: 2},
		},
		{
			name: "measured and one-target together are indeterminate",
			runs: []Run{sealedRun(KindScan, d, q), sealedRun(KindScan, r)},
			want: Progress{State: ProgressIndeterminate, Running: 2},
		},
		{
			name: "a cancelled sync with an item still running warns and keeps its percent",
			runs: []Run{cancelledRun(KindSync, d, r, s)},
			want: Progress{State: ProgressWarning, Percent: 66, Measured: true, Running: 1, Stopping: 1},
		},
		{
			name: "a cancelled one-target run warns at 100",
			runs: []Run{cancelledRun(KindSync, r)},
			want: Progress{State: ProgressWarning, Percent: 100, Running: 1, Stopping: 1},
		},
		{name: "a settled cancelled run is idle", runs: []Run{cancelledRun(KindSync, d, s)}, want: Progress{}},
		{
			name: "an unread failure is an error", failures: []Failure{failureSeq(3)}, readThrough: 2,
			want: Progress{State: ProgressError, Unread: 1},
		},
		{
			name: "a failure read through its Seq is not an error", failures: []Failure{failureSeq(3)}, readThrough: 3,
			want: Progress{},
		},
		{
			name: "an unread failure beats a running run",
			runs: []Run{sealedRun(KindScan, d, q)}, failures: []Failure{failureSeq(3)}, readThrough: 2,
			want: Progress{State: ProgressError, Percent: 50, Measured: true, Running: 1, Unread: 1},
		},
		{
			name: "an unread failure beats a stopping run",
			runs: []Run{cancelledRun(KindSync, r, r)}, failures: []Failure{failureSeq(3)},
			want: Progress{State: ProgressError, Percent: 0, Measured: true, Running: 1, Stopping: 1, Unread: 1},
		},
		{
			name: "index failures count", readThrough: 0,
			failures: []Failure{{FailureKey: FailureKey{Kind: KindIndex, Target: "forge", Context: "default"}, Seq: 1}},
			want:     Progress{State: ProgressError, Unread: 1},
		},
		{
			name: "only failures above the watermark are unread", readThrough: 2,
			failures: []Failure{failureSeq(1), failureSeq(2), failureSeq(3), failureSeq(4)},
			want:     Progress{State: ProgressError, Unread: 2},
		},
	} {
		t.Run(tc.name, func(t *testing.T) {
			if got := Summarize(tc.runs, tc.failures, tc.readThrough); got != tc.want {
				t.Errorf("Summarize() = %+v, want %+v", got, tc.want)
			}
		})
	}
}

func TestSummarizeNeverFiltersByContext(t *testing.T) {
	run := sealedRun(KindScan, ItemDone, ItemQueued)
	run.Context = "other"
	fail := failureSeq(1)
	fail.Context = "other"
	got := Summarize([]Run{run}, []Failure{fail}, 0)
	if got.State != ProgressError || got.Running != 1 || got.Unread != 1 {
		t.Errorf("Summarize() = %+v, want it to count runs and failures of any context", got)
	}
}

func TestLatestFailureSeq(t *testing.T) {
	for _, tc := range []struct {
		name string
		in   []Failure
		want uint64
	}{
		{"nil", nil, 0},
		{"one", []Failure{failureSeq(4)}, 4},
		{"maximum, not last", []Failure{failureSeq(2), failureSeq(9), failureSeq(5)}, 9},
	} {
		t.Run(tc.name, func(t *testing.T) {
			if got := LatestFailureSeq(tc.in); got != tc.want {
				t.Errorf("LatestFailureSeq() = %d, want %d", got, tc.want)
			}
		})
	}
}

func TestFailuresIn(t *testing.T) {
	a, b := failureSeq(1), failureSeq(2)
	b.Context = "other"
	c := failureSeq(3)
	got := FailuresIn([]Failure{a, b, c}, "default")
	if len(got) != 2 || got[0].Seq != 1 || got[1].Seq != 3 {
		t.Errorf("FailuresIn(default) = %+v, want Seq 1 and 3 in order", got)
	}
	if got := FailuresIn([]Failure{a, b, c}, "absent"); len(got) != 0 {
		t.Errorf("FailuresIn(absent) = %+v, want none", got)
	}
	if got := FailuresIn(nil, "default"); len(got) != 0 {
		t.Errorf("FailuresIn(nil) = %+v, want none", got)
	}
}

// A warning that cannot be measured because another run is unmeasured reports
// a full bar, not the partial figure of the measured one.
func TestSummarizeWarningWithAnUnmeasuredSecondRun(t *testing.T) {
	stopping := cancelledRun(KindSync, ItemDone, ItemRunning)
	single := NewImmediateRun(KindScan, command.ViewWorkspaces, "default", "x", "a")
	got := Summarize([]Run{stopping, single}, nil, 0)
	if got.State != ProgressWarning || got.Percent != 100 || got.Measured {
		t.Errorf("Summarize() = %+v, want Warning, Percent 100, not Measured", got)
	}
}

// Through a real registry: cancelling an open run seals it, and it settles with
// no item in flight, so the summary goes back to idle.
func TestSummarizeAfterCancellingAnOpenRun(t *testing.T) {
	r := New()
	id := r.Start(NewOpenRun(KindClone, command.ViewGitExplorer, "default", "clone"))
	if got := Summarize(r.Snapshot(), nil, 0); got.State != ProgressIndeterminate {
		t.Fatalf("open run: State = %v, want Indeterminate", got.State)
	}
	if !r.Cancel(id) {
		t.Fatal("Cancel refused an open run")
	}
	if got := Summarize(r.Snapshot(), nil, 0); got.State != ProgressIdle {
		t.Errorf("cancelled open run: %+v, want Idle once settled", got)
	}
}
