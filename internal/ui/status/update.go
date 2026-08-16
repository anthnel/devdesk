package status

import (
	"fmt"
	"log"
	"time"

	"github.com/charmbracelet/bubbles/spinner"
	tea "github.com/charmbracelet/bubbletea"

	"github.com/anthnel/devdesk/internal/config"
	"github.com/anthnel/devdesk/internal/status"
	sharedcomponents "github.com/anthnel/devdesk/internal/ui/components"
	"github.com/anthnel/devdesk/internal/ui/datatable"
	"github.com/anthnel/devdesk/internal/ui/status/components"
)

// Init démarre l'application
func (m Model) Init() tea.Cmd {
	return tea.Batch(
		m.spinner.Tick,
		tickCmd(),
		checkComponents(m.config),
	)
}

// Update gère les messages et met à jour l'état
func (m Model) Update(msg tea.Msg) (tea.Model, tea.Cmd) {
	var cmd tea.Cmd
	var cmds []tea.Cmd

	switch msg := msg.(type) {
	case tea.WindowSizeMsg:
		m.resize(msg.Width, msg.Height)

	case tea.KeyMsg:
		return m.handleInputKeyMsg(msg)

	case TickMsg:
		return m.handleTick()

	case spinner.TickMsg:
		if m.checking {
			m.spinner, cmd = m.spinner.Update(msg)
			cmds = append(cmds, cmd)
		}

	case CheckCompleteMsg:
		return m.handleCheckComplete(msg)

	case components.ComponentFormSubmitMsg:
		return m.handleComponentFormSubmit(msg)

	case sharedcomponents.ConfirmModalYesMsg:
		return m.handleConfirmDelete()

	case sharedcomponents.ConfirmModalNoMsg:
		// Annulation de suppression
		m.confirmModal = nil

	case ComponentSavedMsg:
		return m.handleComponentSaved(msg)

	case ComponentDeletedMsg:
		return m.handleComponentDeleted(msg)
	}

	// Mettre à jour la table active si pas en mode formulaire/confirmation
	if m.componentForm == nil && m.confirmModal == nil {
		cmds = append(cmds, m.getCurrentTable().Update(msg))
	}

	return m, tea.Batch(cmds...)
}

// handleTick processes tick messages for auto-refresh timing
func (m Model) handleTick() (tea.Model, tea.Cmd) {
	// Rafraîchir la table pour mettre à jour "Last Check"
	if len(m.components) > 0 {
		m.updateTable()
	}

	// Vérifier si on doit faire un check
	if m.autoRefresh && !m.checking && time.Now().After(m.nextCheck) {
		m.checking = true
		return m, tea.Batch(
			tickCmd(),
			checkComponents(m.config),
		)
	}
	return m, tickCmd()
}

// handleCheckComplete processes completed component checks
func (m Model) handleCheckComplete(msg CheckCompleteMsg) (tea.Model, tea.Cmd) {
	m.checking = false
	m.firstCheck = false
	m.lastCheck = msg.Timestamp
	m.nextCheck = msg.Timestamp.Add(m.refreshInterval)

	if msg.Err != nil {
		m.error = msg.Err.Error()
	} else {
		m.components = msg.Components
		m.logComponentErrors(msg.Components)
		m.updateTable()
		m.error = ""
		// Recalculer les hauteurs des tables maintenant qu'elles ont des données
		m.resize(m.width, m.height)
	}

	return m, nil
}

// logComponentErrors logs warning/error/down components to the log file
func (m *Model) logComponentErrors(components []status.ComponentStatus) {
	for _, comp := range components {
		if comp.Error == "" {
			continue
		}
		switch comp.Status {
		case status.StatusDown:
			log.Printf("[STATUS] DOWN  %s (%s) target=%s: %s", comp.Name, comp.Type, comp.Target, comp.Error)
		case status.StatusError:
			log.Printf("[STATUS] ERROR %s (%s) target=%s: %s", comp.Name, comp.Type, comp.Target, comp.Error)
		case status.StatusWarning:
			log.Printf("[STATUS] WARN  %s (%s) target=%s: %s", comp.Name, comp.Type, comp.Target, comp.Error)
		}
	}
}

// reloadConfigAndCheck reloads config from disk and starts a new check
func (m Model) reloadConfigAndCheck() (tea.Model, tea.Cmd) {
	cfg, err := config.Load()
	if err != nil {
		m.error = err.Error()
		return m, nil
	}

	m.config = cfg
	m.checking = true
	return m, tea.Batch(
		m.spinner.Tick,
		checkComponents(m.config),
	)
}

// handleComponentSaved processes component save results
func (m Model) handleComponentSaved(msg ComponentSavedMsg) (tea.Model, tea.Cmd) {
	if msg.Error != nil {
		m.error = msg.Error.Error()
		return m, nil
	}
	return m.reloadConfigAndCheck()
}

// handleComponentDeleted processes component deletion results
func (m Model) handleComponentDeleted(msg ComponentDeletedMsg) (tea.Model, tea.Cmd) {
	if msg.Error != nil {
		m.error = msg.Error.Error()
		return m, nil
	}
	return m.reloadConfigAndCheck()
}

// resize ajuste les dimensions des tables en fonction de la taille du terminal
func (m *Model) resize(width, height int) {
	m.width = width
	m.height = height
	m.filterBar.Resize(width)

	// Rule 124: footer (tab bar + filter bar) is outside the viewport; subtract table header only
	tableDataHeight := height - 1
	if tableDataHeight < 1 {
		tableDataHeight = 1
	}

	// Both tables are laid out, not just the visible one: switching tabs must
	// not have to wait for a resize to get its widths right. Eleven ratios and
	// two remainders used to live here.
	m.monitorTable.Resize(width, tableDataHeight)
	m.sslTable.Resize(width, tableDataHeight)
}

// getCurrentTable returns the currently active table
func (m *Model) getCurrentTable() *datatable.Model[status.ComponentStatus] {
	if m.activeTab == TabCertificates {
		return &m.sslTable
	}
	return &m.monitorTable
}

// getSelectedComponentIndex returns the index in m.config.Status.Components of
// the row under the cursor.
//
// It used to replay the sort by hand and walk the SSL list by counting, neither
// of which applied the text filter the rows had already been through — so under
// a filter `e` and `ctrl+d` acted on a monitor the user was not looking at
// (D25). The table resolves the cursor against the slice its rows were built
// from; all that is left here is matching a status back to its config entry,
// which is a different question and the only one this view has to answer.
func (m *Model) getSelectedComponentIndex() int {
	selected, ok := m.getCurrentTable().Selected()
	if !ok {
		return -1
	}
	for i, c := range m.config.Status.Components {
		if c.Name == selected.Name && string(c.Type) == string(selected.Type) && c.Target == selected.Target {
			return i
		}
	}
	return -1
}

func (m Model) handleInputKeyMsg(msg tea.KeyMsg) (tea.Model, tea.Cmd) {
	// Filter bar search mode - prioritaire
	if m.filterBar.InEditMode() {
		var cmd tea.Cmd
		m.filterBar, cmd = m.filterBar.Update(msg)
		m.updateTable()
		m.getCurrentTable().GotoTop() // a narrowing query starts from the first match
		return m, cmd
	}

	// Mode formulaire - prioritaire
	if m.componentForm != nil {
		var cmd tea.Cmd
		m.componentForm, cmd = m.componentForm.Update(msg)
		return m, cmd
	}

	// Mode confirmation - prioritaire
	if m.confirmModal != nil {
		var cmd tea.Cmd
		m.confirmModal, cmd = m.confirmModal.Update(msg)
		return m, cmd
	}

	// Mode normal - déléguer selon le type de commande
	switch msg.String() {
	case "/":
		return m, m.filterBar.ActivateSearch()
	case "q", "ctrl+c":
		return m, tea.Quit
	case "ctrl+r":
		return m.handleManualRefresh()
	case "tab", "shift+tab":
		m.switchTab((m.activeTab + 1) % 2)
		return m, nil
	case "ctrl+n", "e", "ctrl+d":
		return m.handleMonitorOperations(msg)
	case ".":
		return m.cycleSort()
	}

	// Everything else goes to the focused table. It used to be an allow-list
	// that did not name pgup or pgdown, so they never reached
	// handleTableNavigation — which handled them, in code nothing could run.
	return m, m.getCurrentTable().Update(msg)
}

// handleManualRefresh runs a check now, whatever the auto-refresh setting says.
//
// It is the only refresh control left here: the interval and auto-refresh are
// settings, and the configuration view owns them. Forcing a check is an action,
// not a setting, so it stays (Rule 111).
func (m Model) handleManualRefresh() (tea.Model, tea.Cmd) {
	if m.checking {
		return m, nil
	}

	m.checking = true
	return m, tea.Batch(
		m.spinner.Tick,
		checkComponents(m.config),
	)
}

// switchTab switches focus to the given tab
func (m *Model) switchTab(tab int) {
	m.activeTab = tab
	m.applyTabFocus()
}

// handleComponentFormSubmit processes form submission for component creation/editing
func (m Model) handleComponentFormSubmit(msg components.ComponentFormSubmitMsg) (tea.Model, tea.Cmd) {
	m.componentForm = nil
	if msg.Original != nil {
		for i, c := range m.config.Status.Components {
			if c.Name == msg.Original.Name && c.Type == msg.Original.Type && c.Target == msg.Original.Target {
				m.config.Status.Components[i] = msg.Component
				break
			}
		}
	} else {
		m.config.Status.Components = append(m.config.Status.Components, msg.Component)
	}
	return m, saveComponent(m.config)
}

// handleConfirmDelete processes confirmed deletion of a component
func (m Model) handleConfirmDelete() (tea.Model, tea.Cmd) {
	m.confirmModal = nil
	if m.selectedIdx >= 0 && m.selectedIdx < len(m.config.Status.Components) {
		m.config.Status.Components = append(
			m.config.Status.Components[:m.selectedIdx],
			m.config.Status.Components[m.selectedIdx+1:]...,
		)
		return m, deleteComponent(m.config)
	}
	return m, nil
}

// handleMonitorOperations handles new, edit, and delete operations
func (m Model) handleMonitorOperations(msg tea.KeyMsg) (tea.Model, tea.Cmd) {
	switch msg.String() {
	case "ctrl+n":
		// Nouveau composant
		m.componentForm = components.NewComponentForm(nil)

	case "e":
		// Éditer le composant sélectionné
		if len(m.components) > 0 {
			idx := m.getSelectedComponentIndex()
			if idx >= 0 && idx < len(m.config.Status.Components) {
				m.selectedIdx = idx
				m.componentForm = components.NewComponentForm(&m.config.Status.Components[idx])
			}
		}

	case "ctrl+d":
		// Supprimer le composant sélectionné
		if len(m.components) > 0 {
			idx := m.getSelectedComponentIndex()
			if idx >= 0 && idx < len(m.config.Status.Components) {
				m.selectedIdx = idx
				compName := m.config.Status.Components[idx].Name
				m.confirmModal = sharedcomponents.NewConfirmModal(
					"Delete Monitor",
					fmt.Sprintf("Are you sure you want to delete '%s'?", compName),
				)
			}
		}
	}

	return m, nil
}

// cycleSort advances the sort on the active table (Rule 111's `.`). The
// certificates tab declares no comparators, so it is inert there.
func (m Model) cycleSort() (tea.Model, tea.Cmd) {
	m.getCurrentTable().CycleSort()
	return m, nil
}
