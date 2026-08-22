package explorer

import (
	"fmt"
	"strings"
	"time"

	"github.com/anthnel/devdesk/internal/command"
	"github.com/anthnel/devdesk/internal/forge"
	"github.com/anthnel/devdesk/internal/ui/components"
	"github.com/anthnel/devdesk/internal/ui/help"
	"github.com/anthnel/devdesk/internal/ui/shortcut"
	"github.com/anthnel/devdesk/internal/ui/theme"
	"github.com/charmbracelet/lipgloss"
)

// View rend la vue
func (m Model) View() string {
	// Priority 1: Forms (full viewport replacement - Rule 112)
	if m.creationForm != nil {
		return m.creationForm.View()
	}

	// Priority 2: the clone list, which is a full viewport of its own — the
	// progress view and the report both (decision 9).
	if m.mode == ModeCloning && m.clone != nil {
		return m.clone.table.View()
	}

	// Priority 3: Modals (centered overlays)
	switch m.mode {
	case ModeLoadingTemplates:
		return lipgloss.Place(
			m.width, m.height,
			lipgloss.Center, lipgloss.Center,
			m.renderLoadingTemplates(),
			lipgloss.WithWhitespaceBackground(theme.ColorBackground),
		)
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
	if !m.shared.IsAuthenticated {
		return contentStyle.Render(renderNotAuthenticated(m.vocab()))
	}

	if m.error != "" {
		return contentStyle.Render(renderError(m.error))
	}

	// The load says so in the footer, with a spinner, and the tree stays on
	// screen. "Nothing here" is therefore conditional on the load being over,
	// or the view would announce the absence of what it is still fetching.
	if len(m.nodes) == 0 {
		if m.loadingTree() {
			return ""
		}
		return contentStyle.Render(renderEmpty(m.vocab()))
	}

	return m.renderTable()
}

// loadingTree reports whether the group tree is still being fetched.
func (m Model) loadingTree() bool { return m.loading || !m.firstLoadDone }

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
//
// The clone list has no breadcrumb — it is a flat list, and the tree's path
// says nothing about it — so it takes the two-line footer plus its own filter
// bar.
func (m Model) GetFooterHeight() int {
	if m.mode == ModeCloning && m.clone != nil {
		return 2 + m.clone.table.FilterBar().ExtraHeight()
	}
	if m.showsTree() {
		// filter bar (when visible) + breadcrumb tab bar + empty line + info line
		return 3 + m.table.FilterBar().ExtraHeight()
	}
	return 2 // empty line + info line
}

// RenderFooter returns the footer content rendered below the viewport (Rule 124).
func (m Model) RenderFooter(width int) string {
	infoLine := m.renderInfoLine(width)

	if m.mode == ModeCloning && m.clone != nil {
		var parts []string
		if bar := m.clone.table.FilterBar(); bar.IsVisible() {
			parts = append(parts, bar.View())
		}
		return strings.Join(append(parts, theme.EmptyLineBg(width), infoLine), "\n")
	}

	if m.showsTree() {
		var parts []string
		if bar := m.table.FilterBar(); bar.IsVisible() {
			parts = append(parts, bar.View())
		}
		parts = append(parts, m.renderTabBar(), theme.EmptyLineBg(width), infoLine)
		return strings.Join(parts, "\n")
	}
	return theme.EmptyLineBg(width) + "\n" + infoLine
}

// renderInfoLine is the footer's one line of text (Rule 128): a message when
// there is one, otherwise whatever the current mode has to say.
func (m Model) renderInfoLine(width int) string {
	return m.footer.View(width, m.status())
}

// status is what the view derives on every frame. None of the three has a
// timer: a clone run outlives the three seconds a message gets, and the load
// and the selection last exactly as long as they last.
func (m Model) status() components.Status {
	switch {
	case m.mode == ModeCloning && m.clone != nil:
		return components.Status{Text: m.cloneStatusLine()}
	case m.mode == ModeSelecting:
		return components.Status{Text: m.selectionStatusLine()}
	case m.shared.IsAuthenticated && m.loadingTree():
		return components.Status{Text: "Loading " + m.vocab().Name + " " + strings.ToLower(m.vocab().Namespaces) + "...", Spinner: true}
	}
	return components.Status{}
}

// selectionStatusLine states the selection in the only terms it can: the
// repository count is not known until discovery has run (decision 7), so what
// is shown is what was actually chosen.
func (m Model) selectionStatusLine() string {
	if m.selection.isEmpty() {
		return "Select groups and projects with space, then press enter"
	}
	roots, exclusions := m.selection.counts()
	line := plural(roots, "selection")
	if exclusions > 0 {
		line += " · " + plural(exclusions, "exclusion")
	}
	return line + " — enter to choose a destination"
}

// showsTree reports whether the tree table is what is on screen, which is what
// decides whether the footer carries a breadcrumb and a filter bar.
func (m Model) showsTree() bool {
	if !m.shared.IsAuthenticated || m.loading || m.error != "" || len(m.nodes) == 0 {
		return false
	}
	if m.creationForm != nil {
		return false
	}
	switch m.mode {
	case ModeNormal, ModeSelecting:
		return true
	default:
		return false
	}
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

// nodeTypeLabel returns the type label for a tree node, in the forge's own
// words: a GitLab user reads Group/Project, a GitHub user Organization/
// Repository.
func nodeTypeLabel(v forge.Vocabulary, node *TreeNode) string {
	if node.Type == NodeTypeGroup {
		return v.Namespace
	}
	return v.Repository
}

// vocab is the wording of the forge this context targets. Resolved from the
// config: the "not authenticated" screen needs the words before a session
// exists, so a value hanging off a live Forge would be unavailable exactly
// where it is needed most.
func (m Model) vocab() forge.Vocabulary {
	if m.config == nil {
		return forge.VocabularyFor("")
	}
	return forge.VocabularyFor(m.config.Forge.Type)
}

// forgeIcon is the glyph naming the active forge.
func (m Model) forgeIcon() string {
	if m.config == nil {
		return theme.ForgeIcon("")
	}
	return theme.ForgeIcon(m.config.Forge.Type)
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

// pipelineStatusStyle colours the CI cell. A failed pipeline is the one thing
// in this table worth spotting without reading, and a green tick beside it is
// what makes it spottable — so success is coloured here where a running
// container is not: the CI column says nothing else, and most rows are not
// successes.
func pipelineStatusStyle(node *TreeNode) lipgloss.Style {
	if node.Type != NodeTypeProject {
		return theme.DimStyle
	}
	switch node.PipelineStatus {
	case "success":
		return theme.StatusOKStyle
	case "failed":
		return theme.StatusErrorStyle
	case "running":
		return lipgloss.NewStyle().Foreground(theme.ColorHighlight)
	case "canceled", "skipped", "manual", "":
		return theme.DimStyle
	default:
		return theme.StatusWarningStyle
	}
}

// Helper rendering functions

// renderNotAuthenticated names the forge and the command that signs into it.
//
// The command comes from internal/command rather than from a literal: it is
// routing identity, the same for both forges, and writing it out here is how a
// message survives a rename by going quietly wrong.
func renderNotAuthenticated(v forge.Vocabulary) string {
	style := lipgloss.NewStyle().Background(theme.ColorBackground).Foreground(theme.ColorError)
	return style.Render(theme.IconWarning + " " + v.Name + " not authenticated\n\nPlease authenticate first with :" + string(command.ViewGitAuth))
}

func renderError(err string) string {
	style := lipgloss.NewStyle().Background(theme.ColorBackground).Foreground(theme.ColorError)
	return style.Render(fmt.Sprintf("%s Error: %s", theme.IconError, err))
}

func renderEmpty(v forge.Vocabulary) string {
	lower := strings.ToLower(v.Namespaces)
	return theme.HelpStyle.Render("No " + lower + " found\n\nYou may not have access to any " + v.Name + " " + lower + ".")
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
	case ModeLoadingTemplates:
		return []shortcut.Shortcut{}
	case ModeSelecting:
		// ←→ keeps drilling here, and that is the point: deselecting inside a
		// ticked group means going into it (decision 10).
		return []shortcut.Shortcut{
			{Key: "space", Description: "Tick or untick"},
			{Key: "←→", Description: "Open/Back"},
			{Key: "enter", Description: "Choose a destination"},
			{Key: "/", Description: "Search"},
			{Key: "esc", Description: "Cancel"},
		}
	case ModeCloning:
		return m.cloningShortcuts()
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

	if !m.shared.IsAuthenticated {
		return []shortcut.Shortcut{
			{Key: "ctrl+p", Description: "Command mode"},
		}
	}

	shortcuts := []shortcut.Shortcut{
		{Key: "←→", Description: "Open/Back"},
		{Key: "N", Description: "New"},
		{Key: "D", Description: "Delete"},
		{Key: "C", Description: "Clone"},
	}

	// ctrl+w: only show when a node is selected (all nodes have a WebURL from GitLab API)
	if node, ok := m.selectedNode(); ok && node.WebURL != "" {
		shortcuts = append(shortcuts, shortcut.Shortcut{Key: "W", Description: "Browser"})
	}

	return append(shortcuts,
		shortcut.Shortcut{Key: ".", Description: "Sort"},
		shortcut.Shortcut{Key: "/", Description: "Search"},
		shortcut.Shortcut{Key: "ctrl+r", Description: "Refresh"},
		shortcut.Shortcut{Key: "ctrl+p", Description: "Command"},
		shortcut.Shortcut{Key: "?", Description: "Help"},
	)
}

// cloningShortcuts is state-aware (Rule 130): `esc` means three different
// things across a run, and offering the same word for all three is how a user
// presses it a second time expecting it to force.
func (m Model) cloningShortcuts() shortcut.Shortcuts {
	esc := shortcut.Shortcut{Key: "esc", Description: "Cancel"}
	switch {
	case m.clone == nil || m.clone.finished:
		esc.Description = "Close"
	case m.clone.cancelling:
		esc.Description = "Waiting for the running clones"
	}
	return []shortcut.Shortcut{
		esc,
		{Key: "/", Description: "Search"},
		{Key: ".", Description: "Sort"},
		{Key: "?", Description: "Help"},
	}
}

func (m Model) GetTitle() string {
	base := m.forgeIcon() + " " + m.vocab().Name + " Explorer"
	if m.creationForm != nil {
		return base + " " + theme.IconChevronRight + " " + m.creationForm.GetTitle()
	}
	switch m.mode {
	case ModeSelecting:
		return base + " " + theme.IconChevronRight + " Select what to clone"
	case ModeCloning:
		// The destination is in the title because it is the one thing the list
		// does not repeat on every row, and it is what a user checks first.
		if m.clone != nil {
			return base + " " + theme.IconChevronRight + " Cloning into " + m.clone.target
		}
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
	// The username, not the flag: a session whose user could not be read would
	// otherwise put a bare "@" in the header.
	if m.shared.CurrentUser.Username != "" {
		info = append(info, shortcut.HeaderInfo{Key: "User", Value: "@" + m.shared.CurrentUser.Username, Style: theme.HeaderValueStyle})
	}
	return info
}

// GetHelpContent retourne le contenu d'aide de la vue GitLab Explorer
func (m Model) GetHelpContent() help.Content {
	v := m.vocab()
	return help.Content{
		Title: v.Name + " Explorer",
		Description: "A drill-down explorer for browsing " + v.Name + " " + strings.ToLower(v.Namespaces) + " and " +
			strings.ToLower(v.Repositories) + ". Navigate into " + strings.ToLower(v.Namespaces) +
			" with → and go back with ←. Tabs at the bottom show your current path.",
		KeyBindings: []help.KeyBinding{
			{Key: "↑↓", Description: "Navigate the list"},
			{Key: "→", Description: "Drill into selected group"},
			{Key: "←", Description: "Go back to parent group"},
			{Key: "Esc", Description: "Go back to parent group"},
			{Key: "C", Description: "Start a clone selection"},
			{Key: "Space", Description: "Tick or untick the selected row (selection mode)"},
			{Key: "W", Description: "Open the selected group or project in the default web browser"},
			{Key: "N", Description: "Create a new group or project under the current context. Use ←→ to select the type."},
			{Key: "D", Description: "Delete the selected group or project"},
			{Key: ".", Description: "Cycle sort column (Type → Name → Visibility → Created → Activity)"},
			{Key: "/", Description: "Filter the current level by name or path"},
			{Key: "Ctrl+R", Description: "Refresh the explorer"},
			{Key: "ctrl+p", Description: "Open command mode"},
			{Key: "?", Description: "Show this help"},
		},
		Sections: []help.Section{
			{
				Title: "Navigation",
				Body:  "The explorer uses a drill-down model. Press → on a group to see its contents. Press ← to go back. Tabs at the bottom show your current path.",
			},
			{
				Title: "Cloning",
				Body: "Press 'c' to enter selection mode. Every row gains a checkbox; press Space to tick a group or a project. " +
					"Ticking a group takes everything under it — drill in with → and untick what you do not want. " +
					"Press Enter to choose a destination directory in the workspace selector.\n" +
					"The clone list then fills as repositories are discovered, each row showing what it is doing. " +
					"A repository already on disk is skipped untouched; use the workspaces view to update one.\n" +
					"Press Esc to cancel: discovery stops immediately, but clones already running are allowed to finish " +
					"rather than being killed half-written. Press Esc again once it is done to return to the tree.",
			},
			{
				Title: "Creating Groups and Projects",
				Body:  "Press N to open the creation form. Use ←→ on the Type field to switch between Group and Project. The form uses the current group as parent. Project templates are loaded automatically from the OCI registry if configured.",
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
				Body: "You must be authenticated (via :" + string(command.ViewGitAuth) + ") to access the explorer. " +
					"The clone method (SSH or HTTPS) is configurable in the context configuration.",
			},
		},
	}
}
