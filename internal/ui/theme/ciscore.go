package theme

import "github.com/charmbracelet/lipgloss"

// CIScoreState is what a scan can say about a repository's pipeline, and it has
// four values rather than one letter (§3.42).
//
// Three of them are absences and they do not mean the same thing: a repository
// nobody has scanned, one this context cannot scan — plumber only grades the
// forge the context targets, so a GitHub clone in a GitLab context is not
// "ungraded" but ungradeable — and a run that could not conclude. The fourth is
// a grade.
//
// The state is what decides the colour, never the rendered string: deciding it
// from what was printed is what the workspaces view did for secrets and what
// had to be undone.
type CIScoreState int

const (
	// CIScoreNever is a repository nothing has graded yet.
	CIScoreNever CIScoreState = iota
	// CIScoreUnavailable is one that cannot be graded here: not a repository,
	// no remote, or a remote that is not this context's forge.
	CIScoreUnavailable
	// CIScoreWithheld is a run that ran and said it could not conclude —
	// plumber's exit 3 and its dataCollectionDegraded — or a repository with no
	// pipeline at all. The two share a cell because the tab says which, and
	// four cells have no room to carry a distinction the next screen carries.
	CIScoreWithheld
	// CIScoreGraded is a letter, A through E.
	CIScoreGraded
)

// CIScoreVerdict maps a cached verdict onto the four states.
//
// `scanned` is the state of the target itself and `gradeable` says whether this
// context could grade it at all: a repository of another forge shows nothing
// rather than a dash, because a dash means "not yet" and this one never will be
// — not from here.
func CIScoreVerdict(score *string, scanned, gradeable bool) CIScoreState {
	if !gradeable {
		return CIScoreUnavailable
	}
	if !scanned {
		return CIScoreNever
	}
	if score == nil || *score == "" {
		// Scanned, gradeable, and still no letter: the run was withheld. The
		// letter plumber writes in that case is not merely unreliable, it is
		// optimistic — a control that did not run found nothing — so nothing
		// upstream passes it here.
		return CIScoreWithheld
	}
	return CIScoreGraded
}

// CIScoreCell is what the cell prints: plain text, no escape sequence, because
// CIScoreStyle is what colours it (Rule 122).
func CIScoreCell(state CIScoreState, score *string) string {
	switch state {
	case CIScoreGraded:
		return *score
	case CIScoreWithheld:
		return "?"
	case CIScoreNever:
		return "-"
	default:
		return ""
	}
}

// CIScoreStyle colours the grade.
//
// The colour is spent on what is worth spotting without reading: D and E. A is
// the nominal state and keeps the ordinary text colour — a green A on every
// well-configured repository would inform no one and would weaken the two that
// do (Rule 122's colour discipline). Everything that is not a grade is dim.
func CIScoreStyle(state CIScoreState, score *string) lipgloss.Style {
	if state != CIScoreGraded || score == nil {
		return DimStyle
	}
	switch *score {
	case "E":
		return SeverityTextStyle("CRITICAL")
	case "D":
		return SeverityTextStyle("HIGH")
	case "C":
		return SeverityTextStyle("MEDIUM")
	default: // A, B — nominal, and anything the tool may add later
		return lipgloss.NewStyle()
	}
}
