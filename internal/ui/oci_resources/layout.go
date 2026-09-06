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
	if m.registryBrowser != nil {
		m.registryBrowser.SetSize(width-2, height)
	}

	// Widths and height in one call, and the Rule 116 arithmetic is the
	// component's rather than written out here four more times.
	m.registryTable.Resize(width, m.tableHeight())
	m.imageTable.Resize(width, m.tableHeight())
	m.networkTable.Resize(width, m.tableHeight())
	m.volumeTable.Resize(width, m.tableHeight())
}

// The width arithmetic of all four tables used to live here. The images copy
// clamped Name at 20 and then handed the entire shortfall to Scanned, which went
// negative below 96 columns; the volumes copy did the same to its last column;
// the registries copy clamped URL at 20 after the remainder had been computed,
// which pushes the sum back over Rule 116's budget. All four are the solver's
// job now.

// timeAgo returns a compact relative time string (Rule 127).
func timeAgo(t time.Time) string {
	if t.IsZero() {
		return "-"
	}
	return theme.TimeAgo(t)
}
