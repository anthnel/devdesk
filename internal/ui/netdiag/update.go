package netdiag

import (
	"log"

	"github.com/charmbracelet/bubbles/spinner"
	"github.com/charmbracelet/bubbles/textinput"
	tea "github.com/charmbracelet/bubbletea"

	"github.com/anthnel/devdesk/internal/netcheck"
	"github.com/anthnel/devdesk/internal/ui/components"
)

// Init implements tea.Model
func (m *Model) Init() tea.Cmd {
	return tea.Batch(textinput.Blink, m.portsModel.initPorts(), m.topologyModel.initTopology())
}

// InEditMode implements FormView — true when a text input is active, so the
// router hands the key to the field rather than opening the command line.
func (m *Model) InEditMode() bool {
	switch m.activeTab {
	case tabPorts:
		return m.portsModel.InEditMode()
	case tabTopology:
		return false
	}
	if m.filterBar.InEditMode() {
		return true
	}
	if m.state == StateInput {
		return m.focusedField == fieldTarget || m.focusedField == fieldPort || m.focusedField == fieldDNSServer
	}
	return false
}

// FilterBarVisible implements app.FilterBarView.
func (m *Model) FilterBarVisible() bool {
	if m.activeTab == tabPorts {
		return m.portsModel.table.FilterBar().IsVisible()
	}
	return m.activeTab == tabDiagnostics && m.filterBar.IsVisible()
}

// Update implements tea.Model
func (m *Model) Update(msg tea.Msg) (tea.Model, tea.Cmd) {
	switch msg := msg.(type) {
	case tea.WindowSizeMsg:
		m.width = msg.Width
		m.height = msg.Height
		m.resizeInputs()
		m.filterBar.Resize(msg.Width)
		m.rebuildChecksTable()
		m.resizeDetailsViewport()
		m.portsModel.resize(msg.Width, msg.Height)
		m.topologyModel.resize(msg.Width, msg.Height)
		return m, nil

	case spinner.TickMsg:
		return m.handleSpinnerTick(msg)

	case stageDoneMsg:
		return m.handleStageDone(msg)

	case traceDoneMsg:
		return m.handleTraceDone(msg)

	// Ports sub-model messages. The confirm-modal answers are here because the
	// kill asks before it acts (§3.26), and the modal is the ports tab's.
	case portsTickMsg, portsDataMsg, portsKillResultMsg,
		components.ConfirmModalYesMsg, components.ConfirmModalNoMsg:
		var cmd tea.Cmd
		m.portsModel, cmd = m.portsModel.update(msg)
		return m, cmd

	// Topology sub-model messages
	case topoDataMsg:
		var cmd tea.Cmd
		m.topologyModel, cmd = m.topologyModel.update(msg)
		return m, cmd

	case tea.KeyMsg:
		return m.handleKey(msg)
	}

	// Each tab holds its own footer, so an expiry has to be offered to all
	// three: the timer fires wherever the user has since navigated.
	m.footer.Handle(msg)
	m.portsModel.footer.Handle(msg)
	m.topologyModel.footer.Handle(msg)
	return m, nil
}

func (m *Model) handleSpinnerTick(msg spinner.TickMsg) (*Model, tea.Cmd) {
	if m.state == StateRunning || m.tracing {
		var cmd tea.Cmd
		m.spinner, cmd = m.spinner.Update(msg)
		m.footer.SetSpinnerFrame(m.spinner.View())
		return m, cmd
	}
	if m.activeTab == tabTopology {
		var cmd tea.Cmd
		m.topologyModel, cmd = m.topologyModel.update(msg)
		return m, cmd
	}
	return m, nil
}

// handleStageDone records one stage's answers and starts the next.
func (m *Model) handleStageDone(msg stageDoneMsg) (*Model, tea.Cmd) {
	if msg.gen != m.runGen {
		return m, nil // a superseded run
	}
	m.results = msg.results
	m.rebuildChecksTable()

	steps := netcheck.Steps()
	if msg.next >= len(steps) {
		m.state = StateResults
		m.verdict = netcheck.Summarize(m.results.All())
		log.Printf("INFO [netdiag] run finished: verdict=%s over %d checks",
			m.verdict, len(m.results.All()))
		return m, nil
	}

	m.runStep = msg.next
	m.runStage = steps[msg.next]

	tg, err := m.buildTarget()
	if err != nil {
		// The form has not changed since the run started, so this cannot
		// normally fail. Reporting it beats continuing with a target nobody
		// can name.
		log.Printf("ERROR [netdiag] target became invalid mid-run: %v", err)
		m.state = StateResults
		return m, m.footer.Error("Run stopped — the target is no longer valid")
	}
	return m, runStageCmd(m.runGen, tg, msg.next, m.results)
}

func (m *Model) handleTraceDone(msg traceDoneMsg) (*Model, tea.Cmd) {
	if msg.gen != m.runGen {
		return m, nil
	}
	m.tracing = false
	if msg.output == "" {
		return m, m.footer.Error("The trace produced no output — check logs")
	}
	m.traceOutput = msg.output
	m.traceTCP = msg.tcp
	m.state = StateDetails
	m.resizeDetailsViewport()
	m.detailsViewport.SetContent(m.renderDetailsContent(m.detailsViewport.Width))
	m.detailsViewport.GotoTop()
	return m, nil
}

func (m *Model) handleKey(msg tea.KeyMsg) (*Model, tea.Cmd) {
	// A filter bar in edit mode claims every key before anything else, or a
	// query cannot contain the letters the view binds.
	if m.activeTab == tabDiagnostics && m.filterBar.InEditMode() {
		var cmd tea.Cmd
		m.filterBar, cmd = m.filterBar.Update(msg)
		m.rebuildChecksTable()
		return m, cmd
	}

	// Tab / Shift+Tab cycles between the three tabs (Rule 135)
	switch msg.String() {
	case "tab":
		m.activeTab = (m.activeTab + 1) % 3
		return m, nil
	case "shift+tab":
		m.activeTab = (m.activeTab + 2) % 3
		return m, nil
	}

	// Route to active tab
	if m.activeTab == tabPorts {
		var cmd tea.Cmd
		m.portsModel, cmd = m.portsModel.update(msg)
		return m, cmd
	}
	if m.activeTab == tabTopology {
		var cmd tea.Cmd
		m.topologyModel, cmd = m.topologyModel.update(msg)
		return m, cmd
	}

	switch m.state {
	case StateInput:
		return m.handleKeyInput(msg)
	case StateRunning:
		return m.handleKeyRunning(msg)
	case StateResults:
		return m.handleKeyResults(msg)
	case StateDetails:
		return m.handleKeyDetails(msg)
	}
	return m, nil
}

// handleKeyRunning cancels the run. The stage in flight is awaited rather than
// interrupted — it is a network read that will time out on its own — and its
// result is discarded by the generation counter.
func (m *Model) handleKeyRunning(msg tea.KeyMsg) (*Model, tea.Cmd) {
	if msg.String() != "esc" {
		return m, nil
	}
	m.runGen++
	m.state = StateResults
	m.verdict = netcheck.Summarize(m.results.All())
	m.rebuildChecksTable()
	return m, m.footer.Warn("Run cancelled — showing what completed")
}

func (m *Model) handleKeyInput(msg tea.KeyMsg) (*Model, tea.Cmd) {
	switch msg.String() {
	case "down":
		m.moveFocus(1)
		return m, nil
	case "up":
		m.moveFocus(-1)
		return m, nil
	case "enter":
		// enter runs from anywhere in the form: with three fields and one
		// button there is nothing else it could mean, and making the user walk
		// to the button first is a step that buys nothing.
		return m.startRun()
	case "esc":
		return m, nil
	}
	return m.updateActiveInput(msg)
}

func (m *Model) moveFocus(delta int) {
	switch m.focusedField {
	case fieldTarget:
		m.targetInput.Blur()
	case fieldPort:
		m.portInput.Blur()
	case fieldDNSServer:
		m.dnsServerInput.Blur()
	}

	m.focusedField = (m.focusedField + delta + fieldCount) % fieldCount

	switch m.focusedField {
	case fieldTarget:
		m.targetInput.Focus()
	case fieldPort:
		m.portInput.Focus()
	case fieldDNSServer:
		m.dnsServerInput.Focus()
	}
}

func (m *Model) updateActiveInput(msg tea.KeyMsg) (*Model, tea.Cmd) {
	var cmd tea.Cmd
	switch m.focusedField {
	case fieldTarget:
		m.targetInput, cmd = m.targetInput.Update(msg)
	case fieldPort:
		m.portInput, cmd = m.portInput.Update(msg)
	case fieldDNSServer:
		m.dnsServerInput, cmd = m.dnsServerInput.Update(msg)
	}
	return m, cmd
}

func (m *Model) handleKeyResults(msg tea.KeyMsg) (*Model, tea.Cmd) {
	switch msg.String() {
	case "enter":
		return m.openDetails()
	case "esc":
		return m.resetToForm()
	case "ctrl+r":
		return m.startRun()
	case "/":
		return m, m.filterBar.ActivateSearch()
	case "p":
		m.filterBar.SetTokenActive(problemsToken, !m.filterBar.IsTokenActive(problemsToken))
		m.rebuildChecksTable()
		return m, nil
	case "H":
		return m.startTrace()
	}
	return m, m.checksTable.Update(msg)
}

// startTrace runs a route trace, when a check makes one worth running.
func (m *Model) startTrace() (*Model, tea.Cmd) {
	if !m.traceWorthOffering() {
		return m, m.footer.Warn("Nothing here points at a routing problem")
	}
	tg, err := m.buildTarget()
	if err != nil {
		return m, m.footer.Error(capitalize(err.Error()))
	}
	// A refused port is a path question about that port, so it is traced with
	// TCP; a filtered ping is a question about the path itself.
	tcp := m.results.VerdictOf(netcheck.CheckTCP) == netcheck.Fail
	m.tracing = true
	return m, tea.Batch(m.spinner.Tick, traceCmd(m.runGen, m.config.Docker.NetworkToolImage, tg, tcp))
}

func (m *Model) handleKeyDetails(msg tea.KeyMsg) (*Model, tea.Cmd) {
	switch msg.String() {
	case "esc":
		m.state = StateResults
		m.traceOutput = ""
		return m, nil
	case "up":
		m.detailsViewport.ScrollUp(1)
	case "down":
		m.detailsViewport.ScrollDown(1)
	case "pgup":
		m.detailsViewport.HalfPageUp()
	case "pgdown":
		m.detailsViewport.HalfPageDown()
	case "home":
		m.detailsViewport.GotoTop()
	case "end":
		m.detailsViewport.GotoBottom()
	}
	return m, nil
}

func (m *Model) openDetails() (*Model, tea.Cmd) {
	c, ok := m.checksTable.Selected()
	if !ok {
		return m, nil
	}
	m.selected = c
	m.traceOutput = ""
	m.state = StateDetails
	m.resizeDetailsViewport()
	m.detailsViewport.SetContent(m.renderDetailsContent(m.detailsViewport.Width))
	m.detailsViewport.GotoTop()
	return m, nil
}

func (m *Model) resetToForm() (*Model, tea.Cmd) {
	m.state = StateInput
	m.focusedField = fieldTarget
	m.targetInput.Focus()
	m.results = netcheck.Results{}
	m.verdict = netcheck.Unknown
	m.traceOutput = ""
	m.filterBar.ClearSearch()
	m.filterBar.SetTokenActive(problemsToken, false)
	m.rebuildChecksTable()
	return m, nil
}
