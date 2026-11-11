package explorer

import (
	"strings"
	"testing"

	"charm.land/bubbles/v2/spinner"
	"github.com/anthnel/devdesk/internal/jobs"
	"github.com/charmbracelet/x/ansi"
)

// DEVDESK-341 step 7 (option B): the explorer's load lines ("Loading ...",
// "Updating from ...") use the shared activity dot, fed by jobs.FrameMsg.Blink,
// never the braille of an own spinner.Model.

func hasBraille(s string) bool {
	for _, r := range s {
		if r >= 0x2800 && r <= 0x28FF {
			return true
		}
	}
	return false
}

type blinkingView interface{ WantsBlink() bool }

func refreshingModel(t *testing.T) Model {
	t.Helper()
	m := loadedModel(t)
	m.refreshing = 1
	return m
}

func TestLoadLinesCarryTheActivityDotNotTheOwnSpinner(t *testing.T) {
	for name, m := range map[string]Model{"first load": newTestModel(t), "refresh": refreshingModel(t)} {
		t.Run(name, func(t *testing.T) {
			if st := m.status(); !st.Activity || st.Spinner {
				t.Fatalf("status Activity=%v Spinner=%v, want Activity only", st.Activity, st.Spinner)
			}
		})
	}
}

func TestRefreshShowsTheDotBeforeTheUnchangedText(t *testing.T) {
	m := feed(t, refreshingModel(t), jobs.FrameMsg{Blink: true, Frame: "⠹", RenderedFrame: "⠹"})
	footer := m.RenderFooter(160)
	if plain := ansi.Strip(footer); !strings.Contains(plain, "● Updating from GitLab...") {
		t.Errorf("want the dot before the text, got %q", plain)
	}
	if hasBraille(footer) {
		t.Errorf("a braille frame reached the footer: %q", ansi.Strip(footer))
	}
}

func TestFirstLoadShowsTheDot(t *testing.T) {
	m := feed(t, newTestModel(t), jobs.FrameMsg{Blink: true})
	if plain := ansi.Strip(m.RenderFooter(160)); !strings.Contains(plain, "● Loading GitLab groups...") {
		t.Errorf("want the dot before the load text, got %q", plain)
	}
}

func TestLoadDotFollowsTheBlinkPhase(t *testing.T) {
	m := refreshingModel(t)
	off := ansi.Strip(feed(t, m, jobs.FrameMsg{Blink: false}).RenderFooter(160))
	if !strings.Contains(off, "○ Updating from GitLab...") {
		t.Errorf("Blink=false must give the hollow dot, got %q", off)
	}
	// app.motion static: the router sends Blink=true on every frame, steady dot.
	on := ansi.Strip(feed(t, m, jobs.FrameMsg{Blink: true}, jobs.FrameMsg{Blink: true}).RenderFooter(160))
	if !strings.Contains(on, "● Updating from GitLab...") {
		t.Errorf("Blink=true must give the filled dot, got %q", on)
	}
}

func TestLoadDoneMeansNoDot(t *testing.T) {
	m := feed(t, loadedModel(t), jobs.FrameMsg{Blink: true})
	plain := ansi.Strip(m.RenderFooter(160))
	if (strings.Contains(plain, "●") && strings.Contains(plain, "Updating")) || strings.Contains(plain, "Loading GitLab") {
		t.Errorf("an idle explorer still shows a load line: %q", plain)
	}
	if st := m.status(); st.Activity || st.Spinner {
		t.Errorf("idle status carries Activity=%v Spinner=%v", st.Activity, st.Spinner)
	}
}

func TestAnErrorBeatsTheLoadDot(t *testing.T) {
	m := refreshingModel(t)
	m.error = "boom"
	st := m.status()
	if st.Activity || st.Spinner || st.Text != "boom" {
		t.Errorf("status = %+v, want the bare error", st)
	}
}

func TestWantsBlinkOnlyWhileLoading(t *testing.T) {
	cases := map[string]struct {
		m    Model
		want bool
	}{
		"first load": {newTestModel(t), true},
		"refreshing": {refreshingModel(t), true},
		"idle":       {loadedModel(t), false},
	}
	for name, tc := range cases {
		t.Run(name, func(t *testing.T) {
			bv, ok := any(tc.m).(blinkingView)
			if !ok {
				t.Fatal("explorer.Model does not implement WantsBlink() bool (router BlinkingView)")
			}
			if got := bv.WantsBlink(); got != tc.want {
				t.Errorf("WantsBlink() = %v, want %v", got, tc.want)
			}
		})
	}
}

// The own spinner chain is gone: a spinner tick changes nothing in the footer.
func TestAnOwnSpinnerTickNoLongerReachesTheFooter(t *testing.T) {
	m := feed(t, refreshingModel(t), jobs.FrameMsg{Blink: true})
	before := m.RenderFooter(160)
	m, cmd := step(t, m, spinner.TickMsg{})
	if cmd != nil {
		t.Error("a spinner.TickMsg re-armed a Cmd; the own tick chain must be deleted")
	}
	if after := m.RenderFooter(160); after != before || hasBraille(after) {
		t.Errorf("a spinner tick changed the footer:\nbefore %q\nafter  %q", before, after)
	}
}
