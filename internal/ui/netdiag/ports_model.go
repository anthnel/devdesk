package netdiag

import (
	"context"
	"errors"
	"fmt"
	"log"
	"os"
	"strings"
	"time"

	"github.com/charmbracelet/bubbles/spinner"
	tea "github.com/charmbracelet/bubbletea"
	"github.com/charmbracelet/lipgloss"

	"github.com/anthnel/devdesk/internal/ports"
	"github.com/anthnel/devdesk/internal/ui/components"
	"github.com/anthnel/devdesk/internal/ui/datatable"
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

// portsKillResultMsg carries the result of a kill.
type portsKillResultMsg struct {
	pid string
	err error
}

// portsSpinnerTickMsg turns the frame of a row a kill is running on.
//
// This model has no spinner of its own, and its data tick is two seconds apart
// — a frame that advanced on that would look stopped, which is the impression
// the whole thing exists to remove. So the kill starts a tick of its own and it
// stops itself when nothing is left running.
type portsSpinnerTickMsg struct{}

func portsSpinnerCmd() tea.Cmd {
	return tea.Tick(spinner.Dot.FPS, func(time.Time) tea.Msg { return portsSpinnerTickMsg{} })
}

// defaultPortsRefresh is what the interval falls back to. A non-positive one —
// a hand-edited 0 in the config — would make tea.Tick fire without pausing and
// spin a docker exec per frame, so it is refused here rather than trusted.
const defaultPortsRefresh = 2 * time.Second

func portsTickCmd(every time.Duration) tea.Cmd {
	if every <= 0 {
		every = defaultPortsRefresh
	}
	return tea.Tick(every, func(time.Time) tea.Msg {
		return portsTickMsg{}
	})
}

// portsFetchTimeout bounds one read. Reverse DNS is the part that can hang —
// the socket table itself is a system call — and a fetch outliving several
// ticks would stack one behind another.
const portsFetchTimeout = 5 * time.Second

func fetchPortsCmd(numeric bool) tea.Cmd {
	return func() tea.Msg {
		ctx, cancel := context.WithTimeout(context.Background(), portsFetchTimeout)
		defer cancel()
		sockets, err := ports.List(ctx, !numeric)
		return portsDataMsg{ports: sockets, err: err}
	}
}

func killProcessCmd(pid string) tea.Cmd {
	return func() tea.Msg {
		return portsKillResultMsg{pid: pid, err: ports.Kill(pid)}
	}
}

// statusLine is what the Ports tab derives on every frame. Pausing is a state
// rather than an event, so it is rendered from pm.paused instead of set as a
// message — a message would expire after three seconds while still paused.
func (pm *PortsModel) statusLine() components.Status {
	// Staleness outranks the pause: a pause is what the user asked for, an
	// unreachable Docker is not, and only one of the two makes the rows lie.
	if pm.stale {
		return components.Status{Text: pm.staleLabel(), Level: components.LevelError}
	}
	if pm.paused {
		return components.Status{Text: "Paused — press space to resume"}
	}
	return components.Status{}
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
	// refresh is how often the table re-reads the socket table. The spinner tick is separate
	// and much faster: a frame advancing on this interval would look stopped,
	// which is the impression the whole tab exists to remove.
	refresh time.Duration
	width   int
	height  int

	// The table holds the entries, filters them and keeps the cursor. The
	// tableReady / lastTableWidth / lastTableHeight trio that used to live here
	// existed only to avoid recreating the table on every two-second tick and
	// losing the scroll position; SetItems guarantees that instead.
	table datatable.Model[ports.Socket]

	paused bool

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

// portsColumns describes the ports table. Every column is searchable: the query
// used to run against all six joined, and it still does.
// portsColumnState is where a running kill puts its spinner.
const portsColumnState = 1

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

	return []datatable.Column[ports.Socket]{proto, state, local, peer, pid, process}
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
			// The State cell, being exactly what the signal is about to change.
			StatusColumn: portsColumnState,
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
	case portsDataMsg:
		return pm.handleData(msg)
	case portsKillResultMsg:
		return pm.handleKillResult(msg)
	case portsSpinnerTickMsg:
		return pm.handleSpinnerTick()
	case components.ConfirmModalYesMsg:
		pm.confirmModal = nil
		return pm.killSelected()
	case components.ConfirmModalNoMsg:
		pm.confirmModal = nil
		return pm, nil
	case tea.KeyMsg:
		return pm.handleKey(msg)
	}
	return pm, nil
}

// handleSpinnerTick advances the frame and schedules the next one, or lets the
// tick die when the last kill has landed.
func (pm *PortsModel) handleSpinnerTick() (*PortsModel, tea.Cmd) {
	if len(pm.table.BusyLabels()) == 0 {
		return pm, nil
	}
	pm.table.AdvanceSpinner()
	return pm, portsSpinnerCmd()
}

func (pm *PortsModel) handleTick() (*PortsModel, tea.Cmd) {
	if pm.paused {
		return pm, portsTickCmd(pm.refresh)
	}
	return pm, tea.Batch(fetchPortsCmd(pm.numericAddrs), portsTickCmd(pm.refresh))
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
		log.Printf("ERROR [netdiag/ports] ports.Kill pid=%s: %v", msg.pid, msg.err)
		return pm, pm.footer.Error(killFailureMessage(msg.pid, msg.err))
	}
	return pm, pm.footer.Info(fmt.Sprintf("Process %s terminated", msg.pid))
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
// **The "already gone" branch does not fire on Windows**, and that is measured
// too: a PID that does not exist fails OpenProcess with
// ERROR_INVALID_PARAMETER, which maps to neither sentinel, so it falls to the
// generic message. Mapping that code would be a guess — it is what an invalid
// argument returns as well — and the row disappears on the next two-second
// refresh anyway. The branch is kept because ESRCH does map to
// os.ErrProcessDone on Unix.
func killFailureMessage(pid string, err error) string {
	switch {
	case errors.Is(err, os.ErrPermission):
		return fmt.Sprintf("Refused by the system — DevDesk cannot signal PID %s", pid)
	case errors.Is(err, os.ErrProcessDone):
		return fmt.Sprintf("PID %s is no longer running", pid)
	}
	return fmt.Sprintf("Failed to kill PID %s — check logs", pid)
}

func (pm *PortsModel) handleKey(msg tea.KeyMsg) (*PortsModel, tea.Cmd) {
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

// toggleToken flips a filter and returns to the top, since the list under the
// cursor is not the list the user was looking at any more.
func (pm *PortsModel) toggleToken(label string) {
	pm.table.SetTokenActive(label, !pm.table.IsTokenActive(label))
	pm.table.GotoTop()
}

func (pm *PortsModel) handleKeyNormal(msg tea.KeyMsg) (*PortsModel, tea.Cmd) {
	switch msg.String() {
	case "up", "down", "pgup", "pgdown", "home", "end", "/":
		return pm, pm.table.Update(msg)
	case " ":
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
	pm.confirmModal = components.NewConfirmModal(
		"Kill Process",
		fmt.Sprintf("Send SIGKILL to %s (PID %s)?\n\nThe process gets no chance to shut down cleanly.",
			entry.Process, entry.PID),
	)
	return pm, nil
}

// Why K does not apply, written once so the header, the footer and the tests
// cannot drift apart on the wording (Rule 129 — English US).
const (
	reasonNoSocketRow = "No socket selected"
	reasonNoPID       = "The system did not say which process holds this socket"
)

// killable reports whether K has a process to signal (Rule 130).
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
	return shortcut.Availability{}
}

// killSelected sends the signal, once confirmed.
func (pm *PortsModel) killSelected() (*PortsModel, tea.Cmd) {
	entry, ok := pm.table.Selected()
	if !ok {
		return pm, nil
	}
	if entry.PID == "" || pm.table.IsBusy(entry.PID) {
		return pm, nil
	}
	// Keyed on the PID, so every socket the process holds spins at once — which
	// is what happens: the kill takes them all. The State cell is the one to
	// spend, being exactly what the signal is about to change.
	pm.table.MarkBusy(entry.PID, "Killing "+entry.Process+" ("+entry.PID+")")
	return pm, tea.Batch(killProcessCmd(entry.PID), portsSpinnerCmd())
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
