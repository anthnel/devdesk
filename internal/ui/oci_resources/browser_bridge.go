package ociresources

import (
	"log"

	tea "github.com/charmbracelet/bubbletea"
)

// openMultiRegistryBrowser opens the multi-registry browser (triggered by 'b' on Images tab).
func (m Model) openMultiRegistryBrowser() (tea.Model, tea.Cmd) {
	if len(m.registries) == 0 {
		m.errorMsg = "No registries configured — add one in the Registries tab"
		return m, clearInfoMsgCmd()
	}
	browser, cmd := newRegistryBrowser(m.registries, m.width-2, m.height)
	m.registryBrowser = browser
	m.registryBrowser.SetScanCache(m.scanCache)
	return m, cmd
}

// handleMultiRegistryTagsLoaded incorporates tag results from one registry into the browser.
func (m Model) handleMultiRegistryTagsLoaded(msg MultiRegistryTagsLoadedMsg) (tea.Model, tea.Cmd) {
	if m.registryBrowser == nil {
		return m, nil
	}
	if msg.Err != nil {
		log.Printf("ERROR [oci_resources] registry tags %s from %s: %v", msg.Repo, msg.RegistryURL, msg.Err)
		m.registryBrowser.AddRegistryTags(msg.RegistryURL, msg.Alias, msg.Repo, nil)
		return m, nil
	}
	m.registryBrowser.SetScanCache(m.scanCache)
	m.registryBrowser.AddRegistryTags(msg.RegistryURL, msg.Alias, msg.Repo, msg.Tags)
	return m, loadMultiRegistryTagsMetaCmd(msg.RegistryURL, msg.Repo)
}

// handleMultiRegistryTagsMeta stores tag metadata for one registry in the browser.
func (m Model) handleMultiRegistryTagsMeta(msg MultiRegistryTagsMetaMsg) (tea.Model, tea.Cmd) {
	if m.registryBrowser == nil {
		return m, nil
	}
	if msg.Err != nil {
		log.Printf("INFO [oci_resources] tag meta unavailable for %s at %s: %v", msg.Repo, msg.RegistryURL, msg.Err)
		return m, nil
	}
	if msg.Meta != nil {
		m.registryBrowser.SetMultiTagsMeta(msg.RegistryURL, msg.Meta)
	}
	return m, nil
}

// handleRegistryPullComplete processes the result of a pull from the browser.
func (m Model) handleRegistryPullComplete(msg RegistryPullCompleteMsg) (tea.Model, tea.Cmd) {
	if m.registryBrowser == nil {
		return m, nil
	}
	if msg.Err != nil {
		log.Printf("ERROR [oci_resources] pull %s: %v", msg.ImageName, msg.Err)
		m.registryBrowser.SetOperationError("")
		m.errorMsg = "Pull failed — check logs"
		return m, clearInfoMsgCmd()
	}
	m.registryBrowser.SetOperationSuccess()
	m.infoMsg = "Image pulled: " + msg.ImageName
	return m, tea.Batch(fetchImages(), clearInfoMsgCmd())
}

// handleRegistryTagDirectScan starts a direct remote Trivy scan without pulling the image.
func (m Model) handleRegistryTagDirectScan(msg RegistryTagDirectScanMsg) (tea.Model, tea.Cmd) {
	if m.scanningImages[msg.ImageName] {
		m.infoMsg = "Scan already in progress"
		return m, clearInfoMsgCmd()
	}
	job := imageScanJob{Name: msg.ImageName, Target: msg.ImageName}
	return m, batchScanCmd([]imageScanJob{job}, m.defaultScanOpts())
}

// unscannedCount returns the number of images not yet scanned
func (m *Model) unscannedCount() int {
	count := 0
	for _, img := range m.images {
		name := img.Name()
		if img.Repository == "<none>" {
			continue
		}
		if _, ok := m.scanCache[name]; !ok {
			count++
		}
	}
	return count
}

// handleRegistryGroupDetected forwards a group detection result to the active browser.
func (m Model) handleRegistryGroupDetected(msg RegistryGroupDetectedMsg) (tea.Model, tea.Cmd) {
	if m.registryBrowser == nil {
		return m, nil
	}
	if msg.Err != nil {
		log.Printf("INFO [oci_resources] group detection failed for %s: %v", msg.RegistryURL, msg.Err)
	}
	var cmd tea.Cmd
	m.registryBrowser, cmd = m.registryBrowser.HandleGroupDetected(msg)
	return m, cmd
}
