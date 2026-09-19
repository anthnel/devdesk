package templates

import (
	"github.com/anthnel/devdesk/internal/template"
	"github.com/anthnel/devdesk/internal/ui/shortcut"
)

// The reasons a key is refused, as named constants: the header reads them to
// grey, the handler reads them to refuse, and a test checks the two agree
// (Rule 130).
const (
	reasonNoTemplate   = "No template selected"
	reasonListed       = "Listed by the registry — edit it first to keep it in the catalog"
	reasonUnreadable   = "The catalog file could not be read — check logs"
	reasonStillOpening = "The catalog is still loading"
)

// availability is what the selected row permits, computed once and read by both
// halves of the view.
type availability struct {
	New     shortcut.Availability
	Edit    shortcut.Availability
	Delete  shortcut.Availability
	Preview shortcut.Availability
	Scan    shortcut.Availability
	Sync    shortcut.Availability
}

func (m Model) availability() availability {
	var a availability

	// The scanners are a global unavailability: without either, S is greyed
	// whatever the row is.
	a.Scan = m.scannerState()

	// An unreadable catalog refuses every write: saving over a file DevDesk
	// could not parse would replace whatever the user had in it.
	if m.storeErr != nil {
		a.New = shortcut.Unavailable(reasonUnreadable)
		a.Edit = shortcut.Unavailable(reasonUnreadable)
		a.Delete = shortcut.Unavailable(reasonUnreadable)
	}

	if _, ok := m.selectedEntry(); !ok {
		a.Edit = firstRefusal(a.Edit, reasonNoTemplate)
		a.Delete = firstRefusal(a.Delete, reasonNoTemplate)
		a.Preview = shortcut.Unavailable(reasonNoTemplate)
		a.Scan = firstRefusal(a.Scan, reasonNoTemplate)
		a.Sync = shortcut.Unavailable(reasonNoTemplate)
		return a
	}
	a.Sync = m.syncState()
	if entry, ok := m.selectedEntry(); ok {
		if dir, err := template.ScanDir(entry.Slug); err == nil && m.scanning(dir) {
			a.Scan = firstRefusal(a.Scan, reasonScanRunning)
		}
	}
	return a
}

// firstRefusal keeps an earlier refusal over a later one: the catalog being
// unreadable explains more than there being no row.
func firstRefusal(current shortcut.Availability, reason string) shortcut.Availability {
	if !current.Enabled() {
		return current
	}
	return shortcut.Unavailable(reason)
}
