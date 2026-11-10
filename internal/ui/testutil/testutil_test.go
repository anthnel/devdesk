package testutil

import (
	"testing"

	tea "charm.land/bubbletea/v2"
)

// The views dispatch on tea.KeyMsg.String(). If Key() ever stopped round-tripping
// through that method, every Update() test built on this harness would silently
// exercise the default branch instead of the intended one — so the round-trip is
// asserted here rather than assumed.
func TestKeyRoundTripsThroughString(t *testing.T) {
	names := []string{
		"enter", "esc", "up", "down", "left", "right",
		"tab", "shift+tab", "space", "backspace", "delete",
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
	if got := Key("space").String(); got != "space" {
		t.Errorf(`Key("space").String() = %q, want "space"`, got)
	}
	if got := Key("escape").String(); got != "esc" {
		t.Errorf(`Key("escape").String() = %q, want "esc"`, got)
	}
}

// "alt+" is a modifier, not five more runes: a terminal sends ESC then the key,
// and bubbletea reports that as the key carrying Alt. v2 would print the text
// instead of "alt+x" if Text were left set, so Key clears it.
func TestKeyAltPrefixSetsTheModifier(t *testing.T) {
	for _, name := range []string{"alt+:", "alt+x", "alt+enter"} {
		t.Run(name, func(t *testing.T) {
			msg := Key(name)
			if !msg.Mod.Contains(tea.ModAlt) {
				t.Errorf("Key(%q) lacks ModAlt", name)
			}
			if msg.Text != "" {
				t.Errorf("Key(%q).Text = %q, want it cleared", name, msg.Text)
			}
			if got := msg.String(); got != name {
				t.Errorf("Key(%q).String() = %q", name, got)
			}
		})
	}
}

// A bare "alt+" has no key to modify; Key is strict and refuses it.
func TestKeyBareAltPrefixPanics(t *testing.T) {
	defer func() {
		if recover() == nil {
			t.Error(`Key("alt+") did not panic`)
		}
	}()
	Key("alt+")
}

func TestKeyRunes(t *testing.T) {
	tests := []string{"y", "n", "Y", "N", "q", "j", "k", "/", "S", ":"}
	for _, s := range tests {
		t.Run(s, func(t *testing.T) {
			msg := Key(s)
			if msg.Text != s || msg.Mod != 0 {
				t.Errorf("Key(%q) = %#v, want its text set and no modifier", s, msg)
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
		km, ok := m.(tea.KeyPressMsg)
		if !ok {
			t.Fatalf("message %d is %T, want tea.KeyPressMsg", i, m)
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
		km := msgs[i].(tea.KeyPressMsg)
		if len([]rune(km.Text)) != 1 {
			t.Errorf("message %d carries text %q, want one character", i, km.Text)
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

// msgSlice is a message that is a slice but not a sequence, which is what keeps
// the sequence unpacking from being a blanket "any slice" rule.
type msgSlice []int

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

// tea.Sequence is what a view uses when the order matters — "scan starting"
// has to reach Update before the scan itself. Its message type is unexported,
// so without the reflection in sequenced() a sequenced command would report as
// one opaque message and the commands inside it would never run.
func TestMsgsFlattensSequencesInOrder(t *testing.T) {
	cmd := tea.Sequence(
		func() tea.Msg { return msgAlpha{n: 1} },
		tea.Batch(
			func() tea.Msg { return msgAlpha{n: 2} },
			func() tea.Msg { return msgBeta{} },
		),
	)

	msgs := Msgs(cmd)
	if len(msgs) != 3 {
		t.Fatalf("Msgs() returned %d messages, want 3", len(msgs))
	}
	first, ok := msgs[0].(msgAlpha)
	if !ok || first.n != 1 {
		t.Errorf("Msgs()[0] = %#v, want the first command of the sequence", msgs[0])
	}
}

// The unpacking is by shape rather than by type name, so it has to leave alone
// a message that merely happens to be a slice.
func TestMsgsLeavesAnOrdinarySliceMessageAlone(t *testing.T) {
	cmd := func() tea.Msg { return msgSlice{1, 2, 3} }

	msgs := Msgs(cmd)
	if len(msgs) != 1 {
		t.Fatalf("Msgs() returned %d messages, want the slice kept whole", len(msgs))
	}
	if _, ok := msgs[0].(msgSlice); !ok {
		t.Errorf("Msgs()[0] = %T, want msgSlice", msgs[0])
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
