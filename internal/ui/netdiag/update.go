package netdiag

import (
	"log"

	"github.com/charmbracelet/bubbles/spinner"
	"github.com/charmbracelet/bubbles/textinput"
	tea "github.com/charmbracelet/bubbletea"

	"github.com/anthnel/devdesk/internal/ui/components"
)

// Init implements tea.Model
func (m *Model) Init() tea.Cmd {
	return tea.Batch(textinput.Blink, m.portsModel.initPorts(), m.topologyModel.initTopology())
}

// InEditMode implements FormView — true when a text input is active, tests are running,
// or the detail view is open (so esc is forwarded to the view instead of consumed by the app).
func (m *Model) InEditMode() bool {
	switch m.activeTab {
	case tabPorts:
		return m.portsModel.InEditMode()
	case tabTopology:
		return false
	}
	// StateRunning and StateDetails were listed here only to be handed esc, which
	// the router now forwards on its own (§1.3 D15). A running test and a details
	// pane hold no field, so they claim no key.
	if m.state == StateInput {
		return m.focusedField == fieldTarget || m.focusedField == fieldPort || m.focusedField == fieldDNSServer
	}
	return false
}

// FilterBarVisible returns true when the ports filter bar is visible (implements app.FilterBarView).
func (m *Model) FilterBarVisible() bool {
	return m.activeTab == tabPorts && m.portsModel.table.FilterBar().IsVisible()
}

// Update implements tea.Model
func (m *Model) Update(msg tea.Msg) (tea.Model, tea.Cmd) {
	switch msg := msg.(type) {
	case tea.WindowSizeMsg:
		m.width = msg.Width
		m.height = msg.Height
		m.resizeInputs()
		m.rebuildResultsTable()
		m.resizeDetailsViewport()
		m.portsModel.resize(msg.Width, msg.Height)
		m.topologyModel.resize(msg.Width, msg.Height)
		return m, nil

	case spinner.TickMsg:
		if m.state == StateRunning {
			var cmd tea.Cmd
			m.spinner, cmd = m.spinner.Update(msg)
			return m, cmd
		}
		if m.activeTab == tabTopology {
			var cmd tea.Cmd
			m.topologyModel, cmd = m.topologyModel.update(msg)
			return m, cmd
		}
		return m, nil

	case testCompleteMsg:
		return m.handleTestComplete(msg)

	case clearFooterMsg:
		m.footerError = ""
		m.footerInfo = ""
		return m, nil

	// Ports sub-model messages. The confirm-modal answers are here because the
	// kill asks before it acts (§3.26), and the modal is the ports tab's.
	case portsTickMsg, portsDataMsg, portsKillResultMsg, portsClearFooterMsg,
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
	return m, nil
}

func (m *Model) handleTestComplete(msg testCompleteMsg) (*Model, tea.Cmd) {
	if msg.gen != m.runGen {
		return m, nil // stale result from a cancelled run
	}
	m.results[msg.name] = testResult{
		name:    msg.name,
		success: msg.success,
		output:  msg.output,
		done:    true,
	}
	m.doneTests++
	log.Printf("INFO [netdiag] test %q done: success=%v", msg.name, msg.success)

	if m.doneTests >= m.totalTests {
		m.state = StateResults
		m.rebuildResultsTable()
	}
	return m, nil
}

func (m *Model) handleKey(msg tea.KeyMsg) (*Model, tea.Cmd) {
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

func (m *Model) handleKeyRunning(msg tea.KeyMsg) (*Model, tea.Cmd) {
	if msg.String() != "esc" {
		return m, nil
	}
	// Invalidate the current run so arriving testCompleteMsgs are discarded
	m.runGen++
	// Mark all non-done tests as cancelled
	for _, name := range m.resultOrder {
		if res := m.results[name]; !res.done {
			m.results[name] = testResult{name: name, done: true, cancelled: true, output: "cancelled"}
			m.doneTests++
		}
	}
	m.state = StateResults
	m.rebuildResultsTable()
	return m, nil
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
		if m.focusedField == fieldButton {
			return m.startTests()
		}
		return m, nil
	case " ":
		// Space is the only key that toggles checkboxes (Rule 135)
		m.toggleFocusedCheckbox()
		return m, nil
	case "esc":
		return m, nil
	}

	// Forward to active text input
	return m.updateActiveInput(msg)
}

func (m *Model) moveFocus(delta int) {
	// Blur current input if leaving a text field
	switch m.focusedField {
	case fieldTarget:
		m.targetInput.Blur()
	case fieldPort:
		m.portInput.Blur()
	case fieldDNSServer:
		m.dnsServerInput.Blur()
	}

	m.focusedField = (m.focusedField + delta + fieldCount) % fieldCount

	// Focus new input if entering a text field
	switch m.focusedField {
	case fieldTarget:
		m.targetInput.Focus()
	case fieldPort:
		m.portInput.Focus()
	case fieldDNSServer:
		m.dnsServerInput.Focus()
	}
}

func (m *Model) toggleFocusedCheckbox() {
	for i := range m.tests {
		if m.tests[i].fieldIdx == m.focusedField {
			m.tests[i].enabled = !m.tests[i].enabled
			return
		}
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
	// ctrl+r means refresh and only refresh (§3.26). Going back is esc, which
	// handleKeyResults already reaches through the table.
	case "esc":
		return m.resetToForm()
	}
	return m, m.resultsTable.Update(msg)
}

func (m *Model) handleKeyDetails(msg tea.KeyMsg) (*Model, tea.Cmd) {
	switch msg.String() {
	case "esc":
		m.state = StateResults
		return m, nil
	case "f":
		m.rawDetails = !m.rawDetails
		res := m.results[m.selectedTest]
		m.detailsViewport.SetContent(m.renderDetailsContent(res, m.detailsViewport.Width))
		m.detailsViewport.GotoTop()
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
	row, ok := m.resultsTable.Selected()
	if !ok {
		return m, nil
	}
	res, ok := m.results[row.name]
	if !ok {
		return m, nil
	}
	m.selectedTest = row.name
	m.rawDetails = false
	m.state = StateDetails
	m.resizeDetailsViewport()
	m.detailsViewport.SetContent(m.renderDetailsContent(res, m.detailsViewport.Width))
	m.detailsViewport.GotoTop()
	return m, nil
}

func (m *Model) resetToForm() (*Model, tea.Cmd) {
	m.state = StateInput
	m.focusedField = fieldTarget
	m.targetInput.Focus()
	m.results = make(map[string]testResult)
	m.doneTests = 0
	m.totalTests = 0
	m.resultOrder = nil
	return m, nil
}
