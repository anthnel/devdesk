package testutil

import (
	"slices"
	"testing"

	tea "github.com/charmbracelet/bubbletea"
)

func TestKeySpaceIsTheSpaceKey(t *testing.T) {
	for _, name := range []string{" ", "space"} {
		t.Run(name, func(t *testing.T) {
			got := Key(name)
			if got.Type != tea.KeySpace {
				t.Errorf("Key(%q).Type = %v, want KeySpace", name, got.Type)
			}
			if !slices.Equal(got.Runes, []rune{' '}) {
				t.Errorf("Key(%q).Runes = %q, want [' ']", name, got.Runes)
			}
			if got.Alt || got.Paste {
				t.Errorf("Key(%q) carries Alt=%v Paste=%v", name, got.Alt, got.Paste)
			}
		})
	}
}

func TestKeyBuildsExpectedMessages(t *testing.T) {
	tests := []struct {
		name string
		want tea.KeyMsg
		str  string
	}{
		{"enter", tea.KeyMsg{Type: tea.KeyEnter}, "enter"},
		{"shift+tab", tea.KeyMsg{Type: tea.KeyShiftTab}, "shift+tab"},
		{"ctrl+o", tea.KeyMsg{Type: tea.KeyCtrlO}, "ctrl+o"},
		{"ctrl+z", tea.KeyMsg{Type: tea.KeyCtrlZ}, "ctrl+z"},
		{"alt+:", tea.KeyMsg{Type: tea.KeyRunes, Runes: []rune{':'}, Alt: true}, "alt+:"},
		{"y", tea.KeyMsg{Type: tea.KeyRunes, Runes: []rune{'y'}}, "y"},
	}
	for _, tc := range tests {
		t.Run(tc.name, func(t *testing.T) {
			got := Key(tc.name)
			if got.Type != tc.want.Type || got.Alt != tc.want.Alt || !slices.Equal(got.Runes, tc.want.Runes) {
				t.Errorf("Key(%q) = %#v, want %#v", tc.name, got, tc.want)
			}
			if got.String() != tc.str {
				t.Errorf("Key(%q).String() = %q, want %q", tc.name, got.String(), tc.str)
			}
		})
	}
}

func TestKeyPanicsOnUnknownName(t *testing.T) {
	for _, name := range []string{"ctrl+1", "foo", "gg", "ctrl+", ""} {
		t.Run(name, func(t *testing.T) {
			defer func() {
				if recover() == nil {
					t.Errorf("Key(%q) did not panic", name)
				}
			}()
			Key(name)
		})
	}
}

func TestTypeSpaceIsBuiltLikeKeySpace(t *testing.T) {
	msgs := Type("a b")
	if len(msgs) != 3 {
		t.Fatalf("Type(\"a b\") returned %d messages, want 3", len(msgs))
	}
	got, ok := msgs[1].(tea.KeyMsg)
	if !ok {
		t.Fatalf("msgs[1] is %T, want tea.KeyMsg", msgs[1])
	}
	want := Key(" ")
	if got.Type != want.Type || !slices.Equal(got.Runes, want.Runes) {
		t.Errorf("Type space = %#v, want %#v", got, want)
	}
}

func TestPasteIsAPasteKeyMsg(t *testing.T) {
	msg := Paste("q")
	km, ok := msg.(tea.KeyMsg)
	if !ok {
		t.Fatalf("Paste returned %T, want tea.KeyMsg", msg)
	}
	if !km.Paste {
		t.Error("Paste(\"q\").Paste = false, want true")
	}
	if km.Type != tea.KeyRunes || string(km.Runes) != "q" {
		t.Errorf("Paste(\"q\") = %#v, want KeyRunes with \"q\"", km)
	}
	if km.String() == "q" {
		t.Errorf("Paste(\"q\").String() = %q, must differ from a typed q", km.String())
	}
}

func TestIsText(t *testing.T) {
	tests := []struct {
		name string
		msg  tea.Msg
		want bool
	}{
		{"letter", Key("a"), true},
		{"space", Key(" "), true},
		{"paste", Paste("x"), true},
		{"enter", Key("enter"), false},
		{"ctrl+a", Key("ctrl+a"), false},
		{"resize", Resize(1, 1), false},
	}
	for _, tc := range tests {
		t.Run(tc.name, func(t *testing.T) {
			if got := IsText(tc.msg); got != tc.want {
				t.Errorf("IsText(%v) = %v, want %v", tc.name, got, tc.want)
			}
		})
	}
}

func TestText(t *testing.T) {
	tests := []struct {
		name   string
		msg    tea.Msg
		want   string
		wantOK bool
	}{
		{"letter", Key("a"), "a", true},
		{"space", Key(" "), " ", true},
		{"paste", Paste("q?\nx"), "q?\nx", true},
		{"enter", Key("enter"), "", false},
		{"resize", Resize(1, 1), "", false},
	}
	for _, tc := range tests {
		t.Run(tc.name, func(t *testing.T) {
			got, ok := Text(tc.msg)
			if got != tc.want || ok != tc.wantOK {
				t.Errorf("Text(%s) = %q, %v; want %q, %v", tc.name, got, ok, tc.want, tc.wantOK)
			}
		})
	}
}
