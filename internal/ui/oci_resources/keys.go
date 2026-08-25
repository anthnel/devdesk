package ociresources

import (
	tea "github.com/charmbracelet/bubbletea"

	sharedcomponents "github.com/anthnel/devdesk/internal/ui/components"
	"github.com/anthnel/devdesk/internal/ui/keymap"
)

// handleImagesKeyMsg handles keys on the Images tab
func (m Model) handleImagesKeyMsg(msg tea.KeyMsg) (tea.Model, tea.Cmd) {
	switch msg.String() {
	case keymap.Browser:
		return m.openMultiRegistryBrowser()
	case "enter":
		if open := m.imageOpen(); !open.Enabled() {
			return m, m.footer.Warn(open.Reason)
		}
		name := m.getSelectedImage().Name()
		return m, func() tea.Msg { return ScanDetailsRequestMsg{ImageName: name} }
	// Launching a container from an image is "create a resource from the
	// selected row", and nothing else is created from this tab — so it is N,
	// with no collision (§3.26).
	//
	// The three row actions share one guard and one reason (Rule 130): they
	// used to return in silence, with the header dropping them and putting a
	// spinner in the shortcut column in their place.
	case keymap.New:
		if act := m.imageActions(); !act.Enabled() {
			return m, m.footer.Warn(act.Reason)
		}
		return m.openLaunchForm()
	case keymap.Delete:
		if act := m.imageActions(); !act.Enabled() {
			return m, m.footer.Warn(act.Reason)
		}
		return m.deleteSelectedImage()
	case keymap.Prune:
		return m.pruneImages()
	case keymap.Scan:
		if act := m.imageActions(); !act.Enabled() {
			return m, m.footer.Warn(act.Reason)
		}
		return m.scanSelectedImage()
	case keymap.ScanAll:
		return m.confirmScanAll()
	case "ctrl+r":
		m.loading = true
		return m, tea.Batch(m.spinner.Tick, fetchImages(), loadScanCache())
	}
	// Navigation, `/` and `.` are the table's, not the view's.
	return m, m.imageTable.Update(msg)
}

// handleNetworkInspectKeyMsg handles keys when the network inspect overlay is open.
func (m Model) handleNetworkInspectKeyMsg(msg tea.KeyMsg) (tea.Model, tea.Cmd) {
	switch msg.String() {
	case "esc":
		m.networkInspectForm = nil
		return m, nil
	case "c":
		sel := m.networkInspectForm.SelectedContainer()
		containerName := ""
		if sel != nil {
			containerName = sel.Name
		}
		m.connectivityForm = newConnectivityTestForm(
			containerName,
			m.networkInspectForm.networkID,
			m.config.Network.ToolImage,
			m.networkInspectForm.containers,
			m.width-2,
			m.height,
		)
		return m, nil
	}
	var cmd tea.Cmd
	m.networkInspectForm, cmd = m.networkInspectForm.Update(msg)
	return m, cmd
}

// handleConnectivityFormKeyMsg handles keys when the connectivity test form is open.
func (m Model) handleConnectivityFormKeyMsg(msg tea.KeyMsg) (tea.Model, tea.Cmd) {
	if msg.String() == "esc" {
		m.connectivityForm = nil
		return m, nil
	}
	var cmd tea.Cmd
	m.connectivityForm, cmd = m.connectivityForm.Update(msg)
	return m, cmd
}

// handleNetworksKeyMsg handles keys on the Networks tab
func (m Model) handleNetworksKeyMsg(msg tea.KeyMsg) (tea.Model, tea.Cmd) {
	switch msg.String() {
	case "enter":
		return m.openNetworkInspect()
	case keymap.New:
		m.resourceForm = NewResourceForm(resourceFormNetwork, m.width-2)
		return m, nil
	case keymap.Delete:
		return m.deleteSelectedNetwork()
	case keymap.Prune:
		m.pendingAction = "prune-networks"
		m.confirmModal = sharedcomponents.NewConfirmModal("Prune Networks", "Remove all unused networks?")
		return m, nil
	case "ctrl+r":
		m.loadingNets = true
		return m, tea.Batch(m.spinner.Tick, fetchNetworks())
	}
	// Anything the tab does not claim goes to the table. It used to be an
	// allow-list of four navigation keys followed by `return m, nil`, which is
	// why pgup/pgdown died here: datatable has handled them since it existed,
	// but they were never among the four. Forwarding by default is the shape
	// that cannot go stale — the next key datatable gains arrives working.
	return m, m.networkTable.Update(msg)
}

// handleVolumesKeyMsg handles keys on the Volumes tab
func (m Model) handleVolumesKeyMsg(msg tea.KeyMsg) (tea.Model, tea.Cmd) {
	switch msg.String() {
	case keymap.New:
		m.resourceForm = NewResourceForm(resourceFormVolume, m.width-2)
		return m, nil
	case keymap.Delete:
		return m.deleteSelectedVolume()
	case keymap.Prune:
		m.pendingAction = "prune-volumes"
		m.confirmModal = sharedcomponents.NewConfirmModal("Prune Volumes", "Remove all unused volumes?")
		return m, nil
	case "ctrl+r":
		m.loadingVols = true
		return m, tea.Batch(m.spinner.Tick, fetchVolumes())
	}
	return m, m.volumeTable.Update(msg)
}

// handleRegistriesKeyMsg handles keys on the Registries tab
func (m Model) handleRegistriesKeyMsg(msg tea.KeyMsg) (tea.Model, tea.Cmd) {
	switch msg.String() {
	case keymap.New:
		// A member is discovered, not declared, so there is nothing to add here.
		if create := m.registryCreate(); !create.Enabled() {
			return m, m.footer.Warn(create.Reason)
		}
		m.registryForm = NewRegistryForm(m.registries, m.width-2)
		return m, nil
	case keymap.Edit:
		return m.editSelectedRegistry()
	// One key for both directions, because the row already says which way it
	// goes: `l` and `L` were login and logout, a distinction carried by the
	// shift alone on two operations that are each other's opposite (§3.26).
	case keymap.Auth:
		return m.toggleSelectedRegistryAuth()
	case keymap.Delete:
		return m.deleteSelectedRegistry()
	case "ctrl+r":
		return m.refreshRegistries()
	case "right":
		return m.enterSelectedGroup()
	case "left", "esc":
		return m.leaveGroup()
	}
	return m, m.registryTable.Update(msg)
}
