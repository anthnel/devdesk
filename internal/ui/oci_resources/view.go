package ociresources

import (
	"fmt"
	"strings"

	"github.com/charmbracelet/lipgloss"

	"github.com/anthnel/devdesk/internal/config"
	"github.com/anthnel/devdesk/internal/ui/help"
	"github.com/anthnel/devdesk/internal/ui/shortcut"
	"github.com/anthnel/devdesk/internal/ui/theme"
)

// FilterBarVisible returns true when the filter bar is visible (implements app.FilterBarView).
func (m Model) FilterBarVisible() bool {
	if m.registryBrowser != nil {
		return m.registryBrowser.FilterIsVisible()
	}
	return m.activeTab == tabImages && m.filterBar.IsVisible() &&
		m.launchForm == nil && m.resourceForm == nil && m.registryForm == nil &&
		m.connectivityForm == nil && m.networkInspectForm == nil && !m.selectionMode
}

// InEditMode returns true when a form, modal, filter, or selection mode is active
func (m Model) InEditMode() bool {
	return m.launchForm != nil || m.resourceForm != nil || m.registryForm != nil ||
		m.networkInspectForm != nil || m.connectivityForm != nil ||
		m.confirmModal != nil || (m.activeTab == tabImages && m.filterBar.InEditMode()) || m.selectionMode ||
		m.registryBrowser != nil
}

// GetFooterHeight returns the footer height for this view (Rule 124).
func (m Model) GetFooterHeight() int {
	if m.registryBrowser != nil {
		filterExtra := 0
		if m.registryBrowser.FilterIsVisible() {
			filterExtra = 2
		}
		return 2 + filterExtra // empty line + info line + optional filter bar
	}
	filterExtra := 0
	if m.activeTab == tabImages {
		filterExtra = m.filterBar.ExtraHeight()
	}
	if !m.selectionMode && m.launchForm == nil && m.resourceForm == nil && m.registryForm == nil &&
		m.connectivityForm == nil && m.networkInspectForm == nil {
		crumbExtra := 0
		if m.registryGroupSlug != "" {
			crumbExtra = 1 // the drill-down breadcrumb
		}
		return 3 + filterExtra + crumbExtra // tab bar + empty line + info line + filter/breadcrumb
	}
	return 2 // empty line + info line (no filter bar in form/selection mode)
}

// RenderFooter returns the footer content rendered below the viewport (Rule 124).
func (m Model) RenderFooter(width int) string {
	if m.selectionMode || m.registryBrowser != nil || m.launchForm != nil || m.resourceForm != nil || m.registryForm != nil ||
		m.connectivityForm != nil || m.networkInspectForm != nil {
		// No tab bar or filter bar in selection/form mode — empty line + info line
		infoLine := theme.EmptyLineBg(width)
		if m.selectionMode && m.selectionMessage != "" {
			infoLine = lipgloss.NewStyle().
				Foreground(theme.ColorHighlight).
				Background(theme.ColorBackground).
				Width(width).
				Align(lipgloss.Center).
				Render(m.selectionMessage)
		} else if m.errorMsg != "" {
			infoLine = theme.StatusErrorStyle.Width(width).Align(lipgloss.Center).Render(m.errorMsg)
		} else if m.infoMsg != "" {
			infoLine = lipgloss.NewStyle().
				Foreground(theme.ColorHighlight).
				Background(theme.ColorBackground).
				Width(width).
				Align(lipgloss.Center).
				Render(m.infoMsg)
		}
		if m.registryBrowser != nil && m.registryBrowser.FilterIsVisible() {
			return m.registryBrowser.FilterBarView(width) + "\n" + theme.EmptyLineBg(width) + "\n" + infoLine
		}
		return theme.EmptyLineBg(width) + "\n" + infoLine
	}
	var parts []string
	if m.activeTab == tabImages && m.filterBar.IsVisible() {
		parts = append(parts, m.filterBar.View())
	}
	if crumb := m.renderRegistryBreadcrumb(width); crumb != "" {
		parts = append(parts, crumb)
	}
	tabBar := m.renderTabBar(width)
	infoLine := theme.EmptyLineBg(width)
	if m.errorMsg != "" {
		infoLine = theme.StatusErrorStyle.Width(width).Align(lipgloss.Center).Render(m.errorMsg)
	} else if m.infoMsg != "" {
		infoLine = lipgloss.NewStyle().
			Foreground(theme.ColorHighlight).
			Background(theme.ColorBackground).
			Width(width).
			Align(lipgloss.Center).
			Render(m.infoMsg)
	}
	parts = append(parts, tabBar, theme.EmptyLineBg(width), infoLine)
	return strings.Join(parts, "\n")
}

// renderRegistryBreadcrumb renders the drill-down trail below the table when
// the Registries tab has entered a group (Rules 111, 123). Breadcrumb mode: it
// is not navigable, the level you are on is highlighted and the one above is
// dimmed. Empty at the top level, where there is no trail to show.
func (m Model) renderRegistryBreadcrumb(width int) string {
	group := m.drilledGroup()
	if group == nil {
		return ""
	}
	tabs := []theme.TabItem{
		{Label: "Registries"},
		{Label: browserAlias(*group)},
	}
	return theme.PadWithBg(theme.Bg(" ")+theme.RenderTabs(tabs, 1), width)
}

// renderTabBar renders the tab bar below the viewport (Rule 123)
func (m Model) renderTabBar(width int) string {
	tabs := []theme.TabItem{
		{Label: theme.IconDocker + " Images"},
		{Label: theme.IconNetwork + " Networks"},
		{Label: theme.IconVolume + " Volumes"},
		{Label: theme.IconServer + " Registries"},
	}
	return theme.PadWithBg(theme.Bg(" ")+theme.RenderTabs(tabs, int(m.activeTab)), width)
}

// GetTitle returns the view title
func (m Model) GetTitle() string {
	base := theme.IconDocker + " OCI Resources"
	if m.registryBrowser != nil {
		title := base + " " + theme.IconChevronRight + " Registry Browser"
		if m.registryBrowser.state == browserStateTags && m.registryBrowser.repoInput.Value() != "" {
			title += " " + theme.IconChevronRight + " " + m.registryBrowser.repoInput.Value()
		}
		if !m.registryBrowser.registryFilter.isEmpty() {
			title += " [" + m.registryBrowser.registryFilterLabel() + "]"
		}
		return title
	}
	if m.launchForm != nil {
		return base + " " + theme.IconChevronRight + " Launch Container"
	}
	if m.resourceForm != nil {
		return base + " " + theme.IconChevronRight + " " + m.resourceForm.GetTitle()
	}
	if m.registryForm != nil {
		return base + " " + theme.IconChevronRight + " " + m.registryForm.GetTitle()
	}
	if m.connectivityForm != nil {
		return base + " " + theme.IconChevronRight + " Connectivity Test"
	}
	if m.networkInspectForm != nil {
		return base + " " + theme.IconChevronRight + " Network Inspect"
	}
	return base
}

// GetIcon returns the view icon
func (m Model) GetIcon() string {
	return ""
}

// GetHeaderInfo returns the key-value info for the header
func (m Model) GetHeaderInfo(_ string) []shortcut.HeaderInfo {
	switch m.activeTab {
	case tabNetworks:
		return []shortcut.HeaderInfo{
			{Key: "Networks", Value: fmt.Sprintf("%d", len(m.networkTable.Items())), Style: theme.HeaderValueStyle},
		}
	case tabVolumes:
		return []shortcut.HeaderInfo{
			{Key: "Volumes", Value: fmt.Sprintf("%d", len(m.volumeTable.Items())), Style: theme.HeaderValueStyle},
		}
	case tabRegistries:
		return []shortcut.HeaderInfo{
			{Key: "Registries", Value: fmt.Sprintf("%d", len(m.registries)), Style: theme.HeaderValueStyle},
		}
	default:
		filtered := m.filteredImages()
		total := len(filtered)
		var totalDisk int64
		for _, img := range filtered {
			totalDisk += img.UniqueSize
		}
		infos := []shortcut.HeaderInfo{
			{Key: "Images", Value: fmt.Sprintf("%d", total), Style: theme.HeaderValueStyle},
			{Key: "Disk Usage", Value: formatBytes(totalDisk), Style: theme.HeaderValueStyle},
		}
		unscanned := m.unscannedCount()
		if unscanned > 0 {
			infos = append(infos, shortcut.HeaderInfo{
				Key:   "Unscanned",
				Value: fmt.Sprintf("%d", unscanned),
				Style: lipgloss.NewStyle().Foreground(theme.ColorHighlight).Background(theme.ColorBackground),
			})
		}
		return infos
	}
}

// GetShortcuts returns the keyboard shortcuts for the header (Rule 130: dynamic shortcuts)
func (m Model) GetShortcuts() shortcut.Shortcuts {
	if m.registryBrowser != nil {
		switch m.registryBrowser.state {
		case browserStateTags:
			if m.registryBrowser.filterActive {
				return []shortcut.Shortcut{
					{Key: "enter/esc", Description: "Close filter"},
				}
			}
			var shortcuts []shortcut.Shortcut
			if m.registryBrowser.HasSelectedTagScanResults() {
				shortcuts = append(shortcuts, shortcut.Shortcut{Key: "enter", Description: "View CVE details"})
			}
			shortcuts = append(shortcuts,
				shortcut.Shortcut{Key: "ctrl+s", Description: "Scan image"},
				shortcut.Shortcut{Key: "p", Description: "Pull image"},
				shortcut.Shortcut{Key: "r", Description: "Filter registry"},
				shortcut.Shortcut{Key: "/", Description: "Filter"},
				shortcut.Shortcut{Key: ".", Description: "Sort"},
				shortcut.Shortcut{Key: "esc", Description: "Go back"},
			)
			return shortcuts
		case browserStateStatus:
			return nil // blocked during operation
		default: // browserStateInput
			var shortcuts []shortcut.Shortcut
			if m.registryBrowser.focusedField == m.registryBrowser.brFieldSubmit() {
				shortcuts = append(shortcuts, shortcut.Shortcut{Key: "enter", Description: "Browse tags"})
			}
			shortcuts = append(shortcuts, shortcut.Shortcut{Key: "esc", Description: "Close browser"})
			return shortcuts
		}
	}
	if m.selectionMode {
		return []shortcut.Shortcut{
			{Key: "enter", Description: "Select"},
			{Key: "/", Description: "Filter"},
			{Key: ".", Description: "Sort"},
			{Key: "esc", Description: "Cancel"},
		}
	}
	if m.launchForm != nil {
		lf := m.launchForm
		var shortcuts []shortcut.Shortcut
		isCheckbox := lf.isPortField(lf.focusedField) ||
			lf.focusedField == lf.fieldOptionRemove() ||
			lf.focusedField == lf.fieldOptionDetach() ||
			lf.focusedField == lf.fieldOptionInteractive()
		if isCheckbox {
			shortcuts = append(shortcuts, shortcut.Shortcut{Key: "space", Description: "Toggle"})
		} else if lf.focusedField == lf.fieldNetwork() {
			shortcuts = append(shortcuts, shortcut.Shortcut{Key: "←→", Description: "Cycle network"})
		} else if lf.focusedField == lf.fieldSubmit() {
			shortcuts = append(shortcuts, shortcut.Shortcut{Key: "enter", Description: "Launch"})
		}
		shortcuts = append(shortcuts,
			shortcut.Shortcut{Key: "ctrl+y", Description: "Copy command"},
			shortcut.Shortcut{Key: "esc", Description: "Cancel"},
		)
		return shortcuts
	}
	if m.resourceForm != nil {
		var shortcuts []shortcut.Shortcut
		if m.resourceForm.kind == resourceFormNetwork && m.resourceForm.focusedField == 1 {
			shortcuts = append(shortcuts, shortcut.Shortcut{Key: "←→", Description: "Cycle driver"})
		}
		shortcuts = append(shortcuts,
			shortcut.Shortcut{Key: "enter", Description: "Submit"},
			shortcut.Shortcut{Key: "esc", Description: "Cancel"},
		)
		return shortcuts
	}
	if m.connectivityForm != nil {
		cf := m.connectivityForm
		if cf.state == connectivityStateResults {
			return []shortcut.Shortcut{
				{Key: "enter", Description: "New test"},
				{Key: "esc", Description: "Back"},
			}
		}
		var shortcuts []shortcut.Shortcut
		switch cf.focusedField {
		case cFieldTarget:
			shortcuts = append(shortcuts, shortcut.Shortcut{Key: "↑↓", Description: "Browse containers"})
		case cFieldType:
			shortcuts = append(shortcuts, shortcut.Shortcut{Key: "←→", Description: "Cycle type"})
		}
		shortcuts = append(shortcuts,
			shortcut.Shortcut{Key: "enter", Description: "Run test"},
			shortcut.Shortcut{Key: "esc", Description: "Back"},
		)
		return shortcuts
	}
	if m.networkInspectForm != nil {
		return []shortcut.Shortcut{
			{Key: "c", Description: "Connectivity test"},
			{Key: "esc", Description: "Close"},
		}
	}
	if m.confirmModal != nil {
		return []shortcut.Shortcut{
			{Key: "y/n", Description: "Confirm"},
			{Key: "esc", Description: "Cancel"},
		}
	}

	base := []shortcut.Shortcut{
		{Key: "tab", Description: "Switch tab"},
	}

	switch m.activeTab {
	case tabImages:
		base = append(base, shortcut.Shortcut{Key: "enter", Description: "Scan details"})
		if m.isSelectedImageScanning() {
			base = append(base, shortcut.Shortcut{Key: theme.IconRefresh, Description: "scanning..."})
		} else {
			base = append(base,
				shortcut.Shortcut{Key: "ctrl+e", Description: "Launch"},
				shortcut.Shortcut{Key: "ctrl+s", Description: "Scan"},
				shortcut.Shortcut{Key: "ctrl+d", Description: "Delete"},
			)
		}
		base = append(base,
			shortcut.Shortcut{Key: "b", Description: "Browse registries"},
			shortcut.Shortcut{Key: "ctrl+a", Description: "Scan all"},
			shortcut.Shortcut{Key: "A", Description: "Scan unscanned"},
			shortcut.Shortcut{Key: "p", Description: "Prune"},
			shortcut.Shortcut{Key: ".", Description: "Sort"},
			shortcut.Shortcut{Key: "/", Description: "Filter"},
		)
	case tabNetworks:
		base = append(base,
			shortcut.Shortcut{Key: "enter", Description: "Inspect"},
			shortcut.Shortcut{Key: "ctrl+n", Description: "New network"},
			shortcut.Shortcut{Key: "ctrl+d", Description: "Remove"},
			shortcut.Shortcut{Key: "p", Description: "Prune"},
		)
	case tabVolumes:
		base = append(base,
			shortcut.Shortcut{Key: "ctrl+n", Description: "New volume"},
			shortcut.Shortcut{Key: "ctrl+d", Description: "Remove"},
			shortcut.Shortcut{Key: "p", Description: "Prune"},
		)
	case tabRegistries:
		// Inside a group the rows are cached members, not config entries: there
		// is nothing to edit, log into or remove (Rule 130).
		if m.registryGroupSlug != "" {
			return append(base,
				shortcut.Shortcut{Key: "←", Description: "Back to registries"},
				shortcut.Shortcut{Key: "?", Description: "Help"},
			)
		}
		base = append(base,
			shortcut.Shortcut{Key: "ctrl+n", Description: "New registry"},
			shortcut.Shortcut{Key: "e", Description: "Edit registry"},
			shortcut.Shortcut{Key: "l", Description: "Login"},
			shortcut.Shortcut{Key: "L", Description: "Logout"},
			shortcut.Shortcut{Key: "ctrl+d", Description: "Remove"},
		)
		if reg := m.getSelectedRegistry(); reg != nil && reg.Kind == config.KindGroup {
			// Rule 130: only offered on a row that has members.
			base = append(base,
				shortcut.Shortcut{Key: "→", Description: "Show members"},
				shortcut.Shortcut{Key: "ctrl+r", Description: "Refresh group members"},
			)
			return append(base, shortcut.Shortcut{Key: "?", Description: "Help"})
		}
	}

	base = append(base,
		shortcut.Shortcut{Key: "ctrl+r", Description: "Refresh"},
		shortcut.Shortcut{Key: "?", Description: "Help"},
	)
	return base
}

// View renders the view (Rule 112: forms in viewport, modals centered)
func (m Model) View() string {
	// Priority 0: registry browser (viewport)
	if m.registryBrowser != nil {
		return m.registryBrowser.View()
	}
	// Priority 1: launch form (viewport)
	if m.launchForm != nil {
		return m.launchForm.View()
	}
	// Priority 2: resource creation form (viewport)
	if m.resourceForm != nil {
		return m.resourceForm.View()
	}
	// Priority 3: registry form (viewport)
	if m.registryForm != nil {
		return m.registryForm.View()
	}
	// Priority 4: connectivity test form (viewport)
	if m.connectivityForm != nil {
		return m.connectivityForm.View()
	}
	// Priority 5: confirm modal (centered)
	if m.confirmModal != nil {
		return lipgloss.Place(
			m.width, m.height,
			lipgloss.Center, lipgloss.Center,
			m.confirmModal.View(),
			lipgloss.WithWhitespaceBackground(theme.ColorBackground),
		)
	}
	// Priority 6: network inspect form (viewport)
	if m.networkInspectForm != nil {
		return m.networkInspectForm.View()
	}
	// Priority 7: normal view by active tab
	return m.renderNormalView()
}

// renderNormalView renders the active tab's content
func (m Model) renderNormalView() string {
	switch m.activeTab {
	case tabNetworks:
		return m.renderNetworksView()
	case tabVolumes:
		return m.renderVolumesView()
	case tabRegistries:
		return m.renderRegistriesView()
	default:
		return m.renderImagesView()
	}
}

// renderRegistriesView renders the registries table
func (m Model) renderRegistriesView() string {
	if len(m.registries) == 0 {
		return lipgloss.NewStyle().Background(theme.ColorBackground).Padding(1).Render(
			theme.DimStyle.Render("No registries configured — press 'n' to add one"),
		)
	}
	return m.registryTable.View()
}

// renderImagesView renders the images table
func (m Model) renderImagesView() string {
	var sections []string

	if m.loading && len(m.images) == 0 {
		sections = append(sections, theme.SpinnerMessage(m.spinner.View(), "Loading images..."))
	} else if len(m.filteredImages()) == 0 && !m.filterBar.IsVisible() {
		sections = append(sections, theme.DimStyle.Render("No images found"))
	} else {
		// Always render the table when a filter is active so the filter bar stays at the bottom
		sections = append(sections, m.imageTable.View())
	}

	return strings.Join(sections, "\n")
}

// renderNetworksView renders the networks table
func (m Model) renderNetworksView() string {
	if m.loadingNets && len(m.networkTable.Items()) == 0 {
		return lipgloss.NewStyle().Background(theme.ColorBackground).Padding(1).Render(
			theme.SpinnerMessage(m.spinner.View(), "Loading networks..."),
		)
	}
	if len(m.networkTable.Items()) == 0 {
		return lipgloss.NewStyle().Background(theme.ColorBackground).Padding(1).Render(
			theme.DimStyle.Render("No networks found"),
		)
	}
	return m.networkTable.View()
}

// renderVolumesView renders the volumes table
func (m Model) renderVolumesView() string {
	if m.loadingVols && len(m.volumeTable.Items()) == 0 {
		return lipgloss.NewStyle().Background(theme.ColorBackground).Padding(1).Render(
			theme.SpinnerMessage(m.spinner.View(), "Loading volumes..."),
		)
	}
	if len(m.volumeTable.Items()) == 0 {
		return lipgloss.NewStyle().Background(theme.ColorBackground).Padding(1).Render(
			theme.DimStyle.Render("No volumes found"),
		)
	}
	return m.volumeTable.View()
}

// GetHelpContent returns help content for the OCI resources view (Rule 114)
func (m Model) GetHelpContent() help.Content {
	return help.Content{
		Title:       "OCI Resources",
		Description: "This view manages local Docker/OCI resources: Images (with CVE scanning), Networks, Volumes, and Registries. Use Tab/Shift+Tab to switch between tabs.",
		KeyBindings: []help.KeyBinding{
			{Key: "tab / shift+tab", Description: "Switch between Images, Networks, Volumes, and Registries tabs"},
			{Key: "enter (Images)", Description: "View scan details for the selected image (loads from cache; falls back to scan if not yet scanned)"},
			{Key: "ctrl+e (Images)", Description: "Launch a container from the selected image"},
			{Key: "ctrl+s (Images)", Description: "Configure and launch a scan for the selected image"},
			{Key: "ctrl+a (Images)", Description: "Scan all images (purges cache first)"},
			{Key: "A (Images)", Description: "Scan all unscanned images using config defaults"},
			{Key: "ctrl+d", Description: "Delete the selected resource (with confirmation)"},
			{Key: "p", Description: "Prune unused resources (with confirmation)"},
			{Key: "enter (Networks)", Description: "Inspect the selected network — shows connected containers with IP and MAC addresses"},
			{Key: "c (Network Inspect)", Description: "Open a connectivity test form for the selected container (runs from an ephemeral network-multitool container)"},
			{Key: "ctrl+n (Networks)", Description: "Create a new network"},
			{Key: "ctrl+n (Volumes)", Description: "Create a new volume"},
			{Key: "ctrl+n (Registries)", Description: "Add a registry"},
			{Key: "e (Registries)", Description: "Edit the selected registry"},
			{Key: "l (Registries)", Description: "Log in to the selected registry with docker login"},
			{Key: "L (Registries)", Description: "Log out of the selected registry"},
			{Key: ".", Description: "Cycle sort column (Images tab only)"},
			{Key: "b (Images)", Description: "Open the multi-registry browser — search tags across all configured registries"},
			{Key: "r (Browser tags)", Description: "Cycle the active registry filter (shows tags from one registry at a time)"},
			{Key: "p (Browser)", Description: "Pull the selected image tag to the local Docker store"},
			{Key: "ctrl+s (Browser)", Description: "Scan the selected image tag directly via Trivy (no pull required)"},
			{Key: "enter (Browser tags)", Description: "View CVE details for the selected tag (only when scan results are cached)"},
			{Key: "esc (Browser)", Description: "Go back to the previous screen in the registry browser"},
			{Key: "ctrl+r", Description: "Refresh the current tab's data — on a group row, re-run member discovery"},
			{Key: "→ (Registries)", Description: "Show a group's discovered members"},
			{Key: "← (Registries)", Description: "Go back to the registry list"},
			{Key: "/", Description: "Filter images by repository or tag (Images tab only)"},
			{Key: "↑/k", Description: "Move selection up"},
			{Key: "↓/j", Description: "Move selection down"},
			{Key: "g/Home", Description: "Go to top"},
			{Key: "G/End", Description: "Go to bottom"},
			{Key: "alt+:", Description: "Open command mode"},
			{Key: "?", Description: "Show this help"},
		},
		Sections: []help.Section{
			{
				Title: "Images Tab",
				Body: "Shows all local Docker images with disk usage, content size, and CVE scan results.\n" +
					"Press Enter to view the last scan details (loads from cache; falls back to scan if not yet scanned).\n" +
					"Press Ctrl+E to launch a container from the selected image (opens a form with pre-filled port mappings from EXPOSE metadata).\n" +
					"Press Ctrl+S to open the Security view for a configured scan.",
			},
			{
				Title: "Networks Tab",
				Body: "Lists Docker networks (ID, Name, Driver, Scope). Press Enter to inspect the selected network and see connected containers. " +
					"From the inspect overlay, press 'c' to open a connectivity test form that runs ping, curl, or nc from an ephemeral wbitt/network-multitool container. " +
					"Press 'n' to create a new network, Ctrl+D to remove the selected one, 'p' to prune all unused networks.",
			},
			{
				Title: "Volumes Tab",
				Body:  "Lists Docker volumes (Name, Driver, Mountpoint). Press 'n' to create a new volume, Ctrl+D to remove the selected one, 'p' to prune all unused volumes.",
			},
			{
				Title: "Registries Tab",
				Body:  "Lists configured Docker/OCI registries. Press 'n' to add a new registry, 'e' to edit, 'l' to log in, Ctrl+D to remove. Aliases shorten long registry URLs in the Images tab display.",
			},
			{
				Title: "Registry Browser",
				Body: "Opened from the Images tab with 'b'. Type a repository name (e.g. nginx or library/nginx), " +
					"select which registries to search using Space, then press Browse Tags. " +
					"Results show tags from all selected registries concurrently. " +
					"Press 'r' to cycle the registry filter (one registry at a time), '/' to filter by tag name, '.' to sort. " +
					"Press 'p' to pull the selected tag, Ctrl+S to scan it directly via Trivy (no pull), or Enter to view cached CVE details. " +
					"Press Esc to go back to the search form.",
			},
			{
				Title: "Launch Form",
				Body:  "When launching a container, ports are pre-filled from the image's EXPOSE metadata. Fill in environment variables and volume mounts as comma-separated lists. The container runs detached (-d) by default.",
			},
		},
	}
}
