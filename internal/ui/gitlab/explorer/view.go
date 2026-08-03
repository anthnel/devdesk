package explorer

import (
	"fmt"
	"strings"
	"time"

	"github.com/anthnel/devdesk/internal/ui/help"
	"github.com/anthnel/devdesk/internal/ui/shortcut"
	"github.com/anthnel/devdesk/internal/ui/theme"
	"github.com/charmbracelet/lipgloss"
)

// Column width ratios for the table
const (
	colTypeRatio       = 0.08
	colNameRatio       = 0.22
	colSlugRatio       = 0.18
	colVisibilityRatio = 0.12
	colRoleRatio       = 0.10
	colCreatedRatio    = 0.12
	colActivityRatio   = 0.12
)

// numColumns is the number of columns in the explorer table
const numColumns = 8

// View rend la vue
func (m Model) View() string {
	// Priority 1: Forms (full viewport replacement - Rule 112)
	if m.creationForm != nil {
		return m.creationForm.View()
	}

	// Priority 2: Modals (centered overlays)
	switch m.mode {
	case ModePulling:
		return lipgloss.Place(
			m.width, m.height,
			lipgloss.Center, lipgloss.Center,
			m.renderPullingStatus(),
			lipgloss.WithWhitespaceBackground(theme.ColorBackground),
		)
	case ModeLoadingTemplates:
		return lipgloss.Place(
			m.width, m.height,
			lipgloss.Center, lipgloss.Center,
			m.renderLoadingTemplates(),
			lipgloss.WithWhitespaceBackground(theme.ColorBackground),
		)
	case ModeShowingReport:
		if m.reportModal != nil {
			return lipgloss.Place(
				m.width, m.height,
				lipgloss.Center, lipgloss.Center,
				m.reportModal.View(),
				lipgloss.WithWhitespaceBackground(theme.ColorBackground),
			)
		}
	case ModeConfirmingDelete:
		if m.deleteConfirmModal != nil {
			return lipgloss.Place(
				m.width, m.height,
				lipgloss.Center, lipgloss.Center,
				m.deleteConfirmModal.View(),
				lipgloss.WithWhitespaceBackground(theme.ColorBackground),
			)
		}
	}

	// Content style with left padding
	contentStyle := lipgloss.NewStyle().Background(theme.ColorBackground).PaddingLeft(1)

	// Normal view
	if m.shared.GitLabClient == nil {
		return contentStyle.Render(renderNotAuthenticated())
	}

	if m.loading || !m.firstLoadDone {
		loadingStyle := lipgloss.NewStyle().Background(theme.ColorBackground).Padding(1)
		return loadingStyle.Render(theme.SpinnerMessage(m.spinner.View(), "Loading GitLab groups..."))
	}

	if m.error != "" {
		return contentStyle.Render(renderError(m.error))
	}

	if len(m.nodes) == 0 {
		return contentStyle.Render(renderEmpty())
	}

	return m.renderTable()
}

// renderPullingStatus renders the pulling progress indicator
func (m Model) renderPullingStatus() string {
	var b strings.Builder

	b.WriteString(theme.TitleStyle.Render("Pulling..."))
	b.WriteString("\n\n")

	spinnerStyle := lipgloss.NewStyle().Background(theme.ColorBackground).Foreground(theme.ColorPrimary)
	b.WriteString(spinnerStyle.Render(theme.IconHourglass + " Cloning repositories..."))
	b.WriteString("\n\n")

	if m.pullTargetNode != nil {
		targetInfo := theme.DimStyle.Render(fmt.Sprintf("Target: %s", m.pullTargetNode.FullPath))
		b.WriteString(targetInfo)
		b.WriteString("\n\n")
	}

	b.WriteString(theme.HelpStyle.Render("Please wait..."))

	return lipgloss.NewStyle().
		Background(theme.ColorBackground).
		Border(lipgloss.NormalBorder()).
		BorderForeground(theme.ColorPrimary).
		BorderBackground(theme.ColorBackground).
		Padding(1, 2).
		Width(50).
		Render(b.String())
}

// renderLoadingTemplates renders the template loading indicator
func (m Model) renderLoadingTemplates() string {
	var b strings.Builder

	b.WriteString(theme.TitleStyle.Render("Loading templates..."))
	b.WriteString("\n\n")

	spinnerStyle := lipgloss.NewStyle().Background(theme.ColorBackground).Foreground(theme.ColorPrimary)
	b.WriteString(spinnerStyle.Render(theme.IconHourglass + " Fetching templates from registry..."))
	b.WriteString("\n\n")
	b.WriteString(theme.HelpStyle.Render("Please wait..."))

	return lipgloss.NewStyle().
		Background(theme.ColorBackground).
		Border(lipgloss.NormalBorder()).
		BorderForeground(theme.ColorPrimary).
		BorderBackground(theme.ColorBackground).
		Padding(1, 2).
		Width(50).
		Render(b.String())
}

// renderTable renders the table content
func (m Model) renderTable() string {
	return m.table.View()
}

// GetFooterHeight returns the footer height for this view (Rule 124).
func (m Model) GetFooterHeight() int {
	if m.shared.GitLabClient != nil && !m.loading && m.error == "" && len(m.nodes) > 0 {
		switch m.mode {
		case ModePulling, ModeLoadingTemplates, ModeCreatingProject, ModeConfirmingDelete, ModeShowingReport:
			// fall through to empty line + info line
		default:
			if m.creationForm != nil {
				break // fall through to empty line + info line
			}
			return 3 + m.filterBar.ExtraHeight() // filter bar (when visible) + breadcrumb tab bar + empty line + info line
		}
	}
	return 2 // empty line + info line
}

// RenderFooter returns the footer content rendered below the viewport (Rule 124).
func (m Model) RenderFooter(width int) string {
	infoLine := theme.EmptyLineBg(width)
	if m.footerError != "" {
		infoLine = theme.BgLine(theme.StatusErrorStyle.Render(m.footerError), width)
	}

	if m.shared.GitLabClient != nil && !m.loading && m.error == "" && len(m.nodes) > 0 {
		switch m.mode {
		case ModePulling, ModeLoadingTemplates, ModeCreatingProject, ModeConfirmingDelete, ModeShowingReport:
			// fall through to empty line + info line
		default:
			if m.creationForm != nil {
				break // fall through to empty line + info line
			}
			var parts []string
			if m.filterBar.IsVisible() {
				parts = append(parts, m.filterBar.View())
			}
			parts = append(parts, m.renderTabBar(), theme.EmptyLineBg(width), infoLine)
			return strings.Join(parts, "\n")
		}
	}
	return theme.EmptyLineBg(width) + "\n" + infoLine
}

// renderTabBar renders the tab bar at the bottom of the viewport
func (m Model) renderTabBar() string {
	tabs := []theme.TabItem{{Label: theme.IconHome + " home"}}
	for _, node := range m.navigationStack {
		if node != nil {
			tabs = append(tabs, theme.TabItem{Label: node.Name})
		}
	}
	if m.currentGroupNode != nil {
		tabs = append(tabs, theme.TabItem{Label: m.currentGroupNode.Name})
	}
	return theme.PadWithBg(theme.Bg(" ")+theme.RenderTabs(tabs, m.activeTabIndex), m.width)
}

// nodeTypeLabel returns the type label for a tree node
func nodeTypeLabel(node *TreeNode) string {
	if node.Type == NodeTypeGroup {
		return "Group"
	}
	return "Project"
}

// visibilityLabel returns the visibility label for a tree node
func visibilityLabel(node *TreeNode) string {
	switch node.Visibility {
	case "public":
		return "Public"
	case "internal":
		return "Internal"
	case "private":
		return "Private"
	default:
		return ""
	}
}

// pipelineStatusLabel returns a status icon for the last CI pipeline
func pipelineStatusLabel(node *TreeNode) string {
	if node.Type != NodeTypeProject || node.PipelineStatus == "" {
		return ""
	}
	switch node.PipelineStatus {
	case "success":
		return theme.IconOK
	case "failed":
		return theme.IconError
	case "running":
		return theme.IconRunning
	case "pending", "waiting_for_resource", "preparing":
		return theme.IconPending
	case "canceled":
		return theme.IconCanceled
	case "skipped":
		return theme.IconSkipped
	case "manual":
		return theme.IconManual
	default:
		return node.PipelineStatus
	}
}

// Helper rendering functions

func renderNotAuthenticated() string {
	style := lipgloss.NewStyle().Background(theme.ColorBackground).Foreground(theme.ColorError)
	return style.Render(theme.IconWarning + " GitLab not authenticated\n\nPlease authenticate first with :gitlab-auth (or :gla)")
}

func renderError(err string) string {
	style := lipgloss.NewStyle().Background(theme.ColorBackground).Foreground(theme.ColorError)
	return style.Render(fmt.Sprintf("%s Error: %s", theme.IconError, err))
}

func renderEmpty() string {
	return theme.HelpStyle.Render("No groups found\n\nYou may not have access to any GitLab groups.")
}

// timeAgo formats a time pointer as a compact relative string (Rule 127).
func timeAgo(t *time.Time) string {
	if t == nil {
		return ""
	}
	return theme.TimeAgo(*t)
}

// HeaderView interface implementation

func (m Model) GetShortcuts() shortcut.Shortcuts {
	// Mode-specific shortcuts
	switch m.mode {
	case ModePulling, ModeLoadingTemplates:
		return []shortcut.Shortcut{}
	case ModeShowingReport:
		return []shortcut.Shortcut{
			{Key: "tab", Description: "Switch tab"},
			{Key: "enter/esc", Description: "Close"},
		}
	case ModeCreatingProject:
		return []shortcut.Shortcut{
			{Key: "↑↓", Description: "Navigate fields"},
			{Key: "←→", Description: "Select option"},
			{Key: "enter", Description: "Submit"},
			{Key: "esc", Description: "Cancel"},
		}
	case ModeConfirmingDelete:
		// ↑↓ is Rule 138 territory — obvious, so not advertised. It replaced
		// tab here when D4 was fixed.
		return []shortcut.Shortcut{
			{Key: "space", Description: "Toggle"},
			{Key: "y/n", Description: "Confirm"},
			{Key: "esc", Description: "Cancel"},
		}
	}

	// Normal mode
	if m.loading {
		return []shortcut.Shortcut{}
	}

	if m.shared.GitLabClient == nil {
		return []shortcut.Shortcut{
			{Key: "alt+:", Description: "Command mode"},
		}
	}

	shortcuts := []shortcut.Shortcut{
		{Key: "←→", Description: "Open/Back"},
		{Key: "ctrl+n", Description: "New"},
		{Key: "ctrl+d", Description: "Delete"},
		{Key: "p", Description: "Pull"},
	}

	// ctrl+w: only show when a node is selected (all nodes have a WebURL from GitLab API)
	items := m.visibleItems()
	cursor := m.table.Cursor()
	if cursor >= 0 && cursor < len(items) && items[cursor].WebURL != "" {
		shortcuts = append(shortcuts, shortcut.Shortcut{Key: "ctrl+w", Description: "Browser"})
	}

	return append(shortcuts,
		shortcut.Shortcut{Key: ".", Description: "Sort"},
		shortcut.Shortcut{Key: "/", Description: "Search"},
		shortcut.Shortcut{Key: "ctrl+r", Description: "Refresh"},
		shortcut.Shortcut{Key: "alt+:", Description: "Command"},
		shortcut.Shortcut{Key: "?", Description: "Help"},
	)
}

func (m Model) GetTitle() string {
	base := theme.IconGitlab + " GitLab Explorer"
	if m.creationForm != nil {
		return base + " " + theme.IconChevronRight + " " + m.creationForm.GetTitle()
	}
	return base
}

func (m Model) GetIcon() string {
	return ""
}

// GetHeaderInfo returns the key-value info for the header
func (m Model) GetHeaderInfo(context string) []shortcut.HeaderInfo {
	info := []shortcut.HeaderInfo{
		{Key: "Context", Value: context, Style: theme.HeaderValueStyle},
	}
	if m.shared.CurrentUser != nil {
		info = append(info, shortcut.HeaderInfo{Key: "User", Value: "@" + m.shared.CurrentUser.Username, Style: theme.HeaderValueStyle})
	}
	return info
}

// GetHelpContent retourne le contenu d'aide de la vue GitLab Explorer
func (m Model) GetHelpContent() help.Content {
	return help.Content{
		Title:       "GitLab Explorer",
		Description: "A drill-down explorer for browsing GitLab groups and projects. Navigate into groups with → and go back with ←. Tabs at the bottom show your current path.",
		KeyBindings: []help.KeyBinding{
			{Key: "↑↓ / jk", Description: "Navigate the list"},
			{Key: "→ / l", Description: "Drill into selected group"},
			{Key: "← / h", Description: "Go back to parent group"},
			{Key: "Esc", Description: "Go back to parent group"},
			{Key: "p", Description: "Pull/clone the selected project or group into a workspace"},
			{Key: "Ctrl+W", Description: "Open the selected group or project in the default web browser"},
			{Key: "Ctrl+N", Description: "Create a new group or project under the current context. Use ←→ to select the type."},
			{Key: "Ctrl+D", Description: "Delete the selected group or project"},
			{Key: ".", Description: "Cycle sort column (Type → Name → Visibility → Created → Activity)"},
			{Key: "/", Description: "Filter the current level by name or path"},
			{Key: "Ctrl+R", Description: "Refresh the explorer"},
			{Key: "alt+:", Description: "Open command mode"},
			{Key: "?", Description: "Show this help"},
		},
		Sections: []help.Section{
			{
				Title: "Navigation",
				Body:  "The explorer uses a drill-down model. Press → on a group to see its contents. Press ← to go back. Tabs at the bottom show your current path.",
			},
			{
				Title: "Pull / Clone",
				Body:  "Select a project or group and press 'p'. Choose a destination directory in the workspace selector. If you select a group, all subgroups and projects will be cloned recursively, preserving the folder structure.",
			},
			{
				Title: "Creating Groups and Projects",
				Body:  "Press Ctrl+N to open the creation form. Use ←→ on the Type field to switch between Group and Project. The form uses the current group as parent. Project templates are loaded automatically from the OCI registry if configured.",
			},
			{
				Title: "CI Column Legend",
				Body: "The CI column shows the status of the last pipeline for each project:\n" +
					"  " + theme.IconOK + "  success — pipeline passed\n" +
					"  " + theme.IconError + "  failed — pipeline failed\n" +
					"  " + theme.IconRunning + "  running — pipeline in progress\n" +
					"  " + theme.IconPending + "  pending — waiting to run\n" +
					"  " + theme.IconManual + "  manual — awaiting manual trigger\n" +
					"  " + theme.IconCanceled + "  canceled — pipeline was canceled\n" +
					"  " + theme.IconSkipped + "  skipped — pipeline was skipped\n" +
					"Groups show no CI status.",
			},
			{
				Title: "Prerequisites",
				Body:  "You must be authenticated (via :gitlab-auth) to access the explorer. The clone method (SSH or HTTPS) is configurable in the context configuration.",
			},
		},
	}
}
