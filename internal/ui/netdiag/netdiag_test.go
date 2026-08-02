package netdiag

import (
	"io"
	"log"
	"os"
	"testing"

	tea "github.com/charmbracelet/bubbletea"

	"github.com/anthnel/devdesk/internal/config"
	"github.com/anthnel/devdesk/internal/ui/testutil"
)

// Every diagnostic runs in an ephemeral Docker container, so no test executes a
// command Update returns. These tests drive the view through Update() and
// View() only — deliberately, since model.go is due to be split and assertions
// on its internals would pin the current file layout rather than the behaviour.

func TestMain(m *testing.M) {
	log.SetOutput(io.Discard)
	code := m.Run()
	log.SetOutput(os.Stderr)
	os.Exit(code)
}

func testConfig() *config.Config {
	cfg := config.Default()
	cfg.Docker.NetworkToolImage = "nicolaka/netshoot"
	return cfg
}

// newTestModel returns a laid-out model on the diagnostics tab.
func newTestModel(t *testing.T) *Model {
	t.Helper()
	return feed(t, New(testConfig()), tea.WindowSizeMsg{Width: 120, Height: 40})
}

// runningModel returns a model that has started a run over the given target,
// with only the named tests enabled.
func runningModel(t *testing.T, target string, tests ...string) *Model {
	t.Helper()
	m := newTestModel(t)
	m = withOnly(t, m, tests...)
	m = typeInto(t, m, target)
	m.focusedField = fieldButton
	return feed(t, m, testutil.Key("enter"))
}

// resultsModel returns a model whose run has completed.
func resultsModel(t *testing.T) *Model {
	t.Helper()
	m := runningModel(t, "example.com", "Ping", "Netcat")
	return feed(t,
		m,
		testCompleteMsg{gen: m.runGen, name: "Ping", success: true, output: "64 bytes from example.com\nttl=57"},
		testCompleteMsg{gen: m.runGen, name: "Netcat", success: false, output: "connection refused"},
	)
}

// withOnly enables exactly the named tests.
func withOnly(t *testing.T, m *Model, names ...string) *Model {
	t.Helper()
	wanted := map[string]bool{}
	for _, name := range names {
		wanted[name] = true
	}
	for i := range m.tests {
		m.tests[i].enabled = wanted[m.tests[i].name]
	}
	return m
}

// typeInto types s into the target field.
func typeInto(t *testing.T, m *Model, s string) *Model {
	t.Helper()
	m.focusedField = fieldTarget
	m.targetInput.Focus()
	return feed(t, m, testutil.Type(s)...)
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
