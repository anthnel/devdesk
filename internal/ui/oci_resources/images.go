package ociresources

import (
	"fmt"

	tea "github.com/charmbracelet/bubbletea"

	"github.com/anthnel/devdesk/internal/docker"
	"github.com/anthnel/devdesk/internal/jobs"
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
	if m.imageTable.IsBusy(img.ID) {
		return m, m.footer.Warn(busyMessage)
	}
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
	if m.scanningImage(name) {
		return m, m.footer.Warn("Scan already in progress")
	}
	return m, jobs.Start(
		m.scanRun([]string{name}),
		batchScanCmd([]imageScanJob{{Name: name, Target: img.ScanTarget()}}, scan.OptionsFromConfig(m.config)),
	)
}

// scanAllUnscanned triggers batch scanning of all unscanned images using config defaults
// confirmScanAll asks before scanning every image, and the purge is the modal's
// checkbox rather than a second key.
//
// A and ctrl+a differed only by a modifier, and nothing in their shape said
// which one purged the cache — the closest this application came to losing data
// by accident (§3.26). The destructive half is a deliberate gesture now.
func (m Model) confirmScanAll() (tea.Model, tea.Cmd) {
	if m.anyScanRunning() {
		return m, m.footer.Warn("A scan is already running")
	}
	m.scanAllModal = sharedcomponents.NewOptionConfirmModal(
		"Scan All",
		"Scan every image in this list?",
		"Purge cached results first (rescans everything)",
	)
	return m, nil
}

// scanAll scans the whole list when the cache was purged, and only what has
// never been scanned otherwise.
func (m Model) scanAll(purge bool) (tea.Model, tea.Cmd) {
	if purge {
		return m.requestScanAll()
	}
	return m.scanAllUnscanned()
}

func (m Model) scanAllUnscanned() (tea.Model, tea.Cmd) {
	if m.anyScanRunning() {
		return m, nil
	}
	var queue []imageScanJob
	for _, img := range m.images {
		if img.Repository == "<none>" {
			continue
		}
		name := img.Name()
		if _, ok := m.scanCache[name]; !ok {
			queue = append(queue, imageScanJob{Name: name, Target: img.ScanTarget()})
		}
	}
	if len(queue) == 0 {
		return m, m.footer.Warn("All images are already scanned")
	}
	return m, jobs.Start(
		m.scanRun(jobNames(queue)),
		batchScanCmd(queue, scan.OptionsFromConfig(m.config)),
	)
}

// requestScanAll launches a batch scan for all images with the configured options.
// Rule 126: purges the in-memory and disk cache before scanning.
func (m Model) requestScanAll() (tea.Model, tea.Cmd) {
	if m.anyScanRunning() {
		return m, nil
	}
	var queue []imageScanJob
	var cacheKeys []string
	for _, img := range m.images {
		if img.Repository == "<none>" {
			continue
		}
		name := img.Name()
		queue = append(queue, imageScanJob{Name: name, Target: img.ScanTarget()})
		cacheKeys = append(cacheKeys, name)
		delete(m.scanCache, name)
	}
	if len(queue) == 0 {
		return m, nil
	}
	return m, tea.Batch(
		deleteScanCacheCmd(cacheKeys),
		jobs.Start(m.scanRun(cacheKeys), batchScanCmd(queue, scan.OptionsFromConfig(m.config))),
	)
}

// handleImageScanStarting drops whatever the footer still said about the last
// attempt.
//
// The row is already spinning: the router recorded the transition before handing
// the message on, and the snapshot that carried it rebuilt the table. There is
// no chain to restart either — the router holds the only one (D5).
func (m Model) handleImageScanStarting(_ ImageScanStartingMsg) (tea.Model, tea.Cmd) {
	m.footer.Clear()
	return m, nil
}

// handleImageScanFinished updates scan results for a completed image scan.
func (m Model) handleImageScanFinished(msg ImageScanFinishedMsg) (tea.Model, tea.Cmd) {
	if msg.Err != nil {
		m.failedScans[msg.ImageName] = true
	} else {
		delete(m.failedScans, msg.ImageName)
		m.scanCache[msg.ImageName] = msg.Entry
		m.footer.Clear()
	}
	m.updateImageTable()
	if m.registryBrowser != nil {
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
	if msg.ImageName == "" || m.scanningImage(msg.ImageName) {
		return m, nil
	}
	job := imageScanJob{Name: msg.ImageName, Target: msg.ImageName}
	return m, jobs.Start(
		m.scanRun([]string{msg.ImageName}),
		batchScanCmd([]imageScanJob{job}, scan.OptionsFromConfig(m.config)),
	)
}

// jobNames is the target list of a queue of scan jobs, in order.
func jobNames(queue []imageScanJob) []string {
	out := make([]string, 0, len(queue))
	for _, job := range queue {
		out = append(out, job.Name)
	}
	return out
}
