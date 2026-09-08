package containers

import (
	"fmt"
	"strings"

	"github.com/charmbracelet/lipgloss"

	sharedcomponents "github.com/anthnel/devdesk/internal/ui/components"
	"github.com/anthnel/devdesk/internal/ui/help"
	"github.com/anthnel/devdesk/internal/ui/shortcut"
	"github.com/anthnel/devdesk/internal/ui/theme"

	"github.com/anthnel/devdesk/internal/ui/keymap"
)

// InEditMode returns true when a modal or the filter is active
func (m Model) InEditMode() bool {
	return m.anyModal() != nil || m.containerTable.InEditMode()
}

// anyModal is the one place that answers "is a modal open, and which".
//
// It exists because the alternative failed: K's choice modal was added to the
// key chain and to Update, and left out of View, InEditMode and GetShortcuts.
// Nothing said so — the modal opened, took the keyboard from priority 1, and
// rendered nothing, so the whole view went dead on one keystroke. Resolving it
// once means a new modal cannot be half-wired.
func (m Model) anyModal() interface{ View() string } {
	switch {
	case m.confirmModal != nil:
		return m.confirmModal
	case m.choiceModal != nil:
		return m.choiceModal
	}
	return nil
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
	parts = append(parts, theme.EmptyLineBg(width), m.footer.View(width, m.status()))
	return strings.Join(parts, "\n")
}

// status is what the view derives on every frame: the load, then whatever
// action is running. Neither has a timer, and both are displaced by a message.
func (m Model) status() sharedcomponents.Status {
	if m.loading && len(m.containerTable.Items()) == 0 {
		return sharedcomponents.Status{Text: "Loading containers...", Spinner: true}
	}
	return sharedcomponents.Status{Text: m.actionLine()}
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
	return theme.IconContainer + " Containers"
}

// GetIcon returns the view icon
func (m Model) GetIcon() string {
	return ""
}

// GetHeaderInfo returns the key-value info for the header.
//
// The count, and nothing about the filter. It used to read "4 (Active)", which
// put the scope in the one place Rule 136 keeps clear of filters — and said it
// twice over once the search bar existed, in two different vocabularies. The
// filter bar carries it now, tokens and all.
func (m Model) GetHeaderInfo(_ string) []shortcut.HeaderInfo {
	total := len(m.containerTable.Visible())
	return []shortcut.HeaderInfo{
		{Key: "Containers", Value: fmt.Sprintf("%d", total), Style: theme.HeaderValueStyle},
	}
}

// GetShortcuts returns the keyboard shortcuts for the header
func (m Model) GetShortcuts() shortcut.Shortcuts {
	if m.choiceModal != nil {
		return []shortcut.Shortcut{
			{Key: "←→", Description: "Choose"},
			{Key: "enter", Description: "Confirm"},
			{Key: "esc", Description: "Cancel"},
		}
	}
	if m.confirmModal != nil {
		return []shortcut.Shortcut{
			{Key: "y/n", Description: "Confirm"},
			{Key: "esc", Description: "Cancel"},
		}
	}
	return []shortcut.Shortcut{
		{Key: keymap.Kill, Description: "Stop or restart"},
		{Key: "space", Description: "Pause/Resume"},
		{Key: keymap.Prune, Description: "Prune"},
		{Key: keymap.Delete, Description: "Delete"},
		{Key: keymap.Terminal, Description: "Shell"},
		{Key: keymap.Logs, Description: "Logs"},
		{Key: "enter", Description: "Inspect"},
		{Key: "r", Description: "Toggle running"},
		{Key: "p", Description: "Toggle paused"},
		{Key: "s", Description: "Toggle stopped"},
		{Key: "t", Description: "Toggle transient"},
		{Key: "z", Description: "Reset filters"},
		{Key: ".", Description: "Sort"},
		{Key: "ctrl+r", Description: "Refresh"},
		{Key: "/", Description: "Filter"},
		{Key: "?", Description: "Help"},
	}
}

// View renders the view
func (m Model) View() string {
	// Priority 1: whichever modal is open (centered)
	if modal := m.anyModal(); modal != nil {
		return lipgloss.Place(
			m.width, m.height,
			lipgloss.Center, lipgloss.Center,
			modal.View(),
			lipgloss.WithWhitespaceBackground(theme.ColorBackground),
		)
	}

	// Priority 2: normal view
	return m.renderNormalView()
}

// renderNormalView renders the main container list view.
//
// The load says so in the footer, with a spinner, and the table stays on
// screen: a body that swapped itself for a spinner lost its header and its
// columns for the length of every ctrl+r, then got them back — a jump in the
// layout on every refresh. An empty table — Docker has none, or the state
// tokens hide them all — stays a table too (Rule 139): its header and no rows,
// with the count in GetHeaderInfo's "Containers" field.
func (m Model) renderNormalView() string {
	return m.containerTable.View()
}

// GetHelpContent returns help content for the containers view (Rule 114)
func (m Model) GetHelpContent() help.Content {
	return help.Content{
		Title:       "Docker Containers",
		Description: "This view lists and manages Docker containers. It displays real-time metrics (CPU, Memory, Network RX/TX) and supports filtering, sorting, lifecycle actions, and interactive shell access.",
		KeyBindings: []help.KeyBinding{
			{Key: keymap.Kill, Description: "Ask whether to stop or restart the selected container"},
			{Key: "Space", Description: "Toggle pause: pause a running container, or resume a paused one"},
			{Key: keymap.Prune, Description: "Prune: remove all stopped containers (with confirmation)"},
			{Key: keymap.Delete, Description: "Delete the selected container (with confirmation)"},
			{Key: keymap.Terminal, Description: "Open an interactive shell (bash if available, sh otherwise) in the selected container. In place or in a new window, per app.terminal_new_window (running only)"},
			{Key: keymap.Logs, Description: "Open the container's logs in the viewer (Esc returns here)"},
			{Key: "enter", Description: "Open 'docker inspect' in the viewer, as a navigable JSON tree (Esc returns here)"},
			{Key: "r", Description: "Filter: running containers"},
			{Key: "p", Description: "Filter: paused containers"},
			{Key: "s", Description: "Filter: stopped containers (exited, dead)"},
			{Key: "t", Description: "Filter: containers in transition (created, restarting, removing)"},
			{Key: "z", Description: "Reset — drops every state filter and the search, back to running only"},
			{Key: ".", Description: "Cycle sort column (Name → Image → CPU → Mem → Net RX → Net TX → Block RX → Block TX → Created). Each press toggles asc/desc then moves to next column."},
			{Key: "ctrl+r", Description: "Refresh containers and metrics"},
			{Key: "/", Description: "Activate filter input to search by name, image, or state"},
			{Key: "↑/k", Description: "Move selection up"},
			{Key: "↓/j", Description: "Move selection down"},
			{Key: "ctrl+p", Description: "Open command mode"},
			{Key: "?", Description: "Show this help"},
		},
		Sections: []help.Section{
			{
				Title: "Columns",
				Body: "Status (first, untitled): the container state — " + theme.IconCaretRight + " running, " + theme.IconSmallPause + " paused, " + theme.IconSmallSquare + " exited, " + theme.IconCaretUp + " created/restarting, " + theme.IconBan + " dead. A spinner replaces it while an action is running on that container.\n" +
					"Name: Container name.\n" +
					"Image: Docker image.\n" +
					"CPU: CPU usage percentage, as 'docker stats' counts it — relative to one core, so a container busy on two cores reads 200%.\n" +
					"1 core: the CPU percentage as a bar, full at one core. It saturates above that, which is why the number stays beside it. The filled part is green below 75%, orange from there, red from 90%; the empty part stays a fixed muted color at every level.\n" +
					"Mem: Memory usage as compact label (e.g. '150M/8G').\n" +
					"Limit: how full that label is — the share of the limit docker reports for the container, which is the daemon's total when the container declares none. Same coloring as 1 core.\n" +
					"Net RX: Cumulative network bytes received since container start (e.g. '1.2kB', '3.4MB').\n" +
					"Net TX: Cumulative network bytes transmitted since container start.\n" +
					"Block RX: Cumulative block device bytes read since container start.\n" +
					"Block TX: Cumulative block device bytes written since container start.\n" +
					"Ports: One entry per publication — the host port, and an icon saying who can reach it: " +
					theme.IconNetwork + " every interface, " + theme.IconHome + " this machine only, " +
					theme.IconServer + " one named address, " + theme.IconLock + " declared by the image but published by nobody. " +
					"A protocol is named only when it is not tcp. The two lines docker prints for a dual-stack publication are one entry here, and the container-side port is left out — press enter for the full mapping.\n" +
					"Created: Relative timestamp when the container was created.\n" +
					"On a narrow terminal the two gauges are the first columns to go, before the I/O counters: they draw a number that stays on screen without them. They also lose their color on the selected row, like every colored column.",
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
				Body: "The view opens on the running containers, with no filter bar. Every container is loaded either way, so a state is turned on and off instantly — nothing is refetched.\n\n" +
					"'r', 'p', 's' and 't' toggle the four state groups, and they are cumulative: 'r'+'s' asks for running or stopped, and all four together show every container. As soon as one is on, the bar appears and names exactly what is being shown.\n\n" +
					"'z' turns them all off and returns to the opening state — running only, bar hidden. It also drops the search, which is the half a state key cannot reach.\n\n" +
					"Press '/' to search by container name, image, or state. Enter confirms, Esc clears. The search narrows whatever the state filters left.",
			},
			{
				Title: "Shell Access",
				Body:  "'T' opens an interactive shell inside the container — bash if it has one, sh otherwise. In place (type 'exit' to return to the TUI) or in a new terminal window, per app.terminal_new_window. Only available for running containers.",
			},
			{
				Title: "Logs & Inspect",
				Body: "Both open in the document viewer, and Esc there returns to this list.\n\n" +
					"'l' opens the last 500 lines of the container's logs. The viewer reads their levels: 'v' filters " +
					"by verbosity, '/' searches, 'w' wraps long lines, 't' toggles timestamps, ctrl+r reloads, " +
					"ctrl+f follows live output and 'e' opens the system pager. A line with no level of its own " +
					"belongs to the entry above it, so filtering never breaks a stack trace apart.\n\n" +
					"'i' opens 'docker inspect' as a navigable JSON tree: → expands a node, ← collapses it, and 'f' " +
					"shows the raw JSON instead. Neither suspends the TUI.",
			},
			{
				Title: "Prerequisites",
				Body:  "The Docker CLI must be installed and accessible in your PATH. The current user must have permission to run docker commands.",
			},
		},
	}
}
