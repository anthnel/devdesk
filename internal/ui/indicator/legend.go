package indicator

import (
	"strings"

	"github.com/anthnel/devdesk/internal/ui/help"
)

// LegendEntry is one extension row of the legend: a state and its label.
type LegendEntry struct {
	State State
	Label string
}

// legendSectionTitle is the title of the help section every migrated view shows.
const legendSectionTitle = "Activity indicator"

// Legend returns the "Activity indicator" help section: the base rows, then the
// extension rows of the view. Glyphs come from Glyph, never from literals.
func Legend(extension ...LegendEntry) help.Section {
	on, off := Glyph(Running, true), Glyph(Running, false)
	lines := []string{
		"The first column shows what is happening on a row. The glyph carries the",
		"meaning; the color repeats it. Nothing here relies on color alone.",
		"",
		"  " + on + " " + off + "  Running (blinks). Pauses to a steady " + on + " when motion is off.",
		"  " + Glyph(Queued, false) + "  Queued, waiting to start.",
		"  " + Glyph(Failed, false) + "  Failed. Stays until the next attempt on that row; the footer gives the cause.",
		"  (icon)  Idle: the row's usual icon, or an empty cell.",
	}
	for _, e := range extension {
		lines = append(lines, "  "+Glyph(e.State, true)+"  "+e.Label)
	}
	lines = append(lines,
		"",
		"The footer describes the selected row's state in words.",
		"Turn the blinking off with app.motion: static in the context file or in :cfg.",
	)
	return help.Section{Title: legendSectionTitle, Body: strings.Join(lines, "\n")}
}

// Per-view legend extensions (ux.md section 5).
var (
	JobsLegend = []LegendEntry{
		{Done, "done"}, {Cancelled, "cancelled"}, {Skipped, "skipped"},
	}
	CloneLegend = []LegendEntry{
		{Done, "cloned (clone list)"},
		{Skipped, "already on disk, or not started (cancelled) (clone list)"},
	}
	AgentsLegend = []LegendEntry{
		{Suspended, "idle (no activity for 5 min)"}, {Done, "done"}, {Unknown, "unknown"},
	}
	ContainersLegend = []LegendEntry{
		{Restarting, "restarting"}, {Suspended, "paused"}, {Unknown, "created"},
		{Skipped, "exited (code 0)"}, {Failed, "exited (non-zero code) or dead"},
		{Running, "an action (stop, kill, remove) is running"},
	}
)
