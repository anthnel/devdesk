package security

import (
	"testing"

	tea "github.com/charmbracelet/bubbletea"

	"github.com/anthnel/devdesk/internal/command"
	"github.com/anthnel/devdesk/internal/jobs"
	"github.com/anthnel/devdesk/internal/ui/testutil"
)

// ── Putting scans in flight, the way the router does ─────────────────────────
//
// scanTarget.Scanning is derived from the router snapshot now, so a test says
// "this target is being scanned" by handing the view one.

func withJobs(t *testing.T, m Model, runs ...jobs.Run) Model {
	t.Helper()
	return feed(t, m, jobs.ChangedMsg{Runs: runs, Frame: "*", RenderedFrame: "*"})
}

// withFrame is the same snapshot with a named frame, for the tests that check
// the rows are restamped when it moves.
func withFrame(t *testing.T, m Model, frame string, runs ...jobs.Run) Model {
	t.Helper()
	return feed(t, m, jobs.ChangedMsg{Runs: runs, Frame: frame, RenderedFrame: frame})
}

func scanningRun(names ...string) jobs.Run {
	run := jobs.NewRun(jobs.KindScan, command.ViewSecurity, "default", "inventory", names...)
	for i := range run.Items {
		run.Items[i].State = jobs.ItemRunning
	}
	return run
}

func settledScanRun(names ...string) jobs.Run {
	run := jobs.NewRun(jobs.KindScan, command.ViewSecurity, "default", "inventory", names...)
	for i := range run.Items {
		run.Items[i].State = jobs.ItemDone
	}
	return run
}

// scanning puts the named targets in flight.
func scanning(t *testing.T, m Model, names ...string) Model {
	t.Helper()
	return withJobs(t, m, scanningRun(names...))
}

// startedScanRun returns the run a command asks the router to register.
func startedScanRun(cmd tea.Cmd) (jobs.Run, bool) {
	msg, ok := testutil.MsgOf[jobs.StartMsg](cmd)
	if !ok {
		return jobs.Run{}, false
	}
	return msg.Run, true
}

func wantScanRun(t *testing.T, cmd tea.Cmd, names ...string) jobs.Run {
	t.Helper()
	run, started := startedScanRun(cmd)
	if !started {
		t.Fatalf("no run was registered, want a scan over %v", names)
	}
	if run.Kind != jobs.KindScan {
		t.Errorf("run kind = %q, want %q", run.Kind, jobs.KindScan)
	}
	got := make(map[string]bool, len(run.Items))
	for _, item := range run.Items {
		got[item.Target] = true
		if item.State != jobs.ItemQueued {
			t.Errorf("%s starts as %q, want every target queued (D6)", item.Target, item.State)
		}
	}
	if len(got) != len(names) {
		t.Errorf("run holds %d targets, want %d", len(got), len(names))
	}
	for _, name := range names {
		if !got[name] {
			t.Errorf("run does not hold %q", name)
		}
	}
	return run
}
