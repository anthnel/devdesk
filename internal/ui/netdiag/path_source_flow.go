package netdiag

import (
	"strings"

	tea "charm.land/bubbletea/v2"

	"github.com/anthnel/devdesk/internal/ui/components"
	"github.com/anthnel/devdesk/internal/ui/overlay"
	"github.com/anthnel/devdesk/internal/ui/shortcut"
)

const (
	// textReadingSources is the footer state while the selector's list loads.
	textReadingSources = "Reading running containers..."

	// pickerTopRow is the body row the selector starts on: the table's header
	// row stays visible above it.
	pickerTopRow = 1
)

// sourceChoosable is the one answer to "can the selector open here", read by
// the header and by the handler (Rule 130).
func (pm *PathModel) sourceChoosable() shortcut.Availability {
	if pm.running() {
		return shortcut.Unavailable(reasonPathBusy)
	}
	return pm.launchable()
}

// openPicker opens the selector and asks for its list. The engine is read by
// the Cmd only (Rule 110); the answer comes back as a pathSourcesMsg.
func (pm *PathModel) openPicker() (*PathModel, tea.Cmd) {
	if a := pm.sourceChoosable(); !a.Enabled() {
		return pm, pm.footer.Warn(a.Reason)
	}
	pm.sourcesGen++
	p := newPathSourcePicker(pm.source)
	pm.picker = &p
	return pm, loadPathSourcesCmd(pm.sourcesGen, pm.resolveTimeout, pm.engine)
}

// closePicker drops the selector, and with it any listing still on its way.
func (pm *PathModel) closePicker() { pm.picker = nil }

// handleSources fills the selector. An answer to an older request, or one that
// arrives after the selector closed, changes nothing.
func (pm *PathModel) handleSources(msg pathSourcesMsg) (*PathModel, tea.Cmd) {
	if pm.picker == nil || msg.gen != pm.sourcesGen {
		return pm, nil
	}
	p := pm.picker.withList(msg.list)
	pm.picker = &p
	return pm, nil
}

// handlePickerKey gives the open selector every key: nothing reaches the field
// or the tab row.
func (pm *PathModel) handlePickerKey(key string) (*PathModel, tea.Cmd) {
	switch key {
	case "enter":
		return pm.chooseSource()
	case "esc":
		pm.picker = nil
	default:
		p := pm.picker.move(key, pickerRows(pm.pickerHeight()))
		pm.picker = &p
	}
	return pm, nil
}

// chooseSource closes the selector on the entry under the cursor. Choosing the
// source in use changes nothing; another one empties the column and returns to
// editing, the target value kept.
func (pm *PathModel) chooseSource() (*PathModel, tea.Cmd) {
	chosen := pm.picker.selected()
	pm.picker = nil
	if chosen.ID == pm.source.ID {
		return pm, nil
	}
	pm.source = chosen
	pm.target = pathTarget{}
	pm.clearRun()
	pm.state = pathEditing
	return pm, pm.input.Focus()
}

// pickerHeight is the room the selector has: the body below the header row.
func (pm *PathModel) pickerHeight() int { return pm.height - pickerTopRow }

// pickerRows is how many entries fit in height lines, borders excluded.
func pickerRows(height int) int { return max(height-pickerFrameRows, 1) }

// pickerStatus is the footer state while the selector is open: the loading
// dot, then the note that explains a list reduced to this machine (Rule 128).
func (pm *PathModel) pickerStatus() components.Status {
	if pm.picker.loading {
		return components.Status{Text: textReadingSources, Activity: true}
	}
	return components.Status{Text: pm.picker.note}
}

// viewWithPicker draws the selector over the table, which stays the body
// (Rules 112, 139).
func (pm *PathModel) viewWithPicker(body string) string {
	width := pm.table.RenderedWidth()
	box := pm.picker.render(width, pm.pickerHeight())
	x := max((width-pickerBoxWidth(width))/2, 0)
	return strings.Join(overlay.Compose(strings.Split(body, "\n"), box, x, pickerTopRow, width), "\n")
}
