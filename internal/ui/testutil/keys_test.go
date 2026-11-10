package testutil

import (
	"testing"

	tea "charm.land/bubbletea/v2"

	"github.com/anthnel/devdesk/internal/ui/keymap"
)

// [R-compile] against the v1 helper (Key returned a KeyMsg with no Code/Text).
func TestKeySpaceIsTheSpaceKey(t *testing.T) {
	for _, name := range []string{" ", "space"} {
		t.Run(name, func(t *testing.T) {
			got := Key(name)
			if got.Code != tea.KeySpace || got.Text != " " || got.Mod != 0 {
				t.Errorf("Key(%q) = %#v, want {Code: KeySpace, Text: \" \"}", name, got)
			}
			if got.String() != "space" {
				t.Errorf("Key(%q).String() = %q, want \"space\" (computed by bubbletea v2)", name, got.String())
			}
		})
	}
}

// [R-compile] the expected values are v2 fields.
func TestKeyBuildsExpectedMessages(t *testing.T) {
	tests := []struct {
		name string
		want tea.KeyPressMsg
		str  string
	}{
		{"enter", tea.KeyPressMsg{Code: tea.KeyEnter}, "enter"},
		{"shift+tab", tea.KeyPressMsg{Code: tea.KeyTab, Mod: tea.ModShift}, "shift+tab"},
		{"ctrl+o", tea.KeyPressMsg{Code: 'o', Mod: tea.ModCtrl}, "ctrl+o"},
		{"ctrl+z", tea.KeyPressMsg{Code: 'z', Mod: tea.ModCtrl}, "ctrl+z"},
		{"alt+:", tea.KeyPressMsg{Code: ':', Mod: tea.ModAlt}, "alt+:"},
		{"y", tea.KeyPressMsg{Code: 'y', Text: "y"}, "y"},
		{"S", tea.KeyPressMsg{Code: 'S', Text: "S"}, "S"},
	}
	for _, tc := range tests {
		t.Run(tc.name, func(t *testing.T) {
			got := Key(tc.name)
			if got != tc.want {
				t.Errorf("Key(%q) = %#v, want %#v", tc.name, got, tc.want)
			}
			if got.String() != tc.str {
				t.Errorf("Key(%q).String() = %q, want %q", tc.name, got.String(), tc.str)
			}
		})
	}
}

// [C] unchanged behaviour: an unknown name still panics.
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

// [R-compile] the space is a KeyPressMsg, identical to Key("space").
func TestTypeSpaceIsBuiltLikeKeySpace(t *testing.T) {
	msgs := Type("a b")
	if len(msgs) != 3 {
		t.Fatalf("Type(\"a b\") returned %d messages, want 3", len(msgs))
	}
	got, ok := msgs[1].(tea.KeyPressMsg)
	if !ok {
		t.Fatalf("msgs[1] is %T, want tea.KeyPressMsg", msgs[1])
	}
	if got != Key("space") {
		t.Errorf("Type space = %#v, want %#v", got, Key("space"))
	}
}

// [R-compile] a paste is a tea.PasteMsg, never a key press (replaces TestPasteIsAPasteKeyMsg).
func TestPasteIsAPasteMsg(t *testing.T) {
	msg := Paste("a\nb")
	if got, ok := msg.(tea.PasteMsg); !ok || got.Content != "a\nb" {
		t.Fatalf("Paste(\"a\\nb\") = %#v, want tea.PasteMsg{Content: \"a\\nb\"}", msg)
	}
	if _, ok := msg.(tea.KeyMsg); ok {
		t.Error("a paste must not satisfy tea.KeyMsg: views would treat it as a key")
	}
}

// [R-compile] IsText now reads Text/Mod and PasteMsg.
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
		{"alt+x", Key("alt+x"), false},
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

// [R-compile] same as TestIsText.
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
		{"alt+x", Key("alt+x"), "", false},
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

// TestTheSpaceKeyIsNamedLikeKeymapSpace is the migration tripwire: String() is
// computed by bubbletea ("space" in v2), so keymap.Space must say the same.
// [R-assert]: compiles today and fails while keymap.Space is " "; goes green
// when the constant flips to "space".
func TestTheSpaceKeyIsNamedLikeKeymapSpace(t *testing.T) {
	if got := Key("space").String(); got != keymap.Space {
		t.Errorf("Key(\"space\").String() = %q, keymap.Space = %q: bubbletea and the constant disagree on the space key's name", got, keymap.Space)
	}
}
