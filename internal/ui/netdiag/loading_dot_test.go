package netdiag

import (
	"strings"
	"testing"

	"charm.land/bubbles/v2/spinner"
	tea "charm.land/bubbletea/v2"
	"github.com/charmbracelet/x/ansi"

	"github.com/anthnel/devdesk/internal/jobs"
	"github.com/anthnel/devdesk/internal/ui/components"
	"github.com/anthnel/devdesk/internal/ui/indicator"
	"github.com/anthnel/devdesk/internal/ui/testutil"
)

// DEVDESK-373: the diagnostics run and the interfaces read are the shared
// activity dot in the footer, driven by the router frame, not a spinner.Model.

func footerPlain(m *Model) string { return ansi.Strip(m.RenderFooter(160)) }

func dotCount(s string) (on, off int) {
	return strings.Count(s, indicator.Glyph(indicator.Running, true)),
		strings.Count(s, indicator.Glyph(indicator.Running, false))
}

func TestARunningPipelineShowsTheStageAndTheDot(t *testing.T) {
	m := runningModel(t, "example.com")
	st := m.statusLine()
	if st != (components.Status{Text: m.progressLabel(), Activity: true}) || !strings.Contains(st.Text, "(1/") {
		t.Fatalf("statusLine() = %+v, want Activity with the unchanged progress label (n/m)", st)
	}
	if !m.WantsBlink() {
		t.Error("a running pipeline does not ask for the blink")
	}
	plain := footerPlain(m)
	if !strings.Contains(plain, st.Text) {
		t.Errorf("the footer lost the stage label %q\n%s", st.Text, plain)
	}
	if on, off := dotCount(plain); on+off != 1 {
		t.Errorf("the footer holds %d dots, want 1\n%s", on+off, plain)
	}
	if hasSpinnerFrame(plain) {
		t.Errorf("a braille frame in the footer\n%s", plain)
	}
}

func TestCancellingTheRunEndsTheActivity(t *testing.T) {
	m := feed(t, runningModel(t, "example.com"), testutil.Key("esc"))
	if m.statusLine().Activity {
		t.Errorf("statusLine() = %+v after esc", m.statusLine())
	}
	if m.WantsBlink() {
		t.Error("a cancelled run still asks for the blink")
	}
}

func TestTheRunningPipelineScheduledNoSpinnerTick(t *testing.T) {
	m := typeInto(t, newTestModel(t), "example.com")
	_, cmd := step(t, m, testutil.Key("enter"))
	if _, ok := testutil.MsgOf[spinner.TickMsg](cmd); ok {
		t.Error("StartRun batches a spinner tick")
	}
}

func TestAFreshInterfacesTabIsReading(t *testing.T) {
	m := newTestModel(t)
	m.activeTab = tabInterfaces
	want := components.Status{Text: "Reading network interfaces...", Activity: true}
	if got := m.interfacesModel.statusLine(); got != want {
		t.Fatalf("statusLine() = %+v, want %+v", got, want)
	}
	if !m.WantsBlink() {
		t.Error("a first read does not ask for the blink")
	}
	m = feed(t, m, NewInterfacesDataMsg(nil))
	if m.interfacesModel.statusLine().Activity || m.WantsBlink() {
		t.Error("the dot outlived the read")
	}
}

func TestCtrlROnInterfacesShowsTheDotWithoutASpinner(t *testing.T) {
	m := interfacesModel(t)
	m, cmd := step(t, m, testutil.Key("ctrl+r"))
	if !m.interfacesModel.statusLine().Activity || !m.WantsBlink() {
		t.Fatal("ctrl+r did not show the activity")
	}
	if cmd == nil {
		t.Fatal("ctrl+r issued no command")
	}
	if _, ok := testutil.MsgOf[spinner.TickMsg](cmd); ok {
		t.Error("ctrl+r batches a spinner tick")
	}
	m = feed(t, m, NewInterfacesDataMsg(nil))
	if m.interfacesModel.statusLine().Activity {
		t.Error("the dot outlived the read")
	}
}

func TestAFailedReadKeepsThePrecedenceOverTheDot(t *testing.T) {
	m := interfacesModel(t)
	m = feed(t, m, ifaceDataMsg{err: readError("boom")}, testutil.Key("ctrl+r"))
	if m.interfacesModel.statusLine().Activity {
		t.Errorf("statusLine() = %+v: the stale label outranks the load", m.interfacesModel.statusLine())
	}
}

func TestTheFrameReachesBothFootersWhateverTheTab(t *testing.T) {
	m := interfacesModel(t)
	m = feed(t, m, testutil.Key("ctrl+r"), testutil.Key("tab")) // Forward
	if m.activeTab != tabForward {
		t.Fatalf("tab went to %v, want Forward", m.activeTab)
	}
	if m.WantsBlink() {
		t.Error("Forward asks for the blink: only the active tab counts")
	}
	m = feed(t, m, jobs.FrameMsg{Blink: false}, testutil.Key("shift+tab"))
	if !m.WantsBlink() {
		t.Error("back on the loading Interfaces tab the view does not ask for the blink")
	}
	plain := footerPlain(m)
	if on, off := dotCount(plain); on != 0 || off != 1 {
		t.Errorf("want the off phase only (a frozen glyph otherwise): on=%d off=%d\n%s", on, off, plain)
	}
}

func TestTheDiagnosticsFooterFollowsTheFrameFromAnotherTab(t *testing.T) {
	m := runningModel(t, "example.com")
	m = feed(t, m, testutil.Key("tab"), jobs.FrameMsg{Blink: false}, testutil.Key("shift+tab"))
	plain := footerPlain(m)
	if on, off := dotCount(plain); on != 0 || off != 1 {
		t.Errorf("the Diagnostics footer kept a stale phase: on=%d off=%d\n%s", on, off, plain)
	}
}

func TestWantsBlinkFollowsTheActiveTabOnly(t *testing.T) {
	m := runningModel(t, "example.com")
	m = feed(t, m, tea.WindowSizeMsg{Width: 140, Height: 30})
	m.activeTab = tabInterfaces
	m = feed(t, m, NewInterfacesDataMsg(nil))
	if m.WantsBlink() {
		t.Error("a run on another tab keeps the chain armed")
	}
}
