package netdiag

import (
	"fmt"
	"log"
	"strings"
	"time"

	"github.com/charmbracelet/bubbles/spinner"
	tea "github.com/charmbracelet/bubbletea"
	"github.com/charmbracelet/lipgloss"

	dockerpkg "github.com/anthnel/devdesk/internal/docker"
	"github.com/anthnel/devdesk/internal/ui/components"
	"github.com/anthnel/devdesk/internal/ui/datatable"
	"github.com/anthnel/devdesk/internal/ui/theme"
)

// portsTickMsg triggers a data refresh cycle.
type portsTickMsg struct{}

// portsDataMsg carries the result of a RunSS call.
type portsDataMsg struct {
	ports []dockerpkg.PortInfo
	err   error
}

// portsKillResultMsg carries the result of a KillProcess call.
type portsKillResultMsg struct {
	pid string
	err error
}

// portsClearFooterMsg clears the ports model footer after 3 seconds.
type portsClearFooterMsg struct{}

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

func portsTickCmd() tea.Cmd {
	return tea.Tick(2*time.Second, func(time.Time) tea.Msg {
		return portsTickMsg{}
	})
}

func fetchPortsCmd(image string, numeric bool) tea.Cmd {
	return func() tea.Msg {
		ports, err := dockerpkg.RunSS(image, numeric)
		return portsDataMsg{ports: ports, err: err}
	}
}

func killProcessCmd(image, pid string) tea.Cmd {
	return func() tea.Msg {
		err := dockerpkg.KillProcess(image, pid)
		return portsKillResultMsg{pid: pid, err: err}
	}
}

func portsClearFooterCmd() tea.Cmd {
	return tea.Tick(3*time.Second, func(time.Time) tea.Msg {
		return portsClearFooterMsg{}
	})
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
	image  string
	width  int
	height int

	// The table holds the entries, filters them and keeps the cursor. The
	// tableReady / lastTableWidth / lastTableHeight trio that used to live here
	// existed only to avoid recreating the table on every two-second tick and
	// losing the scroll position; SetItems guarantees that instead.
	table datatable.Model[dockerpkg.PortInfo]

	paused bool

	// address display
	numericAddrs bool // true = raw IPs/ports (-n flag), false = DNS names

	footerError string
	footerInfo  string
}

// portsColumns describes the ports table. Every column is searchable: the query
// used to run against all six joined, and it still does.
// portsColumnState is where a running kill puts its spinner.
const portsColumnState = 1

func portsColumns() []datatable.Column[dockerpkg.PortInfo] {
	text := func(get func(dockerpkg.PortInfo) string) datatable.Column[dockerpkg.PortInfo] {
		return datatable.Column[dockerpkg.PortInfo]{Cell: get, Search: get}
	}
	proto := text(func(p dockerpkg.PortInfo) string { return p.Protocol })
	state := text(func(p dockerpkg.PortInfo) string { return p.State })
	local := text(func(p dockerpkg.PortInfo) string { return p.LocalAddr })
	peer := text(func(p dockerpkg.PortInfo) string { return p.PeerAddr })
	pid := text(func(p dockerpkg.PortInfo) string { return p.PID })
	process := text(func(p dockerpkg.PortInfo) string { return p.Process })

	proto.Title, proto.MinWidth = "Proto", 6
	state.Title, state.MinWidth = "State", 10
	state.Style = portStateStyle
	local.Title, local.MinWidth = "Local Address", 26
	peer.Title, peer.MinWidth = "Peer Address", 26
	pid.Title, pid.MinWidth = "PID", 7
	process.Title, process.MinWidth, process.Flex = "Process", 10, 1

	return []datatable.Column[dockerpkg.PortInfo]{proto, state, local, peer, pid, process}
}

// portStateStyle colours the socket state, which is the column this table is
// scanned down: a listening port is something the machine offers, an
// established one is a conversation in progress, and everything else is a
// socket on its way out.
func portStateStyle(p dockerpkg.PortInfo) lipgloss.Style {
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
func matchPortTokens(p dockerpkg.PortInfo, active map[string]bool) bool {
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
func newPortsModel(image string) *PortsModel {
	return &PortsModel{
		image:        image,
		numericAddrs: true,
		table: datatable.New(datatable.Config[dockerpkg.PortInfo]{
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
			Key: func(p dockerpkg.PortInfo) string { return p.PID },
			// The State cell, being exactly what the signal is about to change.
			StatusColumn: portsColumnState,
		}),
	}
}

// initPorts returns the initial commands: an immediate fetch + the first tick.
func (pm *PortsModel) initPorts() tea.Cmd {
	return tea.Batch(fetchPortsCmd(pm.image, pm.numericAddrs), portsTickCmd())
}

// InEditMode returns true when the search input is active.
func (pm *PortsModel) InEditMode() bool {
	return pm.table.InEditMode()
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
	case portsClearFooterMsg:
		pm.footerError = ""
		pm.footerInfo = ""
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
		return pm, portsTickCmd()
	}
	return pm, tea.Batch(fetchPortsCmd(pm.image, pm.numericAddrs), portsTickCmd())
}

func (pm *PortsModel) handleData(msg portsDataMsg) (*PortsModel, tea.Cmd) {
	if msg.err != nil {
		log.Printf("ERROR [netdiag/ports] RunSS: %v", msg.err)
		pm.footerError = "Failed to fetch ports — check logs"
		return pm, portsClearFooterCmd()
	}
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
		log.Printf("ERROR [netdiag/ports] KillProcess pid=%s: %v", msg.pid, msg.err)
		pm.footerError = fmt.Sprintf("Failed to kill PID %s", msg.pid)
	} else {
		pm.footerInfo = fmt.Sprintf("Process %s terminated", msg.pid)
	}
	return pm, portsClearFooterCmd()
}

func (pm *PortsModel) handleKey(msg tea.KeyMsg) (*PortsModel, tea.Cmd) {
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
	case "up", "k", "down", "j", "pgup", "pgdown", "g", "G", "/":
		return pm, pm.table.Update(msg)
	case " ":
		pm.paused = !pm.paused
		pm.table.SetTokenActive(filterTokenPaused, pm.paused)
		if pm.paused {
			pm.footerInfo = "Paused — press space to resume"
		} else {
			pm.footerInfo = ""
		}
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
		return pm, fetchPortsCmd(pm.image, pm.numericAddrs)
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
	case "ctrl+k":
		return pm.killSelected()
	}
	return pm, nil
}

func (pm *PortsModel) killSelected() (*PortsModel, tea.Cmd) {
	entry, ok := pm.table.Selected()
	if !ok {
		return pm, nil
	}
	if entry.PID == "" {
		pm.footerInfo = "No PID available for this entry"
		return pm, portsClearFooterCmd()
	}
	if pm.table.IsBusy(entry.PID) {
		pm.footerInfo = "Already killing PID " + entry.PID
		return pm, portsClearFooterCmd()
	}
	// Keyed on the PID, so every socket the process holds spins at once — which
	// is what happens: the kill takes them all. The State cell is the one to
	// spend, being exactly what the signal is about to change.
	pm.table.MarkBusy(entry.PID, "Killing "+entry.Process+" ("+entry.PID+")")
	return pm, tea.Batch(killProcessCmd(pm.image, entry.PID), portsSpinnerCmd())
}

func (pm *PortsModel) view() string {
	w := max(pm.width-2, 30)
	var lines []string

	// Table or empty state. When a filter is active, always render the table —
	// an empty result with no explanation reads as "no ports" rather than as
	// "your filter matched none".
	if len(pm.table.Visible()) == 0 && !pm.table.FilterBar().IsVisible() {
		lines = append(lines, theme.EmptyLineBg(w))
		msg := "No active ports found"
		if pm.table.Items() == nil {
			msg = "Loading ports..."
		}
		lines = append(lines, theme.PadWithBg(theme.Bg("  ")+theme.DimStyle.Render(msg), w))
	} else {
		lines = append(lines, pm.table.View())
	}

	return strings.Join(lines, "\n")
}
