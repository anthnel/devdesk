package ociresources

import (
	"fmt"
	"log"
	"slices"
	"strings"

	tea "charm.land/bubbletea/v2"

	"github.com/anthnel/devdesk/internal/docker"
	"github.com/anthnel/devdesk/internal/jobs"
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

// pruneImages freezes the dangling images no container uses and asks for
// confirmation. What is removed is that list and nothing else (OC-22); with none
// there is nothing to confirm.
func (m Model) pruneImages() (tea.Model, tea.Cmd) {
	m.pruneTargets = danglingImageIDs(m.images)
	if len(m.pruneTargets) == 0 {
		return m, m.footer.Info("No dangling images to remove")
	}
	m.pendingAction = "prune-images"
	m.confirmModal = sharedcomponents.NewConfirmModal("Prune Images",
		sharedcomponents.RemovalPreview("dangling images", shortIDs(m.pruneTargets)))
	return m, nil
}

// danglingImageIDs lists, once each and in order, the images that have no tag and
// that no container uses. An ID a tagged row also carries is not dangling, and a
// malformed ID is left out.
func danglingImageIDs(images []docker.Image) []string {
	tagged := map[string]bool{}
	for _, img := range images {
		if !isDangling(img) {
			tagged[img.ID] = true
		}
	}
	var ids []string
	for _, img := range images {
		if !isDangling(img) || img.Containers != 0 || tagged[img.ID] || slices.Contains(ids, img.ID) {
			continue
		}
		if !docker.IsImageID(img.ID) {
			log.Printf("ERROR [oci_resources] prune: skipped an image with a malformed ID %q", img.ID)
			continue
		}
		ids = append(ids, img.ID)
	}
	return ids
}

// isDangling reports whether an image has neither a repository nor a tag.
func isDangling(img docker.Image) bool {
	return img.Repository == "<none>" && img.Tag == "<none>"
}

// shortIDs shortens image IDs to the 12 characters `image ls` prints.
func shortIDs(ids []string) []string {
	short := make([]string, 0, len(ids))
	for _, id := range ids {
		id = strings.TrimPrefix(id, "sha256:")
		short = append(short, id[:min(len(id), 12)])
	}
	return short
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
		batchScanCmd([]imageScanJob{{Name: name, Target: img.ScanTarget()}}, m.scanOptions()),
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
		batchScanCmd(queue, m.scanOptions()),
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
		jobs.Start(m.scanRun(cacheKeys), batchScanCmd(queue, m.scanOptions())),
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
	if msg.Err == nil {
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
		batchScanCmd([]imageScanJob{job}, m.scanOptions()),
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
