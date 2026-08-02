package containers

import (
	"io"
	"log"
	"os"
	"strings"
	"testing"

	"github.com/charmbracelet/bubbles/table"
	tea "github.com/charmbracelet/bubbletea"
	"github.com/charmbracelet/lipgloss"
	"github.com/muesli/termenv"

	"github.com/anthnel/devdesk/internal/config"
	"github.com/anthnel/devdesk/internal/docker"
	"github.com/anthnel/devdesk/internal/ui/testutil"
)

// Every Cmd in this package shells out to the Docker CLI, so no test executes
// one. Assertions are on model state and on whether a command was returned at
// all, per the contract internal/ui/testutil documents.
//
// Two key handlers are excluded on purpose: "s" and "S" call detectShell, which
// runs `docker exec` synchronously inside Update. The tests drive them only in
// states where they return before reaching it. See the note in docs/backlog.md.

// The view logs every Docker failure it is handed, and several fixtures are
// failures on purpose.
func TestMain(m *testing.M) {
	log.SetOutput(io.Discard)
	code := m.Run()
	log.SetOutput(os.Stderr)
	os.Exit(code)
}

// containerFixtures covers one container per state that changes behaviour:
// running (metrics and actions), paused (unpause), exited (error styling) and
// dead. Names are deliberately out of alphabetical order.
func containerFixtures() []docker.Container {
	return []docker.Container{
		{
			ID: "aaaa111122223333", Name: "web", Image: "nginx:1.27", State: "running",
			CreatedAt: "2026-08-01 12:14:13 +0100 CET", Ports: "0.0.0.0:80->80/tcp",
			CPUPercent: 12.5, MemUsage: "150MiB / 7.776GiB", MemPercent: 1.9,
			NetIO: "1.2kB / 3.4kB", NetRX: 1200, NetTX: 3400,
			BlockIO: "1.2MB / 600kB", BlockRX: 1_200_000, BlockTX: 600_000,
		},
		{
			ID: "bbbb111122223333", Name: "api", Image: "golang:1.24", State: "exited",
			CreatedAt: "2026-07-30 09:00:00 +0100 CET", Ports: "",
			CPUPercent: 99, MemUsage: "900MiB / 7.776GiB", // must be ignored: not running
		},
		{
			ID: "cccc111122223333", Name: "cache", Image: "redis:7", State: "paused",
			CreatedAt: "2026-07-29 09:00:00 +0100 CET",
			// Distinct from the others so every sort column is a total order:
			// sort.Slice is not stable, and ties would make the expected
			// sequences ambiguous.
			CPUPercent: 5, MemPercent: 0.5, NetRX: 500, NetTX: 400, BlockRX: 300, BlockTX: 200,
		},
		{
			ID: "dddd111122223333", Name: "zombie", Image: "busybox", State: "dead",
			CreatedAt:  "not a timestamp",
			CPUPercent: 1, MemPercent: 0.1, NetRX: 100, NetTX: 90, BlockRX: 80, BlockTX: 70,
		},
	}
}

// newTestModel returns a laid-out model with no data yet.
func newTestModel(t *testing.T) Model {
	t.Helper()
	return feed(t, New(config.Default()), tea.WindowSizeMsg{Width: 160, Height: 30})
}

// rawModel returns a model that has absorbed a container list and kept the
// view's own default sort — which is name *descending*; see
// TestDefaultSortIsNameDescending and D9 in docs/backlog.md.
func rawModel(t *testing.T) Model {
	t.Helper()
	return feed(t, newTestModel(t), ContainersListMsg{Containers: containerFixtures()})
}

// loadedModel returns a loaded model sorted by name ascending, so the row order
// is api, cache, web, zombie. Selection and action tests use it: they are about
// resolving the cursor, not about the direction the view happens to start in.
func loadedModel(t *testing.T) Model {
	t.Helper()
	m := rawModel(t)
	m.sortAsc = true
	m.updateTable()
	return m
}

// logsModel returns a model showing the logs viewport for the running fixture.
func logsModel(t *testing.T, content string) Model {
	t.Helper()
	m := feed(t, loadedModel(t), testutil.Key("l"))
	return feed(t, m, ContainerLogsLoadedMsg{Content: content})
}

func feed(t *testing.T, m Model, msgs ...tea.Msg) Model {
	t.Helper()
	for _, msg := range msgs {
		m, _ = step(t, m, msg)
	}
	return m
}

func step(t *testing.T, m Model, msg tea.Msg) (Model, tea.Cmd) {
	t.Helper()
	next, cmd := m.Update(msg)
	updated, ok := next.(Model)
	if !ok {
		t.Fatalf("Update() returned %T, want containers.Model", next)
	}
	return updated, cmd
}

// withTrueColor forces the lipgloss default renderer to emit escape sequences
// for the duration of one test. Without it the renderer detects no TTY, falls
// back to the Ascii profile and strips every colour — which would make any
// assertion about styling pass whatever the code does.
func withTrueColor(t *testing.T) {
	t.Helper()
	previous := lipgloss.ColorProfile()
	lipgloss.SetColorProfile(termenv.TrueColor)
	t.Cleanup(func() { lipgloss.SetColorProfile(previous) })
}

// ansiPrefix returns the leading escape sequence of s, up to and including its
// terminating "m", or "" when s is unstyled.
func ansiPrefix(s string) string {
	if !strings.HasPrefix(s, "\x1b[") {
		return ""
	}
	end := strings.Index(s, "m")
	if end < 0 {
		return ""
	}
	return s[:end+1]
}

// rowNames returns the Name cell of every table row.
func rowNames(rows []table.Row) []string {
	names := make([]string, 0, len(rows))
	for _, row := range rows {
		names = append(names, row[0])
	}
	return names
}
