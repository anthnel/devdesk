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
	"github.com/charmbracelet/x/ansi"

	"github.com/anthnel/devdesk/internal/jobs"
	"github.com/anthnel/devdesk/internal/ui/testutil"
)

// DEVDESK-211 step 3: the terminal progress bar (OSC 9;4), proven on the bytes
// a real tea.Program writes (no TTY). Like the background tests, these pin the
// framework's contract with the router: a Bubble Tea bump that stops resetting
// the bar on exit, or a router that shares a pointer, fails here.

var progressSeqRe = regexp.MustCompile("\x1b\\]9;4;([^\x07\x1b]*)(\x07|\x1b\\\\)")

// progressSequences returns the ordered payloads ("1;50", "0", ...) of every
// OSC 9;4 sequence in out.
func progressSequences(out string) []string {
	var seqs []string
	for _, m := range progressSeqRe.FindAllStringSubmatch(out, -1) {
		seqs = append(seqs, m[1])
	}
	return seqs
}

// payloadOf is the payload of a sequence the library builds, so the expected
// values come from x/ansi and not from a copy of its format.
func payloadOf(seq string) string {
	return strings.TrimSuffix(strings.TrimPrefix(seq, "\x1b]9;4;"), "\x07")
}

var (
	progReset = payloadOf(ansi.ResetProgressBar)
	prog0     = payloadOf(ansi.SetProgressBar(0))
	prog50    = payloadOf(ansi.SetProgressBar(50))
)

func sameSeqs(got, want []string) bool { return strings.Join(got, "|") == strings.Join(want, "|") }

// startScanBeforeRun registers a measured 4-target scan before the program
// starts, so the first frame already carries a bar.
func startScanBeforeRun(a *App) jobs.JobID {
	id := a.jobs.Start(scanRun("default", "a", "b", "c", "d"))
	a.jobsChanged()
	return id
}

func waitSeqs(out *lockedBuffer, what string, n int) error {
	return waitOut(out, what, func(s string) bool { return len(progressSequences(s)) >= n })
}

func TestProgressBarWritesNothingWithoutAJob(t *testing.T) {
	a := programRouter(t, &fakeView{body: "hello"})
	out, err := runRouterTrueColor(t, a, func(p *tea.Program, _ *lockedBuffer) error { p.Quit(); return nil })
	if err != nil {
		t.Fatal(err)
	}
	if strings.Contains(out, "\x1b]9;4") {
		t.Errorf("no job, yet the program wrote %q", progressSequences(out))
	}
}

func TestProgressBarIsResetLastOnNormalQuit(t *testing.T) {
	a := programRouter(t, &fakeView{body: "hello"})
	startScanBeforeRun(a)
	out, err := runRouterTrueColor(t, a, func(p *tea.Program, out *lockedBuffer) error {
		if err := waitSeqs(out, "the first bar", 1); err != nil {
			return err
		}
		p.Quit()
		return nil
	})
	if err != nil {
		t.Fatal(err)
	}
	if want := []string{prog0, progReset}; !sameSeqs(progressSequences(out), want) {
		t.Errorf("progress sequences = %q, want %q", progressSequences(out), want)
	}
}

func TestProgressBarIsResetLastOnCtrlC(t *testing.T) {
	a := programRouter(t, &fakeView{body: "hello"})
	startScanBeforeRun(a)
	out, err := runRouterTrueColor(t, a, func(p *tea.Program, out *lockedBuffer) error {
		if err := waitSeqs(out, "the first bar", 1); err != nil {
			return err
		}
		p.Send(testutil.Key("ctrl+c"))
		return nil
	})
	if err != nil {
		t.Fatal(err)
	}
	if want := []string{prog0, progReset}; !sameSeqs(progressSequences(out), want) {
		t.Errorf("progress sequences = %q, want %q", progressSequences(out), want)
	}
}

// The scan is driven from the view's Update, which runs on the program's own
// goroutine: the registry is only ever touched there (Rule 110).
func TestProgressBarGoesFromZeroToFiftyOnAFourTargetScan(t *testing.T) {
	v := &fakeView{body: "hello"}
	a := programRouter(t, v)
	var id jobs.JobID
	step := 0
	v.onKey = func(*fakeView) {
		step++
		switch step {
		case 1:
			id = a.jobs.Start(scanRun("default", "a", "b", "c", "d"))
		case 2:
			a.jobs.Advance(id, "a", jobs.ItemDone, "")
			a.jobs.Advance(id, "b", jobs.ItemDone, "")
		case 3:
			a.jobs.Advance(id, "c", jobs.ItemDone, "")
			a.jobs.Advance(id, "d", jobs.ItemDone, "")
		}
		a.jobsChanged()
	}
	out, err := runRouterTrueColor(t, a, func(p *tea.Program, out *lockedBuffer) error {
		for i, want := range []int{1, 2, 3} {
			p.Send(testutil.Key("x"))
			if err := waitSeqs(out, fmt.Sprintf("sequence %d", want), want); err != nil {
				return fmt.Errorf("step %d: %w", i+1, err)
			}
		}
		p.Quit()
		return nil
	})
	if err != nil {
		t.Fatal(err)
	}
	// The settled run clears the bar, and the exit has nothing left to reset.
	if want := []string{prog0, prog50, progReset}; !sameSeqs(progressSequences(out), want) {
		t.Errorf("progress sequences = %q, want %q", progressSequences(out), want)
	}
}

func TestProgressBarIsResetThenReissuedAroundAnExec(t *testing.T) {
	// One P makes the renderer ticker race deterministic, as in testExec.
	prev := runtime.GOMAXPROCS(1)
	t.Cleanup(func() { runtime.GOMAXPROCS(prev) })
	out := &lockedBuffer{}
	fe := &fakeExec{out: out}
	v := &execView{fakeView: fakeView{body: "hello"}, fe: fe, cb: make(chan error, 1)}
	a := programRouter(t, v)
	startScanBeforeRun(a)

	got, err := runRouterToTrueColor(t, a, out, func(p *tea.Program, _ *lockedBuffer) error {
		if err := waitSeqs(out, "the first bar", 1); err != nil {
			return err
		}
		p.Send(testutil.Key("x"))
		select {
		case cbErr := <-v.cb:
			if cbErr != nil {
				return fmt.Errorf("exec callback got %v", cbErr)
			}
		case <-time.After(5 * time.Second):
			return errors.New("the exec callback never ran")
		}
		// The bar is drawn again while the program runs, not only at exit.
		if err := waitOut(out, "the bar after the child", func(s string) bool { return strings.Count(s, ansi.SetProgressBar(0)) >= 2 }); err != nil {
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
	if want := []string{prog0, progReset}; !sameSeqs(progressSequences(captured), want) {
		t.Errorf("before the child: %q, want %q (the child must see no bar)", progressSequences(captured), want)
	}
	if want := []string{prog0, progReset}; !sameSeqs(progressSequences(got[len(captured):]), want) {
		t.Errorf("after the child: %q, want %q", progressSequences(got[len(captured):]), want)
	}
}
