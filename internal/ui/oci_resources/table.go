package ociresources

import (
	"fmt"
	"sort"
	"strings"

	"github.com/charmbracelet/bubbles/spinner"
	"github.com/charmbracelet/bubbles/table"

	"github.com/anthnel/devdesk/internal/config"
	"github.com/anthnel/devdesk/internal/docker"
	"github.com/anthnel/devdesk/internal/ui/datatable"
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

// networkColumns describes the Networks tab. Neither this table nor the volumes
// one sorts or filters today, so no Less and no Search: `/` and `.` stay inert
// rather than being wired to a bar this view's footer does not render.
func networkColumns() []datatable.Column[docker.Network] {
	return []datatable.Column[docker.Network]{
		{Title: "ID", MinWidth: 14, Cell: func(n docker.Network) string { return shortID(n.ID) }},
		{Title: "Name", MinWidth: 20, Flex: 1, Cell: func(n docker.Network) string { return n.Name }},
		{Title: "Driver", MinWidth: 12, Cell: func(n docker.Network) string { return n.Driver }},
		{Title: "Scope", MinWidth: 10, Cell: func(n docker.Network) string { return n.Scope }},
	}
}

// volumeColumns describes the Volumes tab.
func volumeColumns() []datatable.Column[docker.Volume] {
	return []datatable.Column[docker.Volume]{
		{Title: "Name", MinWidth: 30, Cell: func(v docker.Volume) string { return v.Name }},
		{Title: "Driver", MinWidth: 12, Cell: func(v docker.Volume) string { return v.Driver }},
		{Title: "Mountpoint", MinWidth: 20, Flex: 1, Cell: func(v docker.Volume) string { return v.Mountpoint }},
	}
}

// shortID truncates a Docker ID to the twelve characters the CLI shows.
func shortID(id string) string {
	if len(id) > 12 {
		return id[:12]
	}
	return id
}

// updateRegistryTable rebuilds the registry table rows
// membersCell reports what the last discovery for a group found, and when.
//
// The "when" is not decoration: a cache with no visible age is worse than the
// re-detection it replaced, because it looks current whatever it holds
// (§3.8, decision 3). Plain text only — Rule 122.
func (m *Model) membersCell(reg config.RegistryItem) string {
	if reg.Kind != config.KindGroup {
		return ""
	}
	if m.refreshingGroups[reg.Slug] {
		return m.spinner.View() + "refreshing"
	}
	entry, ok := m.groupCache[reg.Slug]
	if !ok {
		return "never" // declared a group, never asked
	}
	return fmt.Sprintf("%d · %s", len(entry.Members), timeAgo(entry.DiscoveredAt))
}

// loggedCell reports the docker login status for a registry URL.
func (m *Model) loggedCell(mode, url string) string {
	if !config.UsesCredentials(mode) {
		return "-"
	}
	if m.registryLoginStatus[url] {
		return theme.IconOK
	}
	return theme.IconError
}

// drilledGroup returns the group the tab has entered, or nil at the top level.
func (m *Model) drilledGroup() *config.RegistryItem {
	if m.registryGroupSlug == "" {
		return nil
	}
	for i := range m.registries {
		if m.registries[i].Slug == m.registryGroupSlug {
			return &m.registries[i]
		}
	}
	return nil
}

func (m *Model) updateRegistryTable() {
	if group := m.drilledGroup(); group != nil {
		m.updateGroupMemberTable(*group)
		return
	}
	rows := make([]table.Row, 0, len(m.registries))
	for _, reg := range m.registries {
		rows = append(rows, table.Row{
			reg.Alias, reg.URL, reg.Kind, reg.AuthMode,
			m.loggedCell(reg.AuthMode, reg.URL), m.membersCell(reg),
		})
	}
	m.setRegistryRows(rows)
}

// updateGroupMemberTable fills the table with one group's discovered members.
//
// A member is not a config entry: it has no alias of its own to edit, its
// credentials are the group's, and it is what the cache holds — so the columns
// say `inherit` and carry no member count.
func (m *Model) updateGroupMemberTable(group config.RegistryItem) {
	entry := m.groupCache[group.Slug]
	rows := make([]table.Row, 0, len(entry.Members))
	for _, member := range entry.Members {
		rows = append(rows, table.Row{
			member.Alias, member.URL, "member", config.AuthInherit,
			m.loggedCell(group.AuthMode, group.URL), "",
		})
	}
	m.setRegistryRows(rows)
}

func (m *Model) setRegistryRows(rows []table.Row) {
	m.registryTable.SetRows(rows)
	m.registryTable.SetStyles(theme.DefaultTableStyles())
	m.registryTable.SetHeight(m.tableHeight())
}
