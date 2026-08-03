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

// getSelectedImage returns the selected image or nil
func (m Model) getSelectedImage() *docker.Image {
	sorted := m.sortedImages(m.filteredImages())
	cursor := m.imageTable.Cursor()
	if cursor < 0 || cursor >= len(sorted) {
		return nil
	}
	return &sorted[cursor]
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
	return m, batchScanCmd([]imageScanJob{{Name: name, Target: img.ScanTarget()}}, m.defaultScanOpts())
}

// defaultScanOpts returns default scan options from configuration
func (m Model) defaultScanOpts() scan.ScanOptions {
	return scan.ScanOptions{
		EnableVuln:      m.config.Scan.EnableVuln,
		EnableSecret:    m.config.Scan.EnableSecret,
		EnableMisconfig: m.config.Scan.EnableMisconfig,
		EnableLicense:   m.config.Scan.EnableLicense,
		GenerateSBOM:    m.config.Scan.GenerateSBOM,
		SBOMOutputDir:   m.config.Scan.SBOMOutputDir,
		TrivyImage:      m.config.Scan.TrivyImage,
		GitleaksImage:   m.config.Scan.GitleaksImage,
		TrivyServer:     m.config.Scan.TrivyServer,
		IgnoreUnfixed:   m.config.Scan.IgnoreUnfixed,
		GitleaksHistory: m.config.Scan.GitleaksHistory,
		GitleaksConfig:  m.config.Scan.GitleaksConfig,
	}
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
	return m, batchScanCmd(jobs, m.defaultScanOpts())
}

// sortableColumns lists columns in cycle order for the '.' key
var sortableColumns = []sortField{
	sortByName,
	sortByDiskUsage,
	sortByContentSize,
	sortByCritical,
	sortByHigh,
	sortByMedium,
	sortByLow,
	sortByScanned,
}

// cycleSort cycles through sort options
func (m Model) cycleSort() (tea.Model, tea.Cmd) {
	if m.sortAsc {
		m.sortAsc = false
	} else {
		m.sortAsc = true
		nextIdx := 0
		for i, col := range sortableColumns {
			if col == m.sortColumn {
				nextIdx = (i + 1) % len(sortableColumns)
				break
			}
		}
		m.sortColumn = sortableColumns[nextIdx]
	}
	m.updateImageTable()
	return m, nil
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
	return m, tea.Batch(deleteScanCacheCmd(cacheKeys), batchScanCmd(jobs, m.defaultScanOpts()))
}

// handleLaunchBatchScan starts a batch scan of all images with the configured options.
func (m Model) handleLaunchBatchScan(msg LaunchBatchScanMsg) (tea.Model, tea.Cmd) {
	if m.scanning {
		return m, nil
	}
	var jobs []imageScanJob
	for _, img := range m.images {
		if img.Repository == "<none>" {
			continue
		}
		jobs = append(jobs, imageScanJob{Name: img.Name(), Target: img.ScanTarget()})
	}
	if len(jobs) == 0 {
		return m, nil
	}
	m.lastScanOptions = msg.Opts
	m.scanning = true
	return m, batchScanCmd(jobs, msg.Opts)
}

// handleLaunchSingleImageScan starts a scan for a single image with the configured options.
// The ImageName is user-provided (from security view) so Name and Target are identical.
func (m Model) handleLaunchSingleImageScan(msg LaunchSingleImageScanMsg) (tea.Model, tea.Cmd) {
	if m.scanningImages[msg.ImageName] {
		return m, nil
	}
	m.lastScanOptions = msg.Opts
	m.scanning = true
	job := imageScanJob{Name: msg.ImageName, Target: msg.ImageName}
	return m, batchScanCmd([]imageScanJob{job}, msg.Opts)
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
