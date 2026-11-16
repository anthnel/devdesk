package app

import (
	"fmt"
	"strings"
	"testing"

	tea "charm.land/bubbletea/v2"
	"charm.land/lipgloss/v2"
	"github.com/charmbracelet/x/ansi"

	"github.com/anthnel/devdesk/internal/command"
	"github.com/anthnel/devdesk/internal/ui/keymap"
	"github.com/anthnel/devdesk/internal/ui/netdiag"
	"github.com/anthnel/devdesk/internal/ui/testutil"
)

// DEVDESK-472 criterion 2: going from the Diagnostics form or results into the
// trace sub-level and back must not change the number of lines of the screen.
// The sub-level's own footer is pinned in the netdiag package; this pins the
// whole screen through the router, which is what decides how many lines the
// viewport gets once the footer says how tall it is (Rule 124).
//
// The screen is always as tall as the terminal: lipgloss.Place pads a short
// stack and fitHeight crops a tall one, so counting the lines of Render()
// alone could never fail. What would show a jump is the stack itself, so each
// step also measures header, viewport and footer on their own and requires
// them to add up to the terminal height with nothing padded and nothing cut.

// traceFrame is the screen as the router composed it at one step.
type traceFrame struct {
	screen, header, body, footer int
}

func (f traceFrame) String() string {
	return fmt.Sprintf("screen=%d header=%d body=%d footer=%d", f.screen, f.header, f.body, f.footer)
}

// measureFrame renders the whole screen and each of its three blocks the way
// App.Render composes them.
func measureFrame(a *App) traceFrame {
	return traceFrame{
		screen: lipgloss.Height(a.Render()),
		header: lipgloss.Height(a.renderHeader()) + lipgloss.Height(a.renderTitleLine()),
		body:   lipgloss.Height(a.renderBody(false)),
		footer: lipgloss.Height(a.renderViewFooter()),
	}
}

// checkFrame fails when the screen at this step is not exactly the terminal
// tall, or when the stack was only made to fit by padding or cropping, or when
// the router laid the viewport out against another footer than the one drawn.
func checkFrame(t *testing.T, a *App, step string) traceFrame {
	t.Helper()
	f := measureFrame(a)
	t.Logf("%-34s %s", step, f)
	if f.screen != a.height {
		t.Errorf("%s: the screen is %d lines, want the terminal's %d (%s)", step, f.screen, a.height, f)
	}
	if got := f.header + f.body + f.footer; got != a.height {
		t.Errorf("%s: header+body+footer = %d lines, want %d with no padding or cropping (%s)", step, got, a.height, f)
	}
	if want := a.getFooterHeight(); f.footer != want || a.lastFooterHeight != want {
		t.Errorf("%s: footer drawn %d lines, view says %d, router laid out for %d", step, f.footer, want, a.lastFooterHeight)
	}
	return f
}

// netdiagView is the router's Network view.
func netdiagView(t *testing.T, a *App) *netdiag.Model {
	t.Helper()
	m, ok := a.views[command.ViewNetdiag].(*netdiag.Model)
	if !ok {
		t.Fatalf("the router holds a %T for netdiag", a.views[command.ViewNetdiag])
	}
	return m
}

// inTraceLevel reports whether the Network view is on the trace sub-level,
// which is the one state whose title names the Path.
func inTraceLevel(t *testing.T, a *App) bool {
	t.Helper()
	return strings.Contains(ansi.Strip(netdiagView(t, a).GetTitle()), "Path")
}

// pressKeys sends each key through the router and returns the last command.
func pressKeys(t *testing.T, a *App, keys ...string) tea.Cmd {
	t.Helper()
	var cmd tea.Cmd
	for _, k := range keys {
		cmd = feedKey(t, a, testutil.Key(k))
	}
	return cmd
}

// runDiagnostics drives the pipeline over the loopback address to its results.
//
// netdiag's stage results are unexported messages, so the pipeline is run for
// real instead of being faked: against 127.0.0.1 it needs no network and takes
// about two seconds. Only the stage messages are fed back; the other messages
// the router batches with them (animation ticks) are dropped, which is also
// what keeps this from waiting on a timer.
func runDiagnostics(t *testing.T, a *App, cmd tea.Cmd) {
	t.Helper()
	for range 20 {
		if cmd == nil {
			return
		}
		next := tea.Cmd(nil)
		for _, msg := range testutil.Msgs(cmd) {
			if strings.HasSuffix(fmt.Sprintf("%T", msg), "stageDoneMsg") {
				_, next = a.Update(msg)
			}
		}
		cmd = next
	}
	t.Fatal("the diagnostic pipeline did not finish")
}

// selectLocalRoute moves the results cursor to the row H acts on and reports
// whether it got there. The router cannot read the table, so the row is
// recognised by H becoming available.
func selectLocalRoute(t *testing.T, a *App) bool {
	t.Helper()
	m := netdiagView(t, a)
	for range 20 {
		if !testutil.ShortcutDisabled(m.GetShortcuts(), keymap.Diagnose) {
			return true
		}
		pressKeys(t, a, "down")
	}
	return false
}

// escapeTraceLevel presses esc until the sub-level is left, measuring the
// screen after each press: a running trace is cancelled first, then esc goes
// back, so there can be two presses.
func escapeTraceLevel(t *testing.T, a *App, label string) {
	t.Helper()
	for i := 1; i <= 3; i++ {
		pressKeys(t, a, "esc")
		checkFrame(t, a, fmt.Sprintf("%s: esc #%d", label, i))
		if !inTraceLevel(t, a) {
			return
		}
	}
	t.Fatalf("%s: esc did not leave the trace sub-level", label)
}

func TestTraceRoundTripKeepsTheScreenHeight(t *testing.T) {
	for _, size := range [][2]int{{120, 40}, {80, 24}} {
		t.Run(fmt.Sprintf("%dx%d", size[0], size[1]), func(t *testing.T) {
			a := routerAt(t, &fakeView{}, size[0], size[1])
			enterNetdiagByCommand(t, a)
			for _, msg := range testutil.Type("127.0.0.1") {
				a.Update(msg)
			}
			runDiagnostics(t, a, pressKeys(t, a, "enter"))

			// Diagnostics results, the filter bar hidden, then visible: the
			// footer is 3 lines in the first case and 3 plus the bar's height
			// in the second, while the sub-level's is always 3. The bar is
			// opened by a search for the route row itself, which keeps that row
			// on screen (the problems filter would hide it: it is not a problem).
			for _, bar := range []bool{false, true} {
				label := "results, bar hidden"
				if bar {
					label = "results, bar visible"
					pressKeys(t, a, "/")
					for _, msg := range testutil.Type("Local route") {
						a.Update(msg)
					}
					pressKeys(t, a, "enter")
				}
				t.Run(label, func(t *testing.T) {
					if !selectLocalRoute(t, a) {
						if !bar {
							t.Skip("H stays greyed on the results: the path trace is unsupported here or the loopback has no Local route row")
						}
						t.Fatal("H is greyed with the bar visible: the search lost the Local route row")
					}
					before := checkFrame(t, a, label)
					if bar != (before.footer > 3) {
						t.Fatalf("precondition: bar visible = %v but the footer is %d lines", bar, before.footer)
					}

					pressKeys(t, a, keymap.Diagnose)
					if !inTraceLevel(t, a) {
						t.Fatalf("H did not open the trace level (title %q)", ansi.Strip(netdiagView(t, a).GetTitle()))
					}
					inTrace := checkFrame(t, a, label+": in the trace level")
					if inTrace.footer != 3 {
						t.Errorf("the trace level's footer is %d lines, want 3", inTrace.footer)
					}

					escapeTraceLevel(t, a, label)
					after := checkFrame(t, a, label+": back")
					if after != before {
						t.Errorf("the screen after the round trip is %s, was %s before", after, before)
					}
				})
			}

			// The form: the Trace Path button is the fifth stop of the focus.
			pressKeys(t, a, keymap.New)
			if inTraceLevel(t, a) {
				t.Fatal("N did not bring the form back")
			}
			before := checkFrame(t, a, "form")
			pressKeys(t, a, "down", "down", "down", "down", "enter")
			if !inTraceLevel(t, a) {
				t.Fatalf("the Trace Path button did not open the trace level (title %q)", ansi.Strip(netdiagView(t, a).GetTitle()))
			}
			if got := checkFrame(t, a, "form: in the trace level"); got.footer != 3 {
				t.Errorf("the trace level's footer is %d lines, want 3", got.footer)
			}
			escapeTraceLevel(t, a, "form")
			if after := checkFrame(t, a, "form: back"); after != before {
				t.Errorf("the screen after the round trip is %s, was %s before", after, before)
			}
		})
	}
}
