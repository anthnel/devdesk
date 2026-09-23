package theme

import "testing"

// The three states, and the pair that looks alike: "looked, found nothing" and
// "nobody looked" are the distinction a bare counter cannot make.
func TestTheMisconfigCellTellsCleanApartFromUnread(t *testing.T) {
	tests := []struct {
		name    string
		state   MisconfigState
		count   int
		partial bool
		want    string
	}{
		{"nobody looked", MisconfigNever, 0, false, "-"},
		{"looked, found nothing", MisconfigClean, 0, false, "0"},
		{"found three", MisconfigFound, 3, false, "3"},
		{"found three, part unread", MisconfigFound, 3, true, "3?"},
		{"found nothing where nothing was rendered", MisconfigClean, 0, true, "0?"},
		// A target nothing read stays a dash whatever else is passed: there is
		// no partial count when there is no count.
		{"unread and partial", MisconfigNever, 0, true, "-"},
	}
	for _, tt := range tests {
		if got := MisconfigCell(tt.state, tt.count, tt.partial); got != tt.want {
			t.Errorf("%s: MisconfigCell = %q, want %q", tt.name, got, tt.want)
		}
	}
}

// Only a count that found something is coloured — a clean target showing a
// coloured zero reads as a problem, which is the opposite of what colour is for.
func TestOnlyAMisconfigCountThatFoundSomethingIsColoured(t *testing.T) {
	dim := MisconfigStyle(MisconfigClean, "CRITICAL")
	if dim.GetForeground() != DimStyle.GetForeground() {
		t.Error("a clean count is coloured, want dim")
	}
	if got := MisconfigStyle(MisconfigNever, "CRITICAL"); got.GetForeground() != DimStyle.GetForeground() {
		t.Error("an unread count is coloured, want dim")
	}
	found := MisconfigStyle(MisconfigFound, "CRITICAL")
	if want := SeverityTextStyle("CRITICAL"); found.GetForeground() != want.GetForeground() {
		t.Error("a count that found a CRITICAL does not carry the CRITICAL colour")
	}
}

// `has` is the cache's nil-ness and `scanned` is the target's own state; either
// one missing means nobody looked.
func TestMisconfigVerdictNeedsBothASummaryAndAScan(t *testing.T) {
	tests := []struct {
		has, scanned bool
		count        int
		want         MisconfigState
	}{
		{false, false, 0, MisconfigNever},
		{true, false, 7, MisconfigNever},
		{false, true, 0, MisconfigNever},
		{true, true, 0, MisconfigClean},
		{true, true, 7, MisconfigFound},
	}
	for _, tt := range tests {
		if got := MisconfigVerdict(tt.has, tt.scanned, tt.count); got != tt.want {
			t.Errorf("MisconfigVerdict(%v, %v, %d) = %v, want %v", tt.has, tt.scanned, tt.count, got, tt.want)
		}
	}
}
