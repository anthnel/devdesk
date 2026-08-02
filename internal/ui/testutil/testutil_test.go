package testutil

import (
	"testing"

	tea "github.com/charmbracelet/bubbletea"
)

// The views dispatch on tea.KeyMsg.String(). If Key() ever stopped round-tripping
// through that method, every Update() test built on this harness would silently
// exercise the default branch instead of the intended one — so the round-trip is
// asserted here rather than assumed.
func TestKeyRoundTripsThroughString(t *testing.T) {
	names := []string{
		"enter", "esc", "up", "down", "left", "right",
		"tab", "shift+tab", " ", "backspace", "delete",
		"home", "end", "pgup", "pgdown",
		"ctrl+a", "ctrl+c", "ctrl+d", "ctrl+k", "ctrl+n",
		"ctrl+o", "ctrl+r", "ctrl+s", "ctrl+w",
	}

	for _, name := range names {
		t.Run(name, func(t *testing.T) {
			if got := Key(name).String(); got != name {
				t.Errorf("Key(%q).String() = %q, want %q", name, got, name)
			}
		})
	}
}

func TestKeyAliases(t *testing.T) {
	if got := Key("space").String(); got != " " {
		t.Errorf(`Key("space").String() = %q, want " "`, got)
	}
	if got := Key("escape").String(); got != "esc" {
		t.Errorf(`Key("escape").String() = %q, want "esc"`, got)
	}
}

// "alt+" is a modifier, not five more runes: a terminal sends ESC then the key,
// and bubbletea reports that as the key carrying Alt. Building it as literal
// runes would round-trip through String() just fine and still not be the message
// the application receives.
func TestKeyAltPrefixSetsTheModifier(t *testing.T) {
	tests := []struct {
		name string
		want tea.KeyType
	}{
		{"alt+:", tea.KeyRunes},
		{"alt+x", tea.KeyRunes},
		{"alt+enter", tea.KeyEnter},
	}

	for _, tt := range tests {
		t.Run(tt.name, func(t *testing.T) {
			msg := Key(tt.name)

			if !msg.Alt {
				t.Errorf("Key(%q).Alt is false", tt.name)
			}
			if msg.Type != tt.want {
				t.Errorf("Key(%q).Type = %v, want %v", tt.name, msg.Type, tt.want)
			}
			if got := msg.String(); got != tt.name {
				t.Errorf("Key(%q).String() = %q", tt.name, got)
			}
			if msg.Type == tea.KeyRunes && len(msg.Runes) != 1 {
				t.Errorf("Key(%q).Runes = %q, want the single key that carries Alt", tt.name, string(msg.Runes))
			}
		})
	}
}

// A bare "alt+" has no key to modify, so it stays literal rather than producing
// an Alt-modified nothing.
func TestKeyBareAltPrefixIsLiteral(t *testing.T) {
	msg := Key("alt+")

	if msg.Alt {
		t.Error(`Key("alt+").Alt is true; there is no key to modify`)
	}
	if got := msg.String(); got != "alt+" {
		t.Errorf(`Key("alt+").String() = %q`, got)
	}
}

func TestKeyRunes(t *testing.T) {
	tests := []string{"y", "n", "Y", "N", "q", "j", "k", "gg", "/"}
	for _, s := range tests {
		t.Run(s, func(t *testing.T) {
			msg := Key(s)
			if msg.Type != tea.KeyRunes {
				t.Errorf("Key(%q).Type = %v, want KeyRunes", s, msg.Type)
			}
			if got := msg.String(); got != s {
				t.Errorf("Key(%q).String() = %q, want %q", s, got, s)
			}
		})
	}
}

func TestKeysBuildsOneMessagePerName(t *testing.T) {
	msgs := Keys("up", "down", "enter")
	if len(msgs) != 3 {
		t.Fatalf("Keys() returned %d messages, want 3", len(msgs))
	}
	want := []string{"up", "down", "enter"}
	for i, m := range msgs {
		km, ok := m.(tea.KeyMsg)
		if !ok {
			t.Fatalf("message %d is %T, want tea.KeyMsg", i, m)
		}
		if km.String() != want[i] {
			t.Errorf("message %d = %q, want %q", i, km.String(), want[i])
		}
	}
}

// Type must emit one keypress per rune: a textinput consumes runes one message
// at a time, so a single multi-rune message would be dropped or mis-parsed.
func TestTypeEmitsOneKeypressPerRune(t *testing.T) {
	msgs := Type("abc")
	if len(msgs) != 3 {
		t.Fatalf("Type(\"abc\") returned %d messages, want 3", len(msgs))
	}
	for i, want := range []string{"a", "b", "c"} {
		km := msgs[i].(tea.KeyMsg)
		if len(km.Runes) != 1 {
			t.Errorf("message %d carries %d runes, want 1", i, len(km.Runes))
		}
		if km.String() != want {
			t.Errorf("message %d = %q, want %q", i, km.String(), want)
		}
	}
}

func TestTypeEmpty(t *testing.T) {
	if msgs := Type(""); len(msgs) != 0 {
		t.Errorf("Type(\"\") returned %d messages, want 0", len(msgs))
	}
}

func TestResize(t *testing.T) {
	msg, ok := Resize(120, 40).(tea.WindowSizeMsg)
	if !ok {
		t.Fatalf("Resize() returned %T, want tea.WindowSizeMsg", msg)
	}
	if msg.Width != 120 || msg.Height != 40 {
		t.Errorf("Resize(120, 40) = %dx%d, want 120x40", msg.Width, msg.Height)
	}
}

type msgAlpha struct{ n int }
type msgBeta struct{}

func TestMsgsNilCommand(t *testing.T) {
	if got := Msgs(nil); got != nil {
		t.Errorf("Msgs(nil) = %v, want nil", got)
	}
}

func TestMsgsCommandReturningNil(t *testing.T) {
	cmd := func() tea.Msg { return nil }
	if got := Msgs(cmd); got != nil {
		t.Errorf("Msgs() = %v, want nil for a command producing no message", got)
	}
}

func TestMsgsSingleCommand(t *testing.T) {
	cmd := func() tea.Msg { return msgAlpha{n: 7} }
	msgs := Msgs(cmd)
	if len(msgs) != 1 {
		t.Fatalf("Msgs() returned %d messages, want 1", len(msgs))
	}
	if got, ok := msgs[0].(msgAlpha); !ok || got.n != 7 {
		t.Errorf("Msgs()[0] = %#v, want msgAlpha{n:7}", msgs[0])
	}
}

// tea.Batch nests commands one level; tea.Batch of a tea.Batch nests further.
// Msgs must flatten the whole tree, otherwise a batched message would be
// reported as an opaque BatchMsg.
func TestMsgsFlattensNestedBatches(t *testing.T) {
	inner := tea.Batch(
		func() tea.Msg { return msgAlpha{n: 2} },
		func() tea.Msg { return msgBeta{} },
	)
	cmd := tea.Batch(
		func() tea.Msg { return msgAlpha{n: 1} },
		inner,
	)

	msgs := Msgs(cmd)
	if len(msgs) != 3 {
		t.Fatalf("Msgs() returned %d messages, want 3", len(msgs))
	}
	for _, m := range msgs {
		if _, isBatch := m.(tea.BatchMsg); isBatch {
			t.Fatal("Msgs() leaked a tea.BatchMsg instead of flattening it")
		}
	}
}

func TestMsgReturnsFirstOrNil(t *testing.T) {
	if got := Msg(nil); got != nil {
		t.Errorf("Msg(nil) = %v, want nil", got)
	}
	cmd := func() tea.Msg { return msgBeta{} }
	if _, ok := Msg(cmd).(msgBeta); !ok {
		t.Errorf("Msg() = %T, want msgBeta", Msg(cmd))
	}
}

func TestMsgOfFindsTypeInBatch(t *testing.T) {
	cmd := tea.Batch(
		func() tea.Msg { return msgAlpha{n: 42} },
		func() tea.Msg { return msgBeta{} },
	)

	got, ok := MsgOf[msgAlpha](cmd)
	if !ok {
		t.Fatal("MsgOf[msgAlpha]() did not find the message")
	}
	if got.n != 42 {
		t.Errorf("MsgOf[msgAlpha]().n = %d, want 42", got.n)
	}

	if _, ok := MsgOf[msgBeta](cmd); !ok {
		t.Error("MsgOf[msgBeta]() did not find the message")
	}
}

func TestMsgOfAbsentType(t *testing.T) {
	cmd := func() tea.Msg { return msgBeta{} }
	if _, ok := MsgOf[msgAlpha](cmd); ok {
		t.Error("MsgOf[msgAlpha]() reported a message that was never produced")
	}
	if _, ok := MsgOf[msgAlpha](nil); ok {
		t.Error("MsgOf[msgAlpha](nil) reported a message")
	}
}
