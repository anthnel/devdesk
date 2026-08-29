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
	"reflect"
	"strings"
	"time"

	tea "github.com/charmbracelet/bubbletea"
	"github.com/charmbracelet/lipgloss"
	"github.com/muesli/termenv"

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
	" ":         tea.KeySpace,
	"space":     tea.KeySpace,
	"backspace": tea.KeyBackspace,
	"delete":    tea.KeyDelete,
	"home":      tea.KeyHome,
	"end":       tea.KeyEnd,
	"pgup":      tea.KeyPgUp,
	"pgdown":    tea.KeyPgDown,
	"ctrl+a":    tea.KeyCtrlA,
	"ctrl+c":    tea.KeyCtrlC,
	"ctrl+d":    tea.KeyCtrlD,
	"ctrl+k":    tea.KeyCtrlK,
	"ctrl+n":    tea.KeyCtrlN,
	"ctrl+o":    tea.KeyCtrlO,
	"ctrl+p":    tea.KeyCtrlP,
	"ctrl+r":    tea.KeyCtrlR,
	"ctrl+s":    tea.KeyCtrlS,
	"ctrl+w":    tea.KeyCtrlW,
}

// Key builds the tea.KeyMsg whose String() equals name.
//
// Names listed in namedKeys map to their dedicated key type ("enter", "esc",
// "ctrl+s", " "). An "alt+" prefix sets the Alt modifier on whatever follows,
// which is how bubbletea reports the ESC-prefixed sequence a terminal sends for
// Alt — Key("alt+:") is a colon carrying Alt, not the five runes "alt+:".
// Anything else is treated as literal runes, so Key("y") and Key("gg") produce
// the rune messages a view matching on those strings expects.
func Key(name string) tea.KeyMsg {
	if rest, isAlt := strings.CutPrefix(name, "alt+"); isAlt && rest != "" {
		msg := Key(rest)
		msg.Alt = true
		return msg
	}
	if kt, ok := namedKeys[name]; ok {
		return tea.KeyMsg{Type: kt}
	}
	return tea.KeyMsg{Type: tea.KeyRunes, Runes: []rune(name)}
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

// TrueColor forces a colour profile for the length of a test.
//
// Under `go test` lipgloss detects no TTY, falls back to the Ascii profile and
// strips every escape sequence — so an assertion about styling passes whatever
// the code does. Any test checking that something is or is not styled has to
// call this first, and Rule 122's tests are exactly that: a cell that must
// carry no escape proves nothing in a profile that emits none.
func TrueColor(t interface {
	Helper()
	Cleanup(func())
}) {
	t.Helper()
	previous := lipgloss.ColorProfile()
	lipgloss.SetColorProfile(termenv.TrueColor)
	t.Cleanup(func() { lipgloss.SetColorProfile(previous) })
}
