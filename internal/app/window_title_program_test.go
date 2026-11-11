package app

import (
	"errors"
	"fmt"
	"regexp"
	"runtime"
	"strings"
	"testing"
	"time"

	tea "charm.land/bubbletea/v2"

	"github.com/anthnel/devdesk/internal/command"
	"github.com/anthnel/devdesk/internal/jobs"
	"github.com/anthnel/devdesk/internal/ui/testutil"
)

// DEVDESK-257 step 3: the window title (OSC 2), proven on the bytes a real
// tea.Program writes (no TTY). They pin Bubble Tea's contract: write on change
// only, clear on exit, clear before a child and write again after it.

var titleSeqRe = regexp.MustCompile("\x1b\\]2;([^\x07\x1b]*)(\x07|\x1b\\\\)")

// titleSequences returns the ordered payloads of every OSC 2 in out.
func titleSequences(out string) []string {
	var seqs []string
	for _, m := range titleSeqRe.FindAllStringSubmatch(out, -1) {
		seqs = append(seqs, m[1])
	}
	return seqs
}

const (
	titleDashboard = "dk · dashboard · default"
	titleSecurity  = "dk · security · default"
)

func waitTitles(out *lockedBuffer, what string, n int) error {
	return waitOut(out, what, func(s string) bool { return len(titleSequences(s)) >= n })
}

func TestWindowTitleIsWrittenOnceAndClearedOnQuit(t *testing.T) {
	a := programRouter(t, &fakeView{body: "hello"})
	out, err := runRouterTrueColor(t, a, func(p *tea.Program, out *lockedBuffer) error {
		if err := waitTitles(out, "the first title", 1); err != nil {
			return err
		}
		p.Quit()
		return nil
	})
	if err != nil {
		t.Fatal(err)
	}
	if want := []string{titleDashboard, ""}; !sameSeqs(titleSequences(out), want) {
		t.Errorf("title sequences = %q, want %q", titleSequences(out), want)
	}
}

func TestWindowTitleIsClearedLastOnCtrlC(t *testing.T) {
	a := programRouter(t, &fakeView{body: "hello"})
	out, err := runRouterTrueColor(t, a, func(p *tea.Program, out *lockedBuffer) error {
		if err := waitTitles(out, "the first title", 1); err != nil {
			return err
		}
		p.Send(testutil.Key("ctrl+c"))
		return nil
	})
	if err != nil {
		t.Fatal(err)
	}
	if want := []string{titleDashboard, ""}; !sameSeqs(titleSequences(out), want) {
		t.Errorf("title sequences = %q, want %q", titleSequences(out), want)
	}
}

func TestWindowTitleIsNotRewrittenByFrames(t *testing.T) {
	a := programRouter(t, &fakeView{body: "hello"})
	out, err := runRouterTrueColor(t, a, func(p *tea.Program, out *lockedBuffer) error {
		if err := waitTitles(out, "the first title", 1); err != nil {
			return err
		}
		for i := 0; i < 5; i++ {
			p.Send(jobs.FrameMsg{Blink: i%2 == 0})
			p.Send(tea.WindowSizeMsg{Width: 100 + i, Height: 30})
		}
		time.Sleep(100 * time.Millisecond)
		p.Quit()
		return nil
	})
	if err != nil {
		t.Fatal(err)
	}
	if want := []string{titleDashboard, ""}; !sameSeqs(titleSequences(out), want) {
		t.Errorf("title sequences = %q, want %q", titleSequences(out), want)
	}
}

func TestWindowTitleChangesWithTheView(t *testing.T) {
	a := programRouter(t, &fakeView{body: "hello"})
	a.views[command.ViewSecurity] = &fakeView{body: "sec"}
	// ctrl+p loads the context names; New() builds this snapshot, programRouter does not.
	a.ctxArgs = &contextArgs{}
	a.suggester.WithProvider("context", a.ctxArgs)
	out, err := runRouterTrueColor(t, a, func(p *tea.Program, out *lockedBuffer) error {
		if err := waitTitles(out, "the first title", 1); err != nil {
			return err
		}
		p.Send(testutil.Key("ctrl+p"))
		for _, m := range testutil.Type("sec") {
			p.Send(m)
		}
		p.Send(testutil.Key("enter"))
		if err := waitTitles(out, "the second title", 2); err != nil {
			return err
		}
		p.Quit()
		return nil
	})
	if err != nil {
		t.Fatal(err)
	}
	if want := []string{titleDashboard, titleSecurity, ""}; !sameSeqs(titleSequences(out), want) {
		t.Errorf("title sequences = %q, want %q", titleSequences(out), want)
	}
}

func testTitleAroundExec(t *testing.T, runErr error) {
	t.Helper()
	prev := runtime.GOMAXPROCS(1)
	t.Cleanup(func() { runtime.GOMAXPROCS(prev) })
	out := &lockedBuffer{}
	fe := &fakeExec{out: out, err: runErr}
	v := &execView{fakeView: fakeView{body: "hello"}, fe: fe, cb: make(chan error, 1)}
	a := programRouter(t, v)

	got, err := runRouterToTrueColor(t, a, out, func(p *tea.Program, _ *lockedBuffer) error {
		if err := waitTitles(out, "the first title", 1); err != nil {
			return err
		}
		p.Send(testutil.Key("x"))
		select {
		case cbErr := <-v.cb:
			if !errors.Is(cbErr, runErr) {
				return fmt.Errorf("exec callback got %v, want %v", cbErr, runErr)
			}
		case <-time.After(5 * time.Second):
			return errors.New("the exec callback never ran")
		}
		if err := waitOut(out, "the title after the child", func(s string) bool {
			return strings.Count(s, titleDashboard) >= 2
		}); err != nil {
			return err
		}
		p.Quit()
		return nil
	})
	if err != nil {
		t.Fatal(err)
	}
	captured := fe.snapshot()
	if captured == "" {
		t.Fatal("the child never ran")
	}
	if want := []string{titleDashboard, ""}; !sameSeqs(titleSequences(captured), want) {
		t.Errorf("before the child: %q, want %q (the child must see no title)", titleSequences(captured), want)
	}
	if want := []string{titleDashboard, ""}; !sameSeqs(titleSequences(got[len(captured):]), want) {
		t.Errorf("after the child: %q, want %q", titleSequences(got[len(captured):]), want)
	}
}

func TestWindowTitleIsClearedThenReissuedAroundAnExec(t *testing.T) { testTitleAroundExec(t, nil) }

func TestWindowTitleAroundAnExecThatFails(t *testing.T) {
	testTitleAroundExec(t, errors.New("child failed"))
}
