package agentusageview

import (
	"testing"
	"time"

	"github.com/anthnel/devdesk/internal/agentusage"
)

var t0 = time.Date(2026, 10, 29, 12, 0, 0, 0, time.UTC)

func sub(st agentusage.State, quiet time.Duration) agentusage.Agent {
	a := agentusage.Agent{ID: "s", State: st}
	if quiet != noActivity {
		a.FirstActivity = t0.Add(-quiet - time.Minute)
		a.LastActivity = t0.Add(-quiet)
	}
	return a
}

func orch(quiet time.Duration) agentusage.Agent {
	a := sub(agentusage.StateUnknown, quiet)
	a.ID, a.Main = "main", true
	return a
}

const noActivity = time.Duration(-1 << 62)

func TestIdleAfterIsFiveMinutes(t *testing.T) {
	if idleAfter != 5*time.Minute {
		t.Errorf("idleAfter = %v, want 5m", idleAfter)
	}
}

func TestRunStateLabels(t *testing.T) {
	for s, want := range map[runState]string{stateUnknown: "unknown", stateRunning: "running", stateIdle: "idle", stateDone: "done"} {
		if got := s.label(); got != want {
			t.Errorf("label(%d) = %q, want %q", s, got, want)
		}
	}
}

func TestAgentStates(t *testing.T) {
	five := 5 * time.Minute
	tests := []struct {
		name   string
		agents []agentusage.Agent
		alive  bool
		want   []runState
	}{
		{"finished sub, alive", []agentusage.Agent{sub(agentusage.StateFinished, time.Hour)}, true, []runState{stateDone}},
		{"finished sub, not alive", []agentusage.Agent{sub(agentusage.StateFinished, time.Hour)}, false, []runState{stateDone}},
		{"unfinished sub, recent", []agentusage.Agent{sub(agentusage.StateUnfinished, time.Second)}, true, []runState{stateRunning}},
		{"unfinished sub, exactly 5m is not idle", []agentusage.Agent{sub(agentusage.StateUnfinished, five)}, true, []runState{stateRunning}},
		{"unfinished sub, 5m+1ns is idle", []agentusage.Agent{sub(agentusage.StateUnfinished, five+1)}, true, []runState{stateIdle}},
		{"unfinished sub, 5m-1ns is not idle", []agentusage.Agent{sub(agentusage.StateUnfinished, five-1)}, true, []runState{stateRunning}},
		{"unfinished sub, not alive, recent", []agentusage.Agent{sub(agentusage.StateUnfinished, time.Second)}, false, []runState{stateRunning}},
		{"unfinished sub, not alive, quiet", []agentusage.Agent{sub(agentusage.StateUnfinished, time.Hour)}, false, []runState{stateIdle}},
		{"unfinished sub, no timestamp never idle", []agentusage.Agent{sub(agentusage.StateUnfinished, noActivity)}, true, []runState{stateRunning}},
		{"unfinished sub, future timestamp not idle", []agentusage.Agent{sub(agentusage.StateUnfinished, -time.Hour)}, true, []runState{stateRunning}},
		{"unknown sub, alive, recent", []agentusage.Agent{sub(agentusage.StateUnknown, time.Second)}, true, []runState{stateRunning}},
		{"unknown sub, alive, 5m exactly", []agentusage.Agent{sub(agentusage.StateUnknown, five)}, true, []runState{stateRunning}},
		{"unknown sub, alive, 5m+1ns", []agentusage.Agent{sub(agentusage.StateUnknown, five+1)}, true, []runState{stateIdle}},
		{"unknown sub, not alive", []agentusage.Agent{sub(agentusage.StateUnknown, time.Second)}, false, []runState{stateUnknown}},
		{"orchestrator alive, recent", []agentusage.Agent{orch(time.Second)}, true, []runState{stateRunning}},
		{"orchestrator alive, 5m exactly", []agentusage.Agent{orch(five)}, true, []runState{stateRunning}},
		{"orchestrator alive, 5m+1ns", []agentusage.Agent{orch(five + 1)}, true, []runState{stateIdle}},
		{"orchestrator alive, no timestamp", []agentusage.Agent{orch(noActivity)}, true, []runState{stateRunning}},
		{"orchestrator not alive is unknown, never done", []agentusage.Agent{orch(time.Hour)}, false, []runState{stateUnknown}},
		{"orchestrator quiet but a sub-agent runs", []agentusage.Agent{orch(time.Hour), sub(agentusage.StateUnfinished, time.Second)}, true, []runState{stateRunning, stateRunning}},
		{"orchestrator quiet, every running sub idle", []agentusage.Agent{orch(time.Hour), sub(agentusage.StateUnfinished, time.Hour)}, true, []runState{stateIdle, stateIdle}},
		{"orchestrator quiet, sub finished", []agentusage.Agent{orch(time.Hour), sub(agentusage.StateFinished, time.Hour)}, true, []runState{stateIdle, stateDone}},
		{"orchestrator after its sub-agents in the slice", []agentusage.Agent{sub(agentusage.StateUnfinished, time.Second), orch(time.Hour)}, true, []runState{stateRunning, stateRunning}},
		{"empty", nil, true, []runState{}},
	}
	for _, tt := range tests {
		t.Run(tt.name, func(t *testing.T) {
			got := agentStates(tt.agents, statusClock{now: t0, alive: tt.alive})
			if len(got) != len(tt.want) {
				t.Fatalf("len = %d, want %d", len(got), len(tt.want))
			}
			for i := range got {
				if got[i] != tt.want[i] {
					t.Errorf("agent %d: %s, want %s", i, got[i].label(), tt.want[i].label())
				}
			}
		})
	}
}
