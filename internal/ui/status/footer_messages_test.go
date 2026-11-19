package status

import (
	"strings"
	"testing"

	"github.com/anthnel/devdesk/internal/ui/components"
	"github.com/anthnel/devdesk/internal/ui/keymap"
	"github.com/anthnel/devdesk/internal/ui/testutil"
)

// DEVDESK-589: Update must offer ClearFooterMsg and PostFooterMsg to the
// footer. Before the fix a timed message never expired on this view and the
// router's PostFooterMsg never reached its footer (Rule 128).

// footerLevels is the three levels a view can set, each with the setter that
// returns the expiry timer. The Cmd is never run: the timer really sleeps 3 s.
var footerLevels = []struct {
	name  string
	level components.Level
	set   func(f *components.FooterMessage, text string)
}{
	{"info", components.LevelInfo, func(f *components.FooterMessage, text string) { _ = f.Info(text) }},
	{"warning", components.LevelWarning, func(f *components.FooterMessage, text string) { _ = f.Warn(text) }},
	{"error", components.LevelError, func(f *components.FooterMessage, text string) { _ = f.Error(text) }},
}

// Acceptance criterion 1: a timed message disappears when its expiry reaches Update.
func TestUpdateExpiresATimedFooterMessage(t *testing.T) {
	const text = "Copied to clipboard"

	for _, tc := range footerLevels {
		t.Run(tc.name, func(t *testing.T) {
			m := loadedModel(t)
			tc.set(&m.footer, text)
			if !strings.Contains(footerText(m), text) {
				t.Fatalf("fixture error: footer = %q, want the message before its expiry", footerText(m))
			}

			m = feed(t, m, components.ClearFooterMsg{ID: m.footer.ID()})

			if m.footer.IsSet() {
				t.Errorf("footer still holds %q after its ClearFooterMsg went through Update", m.footer.Text())
			}
			if got := footerText(m); strings.Contains(got, text) {
				t.Errorf("footer = %q after the expiry, want the message gone", got)
			}
		})
	}
}

// A stale expiry (its predecessor's timer) or one addressed to another view
// must leave the message on screen. This holds before and after the fix; it
// pins the contract so the fix cannot clear unconditionally.
func TestUpdateKeepsAFooterMessageOnAStaleExpiry(t *testing.T) {
	const text = "Copied to clipboard"

	m := loadedModel(t)
	_ = m.footer.Info(text)
	stale := m.footer.ID() - 1

	m = feed(t, m, components.ClearFooterMsg{ID: stale})

	if got := footerText(m); !strings.Contains(got, text) {
		t.Errorf("footer = %q after a stale ClearFooterMsg, want %q to stay", got, text)
	}
}

// Acceptance criterion 2: the router's broadcast lands in the active Status view.
func TestUpdateAdoptsAPostFooterMsg(t *testing.T) {
	const text = "MCP server did not start"

	for _, tc := range footerLevels {
		t.Run(tc.name, func(t *testing.T) {
			m := loadedModel(t)
			if m.footer.IsSet() {
				t.Fatal("fixture error: the footer holds a message before the broadcast")
			}

			m = feed(t, m, components.PostFooterMsg{ID: 9001, Level: tc.level, Text: text})

			if got := m.footer.Text(); got != text {
				t.Errorf("footer.Text() = %q after a PostFooterMsg, want %q", got, text)
			}
			if got := m.footer.Level(); got != tc.level {
				t.Errorf("footer.Level() = %v, want %v", got, tc.level)
			}
			if got := footerText(m); !strings.Contains(got, text) {
				t.Errorf("rendered footer = %q, want it to show %q", got, text)
			}
		})
	}
}

// The broadcast carries its own ID, and the timer the router built with it
// names that ID: once adopted, that expiry must clear it through Update.
func TestPostFooterMsgExpiresThroughUpdate(t *testing.T) {
	const text = "MCP server did not start"

	m := loadedModel(t)
	m = feed(t, m, components.PostFooterMsg{ID: 9002, Level: components.LevelError, Text: text})
	if !strings.Contains(footerText(m), text) {
		t.Fatalf("footer = %q after a PostFooterMsg, want the broadcast on screen (nothing to expire otherwise)", footerText(m))
	}

	m = feed(t, m, components.ClearFooterMsg{ID: 9002})

	if got := footerText(m); strings.Contains(got, text) {
		t.Errorf("footer = %q after the broadcast's own expiry, want it gone", got)
	}
}

// Update hands a message to the table only outside a form or a confirmation.
// The footer's expiry and the router's broadcast must not depend on that: a
// message set before the form opened would otherwise stay on screen for as
// long as the form is open and past it.
func TestFooterMessagesReachTheFooterWhileAFormOrConfirmationIsOpen(t *testing.T) {
	const text = "Copied to clipboard"

	modes := []struct {
		name string
		open func(t *testing.T, m Model) Model
	}{
		{"form", func(t *testing.T, m Model) Model {
			m = feed(t, m, testutil.Key(keymap.New))
			if m.componentForm == nil {
				t.Fatal("fixture error: the component form is not open")
			}
			return m
		}},
		{"confirmation", func(t *testing.T, m Model) Model {
			m = feed(t, m, testutil.Key(keymap.Delete))
			if m.confirmModal == nil {
				t.Fatal("fixture error: the confirmation is not open")
			}
			return m
		}},
	}

	for _, mode := range modes {
		t.Run(mode.name+" expires a timed message", func(t *testing.T) {
			m := mode.open(t, loadedModel(t))
			_ = m.footer.Info(text)

			m = feed(t, m, components.ClearFooterMsg{ID: m.footer.ID()})

			if m.footer.IsSet() {
				t.Errorf("footer still holds %q after its ClearFooterMsg with the %s open", m.footer.Text(), mode.name)
			}
		})

		t.Run(mode.name+" adopts a PostFooterMsg", func(t *testing.T) {
			m := mode.open(t, loadedModel(t))

			m = feed(t, m, components.PostFooterMsg{ID: 9003, Level: components.LevelError, Text: text})

			if got := m.footer.Text(); got != text {
				t.Errorf("footer.Text() = %q with the %s open, want %q", got, mode.name, text)
			}
		})
	}
}
