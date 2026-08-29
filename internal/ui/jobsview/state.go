package jobsview

import (
	"github.com/charmbracelet/lipgloss"

	"github.com/anthnel/devdesk/internal/jobs"
	"github.com/anthnel/devdesk/internal/ui/theme"
)

// The glyph vocabulary of both tables. One table, five states, and the two
// levels share it: a run that failed and the target that failed it read the
// same, which is what makes → feel like opening the row rather than arriving
// somewhere else.
//
// They are glyphs the application already has. A sixth codepoint for "queued"
// when IconPending is the clock the pipeline column already uses would be a
// second name for one idea (see the note on IconLock in theme/icons.go).

// runStateGlyph is the run's state, or the shared spinner frame while it goes.
//
// Queued spins too. It is waiting on a worker rather than on nothing, the
// difference is in the State column, and a static clock next to a row whose
// counter is climbing reads as a stall.
func runStateGlyph(r runRow) string {
	switch r.Run.State() {
	case jobs.RunRunning, jobs.RunQueued:
		return r.Frame
	case jobs.RunFailed:
		return theme.IconError
	case jobs.RunCancelled:
		return theme.IconCanceled
	default:
		return theme.IconOK
	}
}

// itemStateGlyph is the same for one target. An item that is queued does not
// spin: within a run, waiting for a worker is exactly what distinguishes it
// from the ones that are running, and that is the D6 distinction this level
// exists to show.
func itemStateGlyph(r itemRow) string {
	switch r.Item.State {
	case jobs.ItemRunning:
		return r.Frame
	case jobs.ItemQueued:
		return theme.IconPending
	case jobs.ItemFailed:
		return theme.IconError
	case jobs.ItemSkipped:
		return theme.IconSkipped
	default:
		return theme.IconOK
	}
}

// runStateStyle colours a run.
//
// Rule 122's discipline decides the four answers. `done` is the majority of a
// settled list, so it takes the ordinary text colour — a green on most rows
// would be a colour on the whole table and a signal on none of it. `failed` is
// what deserves to be found without reading. `cancelled` is a warning rather
// than an error: nothing broke, the user stopped it. `queued` is dim, the way
// a zero and a placeholder are: nothing has happened yet.
//
// `running` is left plain on purpose. The spinner in the glyph column already
// says it, it says it by moving, and a second answer in colour would be one
// more thing to keep in step.
func runStateStyle(state jobs.RunState) lipgloss.Style {
	switch state {
	case jobs.RunFailed:
		return theme.StatusErrorStyle
	case jobs.RunCancelled:
		return theme.StatusWarningStyle
	case jobs.RunQueued:
		return theme.DimStyle
	default:
		return lipgloss.NewStyle()
	}
}

// itemStateStyle is the same table one level down. `skipped` joins the warning
// side: a sync refused on a dirty tree did not fail, and the reason is in the
// Detail column beside it.
func itemStateStyle(state jobs.ItemState) lipgloss.Style {
	switch state {
	case jobs.ItemFailed:
		return theme.StatusErrorStyle
	case jobs.ItemSkipped:
		return theme.StatusWarningStyle
	case jobs.ItemQueued:
		return theme.DimStyle
	default:
		return lipgloss.NewStyle()
	}
}
