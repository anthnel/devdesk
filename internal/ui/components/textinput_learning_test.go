package components

import (
	"strings"
	"testing"

	"charm.land/bubbles/v2/textinput"
)

// TestTextInputCursorCountsCellsAndScroll is a learning test: it pins what
// textinput.Cursor() reports for the hardware cursor column, because
// bubbles v2.2.1 reports the rune index (wrong for wide characters, and not
// relative to the scroll offset) and v2.2.2 reports the display width of the
// visible text before the cursor. Only the X column is read; the prompt is
// emptied so X is the column inside the field.
func TestTextInputCursorCountsCellsAndScroll(t *testing.T) {
	tests := []struct {
		name    string
		value   string
		width   int
		place   func(m *textinput.Model)
		virtual bool
		blurred bool
		wantNil bool
		wantX   int
	}{
		{
			name:  "double-width CJK characters count two cells each",
			value: "日本語",
			place: func(m *textinput.Model) { m.SetCursor(2) },
			wantX: 4,
		},
		{
			name:  "an emoji counts two cells",
			value: "😀😀",
			place: func(m *textinput.Model) { m.SetCursor(1) },
			wantX: 2,
		},
		{
			name:  "cursor at the end of text longer than the width stays inside the field",
			value: strings.Repeat("a", 30),
			width: 10,
			place: func(m *textinput.Model) { m.CursorEnd() },
			wantX: 10,
		},
		{
			name:  "scrolled field: cursor three runes before the end is relative to the scroll offset",
			value: strings.Repeat("a", 30),
			width: 10,
			place: func(m *textinput.Model) { m.CursorEnd(); m.SetCursor(27) },
			wantX: 7,
		},
		{
			// Pinned as observed on v2.2.2: the scroll offset is computed in
			// runes against the width in cells, so the visible part before
			// the cursor is 3 wide characters (6 cells), not 2. What matters
			// is that the column is in cells and stays inside the field.
			name:  "scrolled wide text: column is the cell width of the visible part before the cursor",
			value: strings.Repeat("日", 15),
			width: 10,
			place: func(m *textinput.Model) { m.CursorEnd(); m.SetCursor(13) },
			wantX: 6,
		},
		{
			// Pinned as observed on v2.2.2: the value holds two runes (e and
			// U+0301); the combining mark has zero width, so the cursor after
			// both sits at column 1. v2.2.1 reports the rune index, 2.
			name:  "decomposed e + combining acute is one cell wide",
			value: "e\u0301",
			place: func(m *textinput.Model) { m.CursorEnd() },
			wantX: 1,
		},
		{
			name:    "virtual cursor yields no hardware cursor",
			value:   "abc",
			place:   func(m *textinput.Model) { m.CursorEnd() },
			virtual: true,
			wantNil: true,
		},
		{
			name:    "blurred field yields no hardware cursor",
			value:   "abc",
			place:   func(m *textinput.Model) { m.CursorEnd() },
			blurred: true,
			wantNil: true,
		},
	}

	for _, tt := range tests {
		t.Run(tt.name, func(t *testing.T) {
			m := textinput.New()
			m.Prompt = ""
			m.SetVirtualCursor(tt.virtual)
			if tt.width > 0 {
				m.SetWidth(tt.width)
			}
			m.SetValue(tt.value)
			if !tt.blurred {
				m.Focus()
			}
			tt.place(&m)

			c := m.Cursor()
			if tt.wantNil {
				if c != nil {
					t.Fatalf("Cursor() = %+v, want nil", c)
				}
				return
			}
			if c == nil {
				t.Fatal("Cursor() = nil, want a hardware cursor")
			}
			if c.X != tt.wantX {
				t.Errorf("Cursor().X = %d, want %d (pos=%d)", c.X, tt.wantX, m.Position())
			}
		})
	}
}
