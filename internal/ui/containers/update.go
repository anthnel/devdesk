package containers

import (
	"fmt"
	"log"
	"os/exec"
	"runtime"
	"sort"
	"strconv"
	"strings"
	"time"

	"github.com/charmbracelet/bubbles/spinner"
	"github.com/charmbracelet/bubbles/table"
	tea "github.com/charmbracelet/bubbletea"
	"github.com/charmbracelet/x/ansi"
	"gitlab.com/anthnell/devsecops/devdesk/internal/docker"
	sharedcomponents "gitlab.com/anthnell/devsecops/devdesk/internal/ui/components"
	uiterminal "gitlab.com/anthnell/devsecops/devdesk/internal/ui/terminal"
	"gitlab.com/anthnell/devsecops/devdesk/internal/ui/theme"
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

	case spinner.TickMsg:
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
		}
		return m, nil
	}

	// Update table if no modal active and in table state
	if m.state == stateTable && m.confirmModal == nil && !m.filterBar.InEditMode() {
		var cmd tea.Cmd
		m.containerTable, cmd = m.containerTable.Update(msg)
		m.refreshSelectionStyle()
		return m, cmd
	}

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
	if m.filterBar.InEditMode() {
		return m.handleFilterKeyMsg(msg)
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

// handleFilterKeyMsg handles keys when filter input is active
func (m Model) handleFilterKeyMsg(msg tea.KeyMsg) (tea.Model, tea.Cmd) {
	var cmd tea.Cmd
	m.filterBar, cmd = m.filterBar.Update(msg)
	m.updateTable()
	return m, cmd
}

// handleNormalKeyMsg handles keys in normal mode
func (m Model) handleNormalKeyMsg(msg tea.KeyMsg) (tea.Model, tea.Cmd) {
	switch msg.String() {
	case "a":
		m.showAll = !m.showAll
		m.loading = true
		return m, tea.Batch(m.spinner.Tick, fetchContainers(m.showAll))

	case "/":
		return m, m.filterBar.ActivateSearch()

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

	case ".":
		return m.cycleSort()

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

	case "up", "k":
		m.containerTable.MoveUp(1)
		m.refreshSelectionStyle()
		return m, nil

	case "down", "j":
		m.containerTable.MoveDown(1)
		m.refreshSelectionStyle()
		return m, nil

	case "g", "home":
		m.containerTable.GotoTop()
		m.refreshSelectionStyle()
		return m, nil

	case "G", "end":
		m.containerTable.GotoBottom()
		m.refreshSelectionStyle()
		return m, nil
	}

	return m, nil
}

// getSelectedContainer returns the selected container or nil
func (m *Model) getSelectedContainer() *docker.Container {
	sorted := m.sortedContainers(m.filteredContainers())
	cursor := m.containerTable.Cursor()
	if cursor < 0 || cursor >= len(sorted) {
		return nil
	}
	return &sorted[cursor]
}

// stopSelectedContainer stops the selected container
func (m Model) stopSelectedContainer() (tea.Model, tea.Cmd) {
	c := m.getSelectedContainer()
	if c == nil || c.State != "running" {
		return m, nil
	}
	m.pendingAction = "Stopping " + c.Name
	return m, stopContainer(c.ID, c.Name)
}

// restartSelectedContainer restarts the selected container
func (m Model) restartSelectedContainer() (tea.Model, tea.Cmd) {
	c := m.getSelectedContainer()
	if c == nil {
		return m, nil
	}
	m.pendingAction = "Restarting " + c.Name
	return m, restartContainer(c.ID, c.Name)
}

// pauseToggleSelectedContainer pauses a running container or unpauses a paused one
func (m Model) pauseToggleSelectedContainer() (tea.Model, tea.Cmd) {
	c := m.getSelectedContainer()
	if c == nil {
		return m, nil
	}
	switch c.State {
	case "running":
		m.pendingAction = "Pausing " + c.Name
		return m, pauseContainer(c.ID, c.Name)
	case "paused":
		m.pendingAction = "Resuming " + c.Name
		return m, unpauseContainer(c.ID, c.Name)
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
		return m, nil
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
		return m, nil
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
		return m, nil
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
		m.pendingAction = "Pruning containers..."
		return m, pruneContainers()
	default: // confirm-delete
		c := m.getSelectedContainer()
		if c == nil {
			return m, nil
		}
		m.pendingAction = "Removing " + c.Name
		return m, removeContainer(c.ID, c.Name)
	}
}

// handlePruneComplete processes the prune result
func (m Model) handlePruneComplete(msg ContainerPruneMsg) (tea.Model, tea.Cmd) {
	m.pendingAction = ""
	if msg.Err != nil {
		log.Printf("ERROR [containers] prune: %v", msg.Err)
		m.errorMsg = "Prune failed — check logs"
		return m, nil
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
		return m, nil
	}
	m.errorMsg = ""
	m.containers = msg.Containers
	m.updateTable()
	return m, nil
}

// handleContainerMetrics merges metrics into existing containers
func (m Model) handleContainerMetrics(msg ContainerMetricsMsg) (tea.Model, tea.Cmd) {
	if msg.Err != nil || msg.Metrics == nil {
		return m, nil
	}

	for i, c := range m.containers {
		if metrics, ok := msg.Metrics[c.ID]; ok {
			m.containers[i].CPUPercent = metrics.CPUPercent
			m.containers[i].MemUsage = metrics.MemUsage
			m.containers[i].MemPercent = metrics.MemPercent
			m.containers[i].NetIO = metrics.NetIO
			m.containers[i].NetRX = metrics.NetRX
			m.containers[i].NetTX = metrics.NetTX
			m.containers[i].BlockIO = metrics.BlockIO
			m.containers[i].BlockRX = metrics.BlockRX
			m.containers[i].BlockTX = metrics.BlockTX
		}
	}

	m.updateTable()
	return m, nil
}

// handleContainerAction processes action results
func (m Model) handleContainerAction(msg ContainerActionMsg) (tea.Model, tea.Cmd) {
	m.pendingAction = ""
	if msg.Err != nil {
		log.Printf("ERROR [containers] %s %s: %v", msg.Action, msg.ID, msg.Err)
		m.errorMsg = "Action failed — check logs"
		return m, nil
	}
	m.errorMsg = ""
	return m, fetchContainers(m.showAll)
}

// filteredContainers returns containers matching the current filter
func (m *Model) filteredContainers() []docker.Container {
	query := strings.ToLower(m.filterBar.SearchQuery())
	if query == "" {
		return m.containers
	}

	var result []docker.Container
	for _, c := range m.containers {
		if strings.Contains(strings.ToLower(c.Name), query) ||
			strings.Contains(strings.ToLower(c.Image), query) ||
			strings.Contains(strings.ToLower(c.State), query) {
			result = append(result, c)
		}
	}
	return result
}

// sortableColumns lists columns in cycle order for the '.' key
var sortableColumns = []sortField{
	sortByName,
	sortByImage,
	sortByCPU,
	sortByMem,
	sortByNetRX,
	sortByNetTX,
	sortByBlockRX,
	sortByBlockTX,
	sortByCreated,
}

// cycleSort cycles through sort options: each column asc then desc, then next column
func (m Model) cycleSort() (tea.Model, tea.Cmd) {
	if m.sortAsc {
		m.sortAsc = false
	} else {
		m.sortAsc = true
		nextIdx := 0
		for i, col := range sortableColumns {
			if col == m.sortColumn {
				nextIdx = (i + 1) % len(sortableColumns)
				break
			}
		}
		m.sortColumn = sortableColumns[nextIdx]
	}
	m.updateTable()
	return m, nil
}

// sortedContainers returns containers sorted by the current sort column
func (m *Model) sortedContainers(containers []docker.Container) []docker.Container {
	sorted := make([]docker.Container, len(containers))
	copy(sorted, containers)

	sort.Slice(sorted, func(i, j int) bool {
		a, b := sorted[i], sorted[j]

		var less bool
		switch m.sortColumn {
		case sortByImage:
			less = strings.ToLower(a.Image) < strings.ToLower(b.Image)
		case sortByCPU:
			less = a.CPUPercent < b.CPUPercent
		case sortByMem:
			less = a.MemPercent < b.MemPercent
		case sortByNetRX:
			less = a.NetRX < b.NetRX
		case sortByNetTX:
			less = a.NetTX < b.NetTX
		case sortByBlockRX:
			less = a.BlockRX < b.BlockRX
		case sortByBlockTX:
			less = a.BlockTX < b.BlockTX
		case sortByCreated:
			less = a.CreatedAt < b.CreatedAt
		default: // sortByName
			less = strings.ToLower(a.Name) < strings.ToLower(b.Name)
		}

		if m.sortAsc {
			return less
		}
		return !less
	})

	return sorted
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

// updateTable rebuilds the table rows from the current container list
func (m *Model) updateTable() {
	sorted := m.sortedContainers(m.filteredContainers())
	rows := make([]table.Row, 0, len(sorted))

	for _, c := range sorted {
		cpuStr := "-"
		memStr := "-"
		rxStr := "-"
		txStr := "-"
		blkRXStr := "-"
		blkTXStr := "-"

		if c.State == "running" {
			cpuStr = fmt.Sprintf("%.1f%%", c.CPUPercent)
			memStr = formatMemUsage(c.MemUsage)

			if c.NetIO != "" {
				rxStr = formatNetBytes(c.NetRX)
				txStr = formatNetBytes(c.NetTX)
			}
			if c.BlockIO != "" {
				blkRXStr = formatNetBytes(c.BlockRX)
				blkTXStr = formatNetBytes(c.BlockTX)
			}
		}

		imageWithState := stateIcon(c.State) + " " + c.Image

		rows = append(rows, table.Row{
			c.Name,
			imageWithState,
			cpuStr,
			memStr,
			rxStr,
			txStr,
			blkRXStr,
			blkTXStr,
			relativeTime(c.CreatedAt),
			c.Ports,
		})
	}

	// Update column headers with sort indicators
	cols := m.containerTable.Columns()
	if len(cols) >= 10 {
		sortColIndex := map[sortField]int{
			sortByName:    0,
			sortByImage:   1,
			sortByCPU:     2,
			sortByMem:     3,
			sortByNetRX:   4,
			sortByNetTX:   5,
			sortByBlockRX: 6,
			sortByBlockTX: 7,
			sortByCreated: 8,
		}
		baseTitles := map[int]string{
			0: "Name", 1: "Image", 2: "CPU", 3: "Mem",
			4: "Net RX", 5: "Net TX", 6: "Block RX", 7: "Block TX", 8: "Created",
		}
		for idx, title := range baseTitles {
			cols[idx].Title = title
		}
		if idx, ok := sortColIndex[m.sortColumn]; ok {
			arrow := " ▲"
			if !m.sortAsc {
				arrow = " ▼"
			}
			cols[idx].Title = baseTitles[idx] + arrow
		}
		m.containerTable.SetColumns(cols)
	}

	m.containerTable.SetRows(rows)
	m.refreshSelectionStyle()
}

// refreshSelectionStyle updates the table selection color to match the currently selected container's state
func (m *Model) refreshSelectionStyle() {
	sorted := m.sortedContainers(m.filteredContainers())
	cursor := m.containerTable.Cursor()
	if cursor < 0 || cursor >= len(sorted) {
		m.containerTable.SetStyles(theme.TableStylesForState("normal"))
		return
	}
	state := "normal"
	if s := sorted[cursor].State; s == "exited" || s == "dead" {
		state = "error"
	}
	m.containerTable.SetStyles(theme.TableStylesForState(state))
}

// resize adjusts the table and logs viewport dimensions
func (m *Model) resize(width, height int) {
	m.width = width
	m.height = height

	m.filterBar.Resize(width)
	m.containerTable.SetHeight(height - 2)

	m.logsViewport.Width = width
	m.logsViewport.Height = height

	// Reflow wrapped content at the new width
	if m.state == stateLogs && m.logsWrapEnabled && m.logsRawContent != "" {
		m.refreshLogsViewport()
	}

	contentWidth := width - 2
	columns := m.containerTable.Columns()
	numCols := len(columns)
	if numCols >= 10 {
		available := contentWidth - numCols*2             // cell padding
		columns[0].Width = int(float64(available) * 0.11) // Name
		columns[1].Width = int(float64(available) * 0.16) // Image
		columns[2].Width = int(float64(available) * 0.09) // CPU
		columns[3].Width = int(float64(available) * 0.09) // Mem
		columns[4].Width = int(float64(available) * 0.07) // Net RX
		columns[5].Width = int(float64(available) * 0.07) // Net TX
		columns[6].Width = int(float64(available) * 0.07) // Block RX
		columns[7].Width = int(float64(available) * 0.07) // Block TX
		columns[8].Width = int(float64(available) * 0.09) // Created
		columns[9].Width = available - columns[0].Width - columns[1].Width - columns[2].Width - columns[3].Width - columns[4].Width - columns[5].Width - columns[6].Width - columns[7].Width - columns[8].Width
		m.containerTable.SetColumns(columns)
	}
}
