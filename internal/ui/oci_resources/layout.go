package ociresources

import (
	"time"

	"github.com/anthnel/devdesk/internal/ui/theme"
)

// tableHeight computes the data row count for the table based on current state
func (m *Model) tableHeight() int {
	return max(m.height-1, 1)
}

// resize adjusts all table dimensions
func (m *Model) resize(width, height int) {
	m.width = width
	m.height = height
	m.filterBar.Resize(width)

	if m.launchForm != nil {
		m.launchForm.SetWidth(width - 2)
	}
	if m.resourceForm != nil {
		m.resourceForm.SetWidth(width - 2)
	}
	if m.registryForm != nil {
		m.registryForm.SetWidth(width - 2)
	}
	if m.networkInspectForm != nil {
		m.networkInspectForm.resize(width-2, height)
	}
	if m.connectivityForm != nil {
		m.connectivityForm.width = width - 2
		m.connectivityForm.height = height
	}
	if m.registryBrowser != nil {
		m.registryBrowser.SetSize(width-2, height)
	}

	m.imageTable.SetHeight(m.tableHeight())
	m.networkTable.SetHeight(m.tableHeight())
	m.volumeTable.SetHeight(m.tableHeight())
	m.registryTable.SetHeight(m.tableHeight())

	m.resizeImageTable(width)
	m.resizeNetworkTable(width)
	m.resizeVolumeTable(width)
	m.resizeRegistryTable(width)
}

func (m *Model) resizeImageTable(width int) {
	contentWidth := width - 2
	columns := m.imageTable.Columns()
	numCols := len(columns)
	if numCols >= 9 {
		available := contentWidth - numCols*2
		fixedID := 14
		fixedDisk := 12
		fixedContent := 14
		fixedC := 4
		fixedH := 4
		fixedM := 4
		fixedL := 4
		fixedScanned := 14
		fixedTotal := fixedID + fixedDisk + fixedContent + fixedC + fixedH + fixedM + fixedL + fixedScanned
		flexName := max(available-fixedTotal, 20)
		columns[0].Width = fixedID
		columns[1].Width = flexName
		columns[2].Width = fixedDisk
		columns[3].Width = fixedContent
		columns[4].Width = fixedC
		columns[5].Width = fixedH
		columns[6].Width = fixedM
		columns[7].Width = fixedL
		columns[8].Width = available - fixedID - flexName - fixedDisk - fixedContent - fixedC - fixedH - fixedM - fixedL
		m.imageTable.SetColumns(columns)
	}
}

func (m *Model) resizeNetworkTable(width int) {
	contentWidth := width - 2
	columns := m.networkTable.Columns()
	numCols := len(columns)
	if numCols >= 4 {
		available := contentWidth - numCols*2
		fixedID := 14
		fixedDriver := 12
		fixedScope := 10
		flexName := max(available-fixedID-fixedDriver-fixedScope, 20)
		columns[0].Width = fixedID
		columns[1].Width = flexName
		columns[2].Width = fixedDriver
		columns[3].Width = available - fixedID - flexName - fixedDriver
		m.networkTable.SetColumns(columns)
	}
}

func (m *Model) resizeVolumeTable(width int) {
	contentWidth := width - 2
	columns := m.volumeTable.Columns()
	numCols := len(columns)
	if numCols >= 3 {
		available := contentWidth - numCols*2
		fixedDriver := 12
		fixedName := 30
		columns[0].Width = fixedName
		columns[1].Width = fixedDriver
		columns[2].Width = max(available-fixedName-fixedDriver, 20)
		m.volumeTable.SetColumns(columns)
	}
}

func (m *Model) resizeRegistryTable(width int) {
	contentWidth := width - 2
	columns := m.registryTable.Columns()
	numCols := len(columns)
	if numCols >= 6 {
		available := contentWidth - numCols*2
		fixedUsername := 16
		fixedAlias := 10
		fixedAuth := 12 // holds "credentials"
		fixedLogged := 8
		fixedMembers := 16 // holds "12 · 30 days ago"
		fixedSum := fixedUsername + fixedAlias + fixedAuth + fixedLogged + fixedMembers
		flexURL := max(available-fixedSum, 20)
		columns[0].Width = flexURL
		columns[1].Width = fixedUsername
		columns[2].Width = fixedAlias
		columns[3].Width = fixedAuth
		columns[4].Width = fixedLogged
		// Last column absorbs the rounding so the selected row reaches the border.
		columns[5].Width = available - flexURL - fixedUsername - fixedAlias - fixedAuth - fixedLogged
		m.registryTable.SetColumns(columns)
	}
}

// timeAgo returns a compact relative time string (Rule 127).
func timeAgo(t time.Time) string {
	if t.IsZero() {
		return "-"
	}
	return theme.TimeAgo(t)
}
