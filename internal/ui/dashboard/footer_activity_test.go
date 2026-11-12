package dashboard

import (
	"strings"
	"testing"

	"github.com/anthnel/devdesk/internal/command"
	"github.com/anthnel/devdesk/internal/jobs"
	"github.com/anthnel/devdesk/internal/shared"
	"github.com/charmbracelet/x/ansi"
)

// DEVDESK-341 step 2a: the jobs line carries the shared activity dot (phase from
// jobs.FrameMsg.Blink), never the router's braille frame.

func hasBraille(s string) bool {
	for _, r := range s {
		if r >= 0x2800 && r <= 0x28FF {
			return true
		}
	}
	return false
}

func runningDashboard(t *testing.T) Model {
	t.Helper()
	return feed(t, New(testConfig(), &shared.State{}), jobs.ChangedMsg{
		Runs: []jobs.Run{running(command.ViewWorkspaces, jobs.KindScan, "~/work")},
	})
}

func TestTheJobsLineShowsTheActivityDotNotABrailleFrame(t *testing.T) {
	m := runningDashboard(t)
	if st := m.status(); !st.Activity {
		t.Fatalf("status Activity=%v, want Activity only", st.Activity)
	}
	m = feed(t, m, jobs.FrameMsg{Blink: true})
	footer := m.RenderFooter(120)
	if plain := ansi.Strip(footer); !strings.Contains(plain, "● 1 job running") {
		t.Errorf("want the dot before the count, got %q", plain)
	}
	if hasBraille(footer) {
		t.Errorf("a braille frame reached the footer: %q", ansi.Strip(footer))
	}
}

func TestTheJobsLineDotFollowsTheBlinkPhase(t *testing.T) {
	m := runningDashboard(t)
	on := ansi.Strip(feed(t, m, jobs.FrameMsg{Blink: true}).RenderFooter(120))
	off := ansi.Strip(feed(t, m, jobs.FrameMsg{Blink: false}).RenderFooter(120))
	if !strings.Contains(off, "○ 1 job running") {
		t.Errorf("Blink=false must give the hollow dot, got %q", off)
	}
	if strings.Index(on, "1 job") != strings.Index(off, "1 job") {
		t.Errorf("the text moved between phases: %q vs %q", on, off)
	}
}

func TestNoUnfinishedRunMeansNoDot(t *testing.T) {
	m := feed(t, New(testConfig(), &shared.State{}), jobs.ChangedMsg{})
	m = feed(t, m, jobs.FrameMsg{Blink: false})
	if got := ansi.Strip(m.RenderFooter(120)); strings.ContainsAny(got, "●○") {
		t.Errorf("a dot without a running job: %q", got)
	}
}

func TestAFooterErrorHidesTheDotWhileJobsRun(t *testing.T) {
	m := runningDashboard(t)
	m = feed(t, m, jobs.FrameMsg{Blink: true})
	m.footer.Error("it broke")
	got := ansi.Strip(m.RenderFooter(120))
	if !strings.Contains(got, "it broke") || strings.ContainsAny(got, "●○") {
		t.Errorf("want the error and no dot, got %q", got)
	}
}

func TestAFrameStartsNoTimerOfItsOwn(t *testing.T) {
	_, cmd := step(t, runningDashboard(t), jobs.FrameMsg{Blink: true})
	if cmd != nil {
		t.Error("a FrameMsg returned a Cmd; the router owns the chain")
	}
}
