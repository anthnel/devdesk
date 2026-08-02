package status

import (
	"io"
	"log"
	"os"
	"testing"
	"time"

	"github.com/charmbracelet/bubbles/table"
	tea "github.com/charmbracelet/bubbletea"

	"github.com/anthnel/devdesk/internal/config"
	"github.com/anthnel/devdesk/internal/status"
)

// The view issues its checks from Cmds (checkComponents shells out to the
// network, tickCmd sleeps for a second), so no test in this package executes a
// command returned by Update. State is asserted on the model instead, per the
// contract testutil documents.

// The view logs every failing component it is handed, and the fixtures include
// one on purpose. Sending that to io.Discard keeps the run readable; the
// application installs its own log file at startup.
func TestMain(m *testing.M) {
	log.SetOutput(io.Discard)
	code := m.Run()
	log.SetOutput(os.Stderr)
	os.Exit(code)
}

// testConfig builds a config whose Status section holds the given monitors.
func testConfig(components ...config.ComponentConfig) *config.Config {
	cfg := config.Default()
	cfg.Status.RefreshInterval = 30
	cfg.Status.AutoRefresh = true
	cfg.Status.Components = components
	return cfg
}

// monitorConfigs mirrors the fixture returned by checkResults, so
// getSelectedComponentIndex can match statuses back to config entries.
func monitorConfigs() []config.ComponentConfig {
	return []config.ComponentConfig{
		{Name: "web", Type: "https", Target: "example.com"},
		{Name: "api", Type: "http", Target: "api.example.com"},
		{Name: "dns-primary", Type: "dns", Target: "example.com"},
		{Name: "cert", Type: "ssl", Target: "example.com:443"},
	}
}

// checkResults is a completed check over monitorConfigs: three monitors and one
// SSL certificate, which is the split every table assertion depends on.
func checkResults() []status.ComponentStatus {
	daysLeft := 42
	expires := time.Date(2026, 12, 1, 10, 30, 0, 0, time.UTC)

	return []status.ComponentStatus{
		{Name: "web", Type: status.TypeHTTPS, Target: "example.com", Status: status.StatusOK, ResponseTime: 120 * time.Millisecond},
		{Name: "api", Type: status.TypeHTTP, Target: "api.example.com", Status: status.StatusDown, Error: "connection refused"},
		{Name: "dns-primary", Type: status.TypeDNS, Target: "example.com", Status: status.StatusOK, ResponseTime: 8 * time.Millisecond},
		{
			Name: "cert", Type: status.TypeSSL, Target: "example.com:443", Status: status.StatusWarning,
			SSLDaysLeft: &daysLeft, SSLExpires: &expires, SSLIssuer: "Let's Encrypt",
		},
	}
}

// newTestModel returns a laid-out model with no data yet.
func newTestModel(t *testing.T, components ...config.ComponentConfig) Model {
	t.Helper()
	m := New(testConfig(components...))
	return feed(t, m, tea.WindowSizeMsg{Width: 120, Height: 30})
}

// loadedModel returns a model that has already absorbed a completed check.
func loadedModel(t *testing.T) Model {
	t.Helper()
	m := newTestModel(t, monitorConfigs()...)
	return feed(t, m, CheckCompleteMsg{Components: checkResults(), Timestamp: time.Now()})
}

// feed applies messages in order and returns the resulting model, discarding
// the commands. Use step when the command matters.
func feed(t *testing.T, m Model, msgs ...tea.Msg) Model {
	t.Helper()
	for _, msg := range msgs {
		m, _ = step(t, m, msg)
	}
	return m
}

// step applies one message and returns the model and the command it produced.
func step(t *testing.T, m Model, msg tea.Msg) (Model, tea.Cmd) {
	t.Helper()
	next, cmd := m.Update(msg)
	updated, ok := next.(Model)
	if !ok {
		t.Fatalf("Update() returned %T, want status.Model", next)
	}
	return updated, cmd
}

// rowNames returns the first cell of every row, which is the monitor name in
// both tables.
func rowNames(rows []table.Row) []string {
	names := make([]string, 0, len(rows))
	for _, row := range rows {
		names = append(names, row[0])
	}
	return names
}
