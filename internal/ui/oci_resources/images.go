package ociresources

import (
	"fmt"

	tea "github.com/charmbracelet/bubbletea"

	"github.com/anthnel/devdesk/internal/docker"
	"github.com/anthnel/devdesk/internal/scan"
	sharedcomponents "github.com/anthnel/devdesk/internal/ui/components"
)

// openLaunchForm fetches exposed ports then opens the launch form
func (m Model) openLaunchForm() (tea.Model, tea.Cmd) {
	img := m.getSelectedImage()
	if img == nil {
		return m, nil
	}
	name := img.Name()
	return m, fetchImageExposedPortsCmd(name)
}

// isSelectedImageScanning returns true if the currently selected image is being scanned.
func (m Model) isSelectedImageScanning() bool {
	img := m.getSelectedImage()
	if img == nil {
		return false
	}
	return m.scanningImages[img.Name()]
}

// getSelectedImage returns the selected image or nil.
//
// The cursor is resolved against the very slice the rows were built from, so it
// cannot point at one ordering while the screen shows another.
func (m Model) getSelectedImage() *docker.Image {
	row, ok := m.imageTable.Selected()
	if !ok {
		return nil
	}
	return &row.Image
}

// deleteSelectedImage shows confirm modal for deletion
func (m Model) deleteSelectedImage() (tea.Model, tea.Cmd) {
	img := m.getSelectedImage()
	if img == nil {
		return m, nil
	}
	name := img.Name()
	m.pendingAction = "delete-image"
	m.confirmModal = sharedcomponents.NewConfirmModal("Delete Image", fmt.Sprintf("Delete '%s'?", name))
	return m, nil
}

// pruneImages shows confirm modal for pruning
func (m Model) pruneImages() (tea.Model, tea.Cmd) {
	m.pendingAction = "prune-images"
	m.confirmModal = sharedcomponents.NewConfirmModal("Prune Images", "Remove all dangling (unused) images?")
	return m, nil
}

// scanSelectedImage launches a scan for the selected image using saved options
func (m Model) scanSelectedImage() (tea.Model, tea.Cmd) {
	img := m.getSelectedImage()
	if img == nil {
		return m, nil
	}
	name := img.Name()
	if m.scanningImages[name] {
		m.infoMsg = "Scan already in progress"
		return m, clearInfoMsgCmd()
	}
	m.scanning = true
	return m, batchScanCmd([]imageScanJob{{Name: name, Target: img.ScanTarget()}}, scan.OptionsFromConfig(m.config.Scan))
}

// scanAllUnscanned triggers batch scanning of all unscanned images using config defaults
func (m Model) scanAllUnscanned() (tea.Model, tea.Cmd) {
	if m.scanning {
		return m, nil
	}
	var jobs []imageScanJob
	for _, img := range m.images {
		if img.Repository == "<none>" {
			continue
		}
		name := img.Name()
		if _, ok := m.scanCache[name]; !ok {
			jobs = append(jobs, imageScanJob{Name: name, Target: img.ScanTarget()})
		}
	}
	if len(jobs) == 0 {
		m.errorMsg = "All images are already scanned"
		return m, clearInfoMsgCmd()
	}
	m.scanning = true
	return m, batchScanCmd(jobs, scan.OptionsFromConfig(m.config.Scan))
}

// requestScanAll launches a batch scan for all images with the configured options.
// Rule 126: purges the in-memory and disk cache before scanning.
func (m Model) requestScanAll() (tea.Model, tea.Cmd) {
	if m.scanning {
		return m, nil
	}
	var jobs []imageScanJob
	var cacheKeys []string
	for _, img := range m.images {
		if img.Repository == "<none>" {
			continue
		}
		name := img.Name()
		jobs = append(jobs, imageScanJob{Name: name, Target: img.ScanTarget()})
		cacheKeys = append(cacheKeys, name)
		delete(m.scanCache, name)
	}
	if len(jobs) == 0 {
		return m, nil
	}
	m.scanning = true
	return m, tea.Batch(deleteScanCacheCmd(cacheKeys), batchScanCmd(jobs, scan.OptionsFromConfig(m.config.Scan)))
}

// handleImageScanStarting marks an image as currently scanning and refreshes the table.
func (m Model) handleImageScanStarting(msg ImageScanStartingMsg) (tea.Model, tea.Cmd) {
	wasScanning := len(m.scanningImages) > 0
	m.scanningImages[msg.ImageName] = true
	m.infoMsg = ""
	m.updateImageTable()
	if m.registryBrowser != nil {
		m.registryBrowser.SetTagScanning(msg.ImageName, true)
	}
	if !wasScanning {
		return m, m.spinner.Tick
	}
	return m, nil
}

// handleImageScanFinished updates scan results for a completed image scan.
func (m Model) handleImageScanFinished(msg ImageScanFinishedMsg) (tea.Model, tea.Cmd) {
	delete(m.scanningImages, msg.ImageName)
	if msg.Err != nil {
		m.failedScans[msg.ImageName] = true
	} else {
		delete(m.failedScans, msg.ImageName)
		m.scanCache[msg.ImageName] = msg.Entry
		m.errorMsg = ""
	}
	m.scanning = len(m.scanningImages) > 0
	m.updateImageTable()
	if m.registryBrowser != nil {
		m.registryBrowser.SetTagScanning(msg.ImageName, false)
		m.registryBrowser.SetScanCache(m.scanCache)
	}
	return m, nil
}

// handleScanRequest rescans one image by name, whoever asked.
//
// The router sends this when a cached scan's stored result has gone missing:
// the row is still in the list, and the scan that would replace it belongs
// here, next to the cache it writes. The name is both the cache key and the
// scan target, as it is for an image typed rather than picked.
func (m Model) handleScanRequest(msg ScanRequestMsg) (tea.Model, tea.Cmd) {
	if msg.ImageName == "" || m.scanningImages[msg.ImageName] {
		return m, nil
	}
	m.scanning = true
	job := imageScanJob{Name: msg.ImageName, Target: msg.ImageName}
	return m, batchScanCmd([]imageScanJob{job}, scan.OptionsFromConfig(m.config.Scan))
}
