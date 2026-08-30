package ociresources

import (
	"testing"

	tea "github.com/charmbracelet/bubbletea"

	"github.com/anthnel/devdesk/internal/command"
	"github.com/anthnel/devdesk/internal/jobs"
	"github.com/anthnel/devdesk/internal/ui/testutil"
)

// ── Putting scans in flight, the way the router does ─────────────────────────
//
// The view holds no bookkeeping of its own any more: what is running arrives in
// a jobs.ChangedMsg.

func withJobs(t *testing.T, m Model, runs ...jobs.Run) Model {
	t.Helper()
	next, _ := m.Update(jobs.ChangedMsg{Runs: runs, Frame: "*", RenderedFrame: "*"})
	updated, ok := next.(Model)
	if !ok {
		t.Fatalf("Update() returned %T, want ociresources.Model", next)
	}
	return updated
}

// scanningRun builds a scan run with every image already running.
func scanningRun(names ...string) jobs.Run {
	run := jobs.NewRun(jobs.KindScan, command.ViewOCIResources, "default", "images", names...)
	for i := range run.Items {
		run.Items[i].State = jobs.ItemRunning
	}
	return run
}

// scanning puts the given images in flight.
func scanning(t *testing.T, m Model, names ...string) Model {
	t.Helper()
	return withJobs(t, m, scanningRun(names...))
}

// settledScanRun is the same run with every image finished.
func settledScanRun(names ...string) jobs.Run {
	run := jobs.NewRun(jobs.KindScan, command.ViewOCIResources, "default", "images", names...)
	for i := range run.Items {
		run.Items[i].State = jobs.ItemDone
	}
	return run
}

// startedScanRun returns the run a command asks the router to register, if it
// asks for one. It is how a test says "no scan was launched" now that launching
// is a message rather than a flag on the model.
func startedScanRun(cmd tea.Cmd) (jobs.Run, bool) {
	msg, ok := testutil.MsgOf[jobs.StartMsg](cmd)
	if !ok {
		return jobs.Run{}, false
	}
	return msg.Run, true
}

func wantScanRun(t *testing.T, cmd tea.Cmd) jobs.Run {
	t.Helper()
	run, started := startedScanRun(cmd)
	if !started {
		t.Fatal("no run was registered")
	}
	if run.Kind != jobs.KindScan {
		t.Errorf("run kind = %q, want %q", run.Kind, jobs.KindScan)
	}
	return run
}

// pullingRun builds a pull run with every image already running.
func pullingRun(names ...string) jobs.Run {
	run := jobs.NewRun(jobs.KindPull, command.ViewOCIResources, "default", "images", names...)
	for i := range run.Items {
		run.Items[i].State = jobs.ItemRunning
	}
	return run
}

// pulling puts the given images' pulls in flight.
func pulling(t *testing.T, m Model, names ...string) Model {
	t.Helper()
	return withJobs(t, m, pullingRun(names...))
}

// startedPullRun is startedScanRun's counterpart for pull.
func startedPullRun(cmd tea.Cmd) (jobs.Run, bool) {
	msg, ok := testutil.MsgOf[jobs.StartMsg](cmd)
	if !ok {
		return jobs.Run{}, false
	}
	return msg.Run, true
}

// settledPullRun is the same run with every image finished.
func settledPullRun(names ...string) jobs.Run {
	run := jobs.NewRun(jobs.KindPull, command.ViewOCIResources, "default", "images", names...)
	for i := range run.Items {
		run.Items[i].State = jobs.ItemDone
	}
	return run
}
