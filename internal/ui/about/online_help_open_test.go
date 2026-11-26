package about

import (
	"bytes"
	"errors"
	"log"
	"os"
	"strings"
	"testing"

	"github.com/anthnel/devdesk/internal/ui/components"
	"github.com/anthnel/devdesk/internal/ui/keymap"
	"github.com/anthnel/devdesk/internal/ui/testutil"
	"github.com/charmbracelet/x/ansi"
)

// DEVDESK-792: what `W` does once pressed.
//
// This file depends on the opener seam the production code must create:
//
//	var openBrowser = browser.Open   // func(url string) error, package about
//
// and on the message that carries the opener's result back to Update:
//
//	type BrowserOpenedMsg struct{ Error error }
//
// (the name the workspaces view and the forge explorer already use). Until the
// seam exists this file does not compile, which takes the whole test package
// with it: that is the expected red of this task. The seam-free tests are in
// online_help_test.go.

// realBrowserGuard replaces the opener for the whole test binary. A test that
// reaches it has not installed its own fake and would have started a real
// browser on the developer's machine; it panics instead, naming the cause.
func realBrowserGuard(url string) error {
	panic("DEVDESK-792: a test reached the real browser with " + url + "; install a fake with useOpener")
}

func TestMain(m *testing.M) {
	openBrowser = realBrowserGuard
	os.Exit(m.Run())
}

// opener records every address handed to it and answers with err.
type opener struct {
	calls []string
	err   error
}

// useOpener installs a fake opener for one test and restores the guard after.
func useOpener(t *testing.T, err error) *opener {
	t.Helper()

	fake := &opener{err: err}
	openBrowser = func(url string) error {
		fake.calls = append(fake.calls, url)
		return fake.err
	}
	t.Cleanup(func() { openBrowser = realBrowserGuard })
	return fake
}

// captureLog collects what the package logs for the duration of the test.
func captureLog(t *testing.T) *bytes.Buffer {
	t.Helper()

	var buf bytes.Buffer
	previous := log.Writer()
	log.SetOutput(&buf)
	t.Cleanup(func() { log.SetOutput(previous) })
	return &buf
}

// The address is a constant, like SourceURL, and it is the documentation site.
func TestTheHelpAddressIsTheDocumentationSite(t *testing.T) {
	if HelpURL != wantHelpURL {
		t.Errorf("HelpURL = %q, want %q", HelpURL, wantHelpURL)
	}
}

// The guard itself: without a substitution the real browser is unreachable.
func TestTheRealBrowserIsUnreachableFromTests(t *testing.T) {
	defer func() {
		got := recover()
		if got == nil {
			t.Fatal("openBrowser did not panic with no fake installed; a test could start a real browser")
		}
		if msg, _ := got.(string); !strings.Contains(msg, "DEVDESK-792") {
			t.Errorf("panic = %v, want it to name DEVDESK-792", got)
		}
	}()

	_ = openBrowser(wantHelpURL)
}

// ── Pressing W ───────────────────────────────────────────────────────────────

// Pressing the key only asks for work. The opener runs in the command (Rule
// 110), so Update itself neither calls it nor changes the model: not the
// footer, not the scroll offset.
func TestPressingTheOnlineHelpKeyAsksForTheOpenerWithoutCallingIt(t *testing.T) {
	fake := useOpener(t, nil)
	m := press(t, sizedAt(t, 100, 5), "down")
	before := m.offset

	next, cmd := pressCmd(t, m, keymap.Web)

	if cmd == nil {
		t.Fatalf("%q returned no command", keymap.Web)
	}
	if len(fake.calls) != 0 {
		t.Errorf("Update called the opener %d time(s) itself; it belongs in the command (Rule 110)", len(fake.calls))
	}
	if next.footer.IsSet() {
		t.Errorf("the key press itself put %q in the footer; the outcome is not known yet", next.footer.Text())
	}
	if next.offset != before {
		t.Errorf("offset = %d after %q, want %d", next.offset, keymap.Web, before)
	}
}

// Running the command opens the documentation, once, directly: no
// confirmation in between.
func TestTheCommandOpensTheDocumentationOnce(t *testing.T) {
	tests := []struct {
		name    string
		openErr error
	}{
		{"the opener succeeds", nil},
		{"the opener fails", errors.New(`exec: "xdg-open": executable file not found in $PATH`)},
	}

	for _, tt := range tests {
		t.Run(tt.name, func(t *testing.T) {
			fake := useOpener(t, tt.openErr)
			_, cmd := pressCmd(t, sizedAt(t, 100, 20), keymap.Web)

			msg, ok := testutil.MsgOf[BrowserOpenedMsg](cmd)
			if !ok {
				t.Fatalf("running the command produced no BrowserOpenedMsg (got %v)", testutil.Msgs(cmd))
			}
			if len(fake.calls) != 1 || fake.calls[0] != wantHelpURL {
				t.Errorf("opener calls = %v, want exactly [%q]", fake.calls, wantHelpURL)
			}
			if !errors.Is(msg.Error, tt.openErr) {
				t.Errorf("BrowserOpenedMsg.Error = %v, want the opener's error %v", msg.Error, tt.openErr)
			}
		})
	}
}

// ── What comes back ──────────────────────────────────────────────────────────

// Success is a neutral fact: level Info (Rule 128), with its expiry timer.
func TestASuccessfulOpenIsAnInfoMessage(t *testing.T) {
	updated, cmd := sizedAt(t, 100, 20).Update(BrowserOpenedMsg{})
	m := updated.(Model)

	if !m.footer.IsSet() {
		t.Fatal("a successful open said nothing")
	}
	if got := m.footer.Level(); got != components.LevelInfo {
		t.Errorf("footer level = %v, want Info", got)
	}
	if cmd == nil {
		t.Error("no expiry timer was returned; Rule 128 says a message set without its timer never clears")
	}

	m = feedFooterExpiry(t, m)
	if m.footer.IsSet() {
		t.Errorf("footer = %q after its timer fired", m.footer.Text())
	}
}

// Failure is Error level, says where to go instead (the address), and is
// logged in the project's format.
func TestAFailedOpenIsAnErrorThatRepeatsTheAddress(t *testing.T) {
	logged := captureLog(t)
	cause := errors.New(`exec: "xdg-open": executable file not found in $PATH`)

	updated, cmd := sizedAt(t, 100, 20).Update(BrowserOpenedMsg{Error: cause})
	m := updated.(Model)

	if got := m.footer.Level(); got != components.LevelError {
		t.Errorf("footer level = %v, want Error", got)
	}
	if !strings.Contains(m.footer.Text(), wantHelpURL) {
		t.Errorf("footer = %q, want it to carry %q so it can be typed by hand", m.footer.Text(), wantHelpURL)
	}
	if cmd == nil {
		t.Error("no expiry timer was returned (Rule 128)")
	}

	// What the user sees, not just what the model holds.
	if shown := ansi.Strip(m.RenderFooter(100)); !strings.Contains(shown, wantHelpURL) {
		t.Errorf("RenderFooter(100) = %q, want the address on screen", shown)
	}

	line := logged.String()
	if !strings.Contains(line, "ERROR [about] open browser: ") || !strings.Contains(line, cause.Error()) {
		t.Errorf("log = %q, want %q followed by the cause", line, "ERROR [about] open browser: "+cause.Error())
	}

	m = feedFooterExpiry(t, m)
	if m.footer.IsSet() {
		t.Errorf("footer = %q after its timer fired", m.footer.Text())
	}
}

// A success writes nothing to the log: it is not an error.
func TestASuccessfulOpenIsNotLogged(t *testing.T) {
	logged := captureLog(t)

	sizedAt(t, 100, 20).Update(BrowserOpenedMsg{})

	if logged.Len() != 0 {
		t.Errorf("log = %q, want nothing", logged.String())
	}
}

// The whole path, key to footer, for both outcomes.
func TestTheOnlineHelpKeyEndToEnd(t *testing.T) {
	tests := []struct {
		name      string
		openErr   error
		wantLevel components.Level
	}{
		{"opened", nil, components.LevelInfo},
		{"refused by the system", errors.New("no handler"), components.LevelError},
	}

	for _, tt := range tests {
		t.Run(tt.name, func(t *testing.T) {
			captureLog(t)
			useOpener(t, tt.openErr)
			m := sizedAt(t, 100, 20)

			m, cmd := pressCmd(t, m, keymap.Web)
			msg, ok := testutil.MsgOf[BrowserOpenedMsg](cmd)
			if !ok {
				t.Fatal("no BrowserOpenedMsg came back")
			}
			updated, _ := m.Update(msg)
			m = updated.(Model)

			if got := m.footer.Level(); got != tt.wantLevel {
				t.Errorf("footer level = %v, want %v (text %q)", got, tt.wantLevel, m.footer.Text())
			}
			if !m.footer.IsSet() {
				t.Error("the outcome was not reported in the footer")
			}
		})
	}
}

// The expiry of a message that is no longer the current one leaves the
// current one alone (Rule 128): a second W must not be cleared by the first
// one's timer.
func TestAStaleExpiryDoesNotClearTheNewerMessage(t *testing.T) {
	m := sizedAt(t, 100, 20)

	updated, _ := m.Update(BrowserOpenedMsg{})
	first := updated.(Model)
	staleID := first.footer.ID()

	updated, _ = first.Update(BrowserOpenedMsg{Error: errors.New("later failure")})
	second := updated.(Model)
	updated, _ = second.Update(components.ClearFooterMsg{ID: staleID})
	second = updated.(Model)

	if !second.footer.IsSet() {
		t.Error("the first message's timer cleared the second message")
	}
}

// feedFooterExpiry delivers the expiry of the message currently displayed,
// without sleeping through the real three seconds.
func feedFooterExpiry(t *testing.T, m Model) Model {
	t.Helper()

	updated, _ := m.Update(components.ClearFooterMsg{ID: m.footer.ID()})
	return updated.(Model)
}
