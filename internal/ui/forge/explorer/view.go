package explorer

import (
	"fmt"
	"strings"
	"time"

	"github.com/anthnel/devdesk/internal/command"
	"github.com/anthnel/devdesk/internal/forge"
	"github.com/anthnel/devdesk/internal/ui/components"
	"github.com/anthnel/devdesk/internal/ui/help"
	"github.com/anthnel/devdesk/internal/ui/keymap"
	"github.com/anthnel/devdesk/internal/ui/shortcut"
	"github.com/anthnel/devdesk/internal/ui/theme"
	"github.com/charmbracelet/lipgloss"
)

// View renders the view
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
	// screen (Rule 139): whether it is loading, genuinely empty, or filtered
	// down to nothing, the body is the table alone — the row count is a header
	// field instead.
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

// nodeKindIcon is the glyph the first column carries: a namespace or a
// repository.
//
// It says in one cell what the Type column used to say in thirteen, and it says
// it in no forge's words — which is the one thing lost and the one thing gained.
// Lost: a GitHub user no longer reads "Organization" on the row. Gained: the
// personal account's row no longer *claims* to be one (see docs/architecture/
// forge.md), and the vocabulary moved to the help legend, where nodeTypeLabel
// still resolves it and where a sentence has room to be right.
func nodeKindIcon(node *TreeNode) string {
	if node.Type == NodeTypeGroup {
		return theme.IconNamespace
	}
	return theme.IconRepository
}

// nodeKindRole is the same distinction as a colour, and it is what keeps the
// kind readable in the clone selection: there the glyph becomes a checkbox, so
// the hue is all that is left to say group or repository.
func nodeKindRole(node *TreeNode) theme.IconRole {
	if node.Type == NodeTypeGroup {
		return theme.IconRoleNamespace
	}
	return theme.IconRoleRepository
}

// visibilityIcon is GitLab's and GitHub's own alphabet: a globe for what anyone
// can read, a shield for what a signed-in user can, a lock for neither.
//
// An unknown visibility renders **nothing** rather than a placeholder glyph. The
// field is the backend's, and a forge that grows a fourth value would otherwise
// show one of the three existing ones — a wrong answer where an empty cell is a
// true one.
func visibilityIcon(node *TreeNode) string {
	switch node.Visibility {
	case "public":
		return theme.IconVisibilityPublic
	case "internal":
		return theme.IconVisibilityInternal
	case "private":
		return theme.IconLock
	}
	return ""
}

// visibilityRole colours the glyph above. An unknown visibility has no role, so
// IconStyle falls back to plain text — which is what an empty cell should be.
func visibilityRole(node *TreeNode) theme.IconRole {
	switch node.Visibility {
	case "public":
		return theme.IconRoleVisPublic
	case "internal":
		return theme.IconRoleVisInternal
	case "private":
		return theme.IconRoleVisPrivate
	}
	return ""
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

// pipelineStatusLabel returns a status icon for the last CI pipeline.
//
// Every value forge.CIStatuses() declares has a case here, and
// TestEveryDeclaredCIStatusHasAnIcon walks that list to prove it. The default
// branch is therefore unreachable for a backend that keeps its promise — it
// stays because a backend that breaks it should still render *something*, and
// because that is how D66 was found: `created` and `scheduled` were missing
// from this switch, so a GitLab pipeline in either state printed its own name
// truncated to six cells.
//
// It switches on the constants rather than on literals so the two lists cannot
// drift again in silence: a renamed status stops compiling here.
func pipelineStatusLabel(node *TreeNode) string {
	if node.Type != NodeTypeProject || node.PipelineStatus == "" {
		return ""
	}
	switch node.PipelineStatus {
	case forge.CIStatusSuccess:
		return theme.IconOK
	case forge.CIStatusFailed:
		return theme.IconError
	case forge.CIStatusRunning:
		return theme.IconRunning
	// The five ways a pipeline can have not started yet. They share one glyph
	// because the difference between them is the scheduler's business, not the
	// reader's: all five answer "nothing has run".
	case forge.CIStatusPending, forge.CIStatusWaitingForResource, forge.CIStatusPreparing,
		forge.CIStatusCreated, forge.CIStatusScheduled:
		return theme.IconPending
	case forge.CIStatusCanceled:
		return theme.IconCanceled
	case forge.CIStatusSkipped:
		return theme.IconSkipped
	case forge.CIStatusManual:
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

	// Signed out is a different screen — there is no tree, so there is nothing
	// to grey. That is a mode, not a state (Rule 130).
	if !m.shared.IsAuthenticated {
		return []shortcut.Shortcut{
			{Key: "ctrl+p", Description: "Command mode"},
		}
	}

	// Normal mode. A load in flight greys the lot rather than emptying it: the
	// tree is the same screen either side of a refresh, and a column that
	// vanishes and comes back on every ctrl+r is the flicker Rule 130 is about.
	loading := m.loading
	browse := m.browsable()
	act := m.actionable()

	return []shortcut.Shortcut{
		{Key: "←→", Description: "Open/Back", Disabled: loading},
		{Key: keymap.New, Description: "New", Disabled: loading},
		{Key: keymap.Delete, Description: "Delete", Disabled: loading || !act.Enabled()},
		{Key: keymap.Clone, Description: "Clone", Disabled: loading},
		{Key: keymap.Web, Description: "Browser", Disabled: loading || !browse.Enabled()},
		{Key: ".", Description: "Sort", Disabled: loading},
		{Key: "/", Description: "Search", Disabled: loading},
		{Key: "ctrl+r", Description: "Refresh"},
		{Key: "ctrl+p", Description: "Command"},
		{Key: "?", Description: "Help"},
	}
}

// reasonNoWebURL is why W does not apply. The row exists, it simply has no page.
const reasonNoWebURL = "This entry has no web page to open"

// reasonNotCreatedYet is why an action does not apply to a row the forge has
// not confirmed. It is a different sentence from busyMessage on purpose: that
// one says "wait, something is running", this one says "there is nothing there
// to act on yet" — and the second is what a placeholder row means.
const reasonNotCreatedYet = "This entry is still being created"

// reasonStillLoading is why an action that needs the level does not apply while
// the level is being fetched.
const reasonStillLoading = "Still loading — wait for the list"

// actionable reports whether the selected row is one the forge can be asked
// about: a real node, and not one already held by work in flight.
//
// A placeholder carries no identifier, so every action addressing one would
// send an empty ID — which the backend answers for some other object, or for
// none. Greying is Rule 130's answer, and the reason is named so the footer can
// say it when the key is pressed anyway.
func (m Model) actionable() shortcut.Availability {
	node, ok := m.selectedNode()
	if !ok {
		return shortcut.Unavailable(reasonNotCreatedYet)
	}
	if node.Creating {
		return shortcut.Unavailable(reasonNotCreatedYet)
	}
	if m.busy(node.FullPath) {
		return shortcut.Unavailable(busyMessage)
	}
	return shortcut.Availability{}
}

// browsable reports whether W can open the selected row.
//
// Every node the forges return carries a WebURL, so in practice this only
// refuses on an empty level — but the handler already checked for it and
// returned in silence, which is what Rule 130 now forbids.
func (m Model) browsable() shortcut.Availability {
	if node, ok := m.selectedNode(); !ok || node.WebURL == "" {
		return shortcut.Unavailable(reasonNoWebURL)
	}
	return shortcut.Availability{}
}

// cloningShortcuts is state-aware (Rule 130): `esc` means three different
// things across a run, and offering the same word for all three is how a user
// presses it a second time expecting it to force.
func (m Model) cloningShortcuts() shortcut.Shortcuts {
	esc := shortcut.Shortcut{Key: "esc", Description: "Cancel"}
	switch {
	case m.clone == nil || m.clone.finished():
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
	if m.shared.IsAuthenticated && m.error == "" {
		info = append(info, shortcut.HeaderInfo{
			Key:   m.vocab().Namespaces,
			Value: fmt.Sprintf("%d", len(m.nodes)),
			Style: theme.HeaderValueStyle,
		})
	}
	return info
}

// GetHelpContent returns the help content for the GitLab Explorer view
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
			{Key: keymap.Clone, Description: "Start a clone selection"},
			{Key: "Space", Description: "Tick or untick the selected row (selection mode)"},
			{Key: keymap.Web, Description: "Open the selected group or project in the default web browser"},
			{Key: keymap.New, Description: "Create a new group or project under the current context. Use ←→ to select the type."},
			{Key: keymap.Delete, Description: "Delete the selected group or project"},
			{Key: ".", Description: "Cycle sort column (Name → Created → Activity, then back to the forge's own order)"},
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
				Body: "Press N to open the creation form. Use ←→ on the Type field to switch between Group and Project. " +
					"The form uses the current group as parent. Project templates are loaded automatically from the OCI registry if configured.\n" +
					"Submitting puts the row on screen straight away, with a spinner in place of its icon while the forge works. " +
					"The row is inert until it settles — it has no identifier yet, so the actions that need one are greyed. " +
					"When the forge answers, the row becomes the real entry and the cursor is left on it; if the creation fails, the row goes away and the footer says so.",
			},
			{
				Title: "Deleting",
				Body: "Press D to delete the selected group or project, then confirm. The row keeps its place and takes a spinner while the forge works, " +
					"and disappears only once the deletion is confirmed — a row that vanished first would be claiming something that had not happened yet.\n" +
					"Both creating and deleting are listed in the :jobs view while they run.",
			},
			{
				Title: "Row Icons",
				Body: "The first column says what the row is, and the Visibility column who can read it:\n" +
					"  " + theme.IconNamespace + "  " + v.Namespace + "\n" +
					"  " + theme.IconRepository + "  " + v.Repository + "\n" +
					"  (spinner)  the forge is being asked to create or delete this row\n" +
					"  " + theme.IconVisibilityPublic + "  " + visibilityLabel(&TreeNode{Visibility: "public"}) + " — anyone can read it\n" +
					"  " + theme.IconVisibilityInternal + "  " + visibilityLabel(&TreeNode{Visibility: "internal"}) + " — any signed-in user can (" + v.Name + " only where the concept exists)\n" +
					"  " + theme.IconLock + "  " + visibilityLabel(&TreeNode{Visibility: "private"}) + " — members only\n" +
					"In clone selection mode the first column shows the checkbox instead; its colour still says " +
					strings.ToLower(v.Namespace) + " or " + strings.ToLower(v.Repository) + ".",
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
