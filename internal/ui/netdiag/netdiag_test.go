package netdiag

import (
	"io"
	"log"
	"os"
	"testing"

	tea "github.com/charmbracelet/bubbletea"

	"github.com/anthnel/devdesk/internal/config"
	"github.com/anthnel/devdesk/internal/netcheck"
	"github.com/anthnel/devdesk/internal/ui/testutil"
)

// The checks run against the real network, so no test executes a command Update
// returns. These tests drive the view through Update() and View() only, feeding
// the pipeline's stage messages by hand — which is also what lets a test set up
// a refused port or a broken chain without either a network or Docker.

func TestMain(m *testing.M) {
	log.SetOutput(io.Discard)
	code := m.Run()
	log.SetOutput(os.Stderr)
	os.Exit(code)
}

func testConfig() *config.Config {
	return config.Default()
}

// newTestModel returns a laid-out model on the diagnostics tab.
func newTestModel(t *testing.T) *Model {
	t.Helper()
	return feed(t, New(testConfig()), tea.WindowSizeMsg{Width: 120, Height: 40})
}

// typeInto types s into the target field.
func typeInto(t *testing.T, m *Model, s string) *Model {
	t.Helper()
	m.focusedField = fieldTarget
	m.targetInput.Focus()
	return feed(t, m, testutil.Type(s)...)
}

// runningModel returns a model that has started a run over the given target.
//
// No test executes the Cmd that would talk to the network: the pipeline is
// driven by feeding stageDoneMsg values built here, which is also what lets a
// test describe a situation — a refused port, an expired certificate — without
// one.
func runningModel(t *testing.T, target string) *Model {
	t.Helper()
	m := typeInto(t, newTestModel(t), target)
	return feed(t, m, testutil.Key("enter"))
}

// deliver walks a running model through the pipeline, landing the given checks
// on the first stage and repeating them on the rest.
func deliver(t *testing.T, m *Model, checks ...netcheck.Check) *Model {
	t.Helper()
	res := netcheck.ResultsOf(checks...)
	steps := netcheck.Steps()
	for i := range steps {
		m = feed(t, m, stageDoneMsg{gen: m.runGen, stage: steps[i], next: i + 1, results: res})
	}
	return m
}

// resultsModel returns a model whose run has completed with a plausible mix.
func resultsModel(t *testing.T) *Model {
	t.Helper()
	return deliver(t, runningModel(t, "example.com"),
		check(netcheck.CheckResolve, netcheck.OK, "example.com resolves to 93.184.216.34 (public)"),
		check(netcheck.CheckTCP, netcheck.Fail, "Port 443 does not accept connections"),
		check(netcheck.CheckTLSChain, netcheck.NotApplicable, "Skipped — TCP connect did not succeed"),
	)
}

// check builds one check the way a stage would.
func check(id netcheck.CheckID, v netcheck.Verdict, summary string) netcheck.Check {
	c := netcheck.Check{ID: id, Stage: netcheck.StageResolve, Verdict: v, Summary: summary}
	c.Title = titleOf(id)
	if v == netcheck.NotApplicable {
		c.Because = netcheck.CheckTCP
	}
	return c
}

func titleOf(id netcheck.CheckID) string {
	switch id {
	case netcheck.CheckResolve:
		return "DNS resolution"
	case netcheck.CheckTCP:
		return "TCP connect"
	case netcheck.CheckICMP:
		return "ICMP echo"
	case netcheck.CheckTLSChain:
		return "Certificate chain"
	default:
		return string(id)
	}
}

func feed(t *testing.T, m *Model, msgs ...tea.Msg) *Model {
	t.Helper()
	for _, msg := range msgs {
		m, _ = step(t, m, msg)
	}
	return m
}

func step(t *testing.T, m *Model, msg tea.Msg) (*Model, tea.Cmd) {
	t.Helper()
	next, cmd := m.Update(msg)
	updated, ok := next.(*Model)
	if !ok {
		t.Fatalf("Update() returned %T, want *netdiag.Model", next)
	}
	return updated, cmd
}
