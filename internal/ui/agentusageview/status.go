package agentusageview

import (
	"time"

	"github.com/anthnel/devdesk/internal/agentusage"
)

// runState is what the State column shows for an agent.
type runState int

const (
	stateUnknown runState = iota
	stateRunning
	stateIdle
	stateDone
)

// label returns the text shown in the State column.
func (s runState) label() string {
	switch s {
	case stateRunning:
		return "running"
	case stateIdle:
		return "idle"
	case stateDone:
		return "done"
	default:
		return "unknown"
	}
}

// statusClock is what the states are computed against: when the last read
// finished, and whether the session counts as alive (not Settled).
type statusClock struct {
	now   time.Time
	alive bool
}

// quiet reports whether the agent has been silent for strictly more than
// idleAfter. An agent with no timestamp, or one stamped in the future, is
// never quiet.
func (c statusClock) quiet(a agentusage.Agent) bool {
	if a.LastActivity.IsZero() {
		return false
	}
	return c.now.Sub(a.LastActivity) > idleAfter
}

// running is the state of an agent that has not finished: running, or idle
// once it has been silent for long enough.
func (c statusClock) running(a agentusage.Agent) runState {
	if c.quiet(a) {
		return stateIdle
	}
	return stateRunning
}

// subAgentState is the state of one sub-agent. A finished one is done; an
// unfinished one is running or idle whether or not the session is alive; one
// whose meta says nothing is judged the same way while the session is alive,
// and is unknown once it is not.
func (c statusClock) subAgentState(a agentusage.Agent) runState {
	switch {
	case a.State == agentusage.StateFinished:
		return stateDone
	case a.State == agentusage.StateUnfinished || c.alive:
		return c.running(a)
	default:
		return stateUnknown
	}
}

// orchestratorState is the state of the orchestrator, which no file says is
// finished: unknown when the session is not alive, idle only if it has been
// silent for long enough and no sub-agent is running, running otherwise.
func (c statusClock) orchestratorState(a agentusage.Agent, subRunning bool) runState {
	if !c.alive {
		return stateUnknown
	}
	if subRunning {
		return stateRunning
	}
	return c.running(a)
}

// agentStates returns one state per agent, same order. It is pure: the
// clock is a parameter.
func agentStates(agents []agentusage.Agent, c statusClock) []runState {
	out := make([]runState, len(agents))
	subRunning := false
	for i, a := range agents {
		if a.Main {
			continue
		}
		out[i] = c.subAgentState(a)
		subRunning = subRunning || out[i] == stateRunning
	}
	for i, a := range agents {
		if a.Main {
			out[i] = c.orchestratorState(a, subRunning)
		}
	}
	return out
}
