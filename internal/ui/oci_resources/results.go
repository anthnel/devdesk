package ociresources

import (
	"log"

	tea "github.com/charmbracelet/bubbletea"

	"github.com/anthnel/devdesk/internal/config"
)

// handleConfirmYes executes the pending action
func (m Model) handleConfirmYes() (tea.Model, tea.Cmd) {
	action := m.pendingAction
	m.confirmModal = nil
	m.pendingAction = ""

	switch action {
	case "delete-image":
		img := m.getSelectedImage()
		if img == nil {
			return m, nil
		}
		name := img.Name()
		m.imageTable.MarkBusy(img.ID, "Removing "+name)
		return m, tea.Batch(removeImageCmd(img.ID, name), m.busyTick())
	case "prune-images":
		m.pruning = "images"
		return m, tea.Batch(pruneImagesCmd(), m.busyTick())
	case "delete-network":
		net := m.getSelectedNetwork()
		if net == nil {
			return m, nil
		}
		m.networkTable.MarkBusy(net.ID, "Removing "+net.Name)
		return m, tea.Batch(removeNetworkCmd(net.ID), m.busyTick())
	case "prune-networks":
		m.pruning = "networks"
		return m, tea.Batch(pruneNetworksCmd(), m.busyTick())
	case "delete-volume":
		vol := m.getSelectedVolume()
		if vol == nil {
			return m, nil
		}
		m.volumeTable.MarkBusy(vol.Name, "Removing "+vol.Name)
		return m, tea.Batch(removeVolumeCmd(vol.Name), m.busyTick())
	case "prune-volumes":
		m.pruning = "volumes"
		return m, tea.Batch(pruneVolumesCmd(), m.busyTick())
	case "delete-registry":
		idx := m.getSelectedRegistryIndex()
		if idx < 0 {
			return m, nil
		}
		regs := m.config.Registry.Registries
		m.config.Registry.Registries = append(regs[:idx], regs[idx+1:]...)
		m.registries = m.config.Registry.Registries
		m.updateRegistryTable()
		if err := config.Save(m.config); err != nil {
			log.Printf("ERROR [oci_resources] delete registry: %v", err)
			// The timer was missing here: the message was set and left until
			// something else happened to clear it (Rule 128).
			return m, m.footer.Error("Failed to save config — check logs")
		}
		return m, nil
	}
	return m, nil
}

// handleImagesList processes the image list response
func (m Model) handleImagesList(msg ImagesListMsg) (tea.Model, tea.Cmd) {
	m.loading = false
	if msg.Err != nil {
		log.Printf("ERROR [oci_resources] list: %v", msg.Err)
		return m, m.footer.Error("Failed to load images — check logs")
	}
	m.footer.Clear()
	m.images = msg.Images
	m.updateImageTable()
	return m, nil
}

// handleScanCacheLoaded updates the scan cache data
func (m Model) handleScanCacheLoaded(msg ScanCacheLoadedMsg) (tea.Model, tea.Cmd) {
	if msg.Entries != nil {
		m.scanCache = msg.Entries
	}
	m.updateImageTable()
	return m, nil
}

// handleImageAction processes action results.
//
// The marker is lifted first, and on every outcome. Clearing it only on success
// would leave the row spinning for the life of the view.
func (m Model) handleImageAction(msg ImageActionMsg) (tea.Model, tea.Cmd) {
	m.imageTable.ClearBusy(msg.ID)
	if msg.Err != nil {
		log.Printf("ERROR [oci_resources] %s %s: %v", msg.Action, msg.Name, msg.Err)
		return m, m.footer.Error("Action failed — check logs")
	}
	m.footer.Clear()
	return m, fetchImages()
}

// handlePruneComplete processes prune results
func (m Model) handlePruneComplete(msg PruneCompleteMsg) (tea.Model, tea.Cmd) {
	m.pruning = ""
	if msg.Err != nil {
		log.Printf("ERROR [oci_resources] prune: %v", msg.Err)
		return m, m.footer.Error("Prune failed — check logs")
	}
	m.footer.Clear()
	return m, fetchImages()
}

// handleNetworksList processes the network list response
func (m Model) handleNetworksList(msg NetworksListMsg) (tea.Model, tea.Cmd) {
	m.loadingNets = false
	if msg.Err != nil {
		log.Printf("ERROR [oci_resources] network list: %v", msg.Err)
		return m, m.footer.Error("Failed to load networks — check logs")
	}
	m.footer.Clear()
	m.networkTable.SetItems(msg.Networks)

	return m, nil
}

// handleNetworkAction processes network action results
func (m Model) handleNetworkAction(msg NetworkActionMsg) (tea.Model, tea.Cmd) {
	m.networkTable.ClearBusy(msg.ID)
	if msg.Err != nil {
		log.Printf("ERROR [oci_resources] network %s: %v", msg.Action, msg.Err)
		return m, m.footer.Error("Network action failed — check logs")
	}
	m.footer.Clear()
	return m, fetchNetworks()
}

// handleNetworkPruneComplete processes network prune results
func (m Model) handleNetworkPruneComplete(msg NetworkPruneCompleteMsg) (tea.Model, tea.Cmd) {
	m.pruning = ""
	if msg.Err != nil {
		log.Printf("ERROR [oci_resources] network prune: %v", msg.Err)
		return m, m.footer.Error("Network prune failed — check logs")
	}
	m.footer.Clear()
	return m, fetchNetworks()
}

// handleVolumesList processes the volume list response
func (m Model) handleVolumesList(msg VolumesListMsg) (tea.Model, tea.Cmd) {
	m.loadingVols = false
	if msg.Err != nil {
		log.Printf("ERROR [oci_resources] volume list: %v", msg.Err)
		return m, m.footer.Error("Failed to load volumes — check logs")
	}
	m.footer.Clear()
	m.volumeTable.SetItems(msg.Volumes)

	return m, nil
}

// handleVolumeAction processes volume action results
func (m Model) handleVolumeAction(msg VolumeActionMsg) (tea.Model, tea.Cmd) {
	m.volumeTable.ClearBusy(msg.Name)
	if msg.Err != nil {
		log.Printf("ERROR [oci_resources] volume %s: %v", msg.Action, msg.Err)
		return m, m.footer.Error("Volume action failed — check logs")
	}
	m.footer.Clear()
	return m, fetchVolumes()
}

// handleVolumePruneComplete processes volume prune results
func (m Model) handleVolumePruneComplete(msg VolumePruneCompleteMsg) (tea.Model, tea.Cmd) {
	m.pruning = ""
	if msg.Err != nil {
		log.Printf("ERROR [oci_resources] volume prune: %v", msg.Err)
		return m, m.footer.Error("Volume prune failed — check logs")
	}
	m.footer.Clear()
	return m, fetchVolumes()
}
