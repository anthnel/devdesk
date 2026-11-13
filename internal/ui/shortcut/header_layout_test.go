package shortcut

import "testing"

// DEVDESK-305 (IS-13): the header's info column has one definition, read by the
// router and by the views that abbreviate to fit it.

func TestHeaderRowsIsSeven(t *testing.T) {
	if HeaderRows != 7 {
		t.Errorf("HeaderRows = %d, want 7", HeaderRows)
	}
	if HeaderInfoSeparator != ": " {
		t.Errorf("HeaderInfoSeparator = %q, want %q", HeaderInfoSeparator, ": ")
	}
}

func TestHeaderInfoWidth(t *testing.T) {
	tests := []struct {
		termWidth, want int
	}{
		{0, 0}, {1, 0}, {2, 0}, {42, 10}, {80, 19}, {81, 19}, {82, 20}, {200, 49},
	}
	for _, tt := range tests {
		if got := HeaderInfoWidth(tt.termWidth); got != tt.want {
			t.Errorf("HeaderInfoWidth(%d) = %d, want %d", tt.termWidth, got, tt.want)
		}
	}
}

func TestHeaderValueWidth(t *testing.T) {
	tests := []struct {
		name      string
		termWidth int
		keys      []string
		want      int
	}{
		{"longest key is Refused", 80, []string{"Images", "Refused"}, 10},
		{"longest key has a space", 80, []string{"Images", "No result"}, 8},
		{"floored at zero", 40, []string{"No result"}, 0},
		{"no keys", 80, nil, 17},
	}
	for _, tt := range tests {
		t.Run(tt.name, func(t *testing.T) {
			if got := HeaderValueWidth(tt.termWidth, tt.keys); got != tt.want {
				t.Errorf("HeaderValueWidth(%d, %v) = %d, want %d", tt.termWidth, tt.keys, got, tt.want)
			}
		})
	}
}
