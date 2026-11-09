package netdiag

import (
	"errors"
	"fmt"
	"os"
	"reflect"
	"strings"
	"testing"

	tea "github.com/charmbracelet/bubbletea"
	"github.com/charmbracelet/x/ansi"

	"github.com/anthnel/devdesk/internal/ui/components"
	"github.com/anthnel/devdesk/internal/ui/indicator"
	"github.com/anthnel/devdesk/internal/ui/keymap"
	"github.com/anthnel/devdesk/internal/ui/testutil"
)

// DEVDESK-150 step 2: the footer of the selected row during a kill (A-05), the
// legend in the help (Rule 114), and the shortcut set staying put (Rule 130).

const killingSSHDLine = "Killing sshd (812)"

// portsFooter is the rendered footer of the Ports tab, escape sequences removed.
func portsFooter(m *Model, width int) string { return ansi.Strip(m.RenderFooter(width)) }

// infoLine is the last line of the footer: the message / status line.
func infoLine(m *Model, width int) string {
	lines := strings.Split(portsFooter(m, width), "\n")
	return strings.TrimSpace(lines[len(lines)-1])
}

func TestTheFooterNamesTheKillOfTheSelectedRow(t *testing.T) {
	m := killingSSHD(t)
	want := components.DescribeRow(components.RowQuery{
		Observed: indicator.Running, ObservedText: killingSSHDLine,
	}).Detail

	status := m.portsModel.statusLine()
	if status.Detail != want {
		t.Fatalf("status Detail = %+v, want the DescribeRow detail %+v", status.Detail, want)
	}
	if status.Level != components.LevelInfo {
		t.Errorf("status Level = %v, want Info", status.Level)
	}
	line := infoLine(m, 120)
	if wantLine := indicator.Glyph(indicator.Running, true) + " " + killingSSHDLine; !strings.Contains(line, wantLine) {
		t.Errorf("footer line = %q, want it to contain %q", line, wantLine)
	}
}

func TestTheFooterOnlyDescribesTheSelectedRow(t *testing.T) {
	m := feed(t, killingSSHD(t), testutil.Key("down")) // cursor on nginx, not busy
	if !m.portsModel.statusLine().Detail.IsZero() {
		t.Errorf("an idle selected row carries a detail: %+v", m.portsModel.statusLine().Detail)
	}
	if line := infoLine(m, 120); hasAnyActivityGlyph(line) || strings.Contains(line, "Killing") {
		t.Errorf("footer line = %q for a row nothing is happening on", line)
	}
}

func TestTheKillDetailComesBeforeThePause(t *testing.T) {
	m := feed(t, killingSSHD(t), testutil.Key(" "))
	line := infoLine(m, 120)
	kill, paused := strings.Index(line, killingSSHDLine), strings.Index(line, "Paused — press space to resume")
	if kill < 0 || paused < 0 || kill > paused {
		t.Errorf("footer line = %q, want the kill detail then the pause text on one line", line)
	}
}

func TestTheKillDetailComesBeforeTheStaleText(t *testing.T) {
	m := feed(t, killingSSHD(t), portsDataMsg{err: errors.New("ss failed")})
	status := m.portsModel.statusLine()
	if status.Level != components.LevelError {
		t.Errorf("status Level = %v, want Error while stale", status.Level)
	}
	line := infoLine(m, 120)
	kill, stale := strings.Index(line, killingSSHDLine), strings.Index(line, "could not be read")
	if kill < 0 || stale < 0 || kill > stale {
		t.Errorf("footer line = %q, want the kill detail then the stale text", line)
	}
}

func TestAFailedKillStaysAFooterErrorWithoutTheFailedGlyph(t *testing.T) {
	m := feed(t, killingSSHD(t), portsKillResultMsg{pid: "812", err: fmt.Errorf("killing: %w", os.ErrPermission)})

	if got := m.portsModel.footer.Level(); got != components.LevelError {
		t.Errorf("footer level = %v, want Error", got)
	}
	line := infoLine(m, 120)
	if !strings.Contains(line, "Refused by the system") {
		t.Errorf("footer line = %q, want the refusal to win", line)
	}
	for _, on := range []bool{true, false} {
		if g := indicator.Glyph(indicator.Failed, on); strings.Contains(portsFooter(m, 120), g) || strings.Contains(m.portsModel.table.View(), g) {
			t.Errorf("a failed kill shows %q: this view remembers no failure (D5)", g)
		}
	}
	if !m.portsModel.statusLine().Detail.IsZero() {
		t.Errorf("the row is at rest, yet the status carries %+v", m.portsModel.statusLine().Detail)
	}

	m = feed(t, m, components.ClearFooterMsg{ID: m.portsModel.footer.ID()})
	if line := infoLine(m, 120); hasAnyActivityGlyph(line) || strings.Contains(line, "Killing") {
		t.Errorf("footer line = %q after the error expired, want nothing about the row", line)
	}
}

func TestTheKillDetailKeepsTheFooterOnOneLineAt60Columns(t *testing.T) {
	idle := feed(t, portsModel(t), tea.WindowSizeMsg{Width: 60, Height: 30})
	busy := feed(t, killingSSHD(t), tea.WindowSizeMsg{Width: 60, Height: 30})

	if busy.GetFooterHeight() != idle.GetFooterHeight() {
		t.Errorf("GetFooterHeight = %d busy, %d idle", busy.GetFooterHeight(), idle.GetFooterHeight())
	}
	if got := len(strings.Split(portsFooter(busy, 60), "\n")); got != busy.GetFooterHeight() {
		t.Errorf("RenderFooter emits %d lines, GetFooterHeight says %d", got, busy.GetFooterHeight())
	}
	if line := infoLine(busy, 60); !strings.Contains(line, killingSSHDLine) {
		t.Errorf("footer line at 60 columns = %q, want %q (the Process column is dropped there)", line, killingSSHDLine)
	}
}

// Rule 130: an operation in progress is not a reason to grey or reorder.
func TestTheShortcutSetIsTheSameBusyIdleOrWithoutAPID(t *testing.T) {
	busy := killingSSHD(t)
	idle := feed(t, killingSSHD(t), testutil.Key("down")) // nginx
	noPID := feed(t, portsModel(t), testutil.Key("end"))  // kernel, no PID
	want := testutil.ShortcutKeys(idle.GetShortcuts())
	for name, m := range map[string]*Model{"busy": busy, "no PID": noPID} {
		if got := testutil.ShortcutKeys(m.GetShortcuts()); !reflect.DeepEqual(got, want) {
			t.Errorf("%s row advertises %v, idle row %v", name, got, want)
		}
	}
	if !testutil.ShortcutEnabled(busy.GetShortcuts(), keymap.Kill) {
		t.Error("K is greyed on a row whose kill is in flight")
	}
	if !testutil.ShortcutDisabled(noPID.GetShortcuts(), keymap.Kill) {
		t.Error("K is lit on a socket with no PID (control for the no-PID state)")
	}
}

func TestThePortsHelpCarriesTheLegendAndNamesTheDot(t *testing.T) {
	m := portsModel(t)
	legend := indicator.Legend()
	var legends int
	var ports string
	for _, s := range m.GetHelpContent().Sections {
		if s.Title == legend.Title {
			legends++
			if s.Body != legend.Body {
				t.Errorf("the %q section is not indicator.Legend() verbatim", legend.Title)
			}
		}
		if s.Title == "Ports Tab — How it works" {
			ports = s.Body
		}
	}
	if legends != 1 {
		t.Errorf("%d sections titled %q, want exactly one", legends, legend.Title)
	}
	if ports == "" {
		t.Fatal("the help lost its Ports section")
	}
	if g := indicator.Glyph(indicator.Running, true); !strings.Contains(ports, g) {
		t.Errorf("the Ports section never names the running glyph %q:\n%s", g, ports)
	}
}
