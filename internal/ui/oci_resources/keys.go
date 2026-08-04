package ociresources

import (
	tea "github.com/charmbracelet/bubbletea"

	sharedcomponents "github.com/anthnel/devdesk/internal/ui/components"
)

// handleImagesKeyMsg handles keys on the Images tab
func (m Model) handleImagesKeyMsg(msg tea.KeyMsg) (tea.Model, tea.Cmd) {
	switch msg.String() {
	case "/":
		return m, m.filterBar.ActivateSearch()
	case "b":
		return m.openMultiRegistryBrowser()
	case "enter":
		img := m.getSelectedImage()
		if img == nil {
			return m, nil
		}
		name := img.Name()
		return m, func() tea.Msg { return ScanDetailsRequestMsg{ImageName: name} }
	case "ctrl+e":
		if m.isSelectedImageScanning() {
			return m, nil
		}
		return m.openLaunchForm()
	case "ctrl+d":
		if m.isSelectedImageScanning() {
			return m, nil
		}
		return m.deleteSelectedImage()
	case "p":
		return m.pruneImages()
	case "ctrl+s":
		if m.isSelectedImageScanning() {
			return m, nil
		}
		return m.scanSelectedImage()
	case "A":
		return m.scanAllUnscanned()
	case "ctrl+a":
		return m.requestScanAll()
	case ".":
		return m.cycleSort()
	case "ctrl+r":
		m.loading = true
		return m, tea.Batch(m.spinner.Tick, fetchImages(), loadScanCache())
	case "up", "k":
		m.imageTable.MoveUp(1)
		return m, nil
	case "down", "j":
		m.imageTable.MoveDown(1)
		return m, nil
	case "g", "home":
		m.imageTable.GotoTop()
		return m, nil
	case "G", "end":
		m.imageTable.GotoBottom()
		return m, nil
	}
	return m, nil
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
			m.config.Docker.NetworkToolImage,
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
	case "ctrl+n":
		m.resourceForm = NewResourceForm(resourceFormNetwork, m.width-2)
		return m, nil
	case "ctrl+d":
		return m.deleteSelectedNetwork()
	case "p":
		m.pendingAction = "prune-networks"
		m.confirmModal = sharedcomponents.NewConfirmModal("Prune Networks", "Remove all unused networks?")
		return m, nil
	case "ctrl+r":
		m.loadingNets = true
		return m, tea.Batch(m.spinner.Tick, fetchNetworks())
	case "up", "k":
		m.networkTable.MoveUp(1)
		return m, nil
	case "down", "j":
		m.networkTable.MoveDown(1)
		return m, nil
	case "g", "home":
		m.networkTable.GotoTop()
		return m, nil
	case "G", "end":
		m.networkTable.GotoBottom()
		return m, nil
	}
	return m, nil
}

// handleVolumesKeyMsg handles keys on the Volumes tab
func (m Model) handleVolumesKeyMsg(msg tea.KeyMsg) (tea.Model, tea.Cmd) {
	switch msg.String() {
	case "ctrl+n":
		m.resourceForm = NewResourceForm(resourceFormVolume, m.width-2)
		return m, nil
	case "ctrl+d":
		return m.deleteSelectedVolume()
	case "p":
		m.pendingAction = "prune-volumes"
		m.confirmModal = sharedcomponents.NewConfirmModal("Prune Volumes", "Remove all unused volumes?")
		return m, nil
	case "ctrl+r":
		m.loadingVols = true
		return m, tea.Batch(m.spinner.Tick, fetchVolumes())
	case "up", "k":
		m.volumeTable.MoveUp(1)
		return m, nil
	case "down", "j":
		m.volumeTable.MoveDown(1)
		return m, nil
	case "g", "home":
		m.volumeTable.GotoTop()
		return m, nil
	case "G", "end":
		m.volumeTable.GotoBottom()
		return m, nil
	}
	return m, nil
}

// handleRegistriesKeyMsg handles keys on the Registries tab
func (m Model) handleRegistriesKeyMsg(msg tea.KeyMsg) (tea.Model, tea.Cmd) {
	switch msg.String() {
	case "ctrl+n":
		// A member is discovered, not declared, so there is nothing to add here.
		if m.registryGroupSlug != "" {
			return m, nil
		}
		m.registryForm = NewRegistryForm(m.registries, m.width-2)
		return m, nil
	case "e":
		return m.editSelectedRegistry()
	case "l":
		return m.loginSelectedRegistry()
	case "L":
		return m.logoutSelectedRegistry()
	case "ctrl+d":
		return m.deleteSelectedRegistry()
	case "ctrl+r":
		return m.refreshRegistries()
	// Rule 111 offers `l`/`h` as aliases for →/←, but `l` is login on this tab
	// and a key has one role (Rule 135). The arrows are the drill-down.
	case "right":
		return m.enterSelectedGroup()
	case "left", "esc":
		return m.leaveGroup()
	case "up", "k":
		m.registryTable.MoveUp(1)
		return m, nil
	case "down", "j":
		m.registryTable.MoveDown(1)
		return m, nil
	case "g", "home":
		m.registryTable.GotoTop()
		return m, nil
	case "G", "end":
		m.registryTable.GotoBottom()
		return m, nil
	}
	return m, nil
}

// handleSelectionKeyMsg handles keys when in selection mode
func (m Model) handleSelectionKeyMsg(msg tea.KeyMsg) (tea.Model, tea.Cmd) {
	switch msg.String() {
	case "esc":
		return m, func() tea.Msg { return SelectionCancelledMsg{} }
	case "enter":
		img := m.getSelectedImage()
		if img == nil {
			return m, nil
		}
		name := img.Name()
		return m, func() tea.Msg { return ImageSelectedMsg{ImageName: name} }
	case "/":
		return m, m.filterBar.ActivateSearch()
	case ".":
		return m.cycleSort()
	case "up", "k":
		m.imageTable.MoveUp(1)
		return m, nil
	case "down", "j":
		m.imageTable.MoveDown(1)
		return m, nil
	case "g", "home":
		m.imageTable.GotoTop()
		return m, nil
	case "G", "end":
		m.imageTable.GotoBottom()
		return m, nil
	}
	return m, nil
}
