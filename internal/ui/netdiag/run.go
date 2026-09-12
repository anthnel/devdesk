package netdiag

import (
	"context"

	tea "github.com/charmbracelet/bubbletea"

	"github.com/anthnel/devdesk/internal/netcheck"
)

// StartRun validates the form and starts the pipeline at its first stage. It
// is exported so a producer that opens the view prefilled — status's H
// (§3.66) — can trigger the run itself, from the router, rather than netdiag
// auto-running on construction.
func (m *Model) StartRun() (*Model, tea.Cmd) {
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
