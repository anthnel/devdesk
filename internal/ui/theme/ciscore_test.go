package theme

import (
	"testing"

	"github.com/charmbracelet/lipgloss"
)

func score(s string) *string { return &s }

// Four states, and three of them are absences that do not mean the same thing.
func TestTheFourStatesOfACIScore(t *testing.T) {
	for _, tc := range []struct {
		name      string
		score     *string
		scanned   bool
		gradeable bool
		want      CIScoreState
		cell      string
	}{
		{"graded", score("B"), true, true, CIScoreGraded, "B"},
		{"scanned, no letter", nil, true, true, CIScoreWithheld, "?"},
		{"scanned, empty letter", score(""), true, true, CIScoreWithheld, "?"},
		{"never scanned", nil, false, true, CIScoreNever, "-"},
		{"not this context's forge", nil, false, false, CIScoreUnavailable, ""},
		// Even a repository that was scanned before the context changed forge
		// reads as unavailable: the cell answers "can this be graded here",
		// and the stale letter is not an answer to it.
		{"scanned, forge changed", score("A"), true, false, CIScoreUnavailable, ""},
	} {
		t.Run(tc.name, func(t *testing.T) {
			got := CIScoreVerdict(tc.score, tc.scanned, tc.gradeable)
			if got != tc.want {
				t.Errorf("CIScoreVerdict = %v, want %v", got, tc.want)
			}
			if cell := CIScoreCell(got, tc.score); cell != tc.cell {
				t.Errorf("cell = %q, want %q", cell, tc.cell)
			}
		})
	}
}

// A dash means "not yet" and an empty cell means "never, not from here". They
// have to stay different, because one of them is a suggestion to press S.
func TestNotYetAndNeverAreDifferentCells(t *testing.T) {
	never := CIScoreCell(CIScoreNever, nil)
	unavailable := CIScoreCell(CIScoreUnavailable, nil)

	if never == unavailable {
		t.Errorf("both absences print %q", never)
	}
}

// The colour separates a grade from an absence. This column has three absences,
// all dim, so uncoloured is already taken: a plain A would differ from a grey
// dash by a shade, in four cells. Green does not say "well done", it says
// *this is a grade*.
func TestTheFiveGradesAreColouredAndBold(t *testing.T) {
	want := map[string]lipgloss.TerminalColor{
		"A": ColorOK,
		"B": ColorOK,
		"C": ColorSeverityMedium,
		"D": ColorSeverityHigh,
		"E": ColorSeverityCritical,
	}
	for letter, colour := range want {
		got := CIScoreStyle(CIScoreGraded, score(letter))
		if got.GetForeground() != colour {
			t.Errorf("%s is %v, want %v", letter, got.GetForeground(), colour)
		}
		if !got.GetBold() {
			t.Errorf("%s is not bold", letter)
		}
		// Rule 115: naming a foreground without a background strips the app
		// background from the rest of the line.
		if got.GetBackground() != ColorBackground {
			t.Errorf("%s does not carry the app background", letter)
		}
	}
}

// The three bad grades stay told apart from the two nominal ones, which is what
// the colour was originally spent on and is not given up by colouring A and B.
func TestABadGradeIsNeverTheNominalColour(t *testing.T) {
	nominal := CIScoreStyle(CIScoreGraded, score("A")).GetForeground()
	if got := CIScoreStyle(CIScoreGraded, score("B")).GetForeground(); got != nominal {
		t.Error("A and B are coloured differently; both are nominal")
	}
	for _, letter := range []string{"C", "D", "E"} {
		if CIScoreStyle(CIScoreGraded, score(letter)).GetForeground() == nominal {
			t.Errorf("%s is not coloured apart from the nominal grades", letter)
		}
	}
}

// A letter nobody has defined gets the weight and no colour of its own: the
// nominal green is a claim about the grade, and claiming it for an unknown
// letter would be a confident guess.
func TestAnUnknownGradeClaimsNoColour(t *testing.T) {
	got := CIScoreStyle(CIScoreGraded, score("F"))
	if got.GetForeground() == ColorOK {
		t.Error("an unknown letter is rendered as nominal")
	}
	if !got.GetBold() {
		t.Error("an unknown letter is not bold")
	}
}

// Everything that is not a grade is dim, including a state whose pointer is
// nil — which is every absence.
func TestEveryAbsenceIsDim(t *testing.T) {
	for _, state := range []CIScoreState{CIScoreNever, CIScoreUnavailable, CIScoreWithheld} {
		if got := CIScoreStyle(state, nil); got.GetForeground() != DimStyle.GetForeground() {
			t.Errorf("state %v is not dim", state)
		}
	}
	// And a graded state with no letter behind it cannot panic on the way to a
	// colour: the pointer is what CIScoreCell would print.
	if got := CIScoreStyle(CIScoreGraded, nil); got.GetForeground() != DimStyle.GetForeground() {
		t.Error("a graded state with no letter is not dim")
	}
}

// The cell carries no escape sequence: it is measured before it is styled, and
// a colour inside it is truncated mid-sequence (Rule 122).
func TestTheCellIsPlainText(t *testing.T) {
	for _, tc := range []struct {
		state CIScoreState
		score *string
	}{
		{CIScoreGraded, score("E")},
		{CIScoreWithheld, nil},
		{CIScoreNever, nil},
		{CIScoreUnavailable, nil},
	} {
		if cell := CIScoreCell(tc.state, tc.score); containsEscape(cell) {
			t.Errorf("state %v printed a styled cell: %q", tc.state, cell)
		}
	}
}

func containsEscape(s string) bool {
	for _, r := range s {
		if r == 0x1b {
			return true
		}
	}
	return false
}
