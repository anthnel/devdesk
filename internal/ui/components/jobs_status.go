package components

import (
	"path/filepath"
	"strconv"
	"strings"

	"github.com/anthnel/devdesk/internal/command"
	"github.com/anthnel/devdesk/internal/jobs"
	"github.com/anthnel/devdesk/internal/ui/theme"
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

// rowSeparator joins a row's detail to the batch progress on the footer line.
const rowSeparator = " · "

// Thresholds under which a part of the row line is dropped rather than cut to
// a stub nobody can read.
const (
	minPathCols   = 6 // a ShortPath below this is just an ellipsis
	minDetailCols = 8 // a detail below this says nothing; the progress stands alone
)

// RowDetail is what the footer says about the selected row: an activity glyph,
// a lead-in, the target (a path or an image reference) and a tail. Only the
// target is ever shortened as a path, which is why it is a field of its own.
type RowDetail struct{ Glyph, Lead, Target, Tail string }

// IsZero reports whether the detail says nothing.
func (d RowDetail) IsZero() bool { return d == RowDetail{} }

// head is everything before the target.
func (d RowDetail) head() string {
	if d.Glyph == "" {
		return d.Lead
	}
	return d.Glyph + " " + d.Lead
}

// String is the detail at full length, the home directory folded.
func (d RowDetail) String() string { return d.head() + theme.FoldHome(d.Target) + d.Tail }

// fit renders the detail in at most room columns. A path target shrinks first
// (never below minPathCols, even when the tail is long), then the end of the
// detail is cut. An image reference is not a path and is only ever cut.
func (d RowDetail) fit(room int) string {
	full := d.String()
	if theme.StringWidth(full) <= room {
		return full
	}
	pathRoom := room - theme.StringWidth(d.head()) - theme.StringWidth(d.Tail)
	// With no tail the cut lands on the path itself, so reducing it below its
	// room first would only trade a readable prefix for an ellipsis.
	if isPathTarget(d.Target) && (pathRoom >= minPathCols || d.Tail != "") {
		full = d.head() + theme.ShortPath(d.Target, max(pathRoom, minPathCols)) + d.Tail
	}
	return theme.TruncateWidth(full, room)
}

// isPathTarget tells a filesystem path from an image reference such as
// "registry/ns/app:1.2", which ShortPath would mangle.
func isPathTarget(target string) bool {
	return filepath.IsAbs(target) || strings.HasPrefix(target, "~")
}

// RowStatusLine composes the footer line for a selected row: its detail, then
// the batch progress, on one line of at most width columns. The progress is
// what gives way last; the detail is dropped whole when fewer than
// minDetailCols are left for it (or fewer than it needs, if it is shorter).
func RowStatusLine(detail RowDetail, progress string, width int) string {
	switch {
	case width <= 0:
		return ""
	case detail.IsZero():
		return theme.TruncateWidth(progress, width)
	case progress == "":
		return detail.fit(width)
	}
	room := width - theme.StringWidth(rowSeparator) - theme.StringWidth(progress)
	if room < min(minDetailCols, theme.StringWidth(detail.String())) {
		return theme.TruncateWidth(progress, width)
	}
	return detail.fit(room) + rowSeparator + progress
}
