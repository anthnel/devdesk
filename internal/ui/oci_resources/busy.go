package ociresources

import "fmt"

// Rows an action is running on, across the four tabs (§3.22).
//
// Every action here is a `docker` invocation that can take seconds — removing a
// large image, a network the daemon is still tearing connections off, a
// `docker login` that has to reach a registry over a slow link. Nothing said so:
// the row was identical to one with nothing happening to it.
//
// All four tables show it in their activity column (blinking dot).

// busyMessage is what every tab says when an action is already running on the
// selected row. One message rather than one per verb, because the user's next
// move is the same in all of them: wait.
const busyMessage = "Already busy — an action is running on this row"

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
