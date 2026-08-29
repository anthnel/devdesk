package components

import (
	"strconv"
	"strings"

	"github.com/anthnel/devdesk/internal/command"
	"github.com/anthnel/devdesk/internal/jobs"
)

// JobsStatusLine is what a footer says while work runs (D9).
//
// It is derived every frame rather than posted as a message, and Rule 128 says
// why: a batch outlives the three seconds a footer message gets, so a line set
// on the first repository would vanish while the tenth was still going.
//
// Three forms, and the third is a deliberate surrender:
//
//	Scanning — 3/12                     one kind, started by origin
//	Scanning — 3/12 · Syncing — 1/4      two kinds, both started by origin
//	3 jobs running — :jobs for details   anything else
//
// The degraded form is what a footer can honestly say about work it does not
// own. One line cannot carry four batches launched from three views, and a line
// that showed only the part it recognised would be worse than one that admits
// there is more.
//
// origin is the view asking. A view with no work of its own to name — the
// dashboard, which launches none — passes an empty ViewType and always gets the
// third form, which is the whole of what it has to say.
//
// It lives here rather than in internal/jobs because the third form names a
// command (`:jobs`), and that is interface copy; and here rather than in one
// view because two of them now say it.
func JobsStatusLine(runs []jobs.Run, origin command.ViewType) string {
	live := jobs.Unfinished(runs)
	if len(live) == 0 {
		return ""
	}

	mine := make([]jobs.Run, 0, len(live))
	for _, run := range live {
		if origin != "" && run.Origin == origin {
			mine = append(mine, run)
		}
	}

	// Anything started elsewhere, or more than one batch of a kind, and the
	// detailed line stops being able to tell the truth.
	if len(mine) != len(live) || !oneRunPerKind(mine) {
		return Plural(len(live), "job", "jobs") + " running — :jobs for details"
	}

	parts := make([]string, 0, len(mine))
	for _, kind := range jobs.Kinds() {
		for _, run := range mine {
			if run.Kind == kind {
				parts = append(parts, run.Kind.Verb()+" — "+
					strconv.Itoa(run.Done())+"/"+strconv.Itoa(run.Total()))
			}
		}
	}
	return strings.Join(parts, " · ")
}

// oneRunPerKind reports whether no two runs share a kind. Two scans at once are
// two counters, and "Scanning — 3/12 · Scanning — 1/2" reads as a bug.
func oneRunPerKind(runs []jobs.Run) bool {
	seen := make(map[jobs.Kind]bool, len(runs))
	for _, run := range runs {
		if seen[run.Kind] {
			return false
		}
		seen[run.Kind] = true
	}
	return true
}

// Plural writes a count with the right noun. Exported because three packages
// spell the same sentence now.
func Plural(n int, one, many string) string {
	if n == 1 {
		return "1 " + one
	}
	return strconv.Itoa(n) + " " + many
}
