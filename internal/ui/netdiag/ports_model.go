package netdiag

import (
	"errors"
	"fmt"
	"log"
	"os"
	"strconv"
	"strings"
	"time"

	tea "charm.land/bubbletea/v2"
	"charm.land/lipgloss/v2"

	"github.com/anthnel/devdesk/internal/ports"
	"github.com/anthnel/devdesk/internal/shared"
	"github.com/anthnel/devdesk/internal/ui/components"
	"github.com/anthnel/devdesk/internal/ui/datatable"
	"github.com/anthnel/devdesk/internal/ui/indicator"
	"github.com/anthnel/devdesk/internal/ui/keymap"
	"github.com/anthnel/devdesk/internal/ui/shortcut"
	"github.com/anthnel/devdesk/internal/ui/theme"
)

// portsTickMsg triggers a data refresh cycle.
type portsTickMsg struct{}

// portsDataMsg carries the result of a socket-table read.
type portsDataMsg struct {
	ports []ports.Socket
	err   error
}

// portsKillResultMsg carries the result of a kill. process is the name the
// target had when the confirmation opened, for the sentence that says another
// process holds the PID now (DEVDESK-709).
type portsKillResultMsg struct {
	pid, process string
	err          error
}

// statusLine is what the Ports tab derives on every frame. Pausing is a state
// rather than an event, so it is rendered from pm.paused instead of set as a
// message — a message would expire after three seconds while still paused.
func (pm *PortsModel) statusLine() components.Status {
	// The selected row says what is happening to it, before the tab's own state.
	st := components.Status{Detail: pm.rowDetail()}
	// Staleness outranks the pause: a pause is what the user asked for, an
	// unreachable Docker is not, and only one of the two makes the rows lie.
	if pm.stale {
		st.Text, st.Level = pm.staleLabel(), components.LevelError
	} else if pm.paused {
		st.Text = "Paused — press space to resume"
	}
	return st
}

// rowDetail is what the selected row says about itself: the kill running on
// its PID, and nothing otherwise (this view remembers no failure).
func (pm *PortsModel) rowDetail() components.RowDetail {
	entry, ok := pm.table.Selected()
	if !ok {
		return components.RowDetail{}
	}
	label, busy := pm.table.BusyLabel(entry.PID)
	if !busy {
		return components.RowDetail{}
	}
	return components.DescribeRow(components.RowQuery{
		Observed: indicator.Running, ObservedText: label,
	}).Detail
}

// staleLabel dates the rows on screen, or says there are none to date.
func (pm *PortsModel) staleLabel() string {
	if pm.lastOK.IsZero() {
		return "The socket table could not be read"
	}
	return "The socket table could not be read — ports as of " + theme.TimeAgo(pm.lastOK)
}

// filterTokenProto and filterTokenState are the label constants for FilterBar tokens.
const (
	filterTokenTCP     = "tcp"
	filterTokenUDP     = "udp"
	filterTokenListen  = "LISTEN"
	filterTokenEstab   = "ESTAB"
	filterTokenPaused  = "⏸ PAUSED"
	filterTokenNumeric = "numeric"
)

// PortsModel manages the real-time port monitoring sub-view.
type PortsModel struct {
	// refresh is how often the table re-reads the socket table.
	refresh time.Duration
	width   int
	height  int

	// The table holds the entries, filters them and keeps the cursor. The
	// tableReady / lastTableWidth / lastTableHeight trio that used to live here
	// existed only to avoid recreating the table on every two-second tick and
	// losing the scroll position; SetItems guarantees that instead.
	table datatable.Model[ports.Socket]

	paused bool
	// unfocused is true while the terminal is blurred. It is separate from
	// paused, which is the user's own choice and shows in the bar.
	unfocused bool

	// address display
	// numericAddrs true shows addresses as the system reports them; false asks
	// reverse DNS for the host half. The port half stays numeric either way —
	// resolving it to a service name would be DevDesk guessing where `ss` read
	// /etc/services, and saying less beats saying something the system did not.
	numericAddrs bool

	// confirmModal guards the kill. It is the only action in the application
	// that reaches a process outside it, so it is the one that most needed a
	// confirmation and had none.
	confirmModal *components.ConfirmModal

	// pendingKill is the process the open confirmation is about, captured when
	// it opened. Yes acts on it and on nothing else.
	pendingKill *killTarget

	// ops is what the tab does to processes; tests replace it.
	ops processOps

	// lastOK is when the table last received real data, and stale says the most
	// recent fetch failed.
	//
	// They exist because a failed fetch keeps the rows: dropping them would
	// flash the table empty on a transient hiccup, two seconds apart, and the
	// rows are not wrong — they are dated. What was missing is their age. A
	// footer *message* cannot carry it either: it expires after three seconds
	// (Rule 128), and what was left was a table refreshing into failure with
	// nothing on screen saying so. That is D20's shape — "we could not look"
	// rendered as data.
	lastOK time.Time
	stale  bool

	// footer is this tab's own message line. Each tab keeps one: a shared
	// instance would let a message set on Ports outlive the switch away from it.
	footer components.FooterMessage
}

// Column indexes of the ports table.
const (
	portsColIndicator = iota
	portsColProto
	portsColState
	portsColLocal
	portsColPeer
	portsColPID
	portsColProcess
)

// portsColumns describes the ports table. Every column is searchable but the
// activity column: the query used to run against all six joined, and it still
// does. Column 0 is the activity dot (Rule 125); a busy PID is Running there.
func portsColumns() []datatable.Column[ports.Socket] {
	text := func(sizing datatable.Sizing, get func(ports.Socket) string) datatable.Column[ports.Socket] {
		return datatable.Column[ports.Socket]{Sizing: sizing, Cell: get, Search: get}
	}
	// The two address columns follow their content, and that is the point of
	// §3.45 in one table: they used to sit at a declared 26 whatever the
	// terminal was, cutting "[2606:2800:220:1:248:1893:25c8:1946]:443" in half
	// at 200 columns while Process took 110 cells for "svchost.exe".
	proto := text(datatable.SizingFixed, func(p ports.Socket) string { return p.Protocol })
	state := text(datatable.SizingFixed, func(p ports.Socket) string { return p.State })
	local := text(datatable.SizingContent, func(p ports.Socket) string { return p.LocalAddr })
	peer := text(datatable.SizingContent, func(p ports.Socket) string { return p.PeerAddr })
	pid := text(datatable.SizingFixed, func(p ports.Socket) string { return p.PID })
	process := text(datatable.SizingContent, func(p ports.Socket) string { return p.Process })

	proto.Title, proto.MinWidth = "Proto", 6
	state.Title, state.MinWidth = "State", 10
	state.Style = portStateStyle
	local.Title, local.MinWidth = "Local Address", 26
	peer.Title, peer.MinWidth = "Peer Address", 26
	pid.Title, pid.MinWidth = "PID", 7
	process.Title, process.MinWidth, process.Flex = "Process", 10, 1

	dot := datatable.IndicatorColumn(
		func(ports.Socket) indicator.State { return indicator.Idle },
		nil, nil)

	return []datatable.Column[ports.Socket]{dot, proto, state, local, peer, pid, process}
}

// portStateStyle colours the socket state, which is the column this table is
// scanned down: a listening port is something the machine offers, an
// established one is a conversation in progress, and everything else is a
// socket on its way out.
func portStateStyle(p ports.Socket) lipgloss.Style {
	switch strings.ToUpper(p.State) {
	case "LISTEN":
		return theme.StatusOKStyle
	case "ESTAB", "ESTABLISHED":
		return lipgloss.NewStyle().Foreground(theme.ColorHighlight)
	case "":
		return theme.DimStyle
	default: // TIME-WAIT, CLOSE-WAIT, SYN-SENT — transient, and not the point
		return theme.DimStyle
	}
}

// matchPortTokens applies the toggle filters: OR within a group, AND between
// them. `numeric` and `paused` are shown in the bar but filter nothing — they
// report a mode, which is why they are not consulted here.
func matchPortTokens(p ports.Socket, active map[string]bool) bool {
	return matchesActive(p.Protocol, active, filterTokenTCP, filterTokenUDP) &&
		matchesActive(p.State, active, filterTokenListen, filterTokenEstab)
}

// matchesActive reports whether value equals one of the labels the user turned
// on. A group with nothing on does not filter: that is "no opinion", not
// "match nothing", and getting the two confused would empty the table on open.
func matchesActive(value string, active map[string]bool, labels ...string) bool {
	anyOn := false
	for _, label := range labels {
		if !active[label] {
			continue
		}
		anyOn = true
		if strings.EqualFold(value, label) {
			return true
		}
	}
	return !anyOn
}

// newPortsModel creates a new PortsModel.
func newPortsModel(refresh time.Duration) *PortsModel {
	return &PortsModel{
		refresh:      refresh,
		ops:          realProcessOps(),
		numericAddrs: true,
		table: datatable.New(datatable.Config[ports.Socket]{
			Columns:    portsColumns(),
			SortColumn: -1, // the order ss reports is the order shown
			Tokens: []components.FilterToken{
				{Label: filterTokenTCP},
				{Label: filterTokenUDP},
				{Label: filterTokenListen},
				{Label: filterTokenEstab},
				{Label: filterTokenNumeric},
				{Label: filterTokenPaused},
			},
			TokenMatch: matchPortTokens,
			// A PID, not a socket: killing a process takes every socket it
			// holds, so every one of its rows spins together — which is what
			// actually happens.
			Key: func(p ports.Socket) string { return p.PID },
		}),
	}
}

// initPorts returns the initial commands: an immediate fetch + the first tick.
func (pm *PortsModel) initPorts() tea.Cmd {
	return tea.Batch(fetchPortsCmd(pm.numericAddrs), portsTickCmd(pm.refresh))
}

// InEditMode returns true when the search input or the kill confirmation has
// the keyboard.
func (pm *PortsModel) InEditMode() bool {
	return pm.table.InEditMode() || pm.confirmModal != nil
}

// resize updates terminal dimensions and lays the table out (Rule 116).
func (pm *PortsModel) resize(width, height int) {
	pm.width = width
	pm.height = height
	if width == 0 {
		return
	}
	// pm.height IS the viewport content height sent by the app (Rule 124); the
	// filter bar is rendered in the footer, outside the viewport.
	pm.table.Resize(width, max(height, 3))
}

func (pm *PortsModel) update(msg tea.Msg) (*PortsModel, tea.Cmd) {
	switch msg := msg.(type) {
	case portsTickMsg:
		return pm.handleTick()
	case shared.TerminalFocusMsg:
		return pm.setTerminalFocus(msg)
	case portsDataMsg:
		return pm.handleData(msg)
	case portsKillResultMsg:
		return pm.handleKillResult(msg)
	case portsRefreshMsg:
		return pm, fetchPortsCmd(pm.numericAddrs)
	case components.ConfirmModalYesMsg:
		return pm.handleConfirmYes()
	case components.ConfirmModalNoMsg:
		return pm.handleConfirmNo()
	case tea.KeyPressMsg:
		return pm.handleKey(msg)
	case tea.PasteMsg:
		return pm.handlePaste(msg)
	}
	return pm, nil
}

func (pm *PortsModel) handleTick() (*PortsModel, tea.Cmd) {
	if pm.paused || pm.unfocused {
		return pm, portsTickCmd(pm.refresh)
	}
	return pm, tea.Batch(fetchPortsCmd(pm.numericAddrs), portsTickCmd(pm.refresh))
}

// setTerminalFocus records the focus. On the edge after a blur it reads once,
// with no new tick (the chain kept running), unless the user paused the poll.
func (pm *PortsModel) setTerminalFocus(msg shared.TerminalFocusMsg) (*PortsModel, tea.Cmd) {
	pm.unfocused = !msg.Focused
	if msg.Focused && msg.Resume && !pm.paused {
		return pm, fetchPortsCmd(pm.numericAddrs)
	}
	return pm, nil
}

func (pm *PortsModel) handleData(msg portsDataMsg) (*PortsModel, tea.Cmd) {
	if msg.err != nil {
		log.Printf("ERROR [netdiag/ports] ports.List: %v", msg.err)
		// The rows stay: they are dated, not wrong, and the status line is what
		// says so. No footer message — this is a state, and a state that
		// expires after three seconds is how a dead table came to look alive.
		pm.stale = true
		return pm, nil
	}
	pm.stale = false
	pm.lastOK = time.Now()
	// The cursor and the scroll survive this, which is what the whole
	// tableReady dance existed to achieve on a two-second tick.
	pm.table.SetItems(msg.ports)
	return pm, nil
}

func (pm *PortsModel) handleKillResult(msg portsKillResultMsg) (*PortsModel, tea.Cmd) {
	// On every outcome: a kill that failed has to let the socket state show
	// again rather than go on turning.
	pm.table.ClearBusy(msg.pid)
	if msg.err != nil {
		return pm, pm.refusal(msg.pid, msg.process, msg.err)
	}
	return pm, pm.footer.Info(fmt.Sprintf("Process %s terminated", msg.pid))
}

// identityChangedText is what the footer says when the PID is held by another
// process than the one that was listed: nothing was sent and the table is read
// again (DEVDESK-709, S-01b criterion 2). A process with no name reads by its
// PID alone.
func identityChangedText(process, pid string) string {
	who := "PID " + pid
	if process != "" {
		who = process + " (PID " + pid + ")"
	}
	return who + " is no longer the process that was listed — table refreshed"
}

// refusal is the one place a refused or failed stop becomes a footer line: the
// press of K (the confirmation's read), Yes (the table compared with the
// target) and the kill's result all come here, so one error reads the same
// wherever it surfaces. errors.As comes first: DevDesk's own refusals are
// typed, and the system's sentinels are read last (killFailureMessage).
//
// The three typed refusals are preconditions, a Warn (Rule 128): the action
// cannot be honored as requested, nothing failed. They print their own
// Error(), taken from the extracted error so the "killing process N:" prefix
// ports.Kill adds never reaches the screen.
func (pm *PortsModel) refusal(pid, process string, err error) tea.Cmd {
	var protected *ports.ProtectedError
	var changed *ports.IdentityChangedError
	var unverifiable *ports.UnverifiableError
	switch {
	case errors.As(err, &protected):
		return pm.footer.Warn(protected.Error())
	case errors.As(err, &changed):
		// Another process holds the PID: nothing was sent, and the table is read
		// again so the row on screen stops being the one that was listed.
		return tea.Batch(pm.footer.Warn(identityChangedText(process, pid)), requestPortsRefresh())
	case errors.As(err, &unverifiable):
		// The cause is the system's, for the log; the screen says DevDesk's.
		log.Printf("ERROR [netdiag/ports] identity pid=%s: %v (cause: %v)", pid, err, unverifiable.Cause)
		return pm.footer.Warn(unverifiable.Error())
	}
	log.Printf("ERROR [netdiag/ports] ports.Kill pid=%s: %v", pid, err)
	return pm.footer.Error(killFailureMessage(pid, err))
}

// killFailureMessage says why the signal did not land.
//
// DevDesk signals with the rights it has (§3.43), so the refusal the user meets
// most often is the operating system's — another user's process, a service, one
// Windows protects. "Failed to kill PID 1234" gave them no reason to suspect
// that, and made "access denied" and "already gone" the same sentence.
//
// The platform error itself never reaches the screen: it comes back in the
// machine's own language — measured here, PID 4 answers `OpenProcess: Accès
// refusé.` — which is the route stage's rule (§3.44). errors.Is reads through
// the %w wrapping ports.Kill applies, and both sentinels are the standard
// library's.
//
// The permission branch was measured on Windows: killing PID 4 (System) gives
// ERROR_ACCESS_DENIED, which syscall.Errno maps onto os.ErrPermission. EPERM
// maps the same way on Unix.
//
// The "already gone" branch fires on Unix, where ESRCH maps to
// os.ErrProcessDone. On Windows, OpenProcess on a PID that does not exist fails
// with ERROR_INVALID_PARAMETER, which syscall.Errno maps to neither sentinel;
// internal/ports (identity_windows.go, procos.go) now maps it to
// os.ErrProcessDone, but that mapping has not been confirmed on a real Windows
// host (DEVDESK-727). Until it is, the message may still fall to the generic
// one there, and the row disappears on the next two-second refresh anyway.
func killFailureMessage(pid string, err error) string {
	switch {
	case errors.Is(err, os.ErrPermission):
		return fmt.Sprintf("Refused by the system — DevDesk cannot signal PID %s", pid)
	case errors.Is(err, os.ErrProcessDone):
		return fmt.Sprintf("PID %s is no longer running", pid)
	}
	return fmt.Sprintf("Failed to kill PID %s — check logs", pid)
}

func (pm *PortsModel) handleKey(msg tea.KeyPressMsg) (*PortsModel, tea.Cmd) {
	if pm.confirmModal != nil {
		var cmd tea.Cmd
		pm.confirmModal, cmd = pm.confirmModal.Update(msg)
		return pm, cmd
	}
	if pm.table.InEditMode() {
		cmd := pm.table.Update(msg)
		pm.table.GotoTop() // a narrowing query starts from the first match
		return pm, cmd
	}
	return pm.handleKeyNormal(msg)
}

// handlePaste gives pasted text to the filter bar while it has the keyboard and
// no modal is open; otherwise it is dropped (Rule 111).
func (pm *PortsModel) handlePaste(msg tea.PasteMsg) (*PortsModel, tea.Cmd) {
	if pm.confirmModal != nil || !pm.table.InEditMode() {
		return pm, nil
	}
	cmd := pm.table.Update(msg)
	pm.table.GotoTop() // a narrowing query starts from the first match
	return pm, cmd
}

// toggleToken flips a filter and returns to the top, since the list under the
// cursor is not the list the user was looking at any more.
func (pm *PortsModel) toggleToken(label string) {
	pm.table.SetTokenActive(label, !pm.table.IsTokenActive(label))
	pm.table.GotoTop()
}

func (pm *PortsModel) handleKeyNormal(msg tea.KeyPressMsg) (*PortsModel, tea.Cmd) {
	switch msg.String() {
	case "up", "down", "pgup", "pgdown", "home", "end", "/":
		return pm, pm.table.Update(msg)
	case keymap.Space:
		pm.paused = !pm.paused
		pm.table.SetTokenActive(filterTokenPaused, pm.paused)
		// Pausing is a state, not an event: it is derived in statusLine rather
		// than set as a message, which would expire while still paused.
	case "t":
		pm.toggleToken(filterTokenTCP)
	case "u":
		pm.toggleToken(filterTokenUDP)
	case "l":
		pm.toggleToken(filterTokenListen)
	case "e":
		pm.toggleToken(filterTokenEstab)
	case "n":
		pm.numericAddrs = !pm.numericAddrs
		pm.table.SetTokenActive(filterTokenNumeric, pm.numericAddrs)
		return pm, fetchPortsCmd(pm.numericAddrs)
	case "z":
		for _, label := range []string{
			filterTokenTCP, filterTokenUDP, filterTokenListen, filterTokenEstab, filterTokenPaused,
		} {
			pm.table.SetTokenActive(label, false)
		}
		pm.table.FilterBar().ClearSearch()
		pm.table.SetItems(pm.table.Items()) // re-apply with the query gone
		pm.paused = false
		pm.table.GotoTop()
	case keymap.Kill:
		return pm.confirmKill()
	}
	return pm, nil
}

// confirmKill asks before sending the signal.
//
// This is the one action in the application that reaches outside it: not a
// container to be brought back up, but a process belonging to whoever is at
// this machine, killed with SIGKILL so it gets no chance to save anything. It
// had no confirmation at all, while deleting a container — recreatable from its
// image — had one (§3.26).
func (pm *PortsModel) confirmKill() (*PortsModel, tea.Cmd) {
	if k := pm.killable(); !k.Enabled() {
		return pm, pm.footer.Warn(k.Reason)
	}
	entry, _ := pm.table.Selected()
	if pm.table.IsBusy(entry.PID) {
		return pm, pm.footer.Warn("Already killing PID " + entry.PID)
	}
	// A deferred identity (Windows, over the listing budget) is read here, in
	// Update, one short read at the press of K and never at render. Any refusal
	// shows now, with no confirmation to answer first (DEVDESK-709).
	identity, err := pm.ops.confirm(entry.PID, entry.Identity)
	if err != nil {
		return pm, pm.refusal(entry.PID, entry.Process, err)
	}
	// The target is captured here, once, as values: the table refreshes every
	// two seconds and the cursor stays on an index, so by the time the user
	// answers, the selected row can be another process (DEVDESK-714).
	target := killTarget{PID: entry.PID, Process: entry.Process, Identity: identity}
	pm.pendingKill = &target
	pm.confirmModal = components.NewConfirmModal("Kill Process", killPrompt(target, pm.ops.windows))
	return pm, nil
}

// killTarget is the process a confirmation is about: what the user was shown
// when it opened, whatever the table does afterwards.
type killTarget struct {
	PID      string
	Process  string
	Identity ports.Identity // the one the row was listed with, or read when the confirmation opened (DEVDESK-709)
}

// killPrompt is the text of the confirmation. Windows has no signal to send:
// the process is ended at once, so the sentence says that instead of naming
// a SIGKILL that does not exist there.
func killPrompt(t killTarget, windows bool) string {
	if windows {
		return fmt.Sprintf("Terminate %s (PID %s)?\n\nWindows has no clean stop for this process: it is ended at once and cannot save anything.",
			t.Process, t.PID)
	}
	return fmt.Sprintf("Send SIGKILL to %s (PID %s)?\n\nThe process gets no chance to shut down cleanly.",
		t.Process, t.PID)
}

// killHelpDescription is the help line of K on the Ports tab.
func killHelpDescription(windows bool) string {
	if windows {
		return "End the selected process at once, after confirmation (Ports tab)"
	}
	return "Send SIGKILL to the selected process, after confirmation (Ports tab)"
}

// presence says what the last table received holds of a kill target.
type presence int

const (
	// present: a row of the target's PID has its name and no conflicting identity.
	present presence = iota
	// replaced: a row holds the target's PID under another identity, so another
	// process has it now and nothing may be sent (DEVDESK-709, criterion 2).
	replaced
	// absent: no row holds the PID. The process may be gone, or its PID may
	// belong to another process under another name (D174).
	absent
)

// targetPresence compares the target with the last table received, without a
// system call. A deferred or unknown identity never conflicts (Conflicts needs
// both known): the signal itself verifies those against the process it holds.
func (pm *PortsModel) targetPresence(t killTarget) presence {
	result := absent
	for _, s := range pm.table.Items() {
		switch {
		case s.PID != t.PID:
		case s.Identity.Conflicts(t.Identity):
			result = replaced
		case s.Process == t.Process:
			return present
		}
	}
	return result
}

// Why K does not apply, written once so the header, the footer and the tests
// cannot drift apart on the wording (Rule 129 — English US).
const (
	reasonNoSocketRow = "No socket selected"
	reasonNoPID       = "The system did not say which process holds this socket"
	// reasonUnverifiable greys K on a row DevDesk could not read an identity for
	// (DEVDESK-709, S-01b criterion 4): without it a signal could land on
	// whatever process holds the PID by then.
	reasonUnverifiable = "Cannot verify which process this is"
)

// killable reports whether K has a process to signal (Rule 130).
//
// A protected process (DevDesk itself, the process that started it, PID 1, the
// Windows system processes) is greyed with the reason ports.Protection gives.
//
// A socket the system declines to attribute carries no PID — `List` blanks a
// PID of zero rather than printing it, because process 0 is a whole process
// group on Unix and a row reading "0" would look killable. That is the state
// this greys; whether the operating system will *accept* the signal is a
// different question, and one only the attempt can answer.
func (pm *PortsModel) killable() shortcut.Availability {
	entry, ok := pm.table.Selected()
	switch {
	case !ok:
		return shortcut.Unavailable(reasonNoSocketRow)
	case entry.PID == "":
		return shortcut.Unavailable(reasonNoPID)
	}
	if p := pm.ops.protection(entry.PID); p != ports.Unprotected {
		return shortcut.Unavailable(p.Reason())
	}
	// After the protection, as in ports.Kill: DevDesk's own row says what it
	// is, not that DevDesk cannot verify it. A deferred identity is verifiable:
	// it is read when the confirmation opens, never here.
	if !entry.Identity.Verifiable() {
		return shortcut.Unavailable(reasonUnverifiable)
	}
	return shortcut.Availability{}
}

// handleConfirmNo closes the confirmation without a signal.
func (pm *PortsModel) handleConfirmNo() (*PortsModel, tea.Cmd) {
	pm.confirmModal = nil
	pm.pendingKill = nil
	return pm, nil
}

// handleConfirmYes sends the signal to the target captured when the
// confirmation opened, never to the row under the cursor. A Yes with no
// pending target (nothing was opened) does nothing. A target the last table no
// longer lists is refused with a warning (it may be gone, or its PID may belong
// to another process by now), and so is one the table lists under another
// identity, which says so and reads the table again.
func (pm *PortsModel) handleConfirmYes() (*PortsModel, tea.Cmd) {
	target := pm.pendingKill
	pm.confirmModal = nil
	pm.pendingKill = nil
	if target == nil {
		return pm, nil
	}
	switch pm.targetPresence(*target) {
	case replaced:
		pid, _ := strconv.Atoi(target.PID)
		return pm, pm.refusal(target.PID, target.Process, &ports.IdentityChangedError{PID: pid})
	case absent:
		return pm, pm.footer.Warn(fmt.Sprintf("%s (PID %s) is no longer listed — not stopped", target.Process, target.PID))
	}
	// The state may have changed since the confirmation opened; the verdict on
	// the captured target decides, not the row under the cursor.
	if p := pm.ops.protection(target.PID); p != ports.Unprotected {
		return pm, pm.footer.Warn(fmt.Sprintf("PID %s not stopped: %s", target.PID, p.Reason()))
	}
	if pm.table.IsBusy(target.PID) {
		return pm, nil
	}
	// Keyed on the PID, so every socket the process holds shows the running dot
	// at once — which is what happens: the kill takes them all. The table reads
	// the busy mark at render; the router's frame drives the blink.
	pm.table.MarkBusy(target.PID, "Killing "+target.Process+" ("+target.PID+")")
	return pm, killProcessCmd(pm.ops.kill, *target)
}

// view renders the table. An empty table stays a table (Rule 139) — its
// header and no rows — whether it is genuinely empty, filtered down to
// nothing, still loading, or stale: those are a footer status (statusLine) and
// a header count, never body text.
func (pm *PortsModel) view() string {
	return pm.table.View()
}

// summaryLine counts what is on screen, for the header (Rule 139).
func (pm *PortsModel) summaryLine() string {
	return fmt.Sprintf("%d", len(pm.table.Items()))
}
