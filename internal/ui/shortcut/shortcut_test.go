package shortcut

import (
	"strings"
	"testing"

	"github.com/charmbracelet/lipgloss"
	"github.com/muesli/termenv"
)

// withTrueColor forces a colour profile for the run. Under go test lipgloss
// detects no TTY, falls back to Ascii and strips every escape sequence, which
// would make any assertion about styling pass whatever the code does.
func withTrueColor(t *testing.T) {
	t.Helper()
	previous := lipgloss.ColorProfile()
	lipgloss.SetColorProfile(termenv.TrueColor)
	t.Cleanup(func() { lipgloss.SetColorProfile(previous) })
}

func TestMaxLenKey(t *testing.T) {
	tests := []struct {
		name string
		in   Shortcuts
		want int
	}{
		{"empty list", Shortcuts{}, 0},
		{"nil list", nil, 0},
		{"single entry", Shortcuts{{Key: "esc"}}, 3},
		{
			name: "longest key wins regardless of position",
			in:   Shortcuts{{Key: "q"}, {Key: "ctrl+shift+x"}, {Key: "esc"}},
			want: 12,
		},
		{
			// Arrow glyphs are multi-byte; counting bytes would over-pad every
			// other row and break the column alignment.
			name: "multi-byte keys are measured in runes",
			in:   Shortcuts{{Key: "↑↓"}, {Key: "q"}},
			want: 2,
		},
	}

	for _, tt := range tests {
		t.Run(tt.name, func(t *testing.T) {
			if got := tt.in.maxLenKey(); got != tt.want {
				t.Errorf("maxLenKey() = %d, want %d", got, tt.want)
			}
		})
	}
}

func TestToStringsEmpty(t *testing.T) {
	if got := (Shortcuts{}).ToStrings(); got != nil {
		t.Errorf("ToStrings() = %v on an empty list, want nil", got)
	}
}

func TestToStringsWrapsKeysInChevrons(t *testing.T) {
	got := Shortcuts{{Key: "ctrl+s", Description: "Scan"}}.ToStrings()

	if len(got) != 1 {
		t.Fatalf("ToStrings() returned %d lines, want 1", len(got))
	}
	if !strings.Contains(got[0], "<ctrl+s>") {
		t.Errorf("line does not contain the chevron-wrapped key: %q", got[0])
	}
	if !strings.Contains(got[0], "Scan") {
		t.Errorf("line does not contain the description: %q", got[0])
	}
}

func TestToStringsOneLinePerShortcut(t *testing.T) {
	in := Shortcuts{
		{Key: "ctrl+n", Description: "New resource"},
		{Key: "enter", Description: "Open details"},
		{Key: "/", Description: "Filter"},
	}

	got := in.ToStrings()

	if len(got) != len(in) {
		t.Fatalf("ToStrings() returned %d lines, want %d", len(got), len(in))
	}
	for i, line := range got {
		if !strings.Contains(line, in[i].Description) {
			t.Errorf("line %d lost its description %q", i, in[i].Description)
		}
	}
}

// Descriptions must start at the same column for every row, which is the whole
// point of padding against the longest key.
func TestToStringsAlignsDescriptions(t *testing.T) {
	in := Shortcuts{
		{Key: "q", Description: "Quit"},
		{Key: "ctrl+shift+x", Description: "Extract"},
	}

	got := in.ToStrings()

	cols := make([]int, len(got))
	for i, line := range got {
		plain := stripANSI(line)
		cols[i] = strings.Index(plain, in[i].Description)
		if cols[i] < 0 {
			t.Fatalf("description %q not found in line %d", in[i].Description, i)
		}
	}
	if cols[0] != cols[1] {
		t.Errorf("descriptions start at columns %d and %d, want them aligned", cols[0], cols[1])
	}
}

// A disabled shortcut is still a line: the whole point is that nothing moves.
func TestADisabledShortcutKeepsItsPlaceAndItsText(t *testing.T) {
	in := Shortcuts{
		{Key: "S", Description: "Scan", Disabled: true},
		{Key: "F", Description: "Sync"},
	}

	got := in.ToStrings()

	if len(got) != 2 {
		t.Fatalf("ToStrings() returned %d lines, want 2", len(got))
	}
	if !strings.Contains(stripANSI(got[0]), "<S>") || !strings.Contains(got[0], "Scan") {
		t.Errorf("the disabled line lost its key or its description: %q", stripANSI(got[0]))
	}
}

// The key is what says "not now": it loses the colour and the weight an
// available one carries. The description is ColorDim either way.
func TestADisabledKeyIsStyledApartFromAnAvailableOne(t *testing.T) {
	withTrueColor(t)

	disabled := Shortcuts{{Key: "S", Description: "Scan", Disabled: true}}.ToStrings()[0]
	available := Shortcuts{{Key: "S", Description: "Scan"}}.ToStrings()[0]

	if disabled == available {
		t.Fatal("a disabled shortcut renders exactly like an available one — nothing tells them apart")
	}
	if stripANSI(disabled) != stripANSI(available) {
		t.Errorf("the two differ in text, not only in styling:\n disabled %q\navailable %q",
			stripANSI(disabled), stripANSI(available))
	}
}

// Alignment must not depend on what happens to be available: a column that
// re-solves its width as the cursor moves is the thing Disabled exists to stop.
func TestDisablingAShortcutDoesNotMoveTheOthers(t *testing.T) {
	enabled := Shortcuts{
		{Key: "q", Description: "Quit"},
		{Key: "ctrl+shift+x", Description: "Extract"},
	}
	disabled := Shortcuts{
		{Key: "q", Description: "Quit"},
		{Key: "ctrl+shift+x", Description: "Extract", Disabled: true},
	}

	if got, want := disabled.maxLenKey(), enabled.maxLenKey(); got != want {
		t.Errorf("maxLenKey() = %d with a disabled entry, want %d — the longest key still counts", got, want)
	}

	for i, line := range disabled.ToStrings() {
		want := strings.Index(stripANSI(enabled.ToStrings()[i]), enabled[i].Description)
		if got := strings.Index(stripANSI(line), disabled[i].Description); got != want {
			t.Errorf("description %d starts at column %d, want %d", i, got, want)
		}
	}
}

// stripANSI removes escape sequences so column positions can be compared.
func stripANSI(s string) string {
	var b strings.Builder
	inEscape := false
	for _, r := range s {
		switch {
		case r == '\x1b':
			inEscape = true
		case inEscape && r == 'm':
			inEscape = false
		case !inEscape:
			b.WriteRune(r)
		}
	}
	return b.String()
}
