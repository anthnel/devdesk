package ociresources

import (
	"fmt"
	"strings"

	"github.com/charmbracelet/lipgloss"

	sharedcomponents "github.com/anthnel/devdesk/internal/ui/components"
	"github.com/anthnel/devdesk/internal/ui/help"
	"github.com/anthnel/devdesk/internal/ui/keymap"
	"github.com/anthnel/devdesk/internal/ui/shortcut"
	"github.com/anthnel/devdesk/internal/ui/theme"
)

// FilterBarVisible returns true when the filter bar is visible (implements app.FilterBarView).
func (m Model) FilterBarVisible() bool {
	if m.registryBrowser != nil {
		return m.registryBrowser.FilterIsVisible()
	}
	return m.activeTab == tabImages && m.imageTable.FilterBar().IsVisible() &&
		m.launchForm == nil && m.resourceForm == nil && m.registryForm == nil &&
		m.networkInspectForm == nil
}

// modalView renders whichever modal is open, or "" when none is.
func (m Model) modalView() string {
	switch {
	case m.confirmModal != nil:
		return m.confirmModal.View()
	case m.scanAllModal != nil:
		return m.scanAllModal.View()
	}
	return ""
}

// InEditMode returns true when a form, modal or filter is active
func (m Model) InEditMode() bool {
	return m.launchForm != nil || m.resourceForm != nil || m.registryForm != nil ||
		m.networkInspectForm != nil ||
		m.confirmModal != nil || m.scanAllModal != nil ||
		(m.activeTab == tabImages && m.imageTable.InEditMode()) ||
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
		filterExtra = m.imageTable.FilterBar().ExtraHeight()
	}
	if m.launchForm == nil && m.resourceForm == nil && m.registryForm == nil &&
		m.networkInspectForm == nil {
		crumbExtra := 0
		if m.registryGroupSlug != "" {
			crumbExtra = 1 // the drill-down breadcrumb
		}
		return 3 + filterExtra + crumbExtra // tab bar + empty line + info line + filter/breadcrumb
	}
	return 2 // empty line + info line (no filter bar in form mode)
}

// RenderFooter returns the footer content rendered below the viewport (Rule 124).
func (m Model) RenderFooter(width int) string {
	if m.registryBrowser != nil || m.launchForm != nil || m.resourceForm != nil || m.registryForm != nil ||
		m.networkInspectForm != nil {
		// No tab bar or filter bar in form mode — empty line + info line
		infoLine := m.footer.View(width, m.formStatus())
		if m.registryBrowser != nil && m.registryBrowser.FilterIsVisible() {
			return m.registryBrowser.FilterBarView(width) + "\n" + theme.EmptyLineBg(width) + "\n" + infoLine
		}
		return theme.EmptyLineBg(width) + "\n" + infoLine
	}
	var parts []string
	if bar := m.imageTable.FilterBar(); m.activeTab == tabImages && bar.IsVisible() {
		parts = append(parts, bar.View())
	}
	if crumb := m.renderRegistryBreadcrumb(width); crumb != "" {
		parts = append(parts, crumb)
	}
	// An error or a notice the user has not read yet comes first: the progress
	// of what is still running is the least urgent of the three, and the
	// component is what enforces that order now.
	parts = append(parts, m.renderTabBar(width), theme.EmptyLineBg(width), m.footer.View(width, m.status()))
	return strings.Join(parts, "\n")
}

// status is what the view derives on every frame: the active tab's load, then
// whatever action is running. Neither has a timer.
func (m Model) status() sharedcomponents.Status {
	if text, ok := m.loadingLabel(); ok {
		return sharedcomponents.Status{Text: text, Spinner: true}
	}
	return sharedcomponents.Status{Text: m.actionLine()}
}

// formStatus is the status while a form or the browser has the viewport. The
// tables behind them are not on screen, so their loads say nothing here; the
// browser's own search does.
func (m Model) formStatus() sharedcomponents.Status {
	if m.registryBrowser != nil {
		if text, ok := m.registryBrowser.LoadingLabel(); ok {
			return sharedcomponents.Status{Text: text, Spinner: true}
		}
	}
	if m.networkInspectForm != nil && m.networkInspectForm.loading {
		return sharedcomponents.Status{Text: "Loading containers...", Spinner: true}
	}
	return sharedcomponents.Status{}
}

// loadingLabel names what the active tab is fetching, or reports that it is
// settled. Only the tab on screen answers: a volumes load says nothing while
// the Images tab is showing.
func (m Model) loadingLabel() (string, bool) {
	switch m.activeTab {
	case tabImages:
		return "Loading images...", m.loading && len(m.images) == 0
	case tabNetworks:
		return "Loading networks...", m.loadingNets && len(m.networkTable.Items()) == 0
	case tabVolumes:
		return "Loading volumes...", m.loadingVols && len(m.volumeTable.Items()) == 0
	}
	return "", false
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
	if m.registryBrowser != nil && m.registryBrowser.state == browserStateTags {
		return []shortcut.HeaderInfo{
			{Key: "Tags", Value: fmt.Sprintf("%d", len(m.registryBrowser.tagTable.Items())), Style: theme.HeaderValueStyle},
		}
	}
	if m.networkInspectForm != nil {
		return []shortcut.HeaderInfo{
			{Key: "Containers", Value: fmt.Sprintf("%d", len(m.networkInspectForm.containers)), Style: theme.HeaderValueStyle},
		}
	}
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
		visible := m.imageTable.Visible()
		total := len(visible)
		var totalDisk int64
		for _, row := range visible {
			totalDisk += row.Image.UniqueSize
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
			// enter is greyed on a tag nothing has scanned yet, rather than
			// appearing and disappearing as the cursor runs down the list
			// (Rule 130).
			return []shortcut.Shortcut{
				{Key: "enter", Description: "View CVE details",
					Disabled: !m.registryBrowser.HasSelectedTagScanResults()},
				{Key: keymap.Scan, Description: "Scan image"},
				{Key: keymap.Get, Description: "Pull image"},
				{Key: "r", Description: "Filter registry"},
				{Key: "/", Description: "Filter"},
				{Key: ".", Description: "Sort"},
				{Key: "esc", Description: "Go back"},
			}
		default: // browserStateInput
			// enter submits, so it is greyed until the submit button has the
			// focus. A control, not an action: greying is the whole of it, and
			// a footer line on every stray enter in a form would be noise.
			return []shortcut.Shortcut{
				{Key: "enter", Description: "Browse tags",
					Disabled: m.registryBrowser.focusedField != m.registryBrowser.brFieldSubmit()},
				{Key: "esc", Description: "Close browser"},
			}
		}
	}
	if m.launchForm != nil {
		lf := m.launchForm
		isCheckbox := lf.isPortField(lf.focusedField) ||
			lf.focusedField == lf.fieldOptionRemove() ||
			lf.focusedField == lf.fieldOptionDetach() ||
			lf.focusedField == lf.fieldOptionInteractive()
		// The three controls a field can take, greyed rather than swapped: the
		// column changed shape on every ↑↓, in a form where the cursor moves
		// constantly (Rule 130).
		return []shortcut.Shortcut{
			{Key: "space", Description: "Toggle", Disabled: !isCheckbox},
			{Key: "←→", Description: "Cycle network", Disabled: lf.focusedField != lf.fieldNetwork()},
			{Key: "enter", Description: "Launch", Disabled: lf.focusedField != lf.fieldSubmit()},
			{Key: "ctrl+y", Description: "Copy command"},
			{Key: "esc", Description: "Cancel"},
		}
	}
	if m.resourceForm != nil {
		// Only a network has a driver to cycle, and only on its second field.
		onDriver := m.resourceForm.kind == resourceFormNetwork && m.resourceForm.focusedField == 1
		return []shortcut.Shortcut{
			{Key: "←→", Description: "Cycle driver", Disabled: !onDriver},
			{Key: "enter", Description: "Submit"},
			{Key: "esc", Description: "Cancel"},
		}
	}
	if m.networkInspectForm != nil {
		return []shortcut.Shortcut{
			{Key: "esc", Description: "Close"},
		}
	}
	if m.confirmModal != nil || m.scanAllModal != nil {
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
		// The three row actions are greyed while the selected image is being
		// scanned, where they used to be replaced by a fake entry — a spinner
		// and the word "scanning...", in the column that lists keys. The row's
		// own Scanned cell already carries that spinner (Rule 139).
		act := m.imageActions()
		base = append(base,
			shortcut.Shortcut{Key: "enter", Description: "Scan details", Disabled: !m.imageOpen().Enabled()},
			shortcut.Shortcut{Key: keymap.New, Description: "Launch", Disabled: !act.Enabled()},
			shortcut.Shortcut{Key: keymap.Scan, Description: "Scan", Disabled: !act.Enabled()},
			shortcut.Shortcut{Key: keymap.Delete, Description: "Delete", Disabled: !act.Enabled()},
			shortcut.Shortcut{Key: keymap.Browser, Description: "Browse registries"},
			shortcut.Shortcut{Key: keymap.ScanAll, Description: "Scan all"},
			shortcut.Shortcut{Key: keymap.Prune, Description: "Prune"},
			shortcut.Shortcut{Key: ".", Description: "Sort"},
			shortcut.Shortcut{Key: "/", Description: "Filter"},
		)
	case tabNetworks:
		base = append(base,
			shortcut.Shortcut{Key: "enter", Description: "Inspect"},
			shortcut.Shortcut{Key: keymap.New, Description: "New network"},
			shortcut.Shortcut{Key: keymap.Delete, Description: "Remove"},
			shortcut.Shortcut{Key: keymap.Prune, Description: "Prune"},
		)
	case tabVolumes:
		base = append(base,
			shortcut.Shortcut{Key: keymap.New, Description: "New volume"},
			shortcut.Shortcut{Key: keymap.Delete, Description: "Remove"},
			shortcut.Shortcut{Key: keymap.Prune, Description: "Prune"},
		)
	case tabRegistries:
		// Inside a group the rows are cached members, not config entries: there
		// is nothing to edit, log into or remove. That is a level of the same
		// table, not a different screen, so the four are greyed rather than
		// replaced by a two-line column (Rule 130).
		entry := m.registryEntryActions()
		refresh := "Refresh"
		if m.registryDrillIn().Enabled() {
			refresh = "Refresh group members"
		}
		return append(base,
			shortcut.Shortcut{Key: keymap.New, Description: "New registry", Disabled: !m.registryCreate().Enabled()},
			shortcut.Shortcut{Key: keymap.Edit, Description: "Edit registry", Disabled: !entry.Enabled()},
			shortcut.Shortcut{Key: keymap.Auth, Description: "Log in or out", Disabled: !entry.Enabled()},
			shortcut.Shortcut{Key: keymap.Delete, Description: "Remove", Disabled: !entry.Enabled()},
			shortcut.Shortcut{Key: "→", Description: "Show members", Disabled: !m.registryDrillIn().Enabled()},
			shortcut.Shortcut{Key: "←", Description: "Back to registries", Disabled: !m.registryDrillOut().Enabled()},
			shortcut.Shortcut{Key: "ctrl+r", Description: refresh},
			shortcut.Shortcut{Key: "?", Description: "Help"},
		)
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
	// Priority 4: whichever modal is open (centered)
	if modal := m.modalView(); modal != "" {
		return lipgloss.Place(
			m.width, m.height,
			lipgloss.Center, lipgloss.Center,
			modal,
			lipgloss.WithWhitespaceBackground(theme.ColorBackground),
		)
	}
	// Priority 5: network inspect form (viewport)
	if m.networkInspectForm != nil {
		return m.networkInspectForm.View()
	}
	// Priority 6: normal view by active tab
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

// renderRegistriesView renders the registries table. An empty table stays a
// table (Rule 139) — the count lives in GetHeaderInfo's "Registries" field.
func (m Model) renderRegistriesView() string {
	return m.registryTable.View()
}

// renderImagesView renders the images table.
//
// The load says so in the footer, with a spinner, and the table stays on
// screen: a body that swapped itself for a spinner lost its header and its
// columns for the length of every refresh. An empty table — loaded and
// genuinely empty, or filtered down to nothing — stays a table too (Rule 139):
// its header and no rows, with the count in GetHeaderInfo's "Images" field.
func (m Model) renderImagesView() string {
	return m.imageTable.View()
}

// renderNetworksView renders the networks table (Rule 139: no body message).
func (m Model) renderNetworksView() string {
	return m.networkTable.View()
}

// renderVolumesView renders the volumes table (Rule 139: no body message).
func (m Model) renderVolumesView() string {
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
			{Key: "N (Images)", Description: "Launch a container from the selected image"},
			{Key: "S (Images)", Description: "Launch a scan for the selected image"},
			{Key: "A (Images)", Description: "Scan every image. The confirmation carries a checkbox to purge the cached results first — unchecked, only what has never been scanned is scanned"},
			{Key: "A (Images)", Description: "Scan all unscanned images using config defaults"},
			{Key: keymap.Delete, Description: "Delete the selected resource (with confirmation)"},
			{Key: keymap.Prune, Description: "Prune unused resources (with confirmation)"},
			{Key: "enter (Networks)", Description: "Inspect the selected network — shows connected containers with IP and MAC addresses"},
			{Key: "N (Networks)", Description: "Create a new network"},
			{Key: "N (Volumes)", Description: "Create a new volume"},
			{Key: "N (Registries)", Description: "Add a registry"},
			{Key: "E (Registries)", Description: "Edit the selected registry"},
			{Key: "U (Registries)", Description: "Log in or out of the selected registry — the direction follows the Logged column, so there is nothing to get wrong"},
			{Key: ".", Description: "Cycle sort column (Images tab only)"},
			{Key: "b (Images)", Description: "Open the multi-registry browser — search tags across all configured registries"},
			{Key: "r (Browser tags)", Description: "Cycle the active registry filter (shows tags from one registry at a time)"},
			{Key: "p (Browser)", Description: "Pull the selected image tag to the local Docker store"},
			{Key: "S (Browser)", Description: "Scan the selected image tag directly via Trivy (no pull required)"},
			{Key: "enter (Browser tags)", Description: "View CVE details for the selected tag (only when scan results are cached)"},
			{Key: "esc (Browser)", Description: "Go back to the previous screen in the registry browser"},
			{Key: "ctrl+r", Description: "Refresh the current tab's data — on a group row, re-run member discovery"},
			{Key: "→ (Registries)", Description: "Show a group's discovered members"},
			{Key: "← (Registries)", Description: "Go back to the registry list"},
			{Key: "/", Description: "Filter images by repository or tag (Images tab only)"},
			{Key: "↑/k", Description: "Move selection up"},
			{Key: "↓/j", Description: "Move selection down"},
			{Key: "ctrl+p", Description: "Open command mode"},
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
					"Press N to create a new network, D to remove the selected one, P to prune all unused networks.",
			},
			{
				Title: "Volumes Tab",
				Body:  "Lists Docker volumes (Name, Driver, Mountpoint). Press N to create a new volume, D to remove the selected one, P to prune all unused volumes.",
			},
			{
				Title: "Registries Tab",
				Body:  "Lists configured Docker/OCI registries. Press N to add a new registry, E to edit, U to log in or out, D to remove. Aliases shorten long registry URLs in the Images tab display.",
			},
			{
				Title: "Registry Browser",
				Body: "Opened from the Images tab with 'b'. Type a repository name (e.g. nginx or library/nginx), " +
					"select which registries to search using Space, then press Browse Tags. " +
					"Results show tags from all selected registries concurrently. " +
					"Press 'r' to cycle the registry filter (one registry at a time), '/' to filter by tag name, '.' to sort. " +
					"Press G to pull the selected tag, S to scan it directly via Trivy (no pull), or Enter to view cached CVE details. " +
					"Press Esc to go back to the search form.",
			},
			{
				Title: "Launch Form",
				Body:  "When launching a container, ports are pre-filled from the image's EXPOSE metadata. Fill in environment variables and volume mounts as comma-separated lists. The container runs detached (-d) by default.",
			},
		},
	}
}
