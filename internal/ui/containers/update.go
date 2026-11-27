package containers

import (
	"fmt"
	"log"
	"os/exec"
	"slices"
	"strconv"
	"strings"
	"time"

	tea "charm.land/bubbletea/v2"
	"github.com/anthnel/devdesk/internal/docker"
	"github.com/anthnel/devdesk/internal/shared"
	sharedcomponents "github.com/anthnel/devdesk/internal/ui/components"
	"github.com/anthnel/devdesk/internal/ui/keymap"
	ociresources "github.com/anthnel/devdesk/internal/ui/oci_resources"
	"github.com/anthnel/devdesk/internal/ui/teaexec"
	uiterminal "github.com/anthnel/devdesk/internal/ui/terminal"
	"github.com/anthnel/devdesk/internal/ui/theme"
	uiviewer "github.com/anthnel/devdesk/internal/ui/viewer"

	"github.com/anthnel/devdesk/internal/jobs"
)

// Init initializes the containers view
func (m Model) Init() tea.Cmd {
	return tea.Batch(
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

	case tea.KeyPressMsg:
		return m.handleKeyMsg(msg)

	case tea.PasteMsg:
		return m.handlePaste(msg)

	case jobs.FrameMsg:
		// The blink phase of a busy row's dot; nothing is rebuilt.
		m.containerTable.SetBlinkPhase(msg.Blink)
		m.footer.SetBlinkPhase(msg.Blink)
		return m, nil

	case RefreshTickMsg:
		// The next tick is requested here: a tick that does not re-arm itself
		// refreshes once, two seconds after Init, and never again — which is
		// what made ctrl+r the only way to see the gauges move.
		if m.unfocused {
			// Blurred terminal: keep the chain alive, skip the read. The
			// focus edge reads once (DEVDESK-210).
			return m, tickCmd()
		}
		return m, tea.Batch(tickCmd(), fetchContainers(), fetchMetrics())

	case shared.TerminalFocusMsg:
		return m.setTerminalFocus(msg)

	case ContainersListMsg:
		return m.handleContainersList(msg)

	case ContainerMetricsMsg:
		return m.handleContainerMetrics(msg)

	case ContainerUpdatesCheckedMsg:
		return m.handleContainerUpdatesChecked(msg)

	case ContainerActionMsg:
		return m.handleContainerAction(msg)

	case PagerExitMsg:
		// Only the shell path comes back here now. The logs pane went to the
		// viewer, and its pager and follow went with it (sources.go).
		// No tickCmd here: the chain survives the suspension (the tick is queued
		// and handled on the way back), and a second one would double every
		// refresh for the rest of the session.
		return m, fetchContainers()

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

	case ShellDetectedMsg:
		return m.handleShellDetected(msg)

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
func (m Model) handleKeyMsg(msg tea.KeyPressMsg) (tea.Model, tea.Cmd) {
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

// handlePaste hands pasted text to the filter while it has the keyboard and no
// modal is open; otherwise a paste is dropped, never read as keys (Rule 111).
func (m Model) handlePaste(msg tea.PasteMsg) (tea.Model, tea.Cmd) {
	if m.anyModal() == nil && m.containerTable.InEditMode() {
		return m, m.containerTable.Update(msg)
	}
	return m, nil
}

// handleNormalKeyMsg handles keys in normal mode
func (m Model) handleNormalKeyMsg(msg tea.KeyPressMsg) (tea.Model, tea.Cmd) {
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
	case keymap.Space:
		return m.pauseToggleSelectedContainer()

	case keymap.Prune:
		return m.pruneContainers()

	case keymap.Delete:
		return m.deleteSelectedContainer()

	case "ctrl+r":
		m.loading = true
		return m, tea.Batch(fetchContainers(), fetchMetrics())

	// The "new window" variant is a setting rather than a second key: the
	// capability belongs to the environment, not to the moment (§3.26).
	case keymap.Terminal:
		return m.shellSelectedContainer()

	case keymap.Logs:
		return m.logsSelectedContainer()

	case keymap.Jump:
		return m.jumpToSelectedImage()

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
// is what the dot is read from — one answer, so the screen and the refusal
// cannot disagree.
func (m Model) startAction(c *docker.Container, label string, cmd tea.Cmd) (tea.Model, tea.Cmd) {
	if m.containerTable.IsBusy(c.ID) {
		return m, m.footer.Warn(busyMessage)
	}
	m.containerTable.MarkBusy(c.ID, label+" "+c.Name)
	return m, cmd
}

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

// pruneContainers freezes the stopped containers and asks for confirmation. The
// list is read from all the containers the view holds, whatever state filter the
// table shows, and is the only thing the confirmation removes (OC-22). With none
// there is nothing to confirm.
func (m Model) pruneContainers() (tea.Model, tea.Cmd) {
	m.pruneTargets = stoppedContainers(m.containerTable.Items())
	if len(m.pruneTargets) == 0 {
		return m, m.footer.Info("No stopped containers to remove")
	}
	m.pendingAction = "confirm-prune"
	m.confirmModal = sharedcomponents.NewConfirmModal("Prune Containers",
		sharedcomponents.RemovalPreview("stopped containers", containerNames(m.pruneTargets)))
	return m, nil
}

// stoppedContainers keeps the containers `P` may remove: exited, created or dead,
// with an ID that is one. A running, paused, restarting or removing container never
// qualifies.
func stoppedContainers(all []docker.Container) []docker.Container {
	var stopped []docker.Container
	for _, c := range all {
		switch c.State {
		case "exited", "created", "dead":
		default:
			continue
		}
		if !docker.IsContainerID(c.ID) {
			log.Printf("ERROR [containers] prune: skipped a container with a malformed ID %q", c.ID)
			continue
		}
		stopped = append(stopped, c)
	}
	return stopped
}

// shellSelectedContainer opens a shell in the selected container, in place or
// in a window of its own depending on app.terminal_new_window.
//
// Prefers /bin/bash when available, falls back to /bin/sh. The two used to be
// `s` and `S`, spending a second letter on a choice that belongs to the
// environment: there is no window to open under WSL or through SSH (§3.26).
//
// Asking the container which shell it has waits on the daemon, so Update only
// marks the detection pending and returns the Cmd that does it; the answer
// (ShellDetectedMsg) starts the shell (OC-04, DEVDESK-800).
func (m Model) shellSelectedContainer() (tea.Model, tea.Cmd) {
	c := m.getSelectedContainer()
	if c == nil || c.State != "running" {
		return m, nil
	}
	if m.shellDetecting {
		return m, m.footer.Warn("Already opening a shell")
	}
	if !docker.IsContainerID(c.ID) {
		cmd := m.refuseShell(c.ID)
		return m, cmd
	}
	m.shellDetecting = true
	return m, detectShellCmd(c.ID)
}

// detectShellCmd asks the container which shell it has, off the Update loop.
func detectShellCmd(containerID string) tea.Cmd {
	return func() tea.Msg {
		shell, err := docker.DetectShell(containerID)
		return ShellDetectedMsg{ID: containerID, Shell: shell, Err: err}
	}
}

// maxLoggedIDLen bounds a refused ID in the log: it comes from outside and may be
// of any length.
const maxLoggedIDLen = 80

// refuseShell tells the user a shell was not opened because the container ID is
// not one. The refused value goes to the log, quoted, never to the footer.
func (m *Model) refuseShell(id string) tea.Cmd {
	if len(id) > maxLoggedIDLen {
		id = id[:maxLoggedIDLen] + "…"
	}
	log.Printf("ERROR [containers] shell: rejected malformed container ID %q", id)
	return m.footer.Error("Cannot open a shell — container ID refused")
}

// handleShellDetected starts the shell the container answered with.
func (m Model) handleShellDetected(msg ShellDetectedMsg) (tea.Model, tea.Cmd) {
	m.shellDetecting = false
	if msg.Abandoned {
		return m, nil
	}
	shellCmd, err := m.shellCmdFor(msg)
	if err != nil {
		cmd := m.refuseShell(msg.ID)
		return m, cmd
	}
	if m.config.App.TerminalNewWindow {
		return m.openShellWindow(shellCmd.Args)
	}
	return m, teaexec.Process(shellCmd, func(err error) tea.Msg {
		return PagerExitMsg{Err: err}
	})
}

// shellCmdFor builds the session the detection answered for, or refuses it.
func (m Model) shellCmdFor(msg ShellDetectedMsg) (*exec.Cmd, error) {
	if msg.Err != nil {
		return nil, msg.Err
	}
	return docker.ShellCmd(msg.ID, msg.Shell)
}

// openShellWindow runs the shell's argv in a new terminal window using the
// auto-detected terminal emulator (non-blocking).
func (m Model) openShellWindow(innerArgs []string) (tea.Model, tea.Cmd) {
	bin, args, ok := uiterminal.ForCmd(innerArgs)
	if !ok {
		return m, m.footer.Warn("Terminal not detected — turn off app.terminal_new_window for an in-place shell")
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

// jumpToSelectedImage asks the router for the OCI view, on the image the
// selected container runs. The reference is passed as the engine printed it;
// matching it to a row is the OCI view's business (ociresources.imageMatches).
func (m Model) jumpToSelectedImage() (tea.Model, tea.Cmd) {
	c := m.getSelectedContainer()
	if c == nil {
		return m, nil
	}
	image := c.Image
	return m, func() tea.Msg { return ociresources.FocusImageRequestMsg{Image: image} }
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
		return m, removeContainers(containerIDs(m.pruneTargets))
	default: // confirm-delete
		c := m.getSelectedContainer()
		if c == nil {
			return m, nil
		}
		return m.startAction(c, "Removing", removeContainer(c.ID, c.Name))
	}
}

// containerNames lists the names of containers, in order.
func containerNames(containers []docker.Container) []string {
	names := make([]string, 0, len(containers))
	for _, c := range containers {
		names = append(names, c.Name)
	}
	return names
}

// containerIDs lists the IDs of containers, in order.
func containerIDs(containers []docker.Container) []string {
	ids := make([]string, 0, len(containers))
	for _, c := range containers {
		ids = append(ids, c.ID)
	}
	return ids
}

// handlePruneComplete reports what the removal of the frozen list did: how many
// containers went, which ones the engine refused and why, and which were already gone.
func (m Model) handlePruneComplete(msg ContainerPruneMsg) (tea.Model, tea.Cmd) {
	m.pruning = false
	m.pruneTargets = nil
	for _, r := range msg.Report {
		switch r.Status {
		case docker.RemovalRefused:
			log.Printf("ERROR [containers] prune: %s refused (%s): %v", r.Target, r.Reason, r.Err)
		case docker.RemovalGone:
			log.Printf("INFO [containers] prune: %s was already gone", r.Target)
		}
	}
	return m, tea.Batch(m.footer.Removal(msg.Report), fetchContainers())
}

// handleContainersList processes the container list response
func (m Model) handleContainersList(msg ContainersListMsg) (tea.Model, tea.Cmd) {
	m.loading = false
	if msg.Err != nil {
		log.Printf("ERROR [containers] list: %v", msg.Err)
		return m, m.footer.Error("Failed to load containers — check logs")
	}
	m.footer.Clear()
	m.containerTable.SetItems(withKnownMetrics(msg.Containers, m.containerTable.Items()))
	// A new list is a new population — a container started, stopped, or
	// published a port — and the table only measures the first one on its own.
	// Remeasuring on the periodic reload does not make the columns dance: the
	// content columns (Name, Image, Ports) only move when their text does, and
	// the metrics, which change every tick, arrive through a separate message
	// into fixed-width columns.
	m.containerTable.Remeasure()
	return m, m.updates.updatesCmd(msg.Containers, time.Now())
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
			setMetrics(&containers[i], metrics)
		}
	}

	m.containerTable.SetItems(containers)
	return m, nil
}

// setMetrics copies the live sample of src onto dst: what `stats` reports, and
// nothing `ps` does.
func setMetrics(dst *docker.Container, src docker.Container) {
	dst.CPUPercent = src.CPUPercent
	dst.MemUsage = src.MemUsage
	dst.MemPercent = src.MemPercent
	dst.NetIO = src.NetIO
	dst.NetRX = src.NetRX
	dst.NetTX = src.NetTX
	dst.BlockIO = src.BlockIO
	dst.BlockRX = src.BlockRX
	dst.BlockTX = src.BlockTX
}

// withKnownMetrics carries the metrics already on screen over to a fresh list.
//
// The list and the metrics come from two commands, and the list is the quick
// one: `ps` answers in a fraction of the second `stats --no-stream` spends
// waiting for its second sample. A list that replaced the items as it came
// would blank every metric cell until the next sample arrived, on every tick —
// the "-" the columns flashed between refreshes. A container that is gone from
// the list is gone with its numbers.
func withKnownMetrics(fresh, known []docker.Container) []docker.Container {
	byID := make(map[string]docker.Container, len(known))
	for _, c := range known {
		byID[c.ID] = c
	}
	merged := slices.Clone(fresh)
	for i, c := range merged {
		if previous, ok := byID[c.ID]; ok {
			setMetrics(&merged[i], previous)
		}
	}
	return merged
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
		if msg.Action == "remove" {
			// `D` reads like `P`: a container already gone is an information, a
			// running one a refusal that says why, not "Action failed".
			return m, tea.Batch(m.footer.Removal(docker.SingleRemoval(msg.ID, msg.Err)), fetchContainers())
		}
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

// setTerminalFocus records whether the terminal has the focus. On the edge that
// follows a blur it reads the list and the metrics once, without a new tick:
// the chain kept running while blurred (DEVDESK-210).
func (m Model) setTerminalFocus(msg shared.TerminalFocusMsg) (tea.Model, tea.Cmd) {
	m.unfocused = !msg.Focused
	if msg.Focused && msg.Resume {
		return m, tea.Batch(fetchContainers(), fetchMetrics())
	}
	return m, nil
}
