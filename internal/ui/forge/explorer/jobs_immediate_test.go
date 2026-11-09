package explorer

import (
	"testing"

	"github.com/anthnel/devdesk/internal/jobs"
)

// DEVDESK-147: a create or a delete is running from admission, so :jobs does
// not show it queued and does not offer to stop it.
func TestCreateAndDeleteRunsAreRunningFromAdmission(t *testing.T) {
	tests := []struct {
		name string
		run  jobs.Run
		kind jobs.Kind
	}{
		{"create", createRun("g/app", "app"), jobs.KindCreate},
		{"delete", deleteRun("g/app", "app"), jobs.KindDelete},
	}
	for _, tt := range tests {
		t.Run(tt.name, func(t *testing.T) {
			if tt.run.Kind != tt.kind || len(tt.run.Items) != 1 || tt.run.Items[0].Target != "g/app" {
				t.Fatalf("run = %+v", tt.run)
			}
			if got := tt.run.Items[0].State; got != jobs.ItemRunning {
				t.Errorf("item state = %q, want running", got)
			}
			if tt.run.Stoppable() {
				t.Error("Stoppable() = true for work that must never be cut")
			}
		})
	}
}

// The guard is unchanged: a running create or delete still holds its row.
func TestBusyStillHoldsTheRowWhileAnImmediateRunIsRunning(t *testing.T) {
	reg := jobs.New()
	reg.Start(deleteRun("g/app", "app"))
	reg.Start(createRun("g/new", "new"))
	m := Model{jobs: reg.Snapshot()}
	for _, path := range []string{"g/app", "g/new"} {
		if !m.busy(path) {
			t.Errorf("busy(%q) = false while its run is running", path)
		}
	}
}
