package netdiag

import (
	"context"
	"strconv"

	tea "github.com/charmbracelet/bubbletea"

	dockerpkg "github.com/anthnel/devdesk/internal/docker"
	"github.com/anthnel/devdesk/internal/netcheck"
)

// startRun validates the form and starts the pipeline at its first stage.
func (m *Model) startRun() (*Model, tea.Cmd) {
	tg, err := m.buildTarget()
	if err != nil {
		return m, m.footer.Error(capitalize(err.Error()))
	}

	m.runGen++
	m.state = StateRunning
	m.results = netcheck.Results{}
	m.verdict = netcheck.Unknown
	m.runStep = 0
	m.totalSteps = len(netcheck.Steps())
	m.runStage = netcheck.Steps()[0]
	m.traceOutput = ""

	return m, tea.Batch(m.spinner.Tick, runStageCmd(m.runGen, tg, m.checkSettings(), 0, netcheck.Results{}))
}

// runStageCmd runs one stage of the pipeline and reports the accumulated
// results.
//
// The stages are chained through messages rather than run as one command, so
// the footer can name the question being asked. That matters on precisely the
// case worth diagnosing: an unreachable host spends its timeouts one after
// another, and a spinner with nothing beside it is indistinguishable from a
// hang — the defect §3.16 recorded for the clone's "Pulling..." modal.
//
// Nothing the model holds is touched here. The accumulated results are passed
// in by value and returned in the message, which is what netcheck.RunStep
// copies for (Rule 110).
func runStageCmd(gen int, tg netcheck.Target, set netcheck.Settings, step int, prior netcheck.Results) tea.Cmd {
	steps := netcheck.Steps()
	if step >= len(steps) {
		return nil
	}
	id := steps[step]
	return func() tea.Msg {
		res := netcheck.RunStep(context.Background(), tg, netcheck.SystemEnv(set), set, id, prior)
		return stageDoneMsg{gen: gen, stage: id, next: step + 1, results: res}
	}
}

// traceCmd runs a route trace in the network tool container.
//
// This is the one probe still shelling out, and it is the only one that has to:
// traceroute needs raw sockets and a tool worth not reimplementing. Everything
// else answers from this process, on this machine's network stack — which is
// the point, since --network host on Docker Desktop is the VM's stack and not
// the user's.
//
// The consequence is stated rather than left to be discovered: a trace answers
// for the container's view of the network, so it can disagree with the checks
// above it. renderTraceHeader says so on screen.
func traceCmd(gen int, image string, maxHops int, tg netcheck.Target, tcp bool) tea.Cmd {
	return func() tea.Msg {
		var res dockerpkg.DiagResult
		if tcp {
			res = dockerpkg.RunTCPTraceroute(image, tg.Host, strconv.Itoa(tg.Port), maxHops)
		} else {
			res = dockerpkg.RunTraceroute(image, tg.Host, maxHops)
		}
		return traceDoneMsg{gen: gen, output: res.Output, tcp: tcp}
	}
}
