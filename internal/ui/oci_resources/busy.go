package ociresources

import (
	"fmt"

	tea "github.com/charmbracelet/bubbletea"
)

// Rows an action is running on, across the four tabs (§3.22).
//
// Every action here is a `docker` invocation that can take seconds — removing a
// large image, a network the daemon is still tearing connections off, a
// `docker login` that has to reach a registry over a slow link. Nothing said so:
// the row was identical to one with nothing happening to it.
//
// Each table spends a different cell on the spinner, and the choice is the same
// each time: the one the user does not need while the action runs. §3.16 settled
// why it is a cell and not a column of its own — a column costs cells on every
// screen to say nothing on all but one row, and at 80 columns this view has none
// to spare.

// busyMessage is what every tab says when an action is already running on the
// selected row. One message rather than one per verb, because the user's next
// move is the same in all of them: wait.
const busyMessage = "Already busy — an action is running on this row"

// advanceBusySpinners moves every table's frame on and reports whether anything
// is running. It is called from the spinner tick, which is what keeps the frames
// turning; the tick has to go on being scheduled while this returns true, or the
// spinner stops on a row that is still working.
func (m *Model) advanceBusySpinners() bool {
	m.imageTable.AdvanceSpinner()
	m.networkTable.AdvanceSpinner()
	m.volumeTable.AdvanceSpinner()
	m.registryTable.AdvanceSpinner()
	return len(m.busyLabels()) > 0
}

// busyLabels gathers what is running across every tab.
//
// Across all four rather than the active one: an action started on the Images
// tab goes on running after `tab`, and a spinner that stopped turning because
// the user looked elsewhere would read as a hang on the way back.
func (m Model) busyLabels() []string {
	var out []string
	out = append(out, m.imageTable.BusyLabels()...)
	out = append(out, m.networkTable.BusyLabels()...)
	out = append(out, m.volumeTable.BusyLabels()...)
	out = append(out, m.registryTable.BusyLabels()...)
	return out
}

// actionLine names what is running, in words. The spinner on the row says that
// something is; this says what.
//
// Rendered from the current state rather than assigned to a footer message,
// because a footer message expires after three seconds (Rule 128) and these
// outlive that — the same reason §3.17's sync progress is a rendered line.
func (m Model) actionLine() string {
	if m.pruning != "" {
		return "Pruning " + m.pruning + "…"
	}
	labels := m.busyLabels()
	switch len(labels) {
	case 0:
		return ""
	case 1:
		return labels[0] + "…"
	default:
		// Naming them all would run past the line; the count is what the user
		// is waiting on anyway.
		return fmt.Sprintf("%d actions running…", len(labels))
	}
}

// busyTick is what an action returns alongside its command so the frames turn.
// Init starts a tick, but it stops as soon as nothing is loading, and an action
// begun after that would sit on a frozen frame.
func (m Model) busyTick() tea.Cmd { return m.spinner.Tick }
