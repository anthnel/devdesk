package ociresources

import (
	"log"
	"time"

	tea "github.com/charmbracelet/bubbletea"

	"github.com/anthnel/devdesk/internal/cache"
	"github.com/anthnel/devdesk/internal/registrymgr"
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

// handleRegistryGroupDetected records a discovery and forwards it to the browser
// when one is open. A refresh fired from the Registries tab has no browser to
// forward to, and the Members column is what it updates.
func (m Model) handleRegistryGroupDetected(msg RegistryGroupDetectedMsg) (tea.Model, tea.Cmd) {
	delete(m.refreshingGroups, msg.Slug)

	if msg.Err != nil {
		log.Printf("INFO [oci_resources] group detection failed for %s: %v", msg.RegistryURL, msg.Err)
		// The cache keeps what was last known rather than being emptied by an
		// unreachable manager, so the column keeps showing it — stale, and
		// visibly so.
		m.errorMsg = "Group refresh failed — check logs"
		m.updateRegistryTable()
		if m.registryBrowser == nil {
			return m, clearInfoMsgCmd()
		}
	} else if msg.Slug != "" {
		m.groupCache[msg.Slug] = cache.RegistryGroupEntry{
			Members:      toCachedMembers(msg.Members),
			DiscoveredAt: time.Now(),
		}
		m.updateRegistryTable()
	}

	if m.registryBrowser == nil {
		return m, nil
	}
	var cmd tea.Cmd
	m.registryBrowser, cmd = m.registryBrowser.HandleGroupDetected(msg)
	return m, cmd
}

// toCachedMembers converts what the detector returned into what the cache and
// the table hold.
func toCachedMembers(members []registrymgr.GroupMember) []cache.RegistryGroupMember {
	out := make([]cache.RegistryGroupMember, 0, len(members))
	for _, m := range members {
		out = append(out, cache.RegistryGroupMember{Alias: m.Alias, URL: m.URL})
	}
	return out
}

// handleRegistryGroupCacheLoaded takes the discoveries read from disk.
func (m Model) handleRegistryGroupCacheLoaded(msg RegistryGroupCacheLoadedMsg) (tea.Model, tea.Cmd) {
	if msg.Entries != nil {
		m.groupCache = msg.Entries
	}
	m.updateRegistryTable()
	return m, nil
}
