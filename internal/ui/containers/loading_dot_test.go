package containers

import (
	"strings"
	"testing"

	"charm.land/bubbles/v2/spinner"
	"github.com/charmbracelet/x/ansi"

	"github.com/anthnel/devdesk/internal/jobs"
	sharedcomponents "github.com/anthnel/devdesk/internal/ui/components"
	"github.com/anthnel/devdesk/internal/ui/indicator"
	"github.com/anthnel/devdesk/internal/ui/testutil"
)

// DEVDESK-371: the first load of :containers is the shared activity dot, not
// a braille spinner. With rows on screen a refresh is silent (the table keeps
// its rows; only a busy row blinks).

func TestFirstLoadShowsTheActivityDot(t *testing.T) {
	m := newTestModel(t)
	want := sharedcomponents.Status{Text: "Loading containers...", Activity: true}
	if got := m.status(); got != want {
		t.Fatalf("status() = %+v, want %+v", got, want)
	}
	if !m.WantsBlink() {
		t.Error("a first load with an empty table does not ask for the blink")
	}
}

func TestInitSchedulesNoSpinnerTick(t *testing.T) {
	t.Setenv("PATH", t.TempDir()) // no docker: the fetch Cmds stay inert
	if _, ok := testutil.MsgOf[spinner.TickMsg](newTestModel(t).Init()); ok {
		t.Error("Init batches a spinner tick: the footer dot is driven by the router frame")
	}
}

func TestRefreshWithRowsIsSilent(t *testing.T) {
	t.Setenv("PATH", t.TempDir())
	m := loadedModel(t)
	m, cmd := step(t, m, testutil.Key("ctrl+r"))
	if !m.loading {
		t.Fatal("ctrl+r did not start a load")
	}
	if st := m.status(); st.Activity {
		t.Errorf("status() = %+v: a refresh over existing rows must not show the dot", st)
	}
	if m.WantsBlink() {
		t.Error("a silent refresh asks for the blink")
	}
	if cmd == nil {
		t.Fatal("ctrl+r issued no command")
	}
	if _, ok := testutil.MsgOf[spinner.TickMsg](cmd); ok {
		t.Error("ctrl+r batches a spinner tick")
	}
}

func TestABusyRowKeepsTheBlinkThroughARefresh(t *testing.T) {
	t.Setenv("PATH", t.TempDir())
	m := busyVia(t, "stop")
	m, _ = step(t, m, testutil.Key("ctrl+r"))
	if !m.WantsBlink() {
		t.Error("a busy row stopped asking for the blink after ctrl+r")
	}
}

func TestTheLoadingDotFollowsTheRouterFrame(t *testing.T) {
	on, off := indicator.Glyph(indicator.Running, true), indicator.Glyph(indicator.Running, false)
	for _, blink := range []bool{true, false} {
		m := feed(t, newTestModel(t), jobs.FrameMsg{Blink: blink})
		got := ansi.Strip(m.RenderFooter(120))
		want, other := on, off
		if !blink {
			want, other = off, on
		}
		if !strings.Contains(got, want) || strings.Contains(got, other) {
			t.Errorf("blink=%v: footer wants %s and not %s:\n%s", blink, want, other, got)
		}
		if hasSpinnerFrame(got) {
			t.Errorf("blink=%v: the footer shows a braille frame:\n%s", blink, got)
		}
	}
}
