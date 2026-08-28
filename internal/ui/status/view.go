package status

import (
	"fmt"
	"strings"

	"github.com/charmbracelet/lipgloss"

	"github.com/anthnel/devdesk/internal/status"
	sharedcomponents "github.com/anthnel/devdesk/internal/ui/components"
	"github.com/anthnel/devdesk/internal/ui/help"
	"github.com/anthnel/devdesk/internal/ui/shortcut"
	"github.com/anthnel/devdesk/internal/ui/theme"

	"github.com/anthnel/devdesk/internal/ui/keymap"
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
		{Key: keymap.New, Description: "New monitor"},
		{Key: keymap.Edit, Description: "Edit monitor"},
		{Key: keymap.Delete, Description: "Delete monitor"},
		{Key: "ctrl+r", Description: "Refresh"},
		{Key: "tab", Description: "Switch tab"},
		{Key: ".", Description: "Sort"},
		{Key: "/", Description: "Search"},
		{Key: "ctrl+p", Description: "Command mode"},
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

// renderEmpty is what shows when there is nothing to list.
//
// The check says so in the footer, with a spinner, and the body stays empty:
// the load belongs to one line, and the invitation to add a monitor would
// otherwise be replaced by it on every refresh.
func (m Model) renderEmpty() string {
	if m.checking {
		return ""
	}
	return lipgloss.NewStyle().Background(theme.ColorBackground).Padding(1).Render(
		theme.DimStyle.Render("No components configured. Press [ctrl+n] to add a monitor."),
	)
}

// status is what the view derives on every frame. The check has no timer: it
// lasts exactly as long as it lasts.
func (m Model) status() sharedcomponents.Status {
	if m.checking {
		return sharedcomponents.Status{Text: "Checking components...", Spinner: true}
	}
	return sharedcomponents.Status{}
}

// renderTabs renders the tab bar for monitors/certificates
func (m Model) renderTabs() string {
	monitorCount := len(m.monitorTable.Visible())
	sslCount := len(m.sslTable.Visible())

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
			m.footer.View(width, m.status()),
		)
		return strings.Join(parts, "\n")
	}
	return theme.EmptyLineBg(width) + "\n" + m.footer.View(width, m.status())
}

// updateTable refills both tables from the last check.
//
// The text query stays here rather than moving into either table's own filter
// bar: one bar drives two tables, so that the header counts agree with each
// other whichever tab is showing. The component models a bar per table, which
// is the right default and the wrong shape for this one view.
func (m *Model) updateTable() {
	query := strings.ToLower(strings.TrimSpace(m.filterBar.SearchQuery()))

	var monitors, ssls []status.ComponentStatus
	for _, comp := range m.components {
		if !matchesQuery(comp, query) {
			continue
		}
		if comp.Type == "ssl" {
			ssls = append(ssls, comp)
		} else {
			monitors = append(monitors, comp)
		}
	}

	m.monitorTable.SetItems(monitors)
	m.sslTable.SetItems(ssls)
	m.applyTabFocus()
}

// GetHelpContent retourne le contenu d'aide de la vue Status
func (m Model) GetHelpContent() help.Content {
	return help.Content{
		Title:       "Status Monitor",
		Description: "This view monitors the status of your services in real time. Checks are performed automatically at regular intervals. Two tabs display service monitors (HTTP, ICMP, DNS) and SSL certificates respectively.",
		KeyBindings: []help.KeyBinding{
			{Key: keymap.New, Description: "Add a new monitor"},
			{Key: keymap.Edit, Description: "Edit the selected monitor"},
			{Key: keymap.Delete, Description: "Delete the selected monitor (with confirmation)"},
			{Key: "ctrl+r", Description: "Force an immediate refresh"},
			{Key: "tab/shift+tab", Description: "Switch between Service Monitors and SSL Certificates tabs"},
			{Key: "↑/k", Description: "Move selection up"},
			{Key: "↓/j", Description: "Move selection down"},
			{Key: ".", Description: "Cycle sort column and direction (Name, Target, Type, Response). Each column cycles ascending then descending before moving to the next. The active sort is shown with ▲ or ▼ in the column header."},
			{Key: "/", Description: "Filter the active table by name, target or type"},
			{Key: "ctrl+p", Description: "Open command mode"},
			{Key: "?", Description: "Show this help"},
		},
		Sections: []help.Section{
			{
				Title: "Monitor Types",
				Body:  "HTTP/HTTPS: checks URL availability and measures response time.\nICMP (Ping): checks network connectivity to a host.\nDNS: checks domain name resolution.\nSSL: checks the validity and expiration of an SSL/TLS certificate.",
			},
			{
				Title: "Configuration",
				Body:  "Monitors are saved in the active context configuration file (~/.devdesk/contexts/<context>/config.yaml). The refresh interval and auto-refresh are settings, edited in the configuration view (:config, Status tab) — not here.",
			},
		},
	}
}

// formatSSLStatus names a certificate's state, and it does not go through
// StatusType.
//
// Elle le faisait, et c'est ce qui a valu D64 à la boîte Health : SSLChecker
// rend `ERROR` pour un certificat périmé comme pour un qui expire dans six
// jours, donc l'icône était la même pour les deux. La colonne `Days Left`
// d'à côté séparait ce que celle-ci collait — mais elle demandait de lire deux
// cellules pour un fait qui en tient dans une.
//
// Le texte reste brut : la couleur est le `Style` de la colonne (Rule 122).
func formatSSLStatus(comp status.ComponentStatus) string {
	return theme.CertStateIcon(string(status.CertStateOf(comp)))
}

// matchesQuery reports whether a component survives the text filter. Name,
// target and type, which is what the two loops this replaced matched between
// them — the SSL one left type out, and a `type:` query narrowing one table and
// not the other was never deliberate.
func matchesQuery(comp status.ComponentStatus, query string) bool {
	if query == "" {
		return true
	}
	return strings.Contains(strings.ToLower(comp.Name), query) ||
		strings.Contains(strings.ToLower(comp.Target), query) ||
		strings.Contains(strings.ToLower(string(comp.Type)), query)
}

// applyTabFocus gives the keyboard to the active tab's table and blurs the
// other. Focus and Blur carry the styles with them (Rule 118).
func (m *Model) applyTabFocus() {
	if m.activeTab == TabMonitors {
		m.monitorTable.Focus()
		m.sslTable.Blur()
		return
	}
	m.sslTable.Focus()
	m.monitorTable.Blur()
}
