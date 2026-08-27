package theme

import "testing"

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

// The colour is spent on what is worth spotting without reading. A green A on
// every well-configured repository would inform no one and would weaken D and
// E, which is Rule 122's colour discipline.
func TestOnlyTheBadGradesAreColoured(t *testing.T) {
	plain := CIScoreStyle(CIScoreGraded, score("A"))
	if plain.GetForeground() != CIScoreStyle(CIScoreGraded, score("B")).GetForeground() {
		t.Error("A and B are coloured differently; both are nominal")
	}

	for _, letter := range []string{"C", "D", "E"} {
		if CIScoreStyle(CIScoreGraded, score(letter)).GetForeground() == plain.GetForeground() {
			t.Errorf("%s is not coloured apart from the nominal grades", letter)
		}
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
