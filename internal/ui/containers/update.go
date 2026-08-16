package containers

import (
	"fmt"
	"log"
	"os/exec"
	"runtime"
	"slices"
	"strconv"
	"strings"
	"time"

	"github.com/anthnel/devdesk/internal/docker"
	sharedcomponents "github.com/anthnel/devdesk/internal/ui/components"
	uiterminal "github.com/anthnel/devdesk/internal/ui/terminal"
	"github.com/anthnel/devdesk/internal/ui/theme"
	"github.com/charmbracelet/bubbles/spinner"
	tea "github.com/charmbracelet/bubbletea"
	"github.com/charmbracelet/x/ansi"
)

// Init initializes the containers view
func (m Model) Init() tea.Cmd {
	return tea.Batch(
		m.spinner.Tick,
		tickCmd(),
		fetchContainers(m.showAll),
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

	case clearErrorMsg:
		m.errorMsg = ""
		return m, nil

	case spinner.TickMsg:
		// The table's own frame advances whenever anything is running on a row,
		// which is not the same condition as the view's spinner: the list is
		// loaded and on screen while a container stops.
		if len(m.containerTable.BusyLabels()) > 0 {
			m.containerTable.AdvanceSpinner()
			var cmd tea.Cmd
			m.spinner, cmd = m.spinner.Update(msg)
			return m, cmd
		}
		if m.loading || m.logsLoading {
			var cmd tea.Cmd
			m.spinner, cmd = m.spinner.Update(msg)
			return m, cmd
		}

	case RefreshTickMsg:
		return m, tea.Batch(fetchContainers(m.showAll), fetchMetrics())

	case ContainersListMsg:
		return m.handleContainersList(msg)

	case ContainerMetricsMsg:
		return m.handleContainerMetrics(msg)

	case ContainerActionMsg:
		return m.handleContainerAction(msg)

	case ContainerLogsLoadedMsg:
		return m.handleContainerLogsLoaded(msg)

	case PagerExitMsg:
		if m.state == stateLogs {
			// Reload logs on return from follow or external pager
			m.logsLoading = true
			return m, tea.Batch(m.spinner.Tick, fetchContainerLogs(m.logsContainerID, m.logsTimestamps))
		}
		return m, tea.Batch(tickCmd(), fetchContainers(m.showAll))

	case sharedcomponents.ConfirmModalYesMsg:
		return m.handleConfirmYes()

	case sharedcomponents.ConfirmModalNoMsg:
		m.confirmModal = nil
		m.pendingAction = ""
		return m, nil

	case ContainerPruneMsg:
		return m.handlePruneComplete(msg)

	case ShellWindowOpenedMsg:
		if msg.Err != nil {
			log.Printf("ERROR [containers] open shell window: %v", msg.Err)
			m.errorMsg = "Failed to open terminal — check logs"
			return m, clearErrorCmd()
		}
		return m, nil
	}

	// Every message the table reacts to is a key, and keys are routed by
	// handleKeyMsg above, so nothing falls through to it here.
	return m, nil
}

// handleKeyMsg processes keyboard input with priority chain
func (m Model) handleKeyMsg(msg tea.KeyMsg) (tea.Model, tea.Cmd) {
	// Priority 1: logs view
	if m.state == stateLogs {
		return m.handleLogsKeyMsg(msg)
	}

	// Priority 2: confirm modal
	if m.confirmModal != nil {
		var cmd tea.Cmd
		m.confirmModal, cmd = m.confirmModal.Update(msg)
		return m, cmd
	}

	// Priority 3: filter input active
	if m.containerTable.InEditMode() {
		return m, m.containerTable.Update(msg)
	}

	// Priority 4: normal mode
	return m.handleNormalKeyMsg(msg)
}

// handleLogsKeyMsg handles keys when the internal logs viewport is active
func (m Model) handleLogsKeyMsg(msg tea.KeyMsg) (tea.Model, tea.Cmd) {
	switch msg.String() {
	case "esc", "q":
		m.state = stateTable
		return m, nil
	case "e":
		return m.openExternalPager()
	case "w":
		return m.toggleLogsWrap()
	case "t":
		return m.toggleLogsTimestamps()
	case "f":
		return m.followContainerLogs()
	case "ctrl+r":
		m.logsLoading = true
		return m, tea.Batch(m.spinner.Tick, fetchContainerLogs(m.logsContainerID, m.logsTimestamps))
	case "up", "k":
		m.logsViewport.ScrollUp(1)
	case "down", "j":
		m.logsViewport.ScrollDown(1)
	case "pgup":
		m.logsViewport.HalfPageUp()
	case "pgdown":
		m.logsViewport.HalfPageDown()
	case "g", "home":
		m.logsViewport.GotoTop()
	case "G", "end":
		m.logsViewport.GotoBottom()
	}
	return m, nil
}

// handleNormalKeyMsg handles keys in normal mode
func (m Model) handleNormalKeyMsg(msg tea.KeyMsg) (tea.Model, tea.Cmd) {
	switch msg.String() {
	case "a":
		m.showAll = !m.showAll
		m.loading = true
		return m, tea.Batch(m.spinner.Tick, fetchContainers(m.showAll))

	case "K":
		return m.stopSelectedContainer()

	case "r":
		return m.restartSelectedContainer()

	case " ":
		return m.pauseToggleSelectedContainer()

	case "p":
		return m.pruneContainers()

	case "ctrl+d":
		return m.deleteSelectedContainer()

	case "ctrl+r":
		m.loading = true
		return m, tea.Batch(m.spinner.Tick, fetchContainers(m.showAll), fetchMetrics())

	case "s":
		return m.shellSelectedContainer()

	case "S":
		return m.shellSelectedContainerInNewWindow()

	case "l":
		return m.logsSelectedContainer()

	case "i":
		return m.inspectSelectedContainer()

	// Navigation, `/` and `.` are the table's, not the view's.
	case "up", "k", "down", "j", "pgup", "pgdown", "g", "home", "G", "end", "/", ".":
		return m, m.containerTable.Update(msg)
	}

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

// clearErrorMsg wipes the footer message (Rule 128). The view had no timer at
// all: errorMsg was set and left until the next success happened to clear it,
// so a failure could sit under an unrelated screen for minutes.
type clearErrorMsg struct{}

// clearErrorCmd is the three-second timer Rule 128 requires after every footer
// message.
func clearErrorCmd() tea.Cmd {
	return tea.Tick(3*time.Second, func(time.Time) tea.Msg { return clearErrorMsg{} })
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
		m.errorMsg = busyMessage
		return m, clearErrorCmd()
	}
	m.containerTable.MarkBusy(c.ID, label+" "+c.Name)
	return m, cmd
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
	err := exec.Command("docker", "exec", containerID, "which", "bash").Run()
	if err == nil {
		return "/bin/bash"
	}
	return "/bin/sh"
}

// shellSelectedContainer opens a shell in the selected container.
// Prefers /bin/bash when available, falls back to /bin/sh.
func (m Model) shellSelectedContainer() (tea.Model, tea.Cmd) {
	c := m.getSelectedContainer()
	if c == nil || c.State != "running" {
		return m, nil
	}

	shell := detectShell(c.ID)
	shellCmd := exec.Command("docker", "exec", "-it", c.ID, shell)
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
	innerArgs := []string{"docker", "exec", "-it", c.ID, shell}

	bin, args, ok := uiterminal.ForCmd(innerArgs)
	if !ok {
		m.errorMsg = "Terminal not detected — use [s] for in-place shell"
		return m, clearErrorCmd()
	}

	return m, func() tea.Msg {
		cmd := exec.Command(bin, args...)
		return ShellWindowOpenedMsg{Err: cmd.Start()}
	}
}

// logsSelectedContainer switches to the internal logs viewport for the selected container
func (m Model) logsSelectedContainer() (tea.Model, tea.Cmd) {
	c := m.getSelectedContainer()
	if c == nil {
		return m, nil
	}
	m.state = stateLogs
	m.logsContainerID = c.ID
	m.logsContainerName = c.Name
	m.logsLoading = true
	m.logsViewport.GotoTop()
	return m, tea.Batch(m.spinner.Tick, fetchContainerLogs(c.ID, m.logsTimestamps))
}

// openExternalPager launches docker logs in the system pager (fallback for power users)
func (m Model) openExternalPager() (tea.Model, tea.Cmd) {
	if !docker.IsContainerID(m.logsContainerID) {
		log.Printf("ERROR [containers] pager: rejected malformed container ID %q", m.logsContainerID)
		m.errorMsg = "Cannot open pager — invalid container ID"
		return m, clearErrorCmd()
	}

	var pagerCmd *exec.Cmd
	switch runtime.GOOS {
	case "windows":
		// Write to a temp file then open with `more` (always available on Windows; less is not)
		script := fmt.Sprintf(`docker logs --tail 500 %s > "%%TEMP%%\devdesk-logs.txt" 2>&1 && more "%%TEMP%%\devdesk-logs.txt"`, m.logsContainerID)
		pagerCmd = exec.Command("cmd", "/c", script)
	default:
		pagerCmd = exec.Command("sh", "-c", fmt.Sprintf("docker logs --tail 500 %s 2>&1 | ${PAGER:-less} -R", m.logsContainerID))
	}
	return m, tea.ExecProcess(pagerCmd, func(err error) tea.Msg {
		return PagerExitMsg{Err: err}
	})
}

// handleContainerLogsLoaded processes fetched log content into the logs viewport
func (m Model) handleContainerLogsLoaded(msg ContainerLogsLoadedMsg) (tea.Model, tea.Cmd) {
	m.logsLoading = false
	if msg.Err != nil {
		log.Printf("ERROR [containers] logs %s: %v", m.logsContainerName, msg.Err)
		m.logsRawContent = "Failed to load logs — check logs"
		m.logsViewport.SetContent(m.logsRawContent)
		return m, nil
	}
	// Strip ANSI escape sequences (colors, cursor moves, etc.) then normalize
	// line endings. Both are needed: systemd emits colored output, and some
	// containers use \r for progress-bar overwrites — both break the viewport.
	content := ansi.Strip(msg.Content)
	content = strings.ReplaceAll(content, "\r\n", "\n")
	content = strings.ReplaceAll(content, "\r", "")
	m.logsRawContent = content

	m.refreshLogsViewport()
	m.logsViewport.GotoBottom()
	return m, nil
}

// refreshLogsViewport rebuilds the viewport content applying wrap if enabled.
func (m *Model) refreshLogsViewport() {
	if m.logsWrapEnabled {
		m.logsViewport.SetContent(wrapLines(m.logsRawContent, m.logsViewport.Width))
		return
	}
	m.logsViewport.SetContent(m.logsRawContent)
}

// wrapLines soft-wraps content so no line exceeds width runes.
func wrapLines(content string, width int) string {
	if width <= 0 {
		return content
	}
	lines := strings.Split(content, "\n")
	out := make([]string, 0, len(lines))
	for _, line := range lines {
		runes := []rune(line)
		if len(runes) <= width {
			out = append(out, line)
			continue
		}
		for len(runes) > width {
			out = append(out, string(runes[:width]))
			runes = runes[width:]
		}
		if len(runes) > 0 {
			out = append(out, string(runes))
		}
	}
	return strings.Join(out, "\n")
}

// toggleLogsWrap toggles soft word-wrap for the logs viewport.
func (m Model) toggleLogsWrap() (tea.Model, tea.Cmd) {
	m.logsWrapEnabled = !m.logsWrapEnabled
	m.refreshLogsViewport()
	return m, nil
}

// toggleLogsTimestamps refetches logs with or without --timestamps.
func (m Model) toggleLogsTimestamps() (tea.Model, tea.Cmd) {
	m.logsTimestamps = !m.logsTimestamps
	m.logsLoading = true
	return m, tea.Batch(m.spinner.Tick, fetchContainerLogs(m.logsContainerID, m.logsTimestamps))
}

// followContainerLogs suspends the TUI and streams live logs via docker logs -f.
func (m Model) followContainerLogs() (tea.Model, tea.Cmd) {
	args := []string{"logs", "-f", "--tail", "100"}
	if m.logsTimestamps {
		args = append(args, "--timestamps")
	}
	args = append(args, m.logsContainerID)
	followCmd := exec.Command("docker", args...)
	return m, tea.ExecProcess(followCmd, func(err error) tea.Msg {
		return PagerExitMsg{Err: err}
	})
}

// inspectSelectedContainer opens docker inspect in the system pager (less)
func (m Model) inspectSelectedContainer() (tea.Model, tea.Cmd) {
	c := m.getSelectedContainer()
	if c == nil {
		return m, nil
	}
	if !docker.IsContainerID(c.ID) {
		log.Printf("ERROR [containers] inspect: rejected malformed container ID %q", c.ID)
		m.errorMsg = "Cannot inspect — invalid container ID"
		return m, clearErrorCmd()
	}

	var pagerCmd *exec.Cmd
	switch runtime.GOOS {
	case "windows":
		// Write to a temp file then open with `more` (always available on Windows; less is not)
		script := fmt.Sprintf(`docker inspect %s > "%%TEMP%%\devdesk-inspect.txt" 2>&1 && more "%%TEMP%%\devdesk-inspect.txt"`, c.ID)
		pagerCmd = exec.Command("cmd", "/c", script)
	default:
		pagerCmd = exec.Command("sh", "-c", fmt.Sprintf("docker inspect %s | less -R", c.ID))
	}
	return m, tea.ExecProcess(pagerCmd, func(err error) tea.Msg {
		return PagerExitMsg{Err: err}
	})
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
		m.errorMsg = "Prune failed — check logs"
		return m, clearErrorCmd()
	}
	m.errorMsg = ""
	return m, fetchContainers(m.showAll)
}

// handleContainersList processes the container list response
func (m Model) handleContainersList(msg ContainersListMsg) (tea.Model, tea.Cmd) {
	m.loading = false
	if msg.Err != nil {
		log.Printf("ERROR [containers] list: %v", msg.Err)
		m.errorMsg = "Failed to load containers — check logs"
		return m, clearErrorCmd()
	}
	m.errorMsg = ""
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
		m.errorMsg = "Action failed — check logs"
		return m, clearErrorCmd()
	}
	m.errorMsg = ""
	return m, fetchContainers(m.showAll)
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

// resize adjusts the table and logs viewport dimensions
func (m *Model) resize(width, height int) {
	m.width = width
	m.height = height

	// Widths, headers and height in one call; the Rule 116 arithmetic is the
	// component's rather than ten percentages written out here.
	m.containerTable.Resize(width, height-2)

	m.logsViewport.Width = width
	m.logsViewport.Height = height

	// Reflow wrapped content at the new width
	if m.state == stateLogs && m.logsWrapEnabled && m.logsRawContent != "" {
		m.refreshLogsViewport()
	}
}
