// Package testutil provides helpers for driving Bubble Tea models in tests.
//
// The views in this project are testable without a terminal because of two
// properties the codebase already guarantees:
//
//   - Constructors are pure. New(cfg) performs no I/O; Docker and network calls
//     are issued from Init() as tea.Cmd values.
//   - Update() is a pure function (Model, Msg) -> (Model, Cmd). Rule 110 forbids
//     mutating the model inside a Cmd, so feeding synthetic messages to Update()
//     fully determines the resulting state.
//
// These helpers therefore build messages and drain commands; they never spin up
// a tea.Program.
package testutil

import (
	"fmt"
	"reflect"
	"strings"
	"time"

	tea "charm.land/bubbletea/v2"

	"github.com/anthnel/devdesk/internal/ui/shortcut"
)

// FastTimers shortens a package-level timer for the length of the test.
//
// Msgs and MsgOf run the commands they are handed, and a footer message's
// expiry is a tea.Tick that really sleeps for the three seconds Rule 128 gives
// it (components.FooterMsgDuration). A view batches that timer with whatever
// else it returns, so one assertion on such a command costs the suite three
// full seconds.
//
// It takes a pointer rather than importing the duration it shortens: this
// package is imported by components' own tests, and reaching back into
// components from here would be an import cycle.
//
//	testutil.FastTimers(t, &components.FooterMsgDuration)
func FastTimers(t interface{ Cleanup(func()) }, d *time.Duration) {
	restore := *d
	*d = time.Millisecond
	t.Cleanup(func() { *d = restore })
}

// namedKeys maps the key names used throughout the views (they all dispatch on
// tea.KeyMsg.String()) onto the key types that render back to those names.
// The space is deliberately absent: see spaceKey. The ctrl+a..ctrl+z names are
// computed by Key rather than listed.
var namedKeys = map[string]tea.KeyType{
	"enter":     tea.KeyEnter,
	"esc":       tea.KeyEsc,
	"escape":    tea.KeyEscape,
	"up":        tea.KeyUp,
	"down":      tea.KeyDown,
	"left":      tea.KeyLeft,
	"right":     tea.KeyRight,
	"tab":       tea.KeyTab,
	"shift+tab": tea.KeyShiftTab,
	"backspace": tea.KeyBackspace,
	"delete":    tea.KeyDelete,
	"insert":    tea.KeyInsert,
	"home":      tea.KeyHome,
	"end":       tea.KeyEnd,
	"pgup":      tea.KeyPgUp,
	"pgdown":    tea.KeyPgDown,
	"f1":        tea.KeyF1,
	"f2":        tea.KeyF2,
	"f3":        tea.KeyF3,
	"f4":        tea.KeyF4,
	"f5":        tea.KeyF5,
	"f6":        tea.KeyF6,
	"f7":        tea.KeyF7,
	"f8":        tea.KeyF8,
	"f9":        tea.KeyF9,
	"f10":       tea.KeyF10,
	"f11":       tea.KeyF11,
	"f12":       tea.KeyF12,
}

// spaceKey is the one place the space key is built. In bubbletea v1 the space
// is its own key type but also carries its rune, as a real terminal delivers it.
func spaceKey() tea.KeyMsg {
	return tea.KeyMsg{Type: tea.KeySpace, Runes: []rune{' '}}
}

// Key builds the tea.KeyMsg a terminal would deliver for the named key.
//
// Accepted names, and nothing else (anything unknown panics, so a typo fails
// loudly instead of silently typing literal runes):
//   - the names in namedKeys ("enter", "esc", "shift+tab", "pgdown", "f5"...);
//   - "ctrl+a" to "ctrl+z", computed;
//   - " " or "space", the space key;
//   - a single rune ("y", ":"), a KeyRunes message;
//   - an "alt+" prefix on any of the above, which sets Alt: Key("alt+:") is a
//     colon carrying Alt, not the five runes "alt+:".
//
// For several characters typed in a row, use Type; for a bracketed paste, Paste.
//
// The space is built, never compared through String(): in bubbletea v1
// KeyMsg.String() of the space is " ", in v2 it is "space". The v2 equivalents
// of what this function builds, for the migration that will replace it:
//
//	space:    tea.KeyPressMsg{Code: tea.KeySpace, Text: " "}
//	rune r:   tea.KeyPressMsg{Code: r, Text: string(r)}
//	ctrl+x:   tea.KeyPressMsg{Code: 'x', Mod: tea.ModCtrl}
//	alt+...:  the same with Mod: tea.ModAlt
//	enter:    tea.KeyPressMsg{Code: tea.KeyEnter}
func Key(name string) tea.KeyMsg {
	if rest, isAlt := strings.CutPrefix(name, "alt+"); isAlt && rest != "" {
		msg := Key(rest)
		msg.Alt = true
		return msg
	}
	if name == " " || name == "space" {
		return spaceKey()
	}
	if kt, ok := namedKeys[name]; ok {
		return tea.KeyMsg{Type: kt}
	}
	if letter, ok := strings.CutPrefix(name, "ctrl+"); ok && len(letter) == 1 && letter[0] >= 'a' && letter[0] <= 'z' {
		return tea.KeyMsg{Type: tea.KeyCtrlA + tea.KeyType(letter[0]-'a')}
	}
	if r := []rune(name); len(r) == 1 {
		return tea.KeyMsg{Type: tea.KeyRunes, Runes: r}
	}
	panic(fmt.Sprintf("testutil.Key: unknown key name %q (use Type for several characters, Paste for a paste)", name))
}

// Keys builds one message per name, for feeding a sequence to Update().
func Keys(names ...string) []tea.Msg {
	msgs := make([]tea.Msg, 0, len(names))
	for _, n := range names {
		msgs = append(msgs, Key(n))
	}
	return msgs
}

// Type builds one rune message per character of s, reproducing a user typing
// into a text input. Use this instead of Key(s) when the target is a
// bubbles/textinput: it consumes runes one keypress at a time.
func Type(s string) []tea.Msg {
	msgs := make([]tea.Msg, 0, len(s))
	for _, r := range s {
		if r == ' ' {
			msgs = append(msgs, spaceKey())
			continue
		}
		msgs = append(msgs, tea.KeyMsg{Type: tea.KeyRunes, Runes: []rune{r}})
	}
	return msgs
}

// Resize builds the window size message views use to lay themselves out.
func Resize(width, height int) tea.Msg {
	return tea.WindowSizeMsg{Width: width, Height: height}
}

// Msgs executes cmd and returns every message it produced, flattening the
// tea.Batch and tea.Sequence trees. A nil command, or one returning nil, yields
// no messages.
//
// The command runs synchronously on the calling goroutine. Do not pass a command
// that sleeps or performs I/O — tea.Tick blocks for its whole duration, and the
// Docker runners shell out. Assert on the command's identity or on model state
// instead for those.
func Msgs(cmd tea.Cmd) []tea.Msg {
	if cmd == nil {
		return nil
	}
	msg := cmd()
	if msg == nil {
		return nil
	}
	if batch, ok := msg.(tea.BatchMsg); ok {
		var out []tea.Msg
		for _, c := range batch {
			out = append(out, Msgs(c)...)
		}
		return out
	}
	if seq, ok := sequenced(msg); ok {
		var out []tea.Msg
		for _, c := range seq {
			out = append(out, Msgs(c)...)
		}
		return out
	}
	return []tea.Msg{msg}
}

// sequenced reports whether msg is what tea.Sequence returns, and unpacks the
// commands it holds.
//
// tea.Batch answers with the exported tea.BatchMsg, but the equivalent for
// tea.Sequence is unexported — the runtime is its only intended reader, and it
// runs the commands one after another rather than concurrently. Its underlying
// type is []tea.Cmd, so reflection recovers them without depending on the name.
// Order is preserved, which is the whole point of a sequence: a view that emits
// "scan starting" before the scan itself relies on the first message arriving
// first.
func sequenced(msg tea.Msg) ([]tea.Cmd, bool) {
	value := reflect.ValueOf(msg)
	if value.Kind() != reflect.Slice || value.Type().Elem() != reflect.TypeFor[tea.Cmd]() {
		return nil, false
	}
	// The element type was just checked, so every assertion below holds; a nil
	// entry comes back as a nil tea.Cmd, which Msgs already ignores.
	cmds := make([]tea.Cmd, 0, value.Len())
	for i := range value.Len() {
		cmds = append(cmds, value.Index(i).Interface().(tea.Cmd))
	}
	return cmds, true
}

// Msg executes cmd and returns the single message it produced, or nil.
// It is the common case of Msgs for commands that emit exactly one message.
func Msg(cmd tea.Cmd) tea.Msg {
	msgs := Msgs(cmd)
	if len(msgs) == 0 {
		return nil
	}
	return msgs[0]
}

// MsgOf reports whether cmd produced a message of type T, returning the first
// such message. It searches through batched commands.
func MsgOf[T tea.Msg](cmd tea.Cmd) (T, bool) {
	for _, m := range Msgs(cmd) {
		if v, ok := m.(T); ok {
			return v, true
		}
	}
	var zero T
	return zero, false
}

// ── Shortcuts (Rule 130) ─────────────────────────────────────────────────────

// HasShortcut reports whether the key is advertised at all, greyed or not.
func HasShortcut(shortcuts shortcut.Shortcuts, key string) bool {
	_, ok := findShortcut(shortcuts, key)
	return ok
}

// ShortcutDisabled reports whether the key is advertised but greyed out.
//
// A key that is missing altogether is **not** disabled — it is absent, which is
// a different failure. Rule 130 says an entry is greyed, never dropped, so a
// test that conflated the two would pass on the very regression it exists to
// catch; assert HasShortcut alongside it.
func ShortcutDisabled(shortcuts shortcut.Shortcuts, key string) bool {
	s, ok := findShortcut(shortcuts, key)
	return ok && s.Disabled
}

// ShortcutEnabled reports whether the key is advertised and acts.
func ShortcutEnabled(shortcuts shortcut.Shortcuts, key string) bool {
	s, ok := findShortcut(shortcuts, key)
	return ok && !s.Disabled
}

// ShortcutKeys is the advertised keys in order — what a test compares across
// states to check the column does not move.
func ShortcutKeys(shortcuts shortcut.Shortcuts) []string {
	keys := make([]string, 0, len(shortcuts))
	for _, s := range shortcuts {
		keys = append(keys, s.Key)
	}
	return keys
}

func findShortcut(shortcuts shortcut.Shortcuts, key string) (shortcut.Shortcut, bool) {
	for _, s := range shortcuts {
		if s.Key == key {
			return s, true
		}
	}
	return shortcut.Shortcut{}, false
}

// Paste builds the message a bracketed paste of s delivers: one KeyRunes
// message carrying the whole text, flagged Paste.
func Paste(s string) tea.Msg {
	return tea.KeyMsg{Type: tea.KeyRunes, Runes: []rune(s), Paste: true}
}

// IsText reports whether msg is a key message carrying text, typed or pasted:
// what a text input would insert. It replaces reading key.Type directly, which
// a v2 migration would break.
func IsText(msg tea.Msg) bool {
	km, ok := msg.(tea.KeyMsg)
	return ok && (km.Type == tea.KeyRunes || km.Type == tea.KeySpace)
}

// Text returns the text a key message carries, typed or pasted, and whether
// msg is such a message. It replaces reading KeyMsg.Runes directly, which a
// v2 migration would break.
func Text(msg tea.Msg) (string, bool) {
	if !IsText(msg) {
		return "", false
	}
	return string(msg.(tea.KeyMsg).Runes), true
}
