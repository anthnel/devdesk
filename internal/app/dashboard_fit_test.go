package app

import (
	"strings"
	"testing"
	"time"

	tea "charm.land/bubbletea/v2"
	"github.com/charmbracelet/x/ansi"

	"github.com/anthnel/devdesk/internal/docker"
	"github.com/anthnel/devdesk/internal/metrics"
	"github.com/anthnel/devdesk/internal/shared"
	"github.com/anthnel/devdesk/internal/status"
	"github.com/anthnel/devdesk/internal/ui/dashboard"
)

// dashboardAt hosts the real dashboard in the router at width x height, fed
// with the data of the DEVDESK-743 report: no forge session, no monitor,
// Docker reachable with two stopped containers, five images, four networks.
func dashboardAt(t *testing.T, width, height int) *App {
	t.Helper()
	a := routerAt(t, &fakeView{}, width, height)
	var m tea.Model = dashboard.New(a.config, a.sharedState)
	for _, msg := range []tea.Msg{
		dashboard.StatusCheckMsg{Result: status.MonitorResult{Timestamp: time.Now()}},
		dashboard.ForgeStatsMsg{},
		dashboard.DockerStatsMsg{Stats: shared.DockerStats{Available: true, Stopped: 2}},
		dashboard.OCIStatsMsg{Stats: shared.OCIStats{Available: true, ImagesCount: 5, ContainersCount: 2, NetworksCount: 4}},
		dashboard.WorkspaceStatsMsg{Count: 0},
		dashboard.HostSampleMsg{Sample: metrics.HostSample{CPUPercent: 4, MemPercent: 40, MemUsed: 6 << 30, MemTotal: 15 << 30, OK: true}},
		dashboard.DockerMetricsMsg{Aggregate: docker.Aggregate{Available: true}},
	} {
		m, _ = m.Update(msg)
	}
	screen, ok := m.(Screen)
	if !ok {
		t.Fatalf("dashboard.Update returned %T, want a Screen", m)
	}
	a.views[a.currentView] = screen
	a.resize(width, height)
	return a
}

// unclosedBoxes counts the boxes whose top border is on screen and whose
// bottom border is not. It counts the right-hand corners: the left ones are
// ambiguous, a tree branch (`└─ networks`) draws the same glyph as a box's
// bottom-left corner, and the title line is a rule without corners.
func unclosedBoxes(screen string) int {
	plain := ansi.Strip(screen)
	return strings.Count(plain, "┐") - strings.Count(plain, "┘")
}

// TestTheDashboardClosesEveryBoxAtTheReportedSize: the screen of the
// DEVDESK-743 report, 175x38, lost the bottom border of Health and Docker
// because the grid needed 27 content lines and got 26.
func TestTheDashboardClosesEveryBoxAtTheReportedSize(t *testing.T) {
	a := dashboardAt(t, 175, 38)
	if n := unclosedBoxes(a.Render()); n != 0 {
		t.Errorf("at 175x38, %d box(es) lost their bottom border:\n%s", n, ansi.Strip(a.Render()))
	}
}

// TestTheDashboardHasNoTitleLine: the titled rule "── Dashboard ──" said what
// the screen already shows (DEVDESK-743). The window title still names it.
func TestTheDashboardHasNoTitleLine(t *testing.T) {
	a := dashboardAt(t, 120, 40)
	if strings.Contains(ansi.Strip(a.Render()), "Dashboard") {
		t.Errorf("the dashboard still draws its title line:\n%s", ansi.Strip(a.Render()))
	}
	if got := windowTitle(a.currentView, a.currentContext); !strings.Contains(got, "dashboard") {
		t.Errorf("window title %q no longer names the dashboard", got)
	}
}

// TestTheDashboardGetsTheTitleLineBack: the line the rule used to take goes to
// the view, not to a blank row.
func TestTheDashboardGetsTheTitleLineBack(t *testing.T) {
	const width, height = 120, 40
	a := dashboardAt(t, width, height)
	want := height - a.lastHeaderHeight - a.lastFooterHeight
	if got := a.viewport.Height(); got != want {
		t.Errorf("dashboard viewport is %d lines, want %d (terminal %d - header %d - footer %d)",
			got, want, height, a.lastHeaderHeight, a.lastFooterHeight)
	}
	if got := strings.Count(a.Render(), "\n") + 1; got != height {
		t.Errorf("the screen is %d lines, want exactly %d", got, height)
	}
}
