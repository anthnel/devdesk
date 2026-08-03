package ociresources

import (
	"fmt"
	"sort"
	"strings"

	"github.com/charmbracelet/bubbles/spinner"
	"github.com/charmbracelet/bubbles/table"

	"github.com/anthnel/devdesk/internal/docker"
	"github.com/anthnel/devdesk/internal/ui/theme"
)

// filteredImages returns images matching the current filter
func (m *Model) filteredImages() []docker.Image {
	query := strings.ToLower(m.filterBar.SearchQuery())
	if query == "" {
		return m.images
	}
	var result []docker.Image
	for _, img := range m.images {
		if strings.Contains(strings.ToLower(img.Repository), query) ||
			strings.Contains(strings.ToLower(img.Tag), query) {
			result = append(result, img)
		}
	}
	return result
}

// sortedImages returns images sorted by the current sort column
func (m *Model) sortedImages(images []docker.Image) []docker.Image {
	sorted := make([]docker.Image, len(images))
	copy(sorted, images)
	sort.Slice(sorted, func(i, j int) bool {
		a, b := sorted[i], sorted[j]
		nameA := a.Name()
		nameB := b.Name()
		cacheA := m.scanCache[nameA]
		cacheB := m.scanCache[nameB]
		var less bool
		switch m.sortColumn {
		case sortByDiskUsage:
			less = a.UniqueSize < b.UniqueSize
		case sortByContentSize:
			less = a.Size < b.Size
		case sortByCritical:
			less = cacheA.Critical < cacheB.Critical
		case sortByHigh:
			less = cacheA.High < cacheB.High
		case sortByMedium:
			less = cacheA.Medium < cacheB.Medium
		case sortByLow:
			less = cacheA.Low < cacheB.Low
		case sortByScanned:
			less = cacheA.ScannedAt.Before(cacheB.ScannedAt)
		default:
			less = strings.ToLower(nameA) < strings.ToLower(nameB)
		}
		if m.sortAsc {
			return less
		}
		return !less
	})
	return sorted
}

// formatBytes formats bytes into human-readable string
func formatBytes(b int64) string {
	switch {
	case b >= 1e9:
		return fmt.Sprintf("%.1f GB", float64(b)/1e9)
	case b >= 1e6:
		return fmt.Sprintf("%.1f MB", float64(b)/1e6)
	case b >= 1e3:
		return fmt.Sprintf("%.1f kB", float64(b)/1e3)
	default:
		return fmt.Sprintf("%d B", b)
	}
}

// formatCVECount formats a CVE count for display, returns "-" if not scanned
func formatCVECount(count int, scanned bool) string {
	if !scanned {
		return "-"
	}
	return fmt.Sprintf("%d", count)
}

// updateImageTable rebuilds the image table rows
func (m *Model) updateImageTable() {
	// Build alias list for display name substitution
	aliases := make([]docker.RegistryAlias, 0, len(m.registries))
	for _, reg := range m.registries {
		if reg.Alias != "" {
			aliases = append(aliases, docker.RegistryAlias{URL: reg.URL, Alias: reg.Alias})
		}
	}

	sorted := m.sortedImages(m.filteredImages())
	rows := make([]table.Row, 0, len(sorted))
	for _, img := range sorted {
		rawName := img.Name()
		displayName := docker.ApplyAliases(rawName, aliases)
		diskUsage := formatBytes(img.UniqueSize)
		contentSize := formatBytes(img.Size)
		shortID := img.ID
		if len(shortID) > 12 {
			shortID = shortID[:12]
		}
		entry, scanned := m.scanCache[rawName]
		scanning := m.scanningImages[rawName]
		crit := formatCVECount(entry.Critical, scanned)
		high := formatCVECount(entry.High, scanned)
		med := formatCVECount(entry.Medium, scanned)
		low := formatCVECount(entry.Low, scanned)
		scannedAt := "-"
		switch {
		case scanning:
			frame := spinner.Dot.Frames[m.spinnerFrameIdx%len(spinner.Dot.Frames)]
			scannedAt = frame + "scanning"
		case m.failedScans[rawName]:
			scannedAt = theme.IconError + " error"
		case scanned:
			scannedAt = timeAgo(entry.ScannedAt)
		}
		rows = append(rows, table.Row{shortID, displayName, diskUsage, contentSize, crit, high, med, low, scannedAt})
	}

	// Update sort indicators in column headers
	cols := m.imageTable.Columns()
	if len(cols) >= 9 {
		sortColIndex := map[sortField]int{
			sortByName: 1, sortByDiskUsage: 2, sortByContentSize: 3,
			sortByCritical: 4, sortByHigh: 5, sortByMedium: 6, sortByLow: 7, sortByScanned: 8,
		}
		baseTitles := map[int]string{1: "Name", 2: "Disk Usage", 3: "Content Size", 4: "C", 5: "H", 6: "M", 7: "L", 8: "Scanned"}
		for idx, title := range baseTitles {
			cols[idx].Title = title
		}
		if idx, ok := sortColIndex[m.sortColumn]; ok {
			arrow := " ▲"
			if !m.sortAsc {
				arrow = " ▼"
			}
			cols[idx].Title = baseTitles[idx] + arrow
		}
		m.imageTable.SetColumns(cols)
	}
	m.imageTable.SetRows(rows)
	m.imageTable.SetStyles(theme.DefaultTableStyles())
	m.imageTable.SetHeight(m.tableHeight())
}

// updateNetworkTable rebuilds the network table rows
func (m *Model) updateNetworkTable() {
	rows := make([]table.Row, 0, len(m.networks))
	for _, net := range m.networks {
		shortID := net.ID
		if len(shortID) > 12 {
			shortID = shortID[:12]
		}
		rows = append(rows, table.Row{shortID, net.Name, net.Driver, net.Scope})
	}
	m.networkTable.SetRows(rows)
	m.networkTable.SetStyles(theme.DefaultTableStyles())
	m.networkTable.SetHeight(m.tableHeight())
}

// updateVolumeTable rebuilds the volume table rows
func (m *Model) updateVolumeTable() {
	rows := make([]table.Row, 0, len(m.volumes))
	for _, vol := range m.volumes {
		rows = append(rows, table.Row{vol.Name, vol.Driver, vol.Mountpoint})
	}
	m.volumeTable.SetRows(rows)
	m.volumeTable.SetStyles(theme.DefaultTableStyles())
	m.volumeTable.SetHeight(m.tableHeight())
}

// updateRegistryTable rebuilds the registry table rows
func (m *Model) updateRegistryTable() {
	rows := make([]table.Row, 0, len(m.registries))
	for _, reg := range m.registries {
		auth := "no"
		if reg.AuthEnabled {
			auth = "yes"
		}
		logged := "-"
		if reg.AuthEnabled {
			if m.registryLoginStatus[reg.URL] {
				logged = theme.IconOK
			} else {
				logged = theme.IconError
			}
		}
		rows = append(rows, table.Row{reg.URL, reg.Username, reg.Alias, auth, logged})
	}
	m.registryTable.SetRows(rows)
	m.registryTable.SetStyles(theme.DefaultTableStyles())
	m.registryTable.SetHeight(m.tableHeight())
}
