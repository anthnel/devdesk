package netdiag

import (
	"fmt"
	"log"
	"strings"
	"time"

	"github.com/charmbracelet/bubbles/table"
	tea "github.com/charmbracelet/bubbletea"

	dockerpkg "gitlab.com/anthnell/devsecops/devdesk/internal/docker"
	"gitlab.com/anthnell/devsecops/devdesk/internal/ui/components"
	"gitlab.com/anthnell/devsecops/devdesk/internal/ui/theme"
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

	ports    []dockerpkg.PortInfo
	filtered []dockerpkg.PortInfo
	table    table.Model

	// table lifecycle: avoid full recreation on data ticks to preserve scroll position
	tableReady      bool
	lastTableWidth  int
	lastTableHeight int

	// filters and search via shared FilterBar
	filterBar components.FilterBar
	paused    bool

	// address display
	numericAddrs bool // true = raw IPs/ports (-n flag), false = DNS names

	footerError string
	footerInfo  string
}

// newPortsModel creates a new PortsModel.
func newPortsModel(image string) *PortsModel {
	fb := components.NewFilterBarWithTokens([]components.FilterToken{
		{Label: filterTokenTCP},
		{Label: filterTokenUDP},
		{Label: filterTokenListen},
		{Label: filterTokenEstab},
		{Label: filterTokenNumeric},
		{Label: filterTokenPaused},
	})

	return &PortsModel{
		image:        image,
		numericAddrs: true,
		filterBar:    fb,
	}
}

// initPorts returns the initial commands: an immediate fetch + the first tick.
func (pm *PortsModel) initPorts() tea.Cmd {
	return tea.Batch(fetchPortsCmd(pm.image, pm.numericAddrs), portsTickCmd())
}

// InEditMode returns true when the search input is active.
func (pm *PortsModel) InEditMode() bool {
	return pm.filterBar.InEditMode()
}

// resize updates terminal dimensions and rebuilds the table.
func (pm *PortsModel) resize(width, height int) {
	pm.width = width
	pm.height = height
	pm.rebuildTable()
}

func (pm *PortsModel) update(msg tea.Msg) (*PortsModel, tea.Cmd) {
	switch msg := msg.(type) {
	case portsTickMsg:
		return pm.handleTick()
	case portsDataMsg:
		return pm.handleData(msg)
	case portsKillResultMsg:
		return pm.handleKillResult(msg)
	case portsClearFooterMsg:
		pm.footerError = ""
		pm.footerInfo = ""
		return pm, nil
	case tea.KeyMsg:
		return pm.handleKey(msg)
	}
	return pm, nil
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
	pm.ports = msg.ports
	pm.applyFilters()
	pm.rebuildTable()
	return pm, nil
}

func (pm *PortsModel) handleKillResult(msg portsKillResultMsg) (*PortsModel, tea.Cmd) {
	if msg.err != nil {
		log.Printf("ERROR [netdiag/ports] KillProcess pid=%s: %v", msg.pid, msg.err)
		pm.footerError = fmt.Sprintf("Failed to kill PID %s", msg.pid)
	} else {
		pm.footerInfo = fmt.Sprintf("Process %s terminated", msg.pid)
	}
	return pm, portsClearFooterCmd()
}

func (pm *PortsModel) handleKey(msg tea.KeyMsg) (*PortsModel, tea.Cmd) {
	if pm.filterBar.InEditMode() {
		return pm.handleKeySearch(msg)
	}
	return pm.handleKeyNormal(msg)
}

func (pm *PortsModel) handleKeySearch(msg tea.KeyMsg) (*PortsModel, tea.Cmd) {
	var cmd tea.Cmd
	pm.filterBar, cmd = pm.filterBar.Update(msg)
	pm.applyFilters()
	pm.rebuildTable()
	pm.table.GotoTop()
	return pm, cmd
}

func (pm *PortsModel) handleKeyNormal(msg tea.KeyMsg) (*PortsModel, tea.Cmd) {
	switch msg.String() {
	case "up", "k":
		pm.table.MoveUp(1)
	case "down", "j":
		pm.table.MoveDown(1)
	case "pgup":
		pm.table.MoveUp(max(pm.height/2, 1))
	case "pgdown":
		pm.table.MoveDown(max(pm.height/2, 1))
	case "g":
		pm.table.GotoTop()
	case "G":
		pm.table.GotoBottom()
	case " ":
		pm.paused = !pm.paused
		pm.filterBar.SetTokenActive(filterTokenPaused, pm.paused)
		if pm.paused {
			pm.footerInfo = "Paused — press space to resume"
		} else {
			pm.footerInfo = ""
		}
		pm.rebuildTable()
	case "/":
		return pm, pm.filterBar.ActivateSearch()
	case "t":
		pm.filterBar.SetTokenActive(filterTokenTCP, !pm.filterBar.IsTokenActive(filterTokenTCP))
		pm.applyFilters()
		pm.rebuildTable()
		pm.table.GotoTop()
	case "u":
		pm.filterBar.SetTokenActive(filterTokenUDP, !pm.filterBar.IsTokenActive(filterTokenUDP))
		pm.applyFilters()
		pm.rebuildTable()
		pm.table.GotoTop()
	case "l":
		pm.filterBar.SetTokenActive(filterTokenListen, !pm.filterBar.IsTokenActive(filterTokenListen))
		pm.applyFilters()
		pm.rebuildTable()
		pm.table.GotoTop()
	case "e":
		pm.filterBar.SetTokenActive(filterTokenEstab, !pm.filterBar.IsTokenActive(filterTokenEstab))
		pm.applyFilters()
		pm.rebuildTable()
		pm.table.GotoTop()
	case "n":
		pm.numericAddrs = !pm.numericAddrs
		pm.filterBar.SetTokenActive(filterTokenNumeric, pm.numericAddrs)
		return pm, fetchPortsCmd(pm.image, pm.numericAddrs)
	case "z":
		pm.filterBar.SetTokenActive(filterTokenTCP, false)
		pm.filterBar.SetTokenActive(filterTokenUDP, false)
		pm.filterBar.SetTokenActive(filterTokenListen, false)
		pm.filterBar.SetTokenActive(filterTokenEstab, false)
		pm.filterBar.SetTokenActive(filterTokenPaused, false)
		pm.filterBar.ClearSearch()
		pm.paused = false
		pm.applyFilters()
		pm.rebuildTable()
		pm.table.GotoTop()
	case "ctrl+k":
		return pm.killSelected()
	}
	return pm, nil
}

func (pm *PortsModel) killSelected() (*PortsModel, tea.Cmd) {
	idx := pm.table.Cursor()
	if idx < 0 || idx >= len(pm.filtered) {
		return pm, nil
	}
	entry := pm.filtered[idx]
	if entry.PID == "" {
		pm.footerInfo = "No PID available for this entry"
		return pm, portsClearFooterCmd()
	}
	return pm, killProcessCmd(pm.image, entry.PID)
}

func (pm *PortsModel) applyFilters() {
	pm.filtered = nil
	query := strings.ToLower(pm.filterBar.SearchQuery())
	filterTCP := pm.filterBar.IsTokenActive(filterTokenTCP)
	filterUDP := pm.filterBar.IsTokenActive(filterTokenUDP)
	filterListen := pm.filterBar.IsTokenActive(filterTokenListen)
	filterEstab := pm.filterBar.IsTokenActive(filterTokenEstab)

	for _, p := range pm.ports {
		// Proto filter (OR logic): skip if no active proto matches.
		if (filterTCP || filterUDP) &&
			(!filterTCP || !strings.EqualFold(p.Protocol, filterTokenTCP)) &&
			(!filterUDP || !strings.EqualFold(p.Protocol, filterTokenUDP)) {
			continue
		}
		// State filter (OR logic): skip if no active state matches.
		if (filterListen || filterEstab) &&
			(!filterListen || !strings.EqualFold(p.State, filterTokenListen)) &&
			(!filterEstab || !strings.EqualFold(p.State, filterTokenEstab)) {
			continue
		}
		if query != "" {
			haystack := strings.ToLower(p.Protocol + " " + p.State + " " + p.LocalAddr + " " + p.PeerAddr + " " + p.Process + " " + p.PID)
			if !strings.Contains(haystack, query) {
				continue
			}
		}
		pm.filtered = append(pm.filtered, p)
	}
}

// tableColumns computes the column definitions based on current width.
func (pm *PortsModel) tableColumns() []table.Column {
	w := max(pm.width-2, 30)
	available := w - 6*2 // 6 columns × 2-char cell padding
	col1W := 6
	col2W := 10
	col3W := 26
	col4W := 26
	col5W := 7
	col6W := max(available-col1W-col2W-col3W-col4W-col5W, 10)
	return []table.Column{
		{Title: "Proto", Width: col1W},
		{Title: "State", Width: col2W},
		{Title: "Local Address", Width: col3W},
		{Title: "Peer Address", Width: col4W},
		{Title: "PID", Width: col5W},
		{Title: "Process", Width: col6W},
	}
}

// buildRows converts filtered entries to table rows.
func (pm *PortsModel) buildRows() []table.Row {
	var rows []table.Row
	for _, p := range pm.filtered {
		rows = append(rows, table.Row{p.Protocol, p.State, p.LocalAddr, p.PeerAddr, p.PID, p.Process})
	}
	return rows
}

func (pm *PortsModel) rebuildTable() {
	if pm.width == 0 {
		return
	}

	pm.filterBar.Resize(max(pm.width, 30))

	// pm.height IS the viewport content height sent by the app (Rule 124).
	// Filter bar is rendered in the footer (outside the viewport).
	tableHeight := pm.height
	if tableHeight < 3 {
		tableHeight = 3
	}

	rows := pm.buildRows()

	if !pm.tableReady {
		// First render: create the table from scratch.
		t := table.New(
			table.WithColumns(pm.tableColumns()),
			table.WithRows(rows),
			table.WithFocused(true),
			table.WithHeight(tableHeight),
		)
		t.SetStyles(theme.DefaultTableStyles())
		pm.table = t
		pm.tableReady = true
		pm.lastTableWidth = pm.width
		pm.lastTableHeight = tableHeight
		return
	}

	// Structural change (terminal resize or filter bar toggled): update columns + height.
	if pm.width != pm.lastTableWidth || tableHeight != pm.lastTableHeight {
		pm.table.SetColumns(pm.tableColumns())
		pm.table.SetHeight(tableHeight)
		pm.lastTableWidth = pm.width
		pm.lastTableHeight = tableHeight
	}

	// Data update: SetRows preserves cursor index and viewport scroll position.
	pm.table.SetRows(rows)
}

func (pm *PortsModel) view() string {
	w := max(pm.width-2, 30)
	var lines []string

	// Table or empty state. When a filter is active, always render the table.
	if len(pm.filtered) == 0 && !pm.filterBar.IsVisible() {
		lines = append(lines, theme.EmptyLineBg(w))
		msg := "No active ports found"
		if pm.ports == nil {
			msg = "Loading ports..."
		}
		lines = append(lines, theme.PadWithBg(theme.Bg("  ")+theme.DimStyle.Render(msg), w))
	} else {
		lines = append(lines, pm.table.View())
	}

	return strings.Join(lines, "\n")
}
