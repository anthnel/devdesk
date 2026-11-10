package app

import (
	"testing"

	tea "charm.land/bubbletea/v2"

	"github.com/anthnel/devdesk/internal/command"
	"github.com/anthnel/devdesk/internal/jobs"
	"github.com/anthnel/devdesk/internal/shared"
	"github.com/anthnel/devdesk/internal/ui/testutil"
)

// ── DEVDESK-210 step 2: the router tells the views about the focus ──────────

// focusMsgsOf lists, in order, the TerminalFocusMsg a fake view received.
func focusMsgsOf(v *fakeView) []shared.TerminalFocusMsg {
	var out []shared.TerminalFocusMsg
	for _, msg := range v.received {
		if f, ok := msg.(shared.TerminalFocusMsg); ok {
			out = append(out, f)
		}
	}
	return out
}

// twoViews hosts a view on screen (dashboard) and a held one (status).
func twoViews(t *testing.T) (a *App, onScreen, held *fakeView) {
	t.Helper()
	onScreen, held = &fakeView{}, &fakeView{}
	a = router(t, onScreen)
	a.views[command.ViewStatus] = held
	return a, onScreen, held
}

func TestBlurTellsOnlyTheViewOnScreen(t *testing.T) {
	a, onScreen, held := twoViews(t)

	a.Update(tea.BlurMsg{})

	got, ok := receivedOf[shared.TerminalFocusMsg](onScreen)
	if !ok || got != (shared.TerminalFocusMsg{Focused: false, Resume: false}) {
		t.Errorf("view on screen got %+v (received=%v), want {Focused:false Resume:false}", got, ok)
	}
	if n := len(focusMsgsOf(held)); n != 0 {
		t.Errorf("a held view off screen received %d TerminalFocusMsg, want none", n)
	}
}

func TestFocusAsksTheViewOnScreenToResumeOnce(t *testing.T) {
	a, onScreen, held := twoViews(t)
	a.Update(tea.BlurMsg{})
	inits := onScreen.inits

	a.Update(tea.FocusMsg{})
	msgs := focusMsgsOf(onScreen)
	if len(msgs) != 2 || msgs[1] != (shared.TerminalFocusMsg{Focused: true, Resume: true}) {
		t.Fatalf("view on screen got %+v, want [blur, {Focused:true Resume:true}]", msgs)
	}
	if onScreen.inits != inits {
		t.Errorf("focus called Init(): inits %d -> %d", inits, onScreen.inits)
	}

	a.Update(tea.FocusMsg{}) // a second focus with no blur in between
	if n := len(focusMsgsOf(onScreen)); n != 2 {
		t.Errorf("a second FocusMsg sent another TerminalFocusMsg (%d total, want 2)", n)
	}
	if n := len(focusMsgsOf(held)); n != 0 {
		t.Errorf("a held view off screen received %d TerminalFocusMsg, want none", n)
	}
}

func TestFocusWithoutBlurTellsNobody(t *testing.T) {
	a, onScreen, held := twoViews(t)

	_, cmd := a.Update(tea.FocusMsg{})

	if n := len(focusMsgsOf(onScreen)) + len(focusMsgsOf(held)); n != 0 {
		t.Errorf("a focus with no prior blur sent %d TerminalFocusMsg, want none", n)
	}
	if cmd != nil {
		t.Error("a focus with no prior blur returned a Cmd")
	}
}

func TestAViewEnteringTheScreenIsToldTheFocus(t *testing.T) {
	entries := []struct {
		name  string
		enter func(a *App, v *fakeView)
	}{
		{"switchView", func(a *App, v *fakeView) {
			a.views[command.ViewStatus] = v
			a.switchView(command.ViewStatus)
		}},
		{"selection entry", func(a *App, v *fakeView) {
			a.openBrowser(command.ViewWorkspaces, v)
		}},
	}
	for _, blur := range []bool{false, true} {
		for _, e := range entries {
			name := e.name + "/focused"
			if blur {
				name = e.name + "/blurred"
			}
			t.Run(name, func(t *testing.T) {
				a := router(t, &fakeView{})
				if blur {
					blurred(t, a)
				}
				entering := &fakeView{}

				e.enter(a, entering)

				msgs := focusMsgsOf(entering)
				if len(msgs) == 0 {
					t.Fatal("the entering view was never told the focus")
				}
				want := shared.TerminalFocusMsg{Focused: !blur, Resume: false}
				if last := msgs[len(msgs)-1]; last != want {
					t.Errorf("last TerminalFocusMsg = %+v, want %+v", last, want)
				}
			})
		}
	}
}

// The path picker and scan_details also enter through enterView, but the
// picker is a real filebrowser and nothing on it records a TerminalFocusMsg
// yet, so they are not covered here (see the report).

func TestFocusStartsNoScan(t *testing.T) { // Rule 126
	a := router(t, &fakeView{})
	isolateHome(t)
	a.createView(command.ViewWorkspaces)
	a.currentView = command.ViewWorkspaces
	before := len(a.jobs.Snapshot())

	_, blurCmd := a.Update(tea.BlurMsg{})
	_, focusCmd := a.Update(tea.FocusMsg{})

	for _, cmd := range []tea.Cmd{blurCmd, focusCmd} {
		for _, msg := range testutil.Msgs(cmd) {
			if _, ok := msg.(jobs.StartMsg); ok {
				t.Errorf("blur/focus produced a jobs.StartMsg: %+v", msg)
			}
		}
	}
	if after := len(a.jobs.Snapshot()); after != before {
		t.Errorf("jobs snapshot length %d -> %d across blur/focus", before, after)
	}
}

// resumeMarkerMsg is what resumingView's Cmd produces on a resume.
type resumeMarkerMsg struct{}

// resumingView answers a resuming TerminalFocusMsg with a Cmd, like a view
// that restarts its tick when the terminal gets the focus back.
type resumingView struct{ *fakeView }

func (v resumingView) Update(msg tea.Msg) (tea.Model, tea.Cmd) {
	v.fakeView.Update(msg)
	if f, ok := msg.(shared.TerminalFocusMsg); ok && f.Resume {
		return v, func() tea.Msg { return resumeMarkerMsg{} }
	}
	return v, nil
}

// Input after a blur proves the terminal has the focus: the view is told
// first, and the Cmd it answers with must survive Update (key and paste).
func TestInputAfterBlurResumesAndKeepsTheViewCmd(t *testing.T) {
	inputs := []struct {
		name string
		msg  tea.Msg
	}{
		{"key", testutil.Key("down")},
		{"paste", testutil.Paste("hello")},
	}
	for _, in := range inputs {
		t.Run(in.name, func(t *testing.T) {
			fv := &fakeView{editing: true} // a paste only reaches an editing view
			a := router(t, resumingView{fv})
			blurred(t, a)

			_, cmd := a.Update(in.msg)

			resumed := false
			for _, m := range testutil.Msgs(cmd) {
				if _, ok := m.(resumeMarkerMsg); ok {
					resumed = true
				}
			}
			if !resumed {
				t.Errorf("the view's resume Cmd was dropped on a %s", in.name)
			}
			focusIdx, inputIdx := -1, -1
			for i, m := range fv.received {
				switch m.(type) {
				case shared.TerminalFocusMsg:
					focusIdx = i // the last one: the first is the blur
				case tea.KeyPressMsg, tea.PasteMsg:
					inputIdx = i
				}
			}
			if focusIdx < 0 || inputIdx < 0 || focusIdx > inputIdx {
				t.Fatalf("want TerminalFocusMsg before the %s, got %+v", in.name, fv.received)
			}
			if f := fv.received[focusIdx].(shared.TerminalFocusMsg); f != (shared.TerminalFocusMsg{Focused: true, Resume: true}) {
				t.Errorf("TerminalFocusMsg = %+v, want {true true}", f)
			}
		})
	}
}
