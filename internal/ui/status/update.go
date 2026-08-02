package status

import (
	"fmt"
	"log"
	"sort"
	"strings"
	"time"

	"github.com/charmbracelet/bubbles/spinner"
	"github.com/charmbracelet/bubbles/table"
	tea "github.com/charmbracelet/bubbletea"

	"github.com/anthnel/devdesk/internal/config"
	"github.com/anthnel/devdesk/internal/status"
	sharedcomponents "github.com/anthnel/devdesk/internal/ui/components"
	"github.com/anthnel/devdesk/internal/ui/status/components"
	"github.com/anthnel/devdesk/internal/ui/theme"
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

	case components.ComponentFormCancelledMsg:
		m.componentForm = nil
		m.creating = false
		m.editing = false

	case sharedcomponents.ConfirmModalYesMsg:
		return m.handleConfirmDelete()

	case sharedcomponents.ConfirmModalNoMsg:
		// Annulation de suppression
		m.confirmModal = nil
		m.confirming = false

	case ComponentSavedMsg:
		return m.handleComponentSaved(msg)

	case ComponentDeletedMsg:
		return m.handleComponentDeleted(msg)
	}

	// Mettre à jour la table active si pas en mode formulaire/confirmation
	if m.componentForm == nil && m.confirmModal == nil {
		if m.activeTab == TabMonitors {
			m.monitorTable, cmd = m.monitorTable.Update(msg)
		} else {
			m.sslTable, cmd = m.sslTable.Update(msg)
		}
		cmds = append(cmds, cmd)
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
	if !m.paused && !m.checking && time.Now().After(m.nextCheck) {
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

	m.monitorTable.SetHeight(tableDataHeight)
	m.sslTable.SetHeight(tableDataHeight)

	// Largeur du contenu viewport (terminal - 2 pour les bordures du viewport)
	contentWidth := m.width - 2

	// Monitor table columns (5 columns, cell padding = 5×2 = 10)
	monColumns := m.monitorTable.Columns()
	if len(monColumns) >= 5 {
		available := contentWidth - 5*2
		monColumns[0].Width = int(float64(available) * 0.15) // Name
		monColumns[1].Width = int(float64(available) * 0.30) // Target
		monColumns[2].Width = int(float64(available) * 0.17) // Status
		monColumns[3].Width = int(float64(available) * 0.18) // Type
		// Dernière colonne récupère le reste pour remplir toute la largeur
		monColumns[4].Width = available - monColumns[0].Width - monColumns[1].Width - monColumns[2].Width - monColumns[3].Width
		m.monitorTable.SetColumns(monColumns)
	}

	// SSL table columns (6 columns, cell padding = 6×2 = 12)
	sslColumns := m.sslTable.Columns()
	if len(sslColumns) >= 6 {
		available := contentWidth - 6*2
		sslColumns[0].Width = int(float64(available) * 0.15) // Name
		sslColumns[1].Width = int(float64(available) * 0.25) // Host
		sslColumns[2].Width = int(float64(available) * 0.15) // Status
		sslColumns[3].Width = int(float64(available) * 0.10) // Days Left
		sslColumns[4].Width = int(float64(available) * 0.18) // Expires
		// Dernière colonne récupère le reste
		sslColumns[5].Width = available - sslColumns[0].Width - sslColumns[1].Width - sslColumns[2].Width - sslColumns[3].Width - sslColumns[4].Width
		m.sslTable.SetColumns(sslColumns)
	}
}

// getCurrentTable returns the currently active table
func (m *Model) getCurrentTable() *table.Model {
	if m.activeTab == TabCertificates {
		return &m.sslTable
	}
	return &m.monitorTable
}

// getSelectedComponentIndex returns the actual index in m.config.Status.Components array
// accounting for sorting and filtering between monitor and SSL tables
func (m *Model) getSelectedComponentIndex() int {
	cursor := m.getCurrentTable().Cursor()

	if m.activeTab == TabMonitors {
		// Get monitors in sorted order, find the one at cursor position
		var monitors []status.ComponentStatus
		for _, comp := range m.components {
			if comp.Type != "ssl" {
				monitors = append(monitors, comp)
			}
		}
		sorted := m.sortedMonitors(monitors)
		if cursor < 0 || cursor >= len(sorted) {
			return -1
		}
		selected := sorted[cursor]
		// Find matching config component by name+type+target
		for i, c := range m.config.Status.Components {
			if c.Name == selected.Name && string(c.Type) == string(selected.Type) && c.Target == selected.Target {
				return i
			}
		}
	} else {
		// SSL table - no sorting applied, count through SSL monitors
		sslCount := 0
		for i, comp := range m.components {
			if comp.Type != "ssl" {
				continue
			}
			if sslCount == cursor {
				// Find matching config component
				for j, c := range m.config.Status.Components {
					if c.Name == comp.Name && string(c.Type) == string(comp.Type) && c.Target == comp.Target {
						return j
					}
				}
				return i
			}
			sslCount++
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
	case "ctrl+r", " ", "+", "-":
		return m.handleRefreshControls(msg)
	case "up", "down", "k", "j", "tab", "shift+tab", "g", "G", "home", "end":
		return m.handleTableNavigation(msg)
	case "ctrl+n", "e", "ctrl+d":
		return m.handleMonitorOperations(msg)
	case ".":
		return m.cycleSort()
	}

	return m, nil
}

// handleRefreshControls handles refresh, pause, and interval adjustments
func (m Model) handleRefreshControls(msg tea.KeyMsg) (tea.Model, tea.Cmd) {
	switch msg.String() {
	case "ctrl+r":
		// Refresh immédiat
		if !m.checking {
			m.checking = true
			return m, tea.Batch(
				m.spinner.Tick,
				checkComponents(m.config),
			)
		}

	case " ":
		// Toggle pause
		m.paused = !m.paused
		if !m.paused && !m.checking && time.Since(m.lastCheck) >= m.refreshInterval {
			m.checking = true
			return m, tea.Batch(
				m.spinner.Tick,
				checkComponents(m.config),
			)
		}

	case "+":
		// Augmenter l'intervalle
		m.refreshInterval += 5 * time.Second
		if m.refreshInterval > 300*time.Second {
			m.refreshInterval = 300 * time.Second
		}

	case "-":
		// Diminuer l'intervalle
		m.refreshInterval -= 5 * time.Second
		if m.refreshInterval < 5*time.Second {
			m.refreshInterval = 5 * time.Second
		}
	}

	return m, nil
}

// switchTab switches focus to the given tab
func (m *Model) switchTab(tab int) {
	m.activeTab = tab
	if tab == TabMonitors {
		m.monitorTable.Focus()
		m.monitorTable.SetStyles(theme.DefaultTableStyles())
		m.sslTable.Blur()
		m.sslTable.SetStyles(theme.BlurredTableStyles())
	} else {
		m.sslTable.Focus()
		m.sslTable.SetStyles(theme.DefaultTableStyles())
		m.monitorTable.Blur()
		m.monitorTable.SetStyles(theme.BlurredTableStyles())
	}
}

// handleTableNavigation handles up, down, and tab navigation
func (m Model) handleTableNavigation(msg tea.KeyMsg) (tea.Model, tea.Cmd) {
	switch msg.String() {
	case "up", "k":
		m.getCurrentTable().MoveUp(1)
	case "down", "j":
		m.getCurrentTable().MoveDown(1)
	case "g", "home":
		m.getCurrentTable().GotoTop()
	case "G", "end":
		m.getCurrentTable().GotoBottom()
	case "tab":
		m.switchTab((m.activeTab + 1) % 2)
	case "shift+tab":
		m.switchTab((m.activeTab + 1) % 2)
	}

	return m, nil
}

// handleComponentFormSubmit processes form submission for component creation/editing
func (m Model) handleComponentFormSubmit(msg components.ComponentFormSubmitMsg) (tea.Model, tea.Cmd) {
	m.componentForm = nil
	if msg.Original != nil {
		m.editing = false
		for i, c := range m.config.Status.Components {
			if c.Name == msg.Original.Name && c.Type == msg.Original.Type && c.Target == msg.Original.Target {
				m.config.Status.Components[i] = msg.Component
				break
			}
		}
	} else {
		m.creating = false
		m.config.Status.Components = append(m.config.Status.Components, msg.Component)
	}
	return m, saveComponent(m.config)
}

// handleConfirmDelete processes confirmed deletion of a component
func (m Model) handleConfirmDelete() (tea.Model, tea.Cmd) {
	m.confirmModal = nil
	m.confirming = false
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
		m.creating = true
		m.componentForm = components.NewComponentForm(nil)

	case "e":
		// Éditer le composant sélectionné
		if len(m.components) > 0 {
			idx := m.getSelectedComponentIndex()
			if idx >= 0 && idx < len(m.config.Status.Components) {
				m.editing = true
				m.selectedIdx = idx
				m.componentForm = components.NewComponentForm(&m.config.Status.Components[idx])
			}
		}

	case "ctrl+d":
		// Supprimer le composant sélectionné
		if len(m.components) > 0 {
			idx := m.getSelectedComponentIndex()
			if idx >= 0 && idx < len(m.config.Status.Components) {
				m.confirming = true
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

// sortableColumns lists columns in cycle order for the '.' key
var sortableColumns = []sortField{
	sortByName,
	sortByTarget,
	sortByType,
	sortByResponse,
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

// sortedMonitors returns monitor components sorted by the current sort column
func (m *Model) sortedMonitors(components []status.ComponentStatus) []status.ComponentStatus {
	sorted := make([]status.ComponentStatus, len(components))
	copy(sorted, components)

	sort.Slice(sorted, func(i, j int) bool {
		a, b := sorted[i], sorted[j]

		var less bool
		switch m.sortColumn {
		case sortByTarget:
			less = strings.ToLower(a.Target) < strings.ToLower(b.Target)
		case sortByType:
			less = strings.ToLower(string(a.Type)) < strings.ToLower(string(b.Type))
		case sortByResponse:
			less = a.ResponseTime < b.ResponseTime
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
