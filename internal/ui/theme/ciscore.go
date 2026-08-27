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

// CIScoreStyle colours the grade: green for A and B, then the three severity
// colours for C, D and E, all five in bold.
//
// **It builds its own style rather than borrowing SeverityTextStyle.** The
// colours are shared because they are the application's vocabulary for "how bad
// is this"; the weight is not. SeverityTextStyle sets Bold on CRITICAL and HIGH
// only, so a borrowed C would render lighter than a D for a reason that belongs
// to a CVE table — and adding Bold there to even it out would embolden every
// MEDIUM finding in the application. Two things that have no reason to move
// together must not share a style.
//
// The colour is what separates a grade from an absence, which is why A and B
// are green here and would not be in a severity column. This column has three
// absences — `-` never scanned, an empty cell for a repository this context
// cannot grade, `?` for a withheld run — and all three render dim. Uncoloured
// is therefore already taken: a plain `A` differs from a grey `-` by a shade,
// in four cells. Green does not say "well done", it says *this is a grade* as
// against *there is none*.
func CIScoreStyle(state CIScoreState, score *string) lipgloss.Style {
	if state != CIScoreGraded || score == nil {
		return DimStyle
	}
	return ciGradeStyle(*score)
}

// ciGradeStyle maps one letter onto its colour.
//
// A letter the tool may add later gets the weight and no colour of its own,
// rather than falling through to green: the nominal colour is a claim about the
// grade, and claiming it for a letter nobody has defined is the kind of
// confident guess this package avoids everywhere else. The table fills in
// ColorText for a style that names no foreground.
func ciGradeStyle(letter string) lipgloss.Style {
	// Rule 115: a style that names a colour must name a background too, or the
	// cell strips the app background from everything to its right.
	base := lipgloss.NewStyle().Background(ColorBackground).Bold(true)
	switch letter {
	case "E":
		return base.Foreground(ColorSeverityCritical)
	case "D":
		return base.Foreground(ColorSeverityHigh)
	case "C":
		return base.Foreground(ColorSeverityMedium)
	case "A", "B":
		return base.Foreground(ColorOK)
	default:
		return base
	}
}
