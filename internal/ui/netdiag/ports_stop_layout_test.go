package netdiag

import (
	"strings"
	"testing"
	"time"

	"github.com/charmbracelet/x/ansi"

	"github.com/anthnel/devdesk/internal/ports"
	"github.com/anthnel/devdesk/internal/ui/components"
	"github.com/anthnel/devdesk/internal/ui/keymap"
	"github.com/anthnel/devdesk/internal/ui/testutil"
)

// DEVDESK-711 (S-02b), the screen side: the header and the help (Rules 114 and
// 130), and what fits at 80 columns (Rule 128). The machine itself is in
// ports_stop_test.go.

// longProcess is a process name well past what a footer or a modal line can
// carry at 80 columns.
var longProcess = strings.Repeat("x", 40)

// escEntry is the esc entry of the Ports header, and whether there is one.
func escEntry(m *Model) (description string, disabled, found bool) {
	for _, s := range m.GetShortcuts() {
		if s.Key == "esc" {
			return s.Description, s.Disabled, true
		}
	}
	return "", false, false
}

// portsHowItWorks is the body of the Ports tab's help section, whitespace folded.
func portsHowItWorks(t *testing.T, m *Model) string {
	t.Helper()
	for _, s := range m.GetHelpContent().Sections {
		if s.Title == "Ports Tab — How it works" {
			return squash(s.Body)
		}
	}
	t.Fatal("no Ports Tab help section")
	return ""
}

// ── Rule 130: esc is a control, greyed outside the wait ──────────────────────

func TestEscIsOfferedOnlyWhileWaiting(t *testing.T) {
	t.Run("at rest it is there and greyed", func(t *testing.T) {
		m, _ := identityModel(t, false, stopRows()...)
		desc, disabled, found := escEntry(m)
		if !found {
			t.Fatal("esc is missing from the header: the set of keys must not depend on the state (Rule 130)")
		}
		if !disabled {
			t.Error("esc is lit at rest, with no wait to stop")
		}
		if desc != "Stop waiting" {
			t.Errorf("esc reads %q, want %q (a greyed entry keeps its own label)", desc, "Stop waiting")
		}
	})
	t.Run("while the wait is on it is lit", func(t *testing.T) {
		m, _ := waiting(t)
		desc, disabled, found := escEntry(m)
		if !found || disabled {
			t.Errorf("esc found = %v, greyed = %v during the wait, want lit", found, disabled)
		}
		if desc != "Stop waiting" {
			t.Errorf("esc reads %q, want %q", desc, "Stop waiting")
		}
	})
	t.Run("while the SIGTERM is in flight it is greyed", func(t *testing.T) {
		m, _, _ := signalling(t)
		if _, disabled, found := escEntry(m); !found || !disabled {
			t.Errorf("esc found = %v, greyed = %v while the SIGTERM is in flight, want greyed", found, disabled)
		}
	})
	t.Run("while the SIGKILL is in flight it is greyed", func(t *testing.T) {
		m, _, _ := forcing(t)
		if _, disabled, found := escEntry(m); !found || !disabled {
			t.Errorf("esc found = %v, greyed = %v while the SIGKILL is in flight, want greyed", found, disabled)
		}
	})
	t.Run("after esc it is greyed again", func(t *testing.T) {
		m, _ := waiting(t)
		m = feed(t, m, testutil.Key("esc"))
		if _, disabled, found := escEntry(m); !found || !disabled {
			t.Errorf("esc found = %v, greyed = %v after the wait ended, want greyed", found, disabled)
		}
	})
	t.Run("Windows has no entry at all", func(t *testing.T) {
		m, _ := identityModel(t, true, sock("4412", "node.exe"), sock("812", "sshd"))
		if _, _, found := escEntry(m); found {
			t.Error("Windows advertises esc: there is no wait to stop there")
		}
		m = feed(t, m, testutil.Key(keymap.Kill), components.ConfirmModalYesMsg{})
		if _, _, found := escEntry(m); found {
			t.Error("Windows advertises esc during a kill")
		}
	})
}

// ── Rule 114: the help says what K does now ──────────────────────────────────

func TestPortsHelpDescribesTheTwoStepStop(t *testing.T) {
	m, _ := identityModel(t, false, stopRows()...)
	body := portsHowItWorks(t, m)

	for _, want := range []string{
		"SIGTERM",
		"up to 5 seconds",
		"second confirmation offers SIGKILL",
		"esc stops the waiting",
		"On Windows, K ends the process at once",
		"the footer names the step: stopping, waiting for it to exit, or killing it",
	} {
		if !strings.Contains(body, want) {
			t.Errorf("the Ports Tab help does not contain %q", want)
		}
	}
	if strings.Contains(body, "K terminates that process") {
		t.Error("the Ports Tab help still says K terminates the process at once")
	}

	const escHelp = "Stop waiting for a process to exit; the SIGTERM already sent is not taken back (Ports tab)"
	found := false
	for _, kb := range m.GetHelpContent().KeyBindings {
		if kb.Key == "esc (Ports)" {
			found = true
			if kb.Description != escHelp {
				t.Errorf("esc (Ports) help = %q, want %q", kb.Description, escHelp)
			}
		}
	}
	if !found {
		t.Error("the help has no esc (Ports) entry on Linux and macOS")
	}

	win, _ := identityModel(t, true, sock("4412", "node.exe"), sock("812", "sshd"))
	for _, kb := range win.GetHelpContent().KeyBindings {
		if kb.Key == "esc (Ports)" {
			t.Errorf("the Windows help documents esc (Ports): %q", kb.Description)
		}
	}
	if got := helpFor(win); got != windowsHelp {
		t.Errorf("the Windows K help = %q, want it unchanged: %q", got, windowsHelp)
	}
}

// ── Rule 128 at 80 columns ───────────────────────────────────────────────────

// footerFits is the check every footer line of this feature goes through: whole
// at the width, and as many lines as the router budgeted.
func footerFits(t *testing.T, m *Model, width int) {
	t.Helper()
	lines := strings.Split(portsFooter(m, width), "\n")
	if len(lines) != m.GetFooterHeight() {
		t.Errorf("RenderFooter emits %d lines, GetFooterHeight says %d", len(lines), m.GetFooterHeight())
	}
	for i, l := range lines {
		if w := ansi.StringWidth(l); w > width {
			t.Errorf("footer line %d is %d columns wide, want at most %d", i, w, width)
		}
	}
}

func TestTheStopTextsFitAt80Columns(t *testing.T) {
	testutil.FastTimers(t, &components.FooterMsgDuration)
	at80 := func(t *testing.T, rows []ports.Socket) *Model {
		t.Helper()
		m, _ := stopModelWith(t, 80, rows)
		m, _ = step(t, m, components.ConfirmModalYesMsg{})
		return m
	}

	// waitingAt80 is a fresh model on nginx with the SIGTERM delivered: models are
	// pointers mutated in place, so each outcome below starts from its own.
	waitingAt80 := func(t *testing.T, rows []ports.Socket) *Model {
		t.Helper()
		m, _ := termDelivered(t, at80(t, rows), stopT0)
		return m
	}

	t.Run("each footer line of the stop reads whole for nginx (812)", func(t *testing.T) {
		m := at80(t, stopRows())
		if line := infoLine(m, 80); !strings.Contains(line, "Stopping nginx (812)") {
			t.Errorf("while the SIGTERM is in flight the line is %q, want it to name the step", line)
		}
		footerFits(t, m, 80)

		m = waitingAt80(t, stopRows())
		if line := infoLine(m, 80); !strings.Contains(line, "Waiting for nginx (812) to exit") {
			t.Errorf("during the wait the line is %q, want it to name the step", line)
		}
		footerFits(t, m, 80)

		stopped, _ := probeAt(t, waitingAt80(t, stopRows()), stopT0.Add(time.Second), ports.ProcessExited, nil)
		if got := infoLine(stopped, 80); got != stoppedNginx {
			t.Errorf("footer line at 80 columns = %q, want exactly %q", got, stoppedNginx)
		}
		footerFits(t, stopped, 80)

		escaped := feed(t, waitingAt80(t, stopRows()), testutil.Key("esc"))
		if got := infoLine(escaped, 80); got != stoppedWaitingNginx {
			t.Errorf("footer line at 80 columns = %q, want exactly %q", got, stoppedWaitingNginx)
		}
		footerFits(t, escaped, 80)

		asking, _ := probeAt(t, waitingAt80(t, stopRows()), afterGrace, ports.ProcessRunning, nil)
		declined := feed(t, asking, components.ConfirmModalNoMsg{})
		if got := infoLine(declined, 80); got != notForcedNginx {
			t.Errorf("footer line at 80 columns = %q, want exactly %q", got, notForcedNginx)
		}
		footerFits(t, declined, 80)
	})

	t.Run("the greyed K reasons read whole", func(t *testing.T) {
		m := feed(t, at80(t, stopRows()), testutil.Key(keymap.Kill))
		if got := infoLine(m, 80); got != stopPendingHereText {
			t.Errorf("footer line at 80 columns = %q, want exactly %q", got, stopPendingHereText)
		}
		footerFits(t, m, 80)

		m = feed(t, feed(t, m, testutil.Key("down")), testutil.Key(keymap.Kill))
		if got := infoLine(m, 80); got != anotherStopText {
			t.Errorf("footer line at 80 columns = %q, want exactly %q", got, anotherStopText)
		}
		footerFits(t, m, 80)
	})

	t.Run("a 40-character name is cut by the footer, never overflowed", func(t *testing.T) {
		rows := []ports.Socket{sock("812", longProcess), sock("4242", "node")}
		footerFits(t, at80(t, rows), 80)
		footerFits(t, waitingAt80(t, rows), 80)

		escaped := feed(t, waitingAt80(t, rows), testutil.Key("esc"))
		footerFits(t, escaped, 80)
		if line := infoLine(escaped, 80); !strings.HasPrefix(line, "Stopped waiting for") {
			t.Errorf("footer line = %q, want a cut line that still starts the sentence", line)
		}

		stopped, _ := probeAt(t, waitingAt80(t, rows), stopT0.Add(time.Second), ports.ProcessExited, nil)
		footerFits(t, stopped, 80)
		if line := infoLine(stopped, 80); !strings.HasSuffix(line, "stopped") {
			t.Errorf("footer line = %q, want it to end with the verdict", line)
		}
	})
}

func TestTheStopModalsFitAt80Columns(t *testing.T) {
	for _, tt := range []struct {
		name    string
		process string
	}{
		{"nginx", "nginx"},
		{"a 40-character name", longProcess},
	} {
		t.Run(tt.name, func(t *testing.T) {
			rows := []ports.Socket{sock("812", tt.process), sock("4242", "node")}

			first, _ := stopModelWith(t, 80, rows)
			second, _ := askingToForceAt(t, 80, rows)

			for which, m := range map[string]*Model{"the first confirmation": first, "the second confirmation": second} {
				for i, line := range strings.Split(ansi.Strip(m.Render()), "\n") {
					if w := ansi.StringWidth(line); w > 80 {
						t.Errorf("%s, screen line %d is %d columns wide, want at most 80", which, i, w)
					}
				}
				var body []string
				for _, l := range strings.Split(modalBody(m), "\n") {
					if strings.TrimSpace(l) != "" {
						body = append(body, l)
					}
				}
				if len(body) != 2 {
					t.Errorf("%s has %d text lines, want 2 (ConfirmModal does not wrap): %q", which, len(body), body)
				}
				for _, l := range body {
					if w := ansi.StringWidth(l); w > 72 {
						t.Errorf("%s: line %q is %d cells, want at most 72 (80 minus the view border, the modal border and its padding)", which, l, w)
					}
				}
				if tt.process == longProcess {
					if strings.Contains(modalBody(m), longProcess) {
						t.Errorf("%s carries the whole 40-character name: it is cut at 32 cells", which)
					}
					if !strings.Contains(modalBody(m), longProcess[:20]) {
						t.Errorf("%s lost the start of the name: %q", which, modalBody(m))
					}
				}
			}
		})
	}
}

// askingToForceAt is askingToForce at a width and on the given rows (cursor on
// rows[0]).
func askingToForceAt(t *testing.T, width int, rows []ports.Socket) (*Model, *fakeProcs) {
	t.Helper()
	m, procs := stopModelWith(t, width, rows)
	m, _ = step(t, m, components.ConfirmModalYesMsg{})
	m, _ = termDelivered(t, m, stopT0)
	m, _ = step(t, m, portsStopProbeMsg{gen: m.portsModel.stop.gen, at: afterGrace, liveness: ports.ProcessRunning})
	if m.portsModel.stop == nil || m.portsModel.stop.phase != stopAskingForce || m.portsModel.confirmModal == nil {
		t.Fatalf("setup: the second confirmation is not open: %+v", m.portsModel.stop)
	}
	return m, procs
}
