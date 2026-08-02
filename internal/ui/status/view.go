package status

import (
	"fmt"
	"strings"

	"github.com/charmbracelet/bubbles/table"
	"github.com/charmbracelet/lipgloss"

	"github.com/anthnel/devdesk/internal/status"
	"github.com/anthnel/devdesk/internal/ui/help"
	"github.com/anthnel/devdesk/internal/ui/shortcut"
	"github.com/anthnel/devdesk/internal/ui/theme"
)

func (m Model) InEditMode() bool {
	return m.componentForm != nil || m.filterBar.InEditMode()
}

// FilterBarVisible returns true when the filter bar is visible (implements app.FilterBarView).
func (m Model) FilterBarVisible() bool {
	return m.filterBar.IsVisible() && m.componentForm == nil && m.confirmModal == nil && m.error == "" && len(m.components) > 0
}

func (m Model) GetShortcuts() shortcut.Shortcuts {
	// vue edition/ajout composant
	if m.componentForm != nil {
		return []shortcut.Shortcut{
			{Key: "esc", Description: "Cancel"},
			{Key: "enter", Description: "Submit"},
		}
	}

	// vue principale status
	return []shortcut.Shortcut{
		{Key: "ctrl+n", Description: "New monitor"},
		{Key: "e", Description: "Edit monitor"},
		{Key: "ctrl+d", Description: "Delete monitor"},
		{Key: "ctrl+r", Description: "Refresh"},
		{Key: "±", Description: "Adjust interval"},
		{Key: "space", Description: "Pause/Resume"},
		{Key: "tab", Description: "Switch tab"},
		{Key: ".", Description: "Sort"},
		{Key: "/", Description: "Search"},
		{Key: ":", Description: "Command mode"},
		{Key: "?", Description: "Help"},
	}
}

func (m Model) GetTitle() string {
	base := theme.IconService + " Status Monitor"
	if m.componentForm != nil {
		return base + " " + theme.IconChevronRight + " " + m.componentForm.GetTitle()
	}
	return base
}

func (m Model) GetIcon() string {
	return ""
}

// GetHeaderInfo returns the key-value info for the header
func (m Model) GetHeaderInfo(context string) []shortcut.HeaderInfo {
	return []shortcut.HeaderInfo{
		{Key: "Context", Value: context, Style: theme.HeaderValueStyle},
		{Key: "Refresh", Value: fmt.Sprintf("%ds", int(m.refreshInterval.Seconds())), Style: theme.HeaderValueStyle},
	}
}

// View rend la vue
func (m Model) View() string {
	// Mode formulaire - overlay
	if m.componentForm != nil {
		return m.componentForm.View()
	}

	// Mode confirmation - overlay
	if m.confirmModal != nil {
		return lipgloss.Place(
			m.width,
			m.height,
			lipgloss.Center,
			lipgloss.Center,
			m.confirmModal.View(),
			lipgloss.WithWhitespaceBackground(theme.ColorBackground),
		)
	}

	// Content - HAUTEUR DYNAMIQUE qui remplit exactement l'espace disponible
	var innerContent string
	if m.error != "" {
		innerContent = m.renderError()
	} else if len(m.components) == 0 && !m.firstCheck {
		innerContent = m.renderEmpty()
	} else {
		innerContent = m.renderTable()
	}

	return innerContent
}

func (m Model) renderError() string {
	msg := fmt.Sprintf("✗ Error: %s", m.error)
	return lipgloss.NewStyle().
		Background(theme.ColorBackground).
		Padding(2, 4).
		Foreground(theme.ColorError).
		Render(msg)
}

func (m Model) renderEmpty() string {
	var content string
	if m.checking {
		content = theme.SpinnerMessage(m.spinner.View(), "Checking components...")
	} else {
		content = theme.DimStyle.Render("No components configured. Press [ctrl+n] to add a monitor.")
	}
	return lipgloss.NewStyle().Background(theme.ColorBackground).Padding(1).Render(content)
}

// renderTabs renders the tab bar for monitors/certificates
func (m Model) renderTabs() string {
	monitorCount := len(m.monitorTable.Rows())
	sslCount := len(m.sslTable.Rows())

	return theme.RenderTabs([]theme.TabItem{
		{Label: fmt.Sprintf("Service Monitors (%d)", monitorCount)},
		{Label: fmt.Sprintf("SSL Certificates (%d)", sslCount)},
	}, m.activeTab)
}

func (m Model) renderTable() string {
	if m.activeTab == TabMonitors {
		return m.monitorTable.View()
	}
	return m.sslTable.View()
}

// GetFooterHeight returns the footer height for this view (Rule 124).
func (m Model) GetFooterHeight() int {
	if m.componentForm == nil && m.confirmModal == nil && m.error == "" && len(m.components) > 0 {
		return 3 + m.filterBar.ExtraHeight() // filter bar (when visible) + tab bar + empty line + info line
	}
	return 2 // empty line + info line
}

// RenderFooter returns the footer content rendered below the viewport (Rule 124).
func (m Model) RenderFooter(width int) string {
	if m.componentForm == nil && m.confirmModal == nil && m.error == "" && len(m.components) > 0 {
		var parts []string
		if m.filterBar.IsVisible() {
			parts = append(parts, m.filterBar.View())
		}
		parts = append(parts,
			theme.PadWithBg(theme.Bg(" ")+m.renderTabs(), width),
			theme.EmptyLineBg(width),
			theme.EmptyLineBg(width),
		)
		return strings.Join(parts, "\n")
	}
	return theme.EmptyLineBg(width) + "\n" + theme.EmptyLineBg(width)
}

// updateTable met à jour les données des deux tables
func (m *Model) updateTable() {
	monitorRows := []table.Row{}
	sslRows := []table.Row{}

	query := strings.ToLower(m.filterBar.SearchQuery())

	// Séparer les composants par type
	var monitors, ssls []status.ComponentStatus
	for _, comp := range m.components {
		if comp.Type == "ssl" {
			ssls = append(ssls, comp)
		} else {
			monitors = append(monitors, comp)
		}
	}

	// Trier les monitors
	monitors = m.sortedMonitors(monitors)

	for _, comp := range monitors {
		// Apply text filter
		if query != "" {
			nameMatch := strings.Contains(strings.ToLower(comp.Name), query)
			targetMatch := strings.Contains(strings.ToLower(comp.Target), query)
			typeMatch := strings.Contains(strings.ToLower(string(comp.Type)), query)
			if !nameMatch && !targetMatch && !typeMatch {
				continue
			}
		}
		typeStr := string(comp.Type)
		if typeStr == "" {
			typeStr = "unknown"
		}

		var statusStr string
		switch comp.Status {
		case "OK":
			statusStr = theme.IconOK
		case "DOWN":
			statusStr = theme.IconError
		case "ERROR":
			statusStr = theme.IconWarning
		default:
			statusStr = string(comp.Status)
		}

		responseStr := "-"
		if comp.ResponseTime > 0 {
			responseStr = fmt.Sprintf("%dms", comp.ResponseTime.Milliseconds())
		}

		monitorRows = append(monitorRows, table.Row{
			comp.Name,
			comp.Target,
			statusStr,
			typeStr,
			responseStr,
		})
	}

	for _, comp := range ssls {
		// Apply text filter
		if query != "" {
			nameMatch := strings.Contains(strings.ToLower(comp.Name), query)
			targetMatch := strings.Contains(strings.ToLower(comp.Target), query)
			if !nameMatch && !targetMatch {
				continue
			}
		}

		statusStr := formatSSLStatus(comp)
		daysLeftStr := "-"
		expiresStr := "-"
		issuerStr := "-"

		if comp.SSLDaysLeft != nil {
			daysLeftStr = fmt.Sprintf("%d", *comp.SSLDaysLeft)
		}
		if comp.SSLExpires != nil {
			expiresStr = comp.SSLExpires.Format("2006-01-02 15:04")
		}
		if comp.SSLIssuer != "" {
			issuerStr = comp.SSLIssuer
		}

		sslRows = append(sslRows, table.Row{
			comp.Name,
			comp.Target,
			statusStr,
			daysLeftStr,
			expiresStr,
			issuerStr,
		})
	}

	m.monitorTable.SetRows(monitorRows)
	m.sslTable.SetRows(sslRows)

	// Update column headers with sort indicators
	monCols := m.monitorTable.Columns()
	if len(monCols) >= 5 {
		sortColIndex := map[sortField]int{
			sortByName:     0,
			sortByTarget:   1,
			sortByType:     3,
			sortByResponse: 4,
		}
		baseTitles := map[int]string{
			0: "Name", 1: "Target", 3: "Type", 4: "Response",
		}
		for idx, title := range baseTitles {
			monCols[idx].Title = title
		}
		if idx, ok := sortColIndex[m.sortColumn]; ok {
			arrow := " ▲"
			if !m.sortAsc {
				arrow = " ▼"
			}
			monCols[idx].Title = baseTitles[idx] + arrow
		}
		monCols[2].Title = "Status"
		m.monitorTable.SetColumns(monCols)
	}

	// Appliquer les styles et le focus selon le tab actif
	if m.activeTab == TabMonitors {
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

// GetHelpContent retourne le contenu d'aide de la vue Status
func (m Model) GetHelpContent() help.Content {
	return help.Content{
		Title:       "Status Monitor",
		Description: "This view monitors the status of your services in real time. Checks are performed automatically at regular intervals. Two tabs display service monitors (HTTP, ICMP, DNS) and SSL certificates respectively.",
		KeyBindings: []help.KeyBinding{
			{Key: "ctrl+n", Description: "Add a new monitor"},
			{Key: "e", Description: "Edit the selected monitor"},
			{Key: "ctrl+d", Description: "Delete the selected monitor (with confirmation)"},
			{Key: "ctrl+r", Description: "Force an immediate refresh"},
			{Key: "space", Description: "Pause / resume automatic refresh"},
			{Key: "+/-", Description: "Increase / decrease refresh interval"},
			{Key: "tab/shift+tab", Description: "Switch between Service Monitors and SSL Certificates tabs"},
			{Key: "↑/k", Description: "Move selection up"},
			{Key: "↓/j", Description: "Move selection down"},
			{Key: "g/Home", Description: "Go to top of list"},
			{Key: "G/End", Description: "Go to bottom of list"},
			{Key: ".", Description: "Cycle sort column and direction (Name, Target, Type, Response). Each column cycles ascending then descending before moving to the next. The active sort is shown with ▲ or ▼ in the column header."},
			{Key: "/", Description: "Filter the active table by name, target or type"},
			{Key: ":", Description: "Open command mode"},
			{Key: "?", Description: "Show this help"},
		},
		Sections: []help.Section{
			{
				Title: "Monitor Types",
				Body:  "HTTP/HTTPS: checks URL availability and measures response time.\nICMP (Ping): checks network connectivity to a host.\nDNS: checks domain name resolution.\nSSL: checks the validity and expiration of an SSL/TLS certificate.",
			},
			{
				Title: "Configuration",
				Body:  "Monitors are saved in the active context configuration file (~/.devdesk/contexts/<context>/config.yaml). Refresh interval and auto-refresh are configurable.",
			},
		},
	}
}

// formatSSLStatus formate le statut pour les certificats SSL
func formatSSLStatus(comp status.ComponentStatus) string {
	switch comp.Status {
	case "OK":
		// return theme.StatusOKStyle.Render("✓ OK")
		return theme.IconOK
	case "WARNING":
		// warningStyle := lipgloss.NewStyle().Foreground(theme.ColorWarn).Bold(true)
		return theme.IconWarning
	case "ERROR":
		// return theme.StatusErrorStyle.Render("✗ ERROR")
		return theme.IconError
	case "DOWN":
		// return theme.StatusDownStyle.Render("✗ DOWN")
		return theme.IconError
	default:
		return string(comp.Status)
	}
}
