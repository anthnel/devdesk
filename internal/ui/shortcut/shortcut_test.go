package shortcut

import (
	"strings"
	"testing"
)

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
