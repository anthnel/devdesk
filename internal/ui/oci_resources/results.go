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
		return m, removeImageCmd(img.ID, name)
	case "prune-images":
		return m, pruneImagesCmd()
	case "delete-network":
		net := m.getSelectedNetwork()
		if net == nil {
			return m, nil
		}
		return m, removeNetworkCmd(net.ID)
	case "prune-networks":
		return m, pruneNetworksCmd()
	case "delete-volume":
		vol := m.getSelectedVolume()
		if vol == nil {
			return m, nil
		}
		return m, removeVolumeCmd(vol.Name)
	case "prune-volumes":
		return m, pruneVolumesCmd()
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
			m.errorMsg = "Failed to save config — check logs"
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
		m.errorMsg = "Failed to load images — check logs"
		return m, clearInfoMsgCmd()
	}
	m.errorMsg = ""
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

// handleImageAction processes action results
func (m Model) handleImageAction(msg ImageActionMsg) (tea.Model, tea.Cmd) {
	if msg.Err != nil {
		log.Printf("ERROR [oci_resources] %s %s: %v", msg.Action, msg.ID, msg.Err)
		m.errorMsg = "Action failed — check logs"
		return m, clearInfoMsgCmd()
	}
	m.errorMsg = ""
	return m, fetchImages()
}

// handlePruneComplete processes prune results
func (m Model) handlePruneComplete(msg PruneCompleteMsg) (tea.Model, tea.Cmd) {
	if msg.Err != nil {
		log.Printf("ERROR [oci_resources] prune: %v", msg.Err)
		m.errorMsg = "Prune failed — check logs"
		return m, clearInfoMsgCmd()
	}
	m.errorMsg = ""
	return m, fetchImages()
}

// handleNetworksList processes the network list response
func (m Model) handleNetworksList(msg NetworksListMsg) (tea.Model, tea.Cmd) {
	m.loadingNets = false
	if msg.Err != nil {
		log.Printf("ERROR [oci_resources] network list: %v", msg.Err)
		m.errorMsg = "Failed to load networks — check logs"
		return m, clearInfoMsgCmd()
	}
	m.errorMsg = ""
	m.networkTable.SetItems(msg.Networks)

	return m, nil
}

// handleNetworkAction processes network action results
func (m Model) handleNetworkAction(msg NetworkActionMsg) (tea.Model, tea.Cmd) {
	if msg.Err != nil {
		log.Printf("ERROR [oci_resources] network %s: %v", msg.Action, msg.Err)
		m.errorMsg = "Network action failed — check logs"
		return m, clearInfoMsgCmd()
	}
	m.errorMsg = ""
	return m, fetchNetworks()
}

// handleNetworkPruneComplete processes network prune results
func (m Model) handleNetworkPruneComplete(msg NetworkPruneCompleteMsg) (tea.Model, tea.Cmd) {
	if msg.Err != nil {
		log.Printf("ERROR [oci_resources] network prune: %v", msg.Err)
		m.errorMsg = "Network prune failed — check logs"
		return m, clearInfoMsgCmd()
	}
	m.errorMsg = ""
	return m, fetchNetworks()
}

// handleVolumesList processes the volume list response
func (m Model) handleVolumesList(msg VolumesListMsg) (tea.Model, tea.Cmd) {
	m.loadingVols = false
	if msg.Err != nil {
		log.Printf("ERROR [oci_resources] volume list: %v", msg.Err)
		m.errorMsg = "Failed to load volumes — check logs"
		return m, clearInfoMsgCmd()
	}
	m.errorMsg = ""
	m.volumeTable.SetItems(msg.Volumes)

	return m, nil
}

// handleVolumeAction processes volume action results
func (m Model) handleVolumeAction(msg VolumeActionMsg) (tea.Model, tea.Cmd) {
	if msg.Err != nil {
		log.Printf("ERROR [oci_resources] volume %s: %v", msg.Action, msg.Err)
		m.errorMsg = "Volume action failed — check logs"
		return m, clearInfoMsgCmd()
	}
	m.errorMsg = ""
	return m, fetchVolumes()
}

// handleVolumePruneComplete processes volume prune results
func (m Model) handleVolumePruneComplete(msg VolumePruneCompleteMsg) (tea.Model, tea.Cmd) {
	if msg.Err != nil {
		log.Printf("ERROR [oci_resources] volume prune: %v", msg.Err)
		m.errorMsg = "Volume prune failed — check logs"
		return m, clearInfoMsgCmd()
	}
	m.errorMsg = ""
	return m, fetchVolumes()
}
