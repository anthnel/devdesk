package workspaces

import (
	"testing"

	"github.com/anthnel/devdesk/internal/jobs"
)

// DEVDESK-227: a delete is running from admission, so :jobs does not show it
// queued and does not offer K, which would mark a live delete skipped.
func TestDeleteRunIsRunningFromAdmission(t *testing.T) {
	run := Model{}.deleteRun("/tmp/ws/app")
	if run.Kind != jobs.KindDelete || len(run.Items) != 1 {
		t.Fatalf("run = %+v", run)
	}
	if got := run.Items[0].State; got != jobs.ItemRunning {
		t.Errorf("item state = %q, want running", got)
	}
	if run.Stoppable() {
		t.Error("Stoppable() = true for a delete that must never be cut")
	}
}
