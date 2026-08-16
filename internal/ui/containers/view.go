package containers

import (
	"fmt"
	"strings"

	"github.com/charmbracelet/lipgloss"

	"github.com/anthnel/devdesk/internal/ui/help"
	"github.com/anthnel/devdesk/internal/ui/shortcut"
	"github.com/anthnel/devdesk/internal/ui/theme"
)

// InEditMode returns true when a modal, filter, or logs viewport is active
func (m Model) InEditMode() bool {
	return m.confirmModal != nil || m.containerTable.InEditMode() || m.state == stateLogs
}

// FilterBarVisible returns true when the filter bar is visible (implements app.FilterBarView).
func (m Model) FilterBarVisible() bool {
	return m.containerTable.FilterBar().IsVisible()
}

// GetFooterHeight returns the footer height for this view (Rule 124).
func (m Model) GetFooterHeight() int {
	// filter bar (when visible) + empty line + info line
	return 2 + m.containerTable.FilterBar().ExtraHeight()
}

// RenderFooter returns the footer content rendered below the viewport (Rule 124).
func (m Model) RenderFooter(width int) string {
	var parts []string
	if bar := m.containerTable.FilterBar(); bar.IsVisible() {
		parts = append(parts, bar.View())
	}
	infoLine := theme.EmptyLineBg(width)
	switch {
	case m.errorMsg != "":
		infoLine = theme.PadWithBg(theme.StatusErrorStyle.Render(m.errorMsg), width)
	case m.actionLine() != "":
		infoLine = theme.PadWithBg(theme.Bg("  ")+
			lipgloss.NewStyle().
				Background(theme.ColorBackground).
				Foreground(theme.ColorHighlight).
				Render(m.actionLine()), width)
	}
	parts = append(parts, theme.EmptyLineBg(width), infoLine)
	return strings.Join(parts, "\n")
}

// actionLine names what is running, in words. The spinner on the row says that
// *something* is; this says what.
//
// It is rendered from the current state rather than assigned to a footer
// message, because a footer message expires after three seconds (Rule 128) and
// a `docker stop` outlives that by seven — the same reason §3.17's sync
// progress is a rendered line rather than footerInfo.
//
// An error takes the line ahead of it: a failure the user has not read yet
// matters more than the progress of what is still running.
func (m Model) actionLine() string {
	if m.pruning {
		return "Pruning containers…"
	}
	labels := m.containerTable.BusyLabels()
	switch len(labels) {
	case 0:
		return ""
	case 1:
		return labels[0] + "…"
	default:
		// Naming them all would run past the line on a wide selection; the count
		// is what the user is waiting on anyway.
		return fmt.Sprintf("%d actions running…", len(labels))
	}
}

// GetTitle returns the view title
func (m Model) GetTitle() string {
	if m.state == stateLogs {
		return theme.IconContainer + " Logs: " + m.logsContainerName
	}
	return theme.IconContainer + " Containers"
}

// GetIcon returns the view icon
func (m Model) GetIcon() string {
	return ""
}

// GetHeaderInfo returns the key-value info for the header
func (m Model) GetHeaderInfo(_ string) []shortcut.HeaderInfo {
	label := "Active"
	if m.showAll {
		label = "All"
	}
	total := len(m.containerTable.Visible())
	return []shortcut.HeaderInfo{
		{Key: "Containers", Value: fmt.Sprintf("%d (%s)", total, label), Style: theme.HeaderValueStyle},
	}
}

// GetShortcuts returns the keyboard shortcuts for the header
func (m Model) GetShortcuts() shortcut.Shortcuts {
	if m.state == stateLogs {
		wrapDesc := "Wrap: off"
		if m.logsWrapEnabled {
			wrapDesc = "Wrap: on"
		}
		tsDesc := "Timestamps: off"
		if m.logsTimestamps {
			tsDesc = "Timestamps: on"
		}
		return []shortcut.Shortcut{
			{Key: "w", Description: wrapDesc},
			{Key: "t", Description: tsDesc},
			{Key: "f", Description: "Follow"},
			{Key: "ctrl+r", Description: "Reload"},
			{Key: "e", Description: "External pager"},
			{Key: "esc", Description: "Back"},
		}
	}
	if m.confirmModal != nil {
		return []shortcut.Shortcut{
			{Key: "y/n", Description: "Confirm"},
			{Key: "esc", Description: "Cancel"},
		}
	}
	return []shortcut.Shortcut{
		{Key: "K", Description: "Stop"},
		{Key: "r", Description: "Restart"},
		{Key: "space", Description: "Pause/Resume"},
		{Key: "p", Description: "Prune"},
		{Key: "ctrl+d", Description: "Delete"},
		{Key: "s", Description: "Shell (in-place)"},
		{Key: "S", Description: "Shell (new window)"},
		{Key: "l", Description: "Logs"},
		{Key: "i", Description: "Inspect"},
		{Key: "a", Description: "Toggle all"},
		{Key: ".", Description: "Sort"},
		{Key: "ctrl+r", Description: "Refresh"},
		{Key: "/", Description: "Filter"},
		{Key: "?", Description: "Help"},
	}
}

// View renders the view
func (m Model) View() string {
	// Priority 1: logs viewport
	if m.state == stateLogs {
		return m.renderLogsView()
	}

	// Priority 2: confirm modal (centered)
	if m.confirmModal != nil {
		return lipgloss.Place(
			m.width, m.height,
			lipgloss.Center, lipgloss.Center,
			m.confirmModal.View(),
			lipgloss.WithWhitespaceBackground(theme.ColorBackground),
		)
	}

	// Priority 3: normal view
	return m.renderNormalView()
}

// renderLogsView renders the internal logs viewport
func (m Model) renderLogsView() string {
	if m.logsLoading {
		return theme.SpinnerMessage(m.spinner.View(), "Loading logs...")
	}
	return m.logsViewport.View()
}

// renderNormalView renders the main container list view
func (m Model) renderNormalView() string {
	var sections []string

	// Loading or table
	if m.loading && len(m.containerTable.Items()) == 0 {
		sections = append(sections, theme.SpinnerMessage(m.spinner.View(), "Loading containers..."))
	} else if len(m.containerTable.Visible()) == 0 && !m.containerTable.FilterBar().IsVisible() {
		sections = append(sections, theme.DimStyle.Render("No containers found"))
	} else {
		// Always render the table when a filter is active so the filter bar stays at the bottom
		sections = append(sections, m.containerTable.View())
	}

	return strings.Join(sections, "\n")
}

// GetHelpContent returns help content for the containers view (Rule 114)
func (m Model) GetHelpContent() help.Content {
	return help.Content{
		Title:       "Docker Containers",
		Description: "This view lists and manages Docker containers. It displays real-time metrics (CPU, Memory, Network RX/TX) and supports filtering, sorting, lifecycle actions, and interactive shell access.",
		KeyBindings: []help.KeyBinding{
			{Key: "K", Description: "Stop the selected container (running only)"},
			{Key: "r", Description: "Restart the selected container"},
			{Key: "Space", Description: "Toggle pause: pause a running container, or resume a paused one"},
			{Key: "p", Description: "Prune: remove all stopped containers (with confirmation)"},
			{Key: "ctrl+d", Description: "Delete the selected container (with confirmation)"},
			{Key: "s", Description: "Open an interactive shell (bash if available, sh otherwise) in the selected container, suspending the TUI (running only)"},
			{Key: "S", Description: "Open the same shell in a new terminal window, leaving the TUI running (running only)"},
			{Key: "l", Description: "View logs in the internal viewport. Press Esc to return, 'e' to open in external pager, 'w' to toggle wrap, 't' to toggle timestamps, 'f' to follow live output."},
			{Key: "i", Description: "View inspect data (JSON) in system pager (less). Press q to return."},
			{Key: "a", Description: "Toggle between active-only and all containers (including stopped)"},
			{Key: ".", Description: "Cycle sort column (Name → Image → CPU → Mem → Net RX → Net TX → Block RX → Block TX → Created). Each press toggles asc/desc then moves to next column."},
			{Key: "ctrl+r", Description: "Refresh containers and metrics"},
			{Key: "/", Description: "Activate filter input to search by name, image, or state"},
			{Key: "↑/k", Description: "Move selection up"},
			{Key: "↓/j", Description: "Move selection down"},
			{Key: "g/Home", Description: "Go to top of list"},
			{Key: "G/End", Description: "Go to bottom of list"},
			{Key: "alt+:", Description: "Open command mode"},
			{Key: "?", Description: "Show this help"},
		},
		Sections: []help.Section{
			{
				Title: "Columns",
				Body: "Status (first, untitled): the container state — " + theme.IconCaretRight + " running, " + theme.IconSmallPause + " paused, " + theme.IconSmallSquare + " exited, " + theme.IconCaretUp + " created/restarting, " + theme.IconBan + " dead. A spinner replaces it while an action is running on that container.\n" +
					"Name: Container name.\n" +
					"Image: Docker image.\n" +
					"CPU: CPU usage percentage.\n" +
					"Mem: Memory usage as compact label (e.g. '150M/8G').\n" +
					"Net RX: Cumulative network bytes received since container start (e.g. '1.2kB', '3.4MB').\n" +
					"Net TX: Cumulative network bytes transmitted since container start.\n" +
					"Block RX: Cumulative block device bytes read since container start.\n" +
					"Block TX: Cumulative block device bytes written since container start.\n" +
					"Created: Relative timestamp when the container was created.\n" +
					"Ports: Published port mappings (host:container).",
			},
			{
				Title: "Running Actions",
				Body: "Stop, restart, pause/resume and delete run in the background. While one is running the container's status column shows a spinner, the row is dimmed, and the footer names what is happening — 'docker stop' waits ten seconds for the container to exit on its own.\n" +
					"A second action on the same container is refused until the first one finishes; the cursor is free to move in the meantime, and the spinner stays with the container rather than following it.",
			},
			{
				Title: "Metrics Refresh",
				Body:  "All metrics (CPU, Memory, Net RX/TX) are fetched every 2 seconds using 'docker stats --no-stream'. Only running containers display metrics; stopped or paused containers show '-' in metric columns.",
			},
			{
				Title: "Filter",
				Body:  "Press '/' to activate the filter input. Type to search by container name, image, or state. Press Enter to confirm the filter or Esc to clear it.",
			},
			{
				Title: "Shell Access",
				Body:  "Pressing 's' suspends the TUI and opens an interactive /bin/sh shell inside the container. Type 'exit' to return to the TUI. Only available for running containers.",
			},
			{
				Title: "Logs & Inspect",
				Body: "Pressing 'l' opens the last 500 lines of container logs in an internal scrollable viewport.\n" +
					"Navigation: ↑/↓ scroll, g/G top/bottom, pgup/pgdn page.\n" +
					"'/' search inline (n/N next/prev match), ctrl+r reload, Esc return.\n" +
					"'w' toggle soft word-wrap (clears active search).\n" +
					"'t' toggle timestamps — refetches logs with RFC3339Nano prefix per line.\n" +
					"'f' follow live output (docker logs -f, Ctrl+C to return).\n" +
					"'e' open in external pager ($PAGER or less).\n" +
					"Active options (wrap, timestamps) are shown in the status bar at the bottom.\n\n" +
					"Pressing 'i' shows the full JSON output of 'docker inspect' in the system pager (less). Press 'q' to return to the TUI.",
			},
			{
				Title: "Prerequisites",
				Body:  "The Docker CLI must be installed and accessible in your PATH. The current user must have permission to run docker commands.",
			},
		},
	}
}
