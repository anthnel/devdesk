package ociresources

import (
	"log"
	"time"

	tea "charm.land/bubbletea/v2"

	"github.com/anthnel/devdesk/internal/config"
	"github.com/anthnel/devdesk/internal/docker"
	"github.com/anthnel/devdesk/internal/jobs"
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
		// Rebuild so the rows' activity (read by WantsBlink) follows the marker.
		m.updateImageTable()
		return m, removeImageCmd(img.ID, name)
	case "prune-images":
		m.pruning = "images"
		return m, removeImagesCmd(m.pruneTargets)
	case "delete-network":
		net := m.getSelectedNetwork()
		if net == nil {
			return m, nil
		}
		m.networkTable.MarkBusy(net.ID, "Removing "+net.Name)
		return m, removeNetworkCmd(net.ID)
	case "prune-networks":
		m.pruning = "networks"
		return m, pruneNetworksCmd()
	case "delete-volume":
		vol := m.getSelectedVolume()
		if vol == nil {
			return m, nil
		}
		m.volumeTable.MarkBusy(vol.Name, "Removing "+vol.Name)
		return m, removeVolumeCmd(vol.Name)
	case "prune-volumes":
		m.pruning = "volumes"
		return m, removeVolumesCmd(m.pruneTargets)
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
		log.Printf("ERROR [oci_resources] list: %s", docker.LogText(msg.Err))
		// Whatever an agent asked for while this was loading is refused with
		// the reason rather than left waiting (§3.61). The reason is the fixed
		// sentence of an engine that could not be asked, never the engine's text
		// (OC-24): an agent is a client of the MCP server.
		return m, tea.Batch(
			m.footer.Error("Failed to load images — check logs"),
			m.refusePendingRequests(imagesRefusal(msg.Err)),
		)
	}
	m.footer.ClearUnlessRemoval() // the bilan of a P or a D outlives the refresh that follows it
	m.images = msg.Images
	m.listed = true
	now := time.Now()
	check := tea.Batch(checkImageUpdatesCmd(m.updates.Due(pulledImageRefs(m.images), now)),
		m.startLocalSignatureChecks(now))
	m.updateImageTable()
	if cmd := m.drainPendingRequests(); cmd != nil {
		return m, tea.Batch(cmd, check)
	}
	return m, check
}

// pulledImageRefs are the images worth asking a registry about: tagged, and
// pulled — an image with no registry digest was built or loaded here, and its
// name would send the question to a registry that has never heard of it.
func pulledImageRefs(images []docker.Image) []string {
	var refs []string
	for _, img := range images {
		if img.Tag == "" || img.Tag == "<none>" || len(img.RepoDigests) == 0 {
			continue
		}
		refs = append(refs, img.Name())
	}
	return refs
}

// handleImageUpdatesChecked records the registries' answers.
func (m Model) handleImageUpdatesChecked(msg ImageUpdatesCheckedMsg) (tea.Model, tea.Cmd) {
	m.updates.Store(msg.Facts)
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
	m.updateImageTable() // drop the Running activity the marker put on the row
	if msg.Err != nil {
		log.Printf("ERROR [oci_resources] %s %s: %v", msg.Action, msg.Name, msg.Err)
		if msg.Action == "remove" {
			// `D` reads like `P`: an image already gone is an information, one a
			// container uses a refusal that says why (OC-22).
			return m, tea.Batch(m.footer.Removal(docker.SingleRemoval(msg.ID, msg.Err)), fetchImages())
		}
		return m, m.footer.Error("Action failed — check logs")
	}
	m.footer.Clear()
	// The tag is gone: the registry must not keep a failure for it.
	return m, tea.Batch(jobs.ForgetTarget(msg.Name), fetchImages())
}

// handlePruneComplete reports what the removal of the frozen list of images did.
func (m Model) handlePruneComplete(msg PruneCompleteMsg) (tea.Model, tea.Cmd) {
	m.pruning = ""
	m.pruneTargets = nil
	logNotRemoved("images", msg.Report)
	return m, tea.Batch(m.footer.Removal(msg.Report), fetchImages())
}

// logNotRemoved logs what a batch did not remove, one line each.
func logNotRemoved(what string, report docker.RemovalReport) {
	for _, r := range report {
		switch r.Status {
		case docker.RemovalRefused:
			log.Printf("ERROR [oci_resources] prune %s: %q refused (%s): %q", what, r.Target, r.Reason, r.Err.Error())
		case docker.RemovalGone:
			log.Printf("INFO [oci_resources] prune %s: %q was already gone", what, r.Target)
		}
	}
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
	m.footer.ClearUnlessRemoval()
	m.volumeTable.SetItems(msg.Volumes)

	return m, nil
}

// handleVolumeAction processes volume action results
func (m Model) handleVolumeAction(msg VolumeActionMsg) (tea.Model, tea.Cmd) {
	m.volumeTable.ClearBusy(msg.Name)
	if msg.Err != nil {
		log.Printf("ERROR [oci_resources] volume %s: %v", msg.Action, msg.Err)
		if msg.Action == "remove" {
			return m, tea.Batch(m.footer.Removal(docker.SingleRemoval(msg.Name, msg.Err)), fetchVolumes())
		}
		return m, m.footer.Error("Volume action failed — check logs")
	}
	m.footer.Clear()
	return m, fetchVolumes()
}

// handleVolumePruneComplete reports what the removal of the frozen list of volumes did.
func (m Model) handleVolumePruneComplete(msg VolumePruneCompleteMsg) (tea.Model, tea.Cmd) {
	m.pruning = ""
	m.pruneTargets = nil
	logNotRemoved("volumes", msg.Report)
	return m, tea.Batch(m.footer.Removal(msg.Report), fetchVolumes())
}

// imagesRefusal is the reason an agent's pending request is refused with when the
// image list failed: the fixed sentence when the engine could not be asked, a
// generic one otherwise.
func imagesRefusal(err error) string {
	if message, ok := docker.UnavailableMessage(err); ok {
		return "Cannot list images: " + message
	}
	return "Cannot list images — check logs"
}
