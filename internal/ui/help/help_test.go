package help

import (
	"strings"
	"testing"
	"unicode/utf8"
)

func TestWordWrap(t *testing.T) {
	tests := []struct {
		name  string
		text  string
		width int
		want  []string
	}{
		{
			name:  "text shorter than the width stays on one line",
			text:  "short text",
			width: 40,
			want:  []string{"short text"},
		},
		{
			name:  "wrapping happens at word boundaries",
			text:  "aaa bbb ccc ddd",
			width: 7,
			want:  []string{"aaa bbb", "ccc ddd"},
		},
		{
			name:  "a word longer than the width is not broken",
			text:  "supercalifragilistic word",
			width: 10,
			want:  []string{"supercalifragilistic", "word"},
		},
		{
			name:  "existing newlines are preserved as paragraph breaks",
			text:  "first\nsecond",
			width: 40,
			want:  []string{"first", "second"},
		},
		{
			name:  "empty paragraphs become blank lines",
			text:  "first\n\nsecond",
			width: 40,
			want:  []string{"first", "", "second"},
		},
	}

	for _, tt := range tests {
		t.Run(tt.name, func(t *testing.T) {
			got := strings.Split(strings.TrimRight(wordWrap(tt.text, tt.width), "\n"), "\n")
			if len(got) != len(tt.want) {
				t.Fatalf("wordWrap() = %q, want %q", got, tt.want)
			}
			for i := range got {
				if got[i] != tt.want[i] {
					t.Errorf("line %d = %q, want %q", i, got[i], tt.want[i])
				}
			}
		})
	}
}

// A non-positive width would otherwise drive the accumulator negative; the guard
// returns the text untouched instead.
func TestWordWrapNonPositiveWidth(t *testing.T) {
	for _, w := range []int{0, -1} {
		if got := wordWrap("some text", w); got != "some text" {
			t.Errorf("wordWrap(_, %d) = %q, want the input unchanged", w, got)
		}
	}
}

func TestWordWrapEmpty(t *testing.T) {
	if got := wordWrap("", 40); got != "\n" {
		t.Errorf("wordWrap(\"\", 40) = %q, want a single newline", got)
	}
}

// wordWrap measures words with len(), which counts bytes. Accented text
// therefore wraps earlier than its rendered width requires: two 5-rune words
// occupy 11 columns but 21 bytes, so a width of 12 wraps them apart.
func TestWordWrapMeasuresColumnsNotBytes(t *testing.T) {
	const width = 12
	text := "ééééé ééééé" // 11 columns, 21 bytes

	lines := strings.Split(strings.TrimRight(wordWrap(text, width), "\n"), "\n")

	if len(lines) != 1 {
		t.Errorf("wrapped into %d lines at width %d although the text renders in 11 columns", len(lines), width)
	}
}

func TestWordWrapBreaksWhenColumnsActuallyExceedWidth(t *testing.T) {
	const width = 8
	text := "ééééé ééééé" // two 5-column words: 11 columns total

	lines := strings.Split(strings.TrimRight(wordWrap(text, width), "\n"), "\n")

	if len(lines) != 2 {
		t.Fatalf("wrapped into %d lines at width %d, want 2", len(lines), width)
	}
	for i, line := range lines {
		if got := utf8.RuneCountInString(line); got != 5 {
			t.Errorf("line %d is %d runes (%q), want 5", i, got, line)
		}
	}
}

func TestRenderIncludesTitleAndDescription(t *testing.T) {
	c := Content{
		Title:       "Workspaces",
		Description: "Browse local git repositories.",
	}

	got := Render(c, 80)

	for _, want := range []string{"Workspaces", "Browse local git repositories."} {
		if !strings.Contains(got, want) {
			t.Errorf("Render() does not contain %q", want)
		}
	}
}

func TestRenderOmitsEmptySections(t *testing.T) {
	got := Render(Content{Title: "Bare"}, 80)

	if strings.Contains(got, "Keyboard Shortcuts") {
		t.Error("Render() emitted the shortcuts heading with no key bindings")
	}
}

func TestRenderIncludesKeyBindings(t *testing.T) {
	c := Content{
		Title: "Containers",
		KeyBindings: []KeyBinding{
			{Key: "ctrl+s", Description: "Scan image"},
			{Key: "enter", Description: "Open details"},
		},
	}

	got := Render(c, 80)

	if !strings.Contains(got, "Keyboard Shortcuts") {
		t.Error("Render() is missing the shortcuts heading")
	}
	for _, want := range []string{"ctrl+s", "Scan image", "enter", "Open details"} {
		if !strings.Contains(got, want) {
			t.Errorf("Render() does not contain %q", want)
		}
	}
}

func TestRenderIncludesSections(t *testing.T) {
	c := Content{
		Title: "Security",
		Sections: []Section{
			{Title: "Scanners", Body: "Trivy covers CVEs. Gitleaks covers secrets."},
			{Title: "Cache", Body: "Results are cached per repository."},
		},
	}

	got := Render(c, 80)

	for _, want := range []string{"Scanners", "Trivy covers CVEs", "Cache", "cached per repository"} {
		if !strings.Contains(got, want) {
			t.Errorf("Render() does not contain %q", want)
		}
	}
}

// The modal reserves 6 columns for padding and borders, with a floor so a narrow
// terminal cannot produce a negative inner width.
func TestRenderNarrowWidthDoesNotPanic(t *testing.T) {
	c := Content{
		Title:       "Narrow",
		Description: "Some description text that is longer than the available width.",
		KeyBindings: []KeyBinding{{Key: "q", Description: "Quit"}},
	}

	for _, w := range []int{0, 1, 10, 45} {
		got := Render(c, w)
		if got == "" {
			t.Errorf("Render(_, %d) returned an empty string", w)
		}
	}
}

func TestRenderKeyBindingsAlignsDescriptions(t *testing.T) {
	bindings := []KeyBinding{
		{Key: "q", Description: "Quit"},
		{Key: "ctrl+shift+x", Description: "Extract"},
	}

	lines := strings.Split(strings.TrimRight(renderKeyBindings(bindings, 80), "\n"), "\n")
	if len(lines) != 2 {
		t.Fatalf("renderKeyBindings() produced %d lines, want 2", len(lines))
	}

	cols := make([]int, 2)
	for i, line := range lines {
		cols[i] = strings.Index(stripANSI(line), bindings[i].Description)
		if cols[i] < 0 {
			t.Fatalf("description %q missing from line %d", bindings[i].Description, i)
		}
	}
	if cols[0] != cols[1] {
		t.Errorf("descriptions start at columns %d and %d, want them aligned", cols[0], cols[1])
	}
}

func TestRenderKeyBindingsEmpty(t *testing.T) {
	if got := renderKeyBindings(nil, 80); got != "" {
		t.Errorf("renderKeyBindings(nil, 80) = %q, want an empty string", got)
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
