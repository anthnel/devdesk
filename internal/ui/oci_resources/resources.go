package ociresources

import (
	"fmt"
	"log"
	"slices"

	tea "charm.land/bubbletea/v2"

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
		return m, m.footer.Error("Failed to inspect network — check logs")
	}
	m.networkInspectForm.SetContainers(msg.Containers)
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
	if m.networkTable.IsBusy(net.ID) {
		return m, m.footer.Warn(busyMessage)
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
	if m.volumeTable.IsBusy(vol.Name) {
		return m, m.footer.Warn(busyMessage)
	}
	m.pendingAction = "delete-volume"
	m.confirmModal = sharedcomponents.NewConfirmModal("Remove Volume", fmt.Sprintf("Remove volume '%s'?", vol.Name))
	return m, nil
}

// pruneVolumes starts the preview of `P` on the Volumes tab: it asks the engine
// which containers use which volumes, stopped ones included (VolumePruneUsersMsg),
// and the confirmation opens from the answer. Nothing is removed here.
func (m Model) pruneVolumes() (tea.Model, tea.Cmd) {
	if m.pruning != "" {
		return m, m.footer.Warn(busyMessage)
	}
	m.pruning = "volumes"
	return m, readVolumeUsersCmd(slices.Clone(m.volumeTable.Items()))
}

// handleVolumePruneUsers freezes the volumes nobody uses and asks for confirmation.
// When the engine could not say who uses what, nothing is unused: no confirmation
// opens.
func (m Model) handleVolumePruneUsers(msg VolumePruneUsersMsg) (tea.Model, tea.Cmd) {
	m.pruning = ""
	if msg.Err != nil {
		log.Printf("ERROR [oci_resources] prune volumes: reading the volume users: %v", msg.Err)
		return m, m.footer.Error("Cannot tell which volumes are in use — check logs")
	}
	m.pruneTargets = nil
	for _, v := range docker.UnusedVolumes(msg.Volumes, msg.Users) {
		if !docker.IsVolumeName(v.Name) {
			log.Printf("ERROR [oci_resources] prune: skipped a volume with a malformed name %q", v.Name)
			continue
		}
		m.pruneTargets = append(m.pruneTargets, v.Name)
	}
	if len(m.pruneTargets) == 0 {
		return m, m.footer.Info("No unused volumes to remove")
	}
	m.pendingAction = "prune-volumes"
	m.confirmModal = sharedcomponents.NewConfirmModal("Prune Volumes", sharedcomponents.RemovalPreview("unused volumes", m.pruneTargets))
	return m, nil
}
