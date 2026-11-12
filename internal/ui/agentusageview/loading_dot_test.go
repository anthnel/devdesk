package agentusageview

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

// DEVDESK-371: the loads of :agent-usage show the shared activity dot, driven
// by the router frame. The 2 s poll stays silent.

func hasTick(msgs []tea.Msg) bool {
	for _, m := range msgs {
		if _, ok := m.(spinner.TickMsg); ok {
			return true
		}
	}
	return false
}

func TestTheSessionsListLoadShowsTheActivityDot(t *testing.T) {
	m := newModel(t)
	want := components.Status{Text: "Reading Claude Code sessions...", Activity: true}
	if got := m.status(); got != want {
		t.Fatalf("status() = %+v, want %+v", got, want)
	}
	if !m.WantsBlink() {
		t.Error("a loading sessions list does not ask for the blink")
	}
	m = loaded(t, m, session("a", "/x", 0))
	if m.status().Activity || m.WantsBlink() {
		t.Errorf("a loaded list keeps the dot: %+v, WantsBlink=%v", m.status(), m.WantsBlink())
	}
}

func TestInitAndRefreshScheduleNoSpinnerTick(t *testing.T) {
	m := newModel(t)
	if hasTick(testutil.Msgs(m.Init())) {
		t.Error("Init batches a spinner tick")
	}
	m = loaded(t, m, session("a", "/x", 0))
	_, cmd := m.Update(testutil.Key("ctrl+r"))
	if hasTick(testutil.Msgs(cmd)) {
		t.Error("ctrl+r on the sessions list batches a spinner tick")
	}
}

func TestARefreshOverRowsKeepsTheDot(t *testing.T) {
	// Decision (plan Q2): :agent-usage keeps its current condition, so a ctrl+r
	// with rows on screen shows the dot, as the spinner did.
	m := loaded(t, newModel(t), session("a", "/x", 0))
	m = feed(t, m, testutil.Key("ctrl+r"))
	want := components.Status{Text: "Reading Claude Code sessions...", Activity: true}
	if got := m.status(); got != want {
		t.Errorf("status() = %+v, want %+v", got, want)
	}
	if !m.WantsBlink() {
		t.Error("a refresh does not ask for the blink")
	}
}

func TestOpeningASessionShowsTheDotOnBothTabs(t *testing.T) {
	m, _, _ := openedFixture(t)
	want := components.Status{Text: "Reading session...", Activity: true}
	if got := m.status(); got != want {
		t.Fatalf("status() = %+v, want %+v", got, want)
	}
	if !m.WantsBlink() {
		t.Error("an opening session does not ask for the blink")
	}
	m = feed(t, m, testutil.Key("tab"))
	if m.tab != tabBreakdown {
		t.Fatalf("precondition: tab = %v, want the Breakdown tab", m.tab)
	}
	if got := m.status(); got != want {
		t.Errorf("Breakdown tab: status() = %+v, want %+v", got, want)
	}
	if !m.WantsBlink() {
		t.Error("the Breakdown tab of an opening session does not ask for the blink")
	}
}

func TestTheFullReadAnswerEndsTheDot(t *testing.T) {
	m, _ := atFixture(t)
	if m.status().Activity {
		t.Errorf("status() = %+v after the full read", m.status())
	}
}

// The 2 s poll is silent: neither the tick nor the incremental answer lights
// the dot, and WantsBlink follows the agents alone.
func TestThePollShowsNoDot(t *testing.T) {
	m, parent, c := openedFixture(t)
	m = feed(t, m, firstDelta(t, c))
	idle := m.WantsBlink()

	updated, readCmd := m.Update(usagePollTickMsg{gen: m.agentGen})
	m = updated.(Model)
	if !m.reading {
		t.Fatal("precondition: the poll tick started no read")
	}
	if m.status().Activity {
		t.Errorf("the poll tick lit the dot: %+v", m.status())
	}
	if m.WantsBlink() != idle {
		t.Errorf("WantsBlink = %v during a poll, want %v (agents only)", m.WantsBlink(), idle)
	}

	// The answer to the incremental read.
	appendLine(t, parent, usageLine("poll-1", "claude-opus-4", 1, 1))
	m = feed(t, m, firstDelta(t, readCmd))
	if m.status().Activity {
		t.Errorf("the incremental answer lit the dot: %+v", m.status())
	}
	if m.WantsBlink() != idle {
		t.Errorf("WantsBlink = %v after a poll answer, want %v", m.WantsBlink(), idle)
	}
}

func TestCtrlROnAReadInFlightIsOneFullReadWithNoCmd(t *testing.T) {
	m, _ := atFixture(t)
	m, _ = pollTick(t, m, m.agentGen) // a silent read is in flight
	if m.agentsLoading {
		t.Fatal("precondition: a poll read must not be a loading state")
	}
	updated, cmd := m.Update(testutil.Key("ctrl+r"))
	m = updated.(Model)
	if !m.agentsLoading {
		t.Error("ctrl+r during a read in flight did not mark the load")
	}
	if !m.status().Activity {
		t.Errorf("status() = %+v, want the dot", m.status())
	}
	if cmd != nil {
		t.Error("ctrl+r during a read in flight returned a Cmd: the read in flight is never doubled")
	}
}

func TestTheLoadingDotFollowsTheRouterFrame(t *testing.T) {
	on, off := indicator.Glyph(indicator.Running, true), indicator.Glyph(indicator.Running, false)
	for _, blink := range []bool{true, false} {
		m := feed(t, newModel(t), jobs.FrameMsg{Blink: blink})
		got := ansi.Strip(m.RenderFooter(120))
		want, other := on, off
		if !blink {
			want, other = off, on
		}
		if !strings.Contains(got, want) || strings.Contains(got, other) {
			t.Errorf("blink=%v: footer wants %s and not %s:\n%s", blink, want, other, got)
		}
		for _, f := range spinner.Dot.Frames {
			if strings.Contains(got, f) {
				t.Errorf("blink=%v: the footer shows a braille frame:\n%s", blink, got)
			}
		}
	}
}
