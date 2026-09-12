package containers

import (
	"fmt"
	"log"
	"os/exec"
	"slices"
	"strconv"
	"strings"
	"time"

	"github.com/anthnel/devdesk/internal/docker"
	"github.com/anthnel/devdesk/internal/engine"
	sharedcomponents "github.com/anthnel/devdesk/internal/ui/components"
	"github.com/anthnel/devdesk/internal/ui/keymap"
	uiterminal "github.com/anthnel/devdesk/internal/ui/terminal"
	"github.com/anthnel/devdesk/internal/ui/theme"
	uiviewer "github.com/anthnel/devdesk/internal/ui/viewer"
	"github.com/charmbracelet/bubbles/spinner"
	tea "github.com/charmbracelet/bubbletea"
)

// Init initializes the containers view
func (m Model) Init() tea.Cmd {
	return tea.Batch(
		m.spinner.Tick,
		tickCmd(),
		fetchContainers(),
	)
}

// Update handles messages
func (m Model) Update(msg tea.Msg) (tea.Model, tea.Cmd) {
	switch msg := msg.(type) {
	case tea.WindowSizeMsg:
		m.resize(msg.Width, msg.Height)
		return m, nil

	case tea.KeyMsg:
		return m.handleKeyMsg(msg)

	case spinner.TickMsg:
		// The table's own frame advances whenever anything is running on a row,
		// which is not the same condition as the view's spinner: the list is
		// loaded and on screen while a container stops.
		if len(m.containerTable.BusyLabels()) > 0 {
			m.containerTable.AdvanceSpinner()
			var cmd tea.Cmd
			m.spinner, cmd = m.spinner.Update(msg)
			m.footer.SetSpinnerFrame(m.spinner.View())
			return m, cmd
		}
		if m.loading {
			var cmd tea.Cmd
			m.spinner, cmd = m.spinner.Update(msg)
			// The load is reported in the footer, so the frame has to reach it —
			// a spinner stuck on frame zero reads as a hang.
			m.footer.SetSpinnerFrame(m.spinner.View())
			return m, cmd
		}

	case RefreshTickMsg:
		return m, tea.Batch(fetchContainers(), fetchMetrics())

	case ContainersListMsg:
		return m.handleContainersList(msg)

	case ContainerMetricsMsg:
		return m.handleContainerMetrics(msg)

	case ContainerActionMsg:
		return m.handleContainerAction(msg)

	case PagerExitMsg:
		// Only the shell path comes back here now. The logs pane went to the
		// viewer, and its pager and follow went with it (sources.go).
		return m, tea.Batch(tickCmd(), fetchContainers())

	case sharedcomponents.ConfirmModalYesMsg:
		return m.handleConfirmYes()

	case sharedcomponents.ConfirmModalNoMsg:
		m.confirmModal = nil
		m.pendingAction = ""
		return m, nil

	case sharedcomponents.ChoiceModalPickedMsg:
		return m.handleChoicePicked(msg)

	case sharedcomponents.ChoiceModalCancelledMsg:
		m.choiceModal = nil
		return m, nil

	case ContainerPruneMsg:
		return m.handlePruneComplete(msg)

	case ShellWindowOpenedMsg:
		if msg.Err != nil {
			log.Printf("ERROR [containers] open shell window: %v", msg.Err)
			return m, m.footer.Error("Failed to open terminal — check logs")
		}
		return m, nil
	}

	// Every message the table reacts to is a key, and keys are routed by
	// handleKeyMsg above, so nothing falls through to it here.
	m.footer.Handle(msg)
	return m, nil
}

// handleKeyMsg processes keyboard input with priority chain
func (m Model) handleKeyMsg(msg tea.KeyMsg) (tea.Model, tea.Cmd) {
	// Priority 1: whichever modal is open
	if m.confirmModal != nil {
		var cmd tea.Cmd
		m.confirmModal, cmd = m.confirmModal.Update(msg)
		return m, cmd
	}
	if m.choiceModal != nil {
		var cmd tea.Cmd
		m.choiceModal, cmd = m.choiceModal.Update(msg)
		return m, cmd
	}

	// Priority 2: filter input active
	if m.containerTable.InEditMode() {
		return m, m.containerTable.Update(msg)
	}

	// Priority 3: normal mode
	return m.handleNormalKeyMsg(msg)
}

// handleNormalKeyMsg handles keys in normal mode
func (m Model) handleNormalKeyMsg(msg tea.KeyMsg) (tea.Model, tea.Cmd) {
	switch msg.String() {
	// The state filters. Nothing is refetched: the list already holds every
	// container, so a state is turned on and off in the frame it is pressed.
	case "r":
		m.toggleToken(filterTokenRunning)
		return m, nil

	case "p":
		m.toggleToken(filterTokenPaused)
		return m, nil

	case "s":
		m.toggleToken(filterTokenStopped)
		return m, nil

	case "t":
		m.toggleToken(filterTokenTransient)
		return m, nil

	case "z":
		return m.clearFilters()

	case keymap.Kill:
		return m.confirmStopOrRestart()

	// Space stays unconfirmed, deliberately: pause is reversible and instant,
	// and a modal seen ten times an hour trains the user to hit y without
	// reading — which is exactly what makes the delete confirmation worthless.
	case " ":
		return m.pauseToggleSelectedContainer()

	case keymap.Prune:
		return m.pruneContainers()

	case keymap.Delete:
		return m.deleteSelectedContainer()

	case "ctrl+r":
		m.loading = true
		return m, tea.Batch(m.spinner.Tick, fetchContainers(), fetchMetrics())

	// The "new window" variant is a setting rather than a second key: the
	// capability belongs to the environment, not to the moment (§3.26).
	case keymap.Terminal:
		return m.shellSelectedContainer()

	case keymap.Logs:
		return m.logsSelectedContainer()

	// Inspect moved off `i`, which the dashboard needed for issues. `enter` was
	// unbound here and is already the inspect gesture in OCI/Networks.
	case "enter":
		return m.inspectSelectedContainer()

	}

	// Navigation, `/` and `.` are the table's, not the view's.
	return m, m.containerTable.Update(msg)
}

// toggleToken flips a state filter and returns to the top, since the list under
// the cursor is not the list the user was looking at any more.
func (m *Model) toggleToken(label string) {
	m.containerTable.SetTokenActive(label, !m.containerTable.IsTokenActive(label))
	m.containerTable.GotoTop()
}

// clearFilters turns every state off and drops the search, which puts the view
// back exactly where it opens: the running containers, and no bar.
//
// That equivalence is the point. `z` on a freshly opened view has to be a
// no-op — two ways to reach one screen is what the arrival state and the reset
// were before, when the default was a lit token and `z` unlit it.
func (m Model) clearFilters() (tea.Model, tea.Cmd) {
	for _, label := range stateTokens {
		m.containerTable.SetTokenActive(label, false)
	}
	m.containerTable.FilterBar().ClearSearch()
	m.containerTable.SetItems(m.containerTable.Items()) // re-apply with the query gone
	m.containerTable.GotoTop()
	return m, nil
}

// getSelectedContainer returns the selected container or nil.
//
// The cursor is resolved against the very slice the rows were built from, so it
// can no longer point at one ordering while the screen shows another.
func (m *Model) getSelectedContainer() *docker.Container {
	c, ok := m.containerTable.Selected()
	if !ok {
		return nil
	}
	return &c
}

// busyMessage is what every action says when one is already running on that
// container. One message rather than five, because the user's next move is the
// same whichever it is: wait.
const busyMessage = "Already busy — an action is running on this container"

// startAction marks the container busy and issues the command, or refuses when
// one is already running on it.
//
// The refusal is the point, and it is not cosmetic: `docker stop` takes the ten
// second grace period by default, and nothing used to stop a second keypress
// from issuing a second command. The second one fails with "no such container"
// on an action that in fact worked, so the user is told an operation failed
// when it did not.
//
// The guard asks the table rather than a field of this model, because the table
// is what the spinner is read from — one answer, so the screen and the refusal
// cannot disagree.
func (m Model) startAction(c *docker.Container, label string, cmd tea.Cmd) (tea.Model, tea.Cmd) {
	if m.containerTable.IsBusy(c.ID) {
		return m, m.footer.Warn(busyMessage)
	}
	m.containerTable.MarkBusy(c.ID, label+" "+c.Name)
	return m, tea.Batch(cmd, m.busyTick())
}

// busyTick is what an action returns alongside its command so the row's frame
// turns. Init starts a tick, but it stops as soon as the list has arrived and
// nothing is running; an action begun after that would sit on frame zero for
// the whole ten second grace period of a `docker stop` — the freeze the spinner
// exists to disprove.
//
// Restarting a loop that is already running costs nothing: bubbles tags each
// tick, so the first one accepted invalidates the other and one loop remains.
func (m Model) busyTick() tea.Cmd { return m.spinner.Tick }

// confirmStopOrRestart asks which of the two to run, or neither.
//
// Both used to act with no confirmation at all, unlike delete — and with caps
// lock on, a `k` meant for scrolling stopped the selected container. Restart is
// the worse of the two, being a stop and a start, so it cuts live connections
// just the same. The confirmation was needed either way; having it is what
// makes the second action free and lets `r` go (§3.26).
func (m Model) confirmStopOrRestart() (tea.Model, tea.Cmd) {
	c := m.getSelectedContainer()
	if c == nil {
		return m, nil
	}
	if m.containerTable.IsBusy(c.ID) {
		return m, m.footer.Warn(busyMessage)
	}
	m.choiceModal = sharedcomponents.NewChoiceModal(
		"Container",
		fmt.Sprintf("What should happen to '%s'?", c.Name),
		choiceStop, choiceRestart,
	)
	return m, nil
}

// The two labels the choice modal offers, named so the handler routes on the
// same strings the modal rendered.
const (
	choiceStop    = "Stop"
	choiceRestart = "Restart"
)

// handleChoicePicked runs whichever action the modal returned.
func (m Model) handleChoicePicked(msg sharedcomponents.ChoiceModalPickedMsg) (tea.Model, tea.Cmd) {
	m.choiceModal = nil
	switch msg.Label {
	case choiceStop:
		return m.stopSelectedContainer()
	case choiceRestart:
		return m.restartSelectedContainer()
	}
	return m, nil
}

// stopSelectedContainer stops the selected container
func (m Model) stopSelectedContainer() (tea.Model, tea.Cmd) {
	c := m.getSelectedContainer()
	if c == nil || c.State != "running" {
		return m, nil
	}
	return m.startAction(c, "Stopping", stopContainer(c.ID, c.Name))
}

// restartSelectedContainer restarts the selected container
func (m Model) restartSelectedContainer() (tea.Model, tea.Cmd) {
	c := m.getSelectedContainer()
	if c == nil {
		return m, nil
	}
	return m.startAction(c, "Restarting", restartContainer(c.ID, c.Name))
}

// pauseToggleSelectedContainer pauses a running container or unpauses a paused one
func (m Model) pauseToggleSelectedContainer() (tea.Model, tea.Cmd) {
	c := m.getSelectedContainer()
	if c == nil {
		return m, nil
	}
	switch c.State {
	case "running":
		return m.startAction(c, "Pausing", pauseContainer(c.ID, c.Name))
	case "paused":
		return m.startAction(c, "Resuming", unpauseContainer(c.ID, c.Name))
	}
	return m, nil
}

// deleteSelectedContainer shows confirm modal for deletion
func (m Model) deleteSelectedContainer() (tea.Model, tea.Cmd) {
	c := m.getSelectedContainer()
	if c == nil {
		return m, nil
	}
	m.pendingAction = "confirm-delete"
	m.confirmModal = sharedcomponents.NewConfirmModal(
		"Delete Container",
		fmt.Sprintf("Are you sure you want to delete '%s'?", c.Name),
	)
	return m, nil
}

// pruneContainers shows confirm modal for pruning stopped containers
func (m Model) pruneContainers() (tea.Model, tea.Cmd) {
	m.pendingAction = "confirm-prune"
	m.confirmModal = sharedcomponents.NewConfirmModal(
		"Prune Containers",
		"Remove all stopped containers?\n\nThis will permanently delete all containers\nthat are not currently running.",
	)
	return m, nil
}

// detectShell returns "/bin/bash" if bash is available in the container, else "/bin/sh".
// The check is synchronous (~20-50ms) since the container is already running.
func detectShell(containerID string) string {
	err := exec.Command(engine.Current().Binary, "exec", containerID, "which", "bash").Run() //nolint:gosec // the binary is a declared engine name or a configured path
	if err == nil {
		return "/bin/bash"
	}
	return "/bin/sh"
}

// shellSelectedContainer opens a shell in the selected container, in place or
// in a window of its own depending on app.terminal_new_window.
//
// Prefers /bin/bash when available, falls back to /bin/sh. The two used to be
// `s` and `S`, spending a second letter on a choice that belongs to the
// environment: there is no window to open under WSL or through SSH (§3.26).
func (m Model) shellSelectedContainer() (tea.Model, tea.Cmd) {
	c := m.getSelectedContainer()
	if c == nil || c.State != "running" {
		return m, nil
	}
	if m.config.App.TerminalNewWindow {
		return m.shellSelectedContainerInNewWindow()
	}

	shell := detectShell(c.ID)
	shellCmd := exec.Command(engine.Current().Binary, "exec", "-it", c.ID, shell) //nolint:gosec // same
	return m, tea.ExecProcess(shellCmd, func(err error) tea.Msg {
		return PagerExitMsg{Err: err}
	})
}

// shellSelectedContainerInNewWindow opens a shell in the selected container in a new
// terminal window using the auto-detected terminal emulator (non-blocking).
func (m Model) shellSelectedContainerInNewWindow() (tea.Model, tea.Cmd) {
	c := m.getSelectedContainer()
	if c == nil || c.State != "running" {
		return m, nil
	}

	shell := detectShell(c.ID)
	innerArgs := []string{engine.Current().Binary, "exec", "-it", c.ID, shell}

	bin, args, ok := uiterminal.ForCmd(innerArgs)
	if !ok {
		return m, m.footer.Warn("Terminal not detected — use [s] for in-place shell")
	}

	return m, func() tea.Msg {
		cmd := exec.Command(bin, args...)
		return ShellWindowOpenedMsg{Err: cmd.Start()}
	}
}

// logsSelectedContainer opens the container's log in the document viewer.
//
// This used to be a pane of its own — a viewport, soft wrap, ANSI stripping,
// scroll keys, reload, follow, a timestamps toggle and an external pager. All of
// it is the viewer's now, and the three things that really were about docker
// travel as the source's optional capabilities (sources.go). What is left here
// is naming the container.
func (m Model) logsSelectedContainer() (tea.Model, tea.Cmd) {
	c := m.getSelectedContainer()
	if c == nil {
		return m, nil
	}
	source := logsSource{ID: c.ID, Container: c.Name}
	return m, func() tea.Msg { return uiviewer.OpenRequestMsg{Source: source} }
}

// inspectSelectedContainer opens `docker inspect` in the document viewer.
//
// The pager is gone, and the Windows temp file with it: that branch existed only
// because `less` is absent there, and `more` cannot read a pipe. The viewer is
// the same answer on every platform, and it never suspends the TUI.
//
// The ID check stays where it was worth keeping — in docker.InspectContainer,
// next to the subprocess it guards, rather than at one of its call sites.
func (m Model) inspectSelectedContainer() (tea.Model, tea.Cmd) {
	c := m.getSelectedContainer()
	if c == nil {
		return m, nil
	}
	if !docker.IsContainerID(c.ID) {
		log.Printf("ERROR [containers] inspect: rejected malformed container ID %q", c.ID)
		return m, m.footer.Error("Cannot inspect — invalid container ID")
	}
	source := inspectSource{ID: c.ID, Container: c.Name}
	return m, func() tea.Msg { return uiviewer.OpenRequestMsg{Source: source} }
}

// handleConfirmYes routes confirmed modal actions
func (m Model) handleConfirmYes() (tea.Model, tea.Cmd) {
	m.confirmModal = nil
	action := m.pendingAction
	m.pendingAction = ""

	switch action {
	case "confirm-prune":
		m.pruning = true
		return m, pruneContainers()
	default: // confirm-delete
		c := m.getSelectedContainer()
		if c == nil {
			return m, nil
		}
		return m.startAction(c, "Removing", removeContainer(c.ID, c.Name))
	}
}

// handlePruneComplete processes the prune result
func (m Model) handlePruneComplete(msg ContainerPruneMsg) (tea.Model, tea.Cmd) {
	m.pruning = false
	if msg.Err != nil {
		log.Printf("ERROR [containers] prune: %v", msg.Err)
		return m, m.footer.Error("Prune failed — check logs")
	}
	m.footer.Clear()
	return m, fetchContainers()
}

// handleContainersList processes the container list response
func (m Model) handleContainersList(msg ContainersListMsg) (tea.Model, tea.Cmd) {
	m.loading = false
	if msg.Err != nil {
		log.Printf("ERROR [containers] list: %v", msg.Err)
		return m, m.footer.Error("Failed to load containers — check logs")
	}
	m.footer.Clear()
	m.containerTable.SetItems(msg.Containers)
	return m, nil
}

// handleContainerMetrics merges metrics into existing containers.
//
// The list is cloned rather than written through Items(): the table's slice is
// what its current rows were built from, and editing it in place would leave
// the two disagreeing until the next rebuild.
func (m Model) handleContainerMetrics(msg ContainerMetricsMsg) (tea.Model, tea.Cmd) {
	if msg.Err != nil || msg.Metrics == nil {
		return m, nil
	}

	containers := slices.Clone(m.containerTable.Items())
	for i, c := range containers {
		if metrics, ok := msg.Metrics[c.ID]; ok {
			containers[i].CPUPercent = metrics.CPUPercent
			containers[i].MemUsage = metrics.MemUsage
			containers[i].MemPercent = metrics.MemPercent
			containers[i].NetIO = metrics.NetIO
			containers[i].NetRX = metrics.NetRX
			containers[i].NetTX = metrics.NetTX
			containers[i].BlockIO = metrics.BlockIO
			containers[i].BlockRX = metrics.BlockRX
			containers[i].BlockTX = metrics.BlockTX
		}
	}

	m.containerTable.SetItems(containers)
	return m, nil
}

// handleContainerAction processes action results.
//
// The marker is cleared before anything else, and on *every* outcome. Clearing
// it only on success would leave the row spinning for the life of the view —
// and worse, hide the state the container still has: a `docker stop` that
// failed has to read `running` again rather than go on turning.
func (m Model) handleContainerAction(msg ContainerActionMsg) (tea.Model, tea.Cmd) {
	m.containerTable.ClearBusy(msg.ID)
	if msg.Err != nil {
		log.Printf("ERROR [containers] %s %s: %v", msg.Action, msg.Name, msg.Err)
		return m, m.footer.Error("Action failed — check logs")
	}
	m.footer.Clear()
	return m, fetchContainers()
}

// formatNetBytes formats bytes into a compact human-readable string (e.g. "1.2kB", "3.4MB")
func formatNetBytes(b int64) string {
	switch {
	case b >= 1e9:
		return fmt.Sprintf("%.1fGB", float64(b)/1e9)
	case b >= 1e6:
		return fmt.Sprintf("%.1fMB", float64(b)/1e6)
	case b >= 1e3:
		return fmt.Sprintf("%.1fkB", float64(b)/1e3)
	default:
		return fmt.Sprintf("%dB", b)
	}
}

// stateIcon returns a compact Nerd Font icon representing the container state
func stateIcon(state string) string {
	switch state {
	case "running":
		return theme.IconCaretRight
	case "paused":
		return theme.IconSmallPause
	case "exited":
		return theme.IconSmallSquare
	case "created", "restarting":
		return theme.IconCaretUp
	case "dead":
		return theme.IconBan
	default:
		return theme.IconGitUntracked
	}
}

// formatMemUsage parses docker MemUsage "150MiB / 7.776GiB" into compact "150M/8G"
func formatMemUsage(memUsage string) string {
	parts := strings.SplitN(memUsage, "/", 2)
	if len(parts) != 2 {
		return memUsage
	}
	used := formatMemValue(strings.TrimSpace(parts[0]))
	total := formatMemValue(strings.TrimSpace(parts[1]))
	return used + "/" + total
}

// formatMemValue converts "150MiB" -> "150M", "7.776GiB" -> "8G", "512KiB" -> "512K"
func formatMemValue(val string) string {
	val = strings.TrimSpace(val)

	// Try each unit suffix
	for _, suffix := range []struct {
		docker string
		short  string
		isGig  bool
	}{
		{"GiB", "G", true},
		{"MiB", "M", false},
		{"KiB", "K", false},
		{"B", "B", false},
	} {
		if strings.HasSuffix(val, suffix.docker) {
			numStr := strings.TrimSuffix(val, suffix.docker)
			num, err := strconv.ParseFloat(numStr, 64)
			if err != nil {
				return val
			}
			if suffix.isGig {
				// Round GiB to nearest integer
				return fmt.Sprintf("%.0f%s", num, suffix.short)
			}
			// For MiB/KiB, show integer
			return fmt.Sprintf("%.0f%s", num, suffix.short)
		}
	}
	return val
}

// relativeTime converts a docker CreatedAt timestamp to a human-readable relative time
func relativeTime(createdAt string) string {
	// Docker format: "2026-02-07 12:14:13 +0100 CET"
	// Try parsing with timezone name suffix
	t, err := time.Parse("2006-01-02 15:04:05 -0700 MST", createdAt)
	if err != nil {
		// Try without timezone name
		t, err = time.Parse("2006-01-02 15:04:05 -0700", createdAt)
		if err != nil {
			return createdAt
		}
	}

	return theme.TimeAgo(t) // Rule 127
}

// resize adjusts the table dimensions
func (m *Model) resize(width, height int) {
	m.width = width
	m.height = height

	// Widths, headers and height in one call; the Rule 116 arithmetic is the
	// component's rather than ten percentages written out here.
	m.containerTable.Resize(width, height-2)
}
