package configuration

import (
	"bytes"
	"errors"
	"log"
	"strings"
	"testing"

	"github.com/anthnel/devdesk/internal/config"
	sharedcomponents "github.com/anthnel/devdesk/internal/ui/components"
	"github.com/anthnel/devdesk/internal/ui/keymap"
	"github.com/anthnel/devdesk/internal/ui/testutil"
)

const (
	testMCPAddr  = "127.0.0.1:54321"
	testMCPToken = "tok_Zm9vYmFyLWJhei1xdXV4"
)

// serving is the view as it is built once the server has bound.
func serving(t *testing.T) Model {
	t.Helper()
	m := New(config.Default(), MCPFacts{Addr: testMCPAddr, Token: testMCPToken})
	return feed(t, m, testutil.Resize(120, 30))
}

// stubClipboard takes the clipboard write for the test, and reports what was
// written to it. The real one is the machine's, and a headless runner has none.
func stubClipboard(t *testing.T, err error) *[]string {
	t.Helper()
	var written []string
	orig := writeClipboard
	writeClipboard = func(s string) error {
		written = append(written, s)
		return err
	}
	t.Cleanup(func() { writeClipboard = orig })
	return &written
}

// pressCopy presses Y and runs whatever it asked for, as the runtime would.
// A refusal's Cmd is the footer's timer, which really sleeps — shortened here.
func pressCopy(t *testing.T, m Model) Model {
	t.Helper()
	testutil.FastTimers(t, &sharedcomponents.FooterMsgDuration)
	updated, cmd := m.Update(testutil.Key(keymap.Copy))
	m = updated.(Model)
	if msg, ok := testutil.MsgOf[MCPCommandCopiedMsg](cmd); ok {
		m = feed(t, m, msg)
	}
	return m
}

// The command is built from the address the listener actually bound, not from
// mcp.listen as typed — a port 0 would otherwise be copied as a port nobody
// listens on.
func TestTheConnectCommandUsesTheBoundAddress(t *testing.T) {
	written := stubClipboard(t, nil)
	cfg := config.Default()
	cfg.MCP.Listen = "127.0.0.1:0"
	m := feed(t, New(cfg, MCPFacts{Addr: testMCPAddr, Token: testMCPToken}), testutil.Resize(120, 30))

	m = pressCopy(t, focusOn(t, m, "State"))

	want := `claude mcp add --transport http devdesk http://127.0.0.1:54321 --header "Authorization: Bearer ` + testMCPToken + `"`
	if len(*written) != 1 || (*written)[0] != want {
		t.Fatalf("clipboard = %q, want exactly %q", *written, want)
	}
	if !strings.Contains(m.footer.Text(), "contains the token") {
		t.Errorf("footer = %q, want it to say a secret was put on the clipboard", m.footer.Text())
	}
}

// Y is an action, so it is refused with a reason rather than silently, and
// greyed on exactly the states where it is refused (Rule 130).
func TestYIsRefusedWithAReasonWhereItCannotCopy(t *testing.T) {
	for _, tt := range []struct {
		name   string
		model  func(t *testing.T) Model
		reason string
	}{
		{"another tab", func(t *testing.T) Model { return focusOn(t, serving(t), "Theme") }, reasonNotOnMCPTab},
		{"server not running", func(t *testing.T) Model { return focusOn(t, newModel(t), "State") }, reasonMCPNotRunning},
		{"server failed", func(t *testing.T) Model {
			m := feed(t, New(config.Default(), MCPFacts{Reason: "address already in use"}), testutil.Resize(120, 30))
			return focusOn(t, m, "State")
		}, reasonMCPNotRunning},
	} {
		t.Run(tt.name, func(t *testing.T) {
			written := stubClipboard(t, nil)
			m := tt.model(t)

			if !testutil.ShortcutDisabled(m.GetShortcuts(), keymap.Copy) {
				t.Errorf("Y is offered where it is refused")
			}
			m = pressCopy(t, m)
			if len(*written) != 0 {
				t.Errorf("clipboard written: %q", *written)
			}
			if m.footer.Text() != tt.reason {
				t.Errorf("footer = %q, want %q", m.footer.Text(), tt.reason)
			}
		})
	}
}

func TestYIsOfferedOnTheMCPTabWhileServing(t *testing.T) {
	for _, label := range []string{"Enabled", "State", "Token"} {
		m := focusOn(t, serving(t), label)
		if !testutil.ShortcutEnabled(m.GetShortcuts(), keymap.Copy) {
			t.Errorf("Y is greyed on %q while the server is serving", label)
		}
	}
}

// A text field keeps its letters: Y typed into Listen is a character, and the
// header does not announce it as an action there.
func TestYIsTypedIntoAFocusedTextField(t *testing.T) {
	written := stubClipboard(t, nil)
	m := focusOn(t, serving(t), "Listen")
	before := m.input.Value()

	if !testutil.ShortcutDisabled(m.GetShortcuts(), keymap.Copy) {
		t.Error("Y is announced as an action on a field it is typed into")
	}
	m = pressCopy(t, m)
	if len(*written) != 0 {
		t.Errorf("clipboard written from a text field: %q", *written)
	}
	if got := m.input.Value(); got != before+"Y" {
		t.Errorf("input = %q, want %q", got, before+"Y")
	}
}

// Rule 130: the column keeps its shape whether or not there is something to
// copy.
func TestTheMCPTabAdvertisesTheSameKeysServingOrNot(t *testing.T) {
	on := testutil.ShortcutKeys(focusOn(t, serving(t), "State").GetShortcuts())
	off := testutil.ShortcutKeys(focusOn(t, newModel(t), "State").GetShortcuts())
	if strings.Join(on, " ") != strings.Join(off, " ") {
		t.Errorf("serving %v, not serving %v — the set of keys must not change", on, off)
	}
}

// §3.77, question 4: no log line carries the command or the token, whether the
// write succeeds or fails.
func TestTheTokenNeverReachesTheLog(t *testing.T) {
	for _, tt := range []struct {
		name string
		err  error
	}{
		{"success", nil},
		// The failure path is the one that logs; it must name the error only.
		{"failure", errors.New("exec: xclip not found")},
	} {
		t.Run(tt.name, func(t *testing.T) {
			var buf bytes.Buffer
			orig, flags := log.Writer(), log.Flags()
			log.SetOutput(&buf)
			t.Cleanup(func() { log.SetOutput(orig); log.SetFlags(flags) })

			stubClipboard(t, tt.err)
			m := pressCopy(t, focusOn(t, serving(t), "Token"))

			if strings.Contains(buf.String(), testMCPToken) {
				t.Errorf("the token reached the log:\n%s", buf.String())
			}
			if tt.err != nil && !strings.Contains(m.footer.Text(), "Failed") {
				t.Errorf("footer = %q, want the failure reported", m.footer.Text())
			}
		})
	}
}
