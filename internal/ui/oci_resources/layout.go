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

	m.registryTable.SetHeight(m.tableHeight())
	m.resizeRegistryTable(width)

	// Widths and height in one call, and the Rule 116 arithmetic is the
	// component's rather than written out here three more times.
	m.imageTable.Resize(width, m.tableHeight())
	m.networkTable.Resize(width, m.tableHeight())
	m.volumeTable.Resize(width, m.tableHeight())
}

// The images, networks and volumes width arithmetic used to live here. The
// images copy clamped Name at 20 and then handed the entire shortfall to
// Scanned, which went negative below 96 columns; the volumes copy did the same
// to its last column. Both are the solver's job now.

func (m *Model) resizeRegistryTable(width int) {
	contentWidth := width - 2
	columns := m.registryTable.Columns()
	numCols := len(columns)
	if numCols >= 6 {
		available := contentWidth - numCols*2
		fixedAlias := 16
		fixedKind := 10
		fixedAuth := 12 // holds "credentials"
		fixedLogged := 8
		fixedMembers := 16 // holds "12 · 30 days ago"
		fixedSum := fixedAlias + fixedKind + fixedAuth + fixedLogged + fixedMembers
		flexURL := max(available-fixedSum, 20)
		columns[0].Width = fixedAlias
		columns[1].Width = flexURL
		columns[2].Width = fixedKind
		columns[3].Width = fixedAuth
		columns[4].Width = fixedLogged
		// Last column absorbs the rounding so the selected row reaches the border.
		columns[5].Width = available - fixedAlias - flexURL - fixedKind - fixedAuth - fixedLogged
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
