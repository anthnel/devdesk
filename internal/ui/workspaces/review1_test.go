package workspaces

import (
	"testing"
	"time"

	"github.com/anthnel/devdesk/internal/command"
	"github.com/anthnel/devdesk/internal/jobs"
	"github.com/anthnel/devdesk/internal/ui/indicator"
)

// DEVDESK-144 review round 1: the fixed busy text and the dot read one source.

func TestBusyTextIgnoresARunFromAnotherContext(t *testing.T) {
	other := jobs.NewRun(jobs.KindScan, command.ViewWorkspaces, "other", "~/work", devdeskPath)
	other.Items[0].State = jobs.ItemRunning
	m := withJobs(t, loadedModel(t), other)
	r := rowNamed(t, m, "devdesk")
	if r.Activity.State != indicator.Idle {
		t.Fatalf("dot = %v, want Idle for another context's run", r.Activity.State)
	}
	if got := cellOf(t, "Scanned", r); got == scanningText {
		t.Errorf("Scanned says %q while the dot is idle", got)
	}
}

func TestBusyTextAgreesWithTheDotForARunningScan(t *testing.T) {
	m := running(t, loadedModel(t), jobs.KindScan, devdeskPath)
	r := rowNamed(t, m, "devdesk")
	if r.Activity.State != indicator.Running || cellOf(t, "Scanned", r) != scanningText {
		t.Errorf("dot %v, Scanned %q", r.Activity.State, cellOf(t, "Scanned", r))
	}
}

func TestFooterDotSurvivesAFailedSelectedRowDuringABatch(t *testing.T) {
	batch := runningRun(jobs.KindSync, "/tmp/workspaces/other01")
	fail := []jobs.Failure{actFailure(jobs.KindSync, testContext, ftDev, "sync failed — check logs", time.Now())}
	m := withSnapshot(t, loadedModel(t), []jobs.Run{batch}, fail)
	m = selectRow(t, m, "devdesk")
	st := m.status()
	if st.Detail.IsZero() {
		t.Fatal("precondition: the failed row should carry a detail")
	}
	if !st.Activity {
		t.Error("activity dot dropped while a batch is still running")
	}
}
