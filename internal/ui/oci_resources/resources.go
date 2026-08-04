package ociresources

import (
	"fmt"
	"log"

	tea "github.com/charmbracelet/bubbletea"

	"github.com/anthnel/devdesk/internal/docker"
	sharedcomponents "github.com/anthnel/devdesk/internal/ui/components"
)

// handleNetworkInspectLoaded processes the result of a network inspect command.
func (m Model) handleNetworkInspectLoaded(msg NetworkInspectLoadedMsg) (tea.Model, tea.Cmd) {
	if m.networkInspectForm == nil || m.networkInspectForm.networkID != msg.NetworkID {
		return m, nil
	}
	if msg.Err != nil {
		log.Printf("ERROR [oci_resources] network inspect %s: %v", msg.NetworkID, msg.Err)
		m.networkInspectForm = nil
		m.errorMsg = "Failed to inspect network — check logs"
		return m, clearInfoMsgCmd()
	}
	m.networkInspectForm.SetContainers(msg.Containers)
	return m, nil
}

// handleDiagnosticTestComplete processes the result of a connectivity test.
func (m Model) handleDiagnosticTestComplete(msg DiagnosticTestCompleteMsg) (tea.Model, tea.Cmd) {
	if m.connectivityForm != nil {
		m.connectivityForm.SetResult(msg.Output, msg.Err)
	}
	return m, nil
}

// openNetworkInspect opens the network inspect overlay for the selected network.
func (m Model) openNetworkInspect() (tea.Model, tea.Cmd) {
	net := m.getSelectedNetwork()
	if net == nil {
		return m, nil
	}
	m.networkInspectForm = newNetworkInspectForm(net.ID, net.Name, m.width-2, m.height)
	return m, inspectNetworkCmd(net.ID, net.Name)
}

// getSelectedNetwork returns the selected network or nil.
//
// The cursor is resolved by the table against the very slice it built its rows
// from, so this can no longer disagree with what is on screen — which is what
// the nine hand-written versions of this could.
func (m Model) getSelectedNetwork() *docker.Network {
	net, ok := m.networkTable.Selected()
	if !ok {
		return nil
	}
	return &net
}

// getSelectedVolume returns the selected volume or nil
func (m Model) getSelectedVolume() *docker.Volume {
	vol, ok := m.volumeTable.Selected()
	if !ok {
		return nil
	}
	return &vol
}

// deleteSelectedNetwork shows confirm modal for network deletion
func (m Model) deleteSelectedNetwork() (tea.Model, tea.Cmd) {
	net := m.getSelectedNetwork()
	if net == nil {
		return m, nil
	}
	m.pendingAction = "delete-network"
	m.confirmModal = sharedcomponents.NewConfirmModal("Remove Network", fmt.Sprintf("Remove network '%s'?", net.Name))
	return m, nil
}

// deleteSelectedVolume shows confirm modal for volume deletion
func (m Model) deleteSelectedVolume() (tea.Model, tea.Cmd) {
	vol := m.getSelectedVolume()
	if vol == nil {
		return m, nil
	}
	m.pendingAction = "delete-volume"
	m.confirmModal = sharedcomponents.NewConfirmModal("Remove Volume", fmt.Sprintf("Remove volume '%s'?", vol.Name))
	return m, nil
}
