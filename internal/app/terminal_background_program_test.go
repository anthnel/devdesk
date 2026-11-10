package app

import (
	"errors"
	"fmt"
	"io"
	"regexp"
	"strings"
	"sync"
	"testing"
	"time"

	tea "charm.land/bubbletea/v2"
	"github.com/charmbracelet/colorprofile"

	"github.com/anthnel/devdesk/internal/ui/testutil"
	"github.com/anthnel/devdesk/internal/ui/theme"
)

// DEVDESK-209 step 4: the terminal background is set and restored by Bubble
// Tea, proven on the bytes a real tea.Program writes (no TTY). These pin the
// framework's contract: a version bump that breaks it fails here.
//
// A panic test makes Bubble Tea print the stack trace to stderr; that is
// accepted noise.

const (
	bgReset = "\x1b]111\x07"
	bgSet   = "\x1b]11;"
)

var bgSeqRe = regexp.MustCompile("\x1b\\](111|11;[^\x07\x1b]*)(\x07|\x1b\\\\)")

// bgSequences returns the ordered "set background" (OSC 11) and "reset
// background" (OSC 111) sequences found in out. Both are searched as whole
// sequences because they share the ESC ] 11 prefix.
func bgSequences(out string) []string {
	var seqs []string
	for _, m := range bgSeqRe.FindAllStringSubmatch(out, -1) {
		if m[1] == "111" {
			seqs = append(seqs, bgReset)
		} else {
			seqs = append(seqs, "\x1b]"+m[1]+"\x07")
		}
	}
	return seqs
}

func setOf(hex string) string { return bgSet + hex + "\x07" }

// waitOut polls until cond(out) holds. It returns an error after a guard
// timeout; it never touches a *testing.T, so it is safe from the script
// goroutine.
func waitOut(out *lockedBuffer, what string, cond func(string) bool) error {
	deadline := time.Now().Add(5 * time.Second)
	for !cond(out.String()) {
		if time.Now().After(deadline) {
			return fmt.Errorf("timed out waiting for %s; output so far: %q", what, out.String())
		}
		time.Sleep(5 * time.Millisecond)
	}
	return nil
}

// routerScript drives the program once the first frame is out. It returns an
// error instead of failing the test: it runs on its own goroutine.
type routerScript func(p *tea.Program, out *lockedBuffer) error

// runRouter runs a around a real program writing to a buffer, after the first
// frame ("hello") script runs; the program is given profile. It always waits
// for the script goroutine before returning, so the script never outlives the
// test. A script error fails the test and kills the program; the program
// outliving a guard timeout fails the test too.
func runRouter(t *testing.T, a *App, profile colorprofile.Profile, script routerScript) (string, error) {
	t.Helper()
	return runRouterTo(t, a, profile, &lockedBuffer{}, script)
}

// runRouterTo is runRouter with the output buffer chosen by the caller, for a
// fake that must read what the program has written.
func runRouterTo(t *testing.T, a *App, profile colorprofile.Profile, out *lockedBuffer, script routerScript) (string, error) {
	t.Helper()
	p := tea.NewProgram(a, tea.WithInput(nil), tea.WithOutput(out),
		tea.WithWindowSize(100, 30), tea.WithoutSignalHandler(), tea.WithColorProfile(profile))
	scriptErr := make(chan error, 1)
	go func() {
		err := waitOut(out, "the first frame", func(s string) bool { return strings.Contains(s, "hello") })
		if err == nil && profile != colorprofile.Ascii {
			// The first SET may land after the first frame; an exit sent
			// before it would race with the write of the SET.
			err = waitOut(out, "the first background SET", func(s string) bool { return strings.Contains(s, bgSet) })
		}
		if err == nil {
			err = script(p, out)
		}
		if err != nil {
			p.Kill()
		}
		scriptErr <- err
	}()
	type result struct{ err error }
	done := make(chan result, 1)
	go func() { _, err := p.Run(); done <- result{err} }()
	var r result
	select {
	case r = <-done:
	case <-time.After(10 * time.Second):
		p.Kill()
		if err := <-scriptErr; err != nil {
			t.Errorf("script: %v", err)
		}
		t.Fatal("the program did not exit")
	}
	if err := <-scriptErr; err != nil {
		t.Errorf("script: %v", err)
	}
	return out.String(), r.err
}

func endsWithReset(t *testing.T, out string) {
	t.Helper()
	seqs := bgSequences(out)
	if len(seqs) == 0 || seqs[len(seqs)-1] != bgReset {
		t.Errorf("bgSequences = %q, want a last element %q", seqs, bgReset)
	}
}

func defaultHex() string {
	theme.ApplyTheme(theme.DefaultTheme())
	return theme.Hex(theme.ColorBackground)
}

func programRouter(t *testing.T, v Screen) *App {
	t.Helper()
	resetThemeAfter(t)
	return router(t, v)
}

func TestBackgroundOnNormalQuit(t *testing.T) {
	a := programRouter(t, &fakeView{body: "hello"})
	out, err := runRouter(t, a, colorprofile.TrueColor, func(p *tea.Program, _ *lockedBuffer) error { p.Quit(); return nil })
	if err != nil {
		t.Fatal(err)
	}
	seqs := bgSequences(out)
	if len(seqs) < 2 || seqs[0] != setOf(defaultHex()) || seqs[len(seqs)-1] != bgReset {
		t.Errorf("bgSequences = %q, want [%q ... %q]", seqs, setOf(defaultHex()), bgReset)
	}
}

func TestBackgroundOnCtrlC(t *testing.T) {
	a := programRouter(t, &fakeView{body: "hello"})
	out, err := runRouter(t, a, colorprofile.TrueColor, func(p *tea.Program, _ *lockedBuffer) error {
		p.Send(testutil.Key("ctrl+c"))
		return nil
	})
	if err != nil {
		t.Fatal(err)
	}
	endsWithReset(t, out)
}

func TestBackgroundOnInterrupt(t *testing.T) {
	a := programRouter(t, &fakeView{body: "hello"})
	out, err := runRouter(t, a, colorprofile.TrueColor, func(p *tea.Program, _ *lockedBuffer) error {
		p.Send(tea.InterruptMsg{})
		return nil
	})
	if !errors.Is(err, tea.ErrInterrupted) {
		t.Errorf("err = %v, want tea.ErrInterrupted", err)
	}
	endsWithReset(t, out)
}

func TestBackgroundOnKill(t *testing.T) {
	a := programRouter(t, &fakeView{body: "hello"})
	out, err := runRouter(t, a, colorprofile.TrueColor, func(p *tea.Program, _ *lockedBuffer) error { p.Kill(); return nil })
	if !errors.Is(err, tea.ErrProgramKilled) {
		t.Errorf("err = %v, want tea.ErrProgramKilled", err)
	}
	endsWithReset(t, out)
}

// panicView panics on a key.
type panicView struct{ fakeView }

func (v *panicView) Update(msg tea.Msg) (tea.Model, tea.Cmd) {
	if _, ok := msg.(tea.KeyPressMsg); ok {
		panic("boom from a view")
	}
	return v, nil
}

func TestBackgroundOnPanic(t *testing.T) {
	a := programRouter(t, &panicView{fakeView{body: "hello"}})
	out, err := runRouter(t, a, colorprofile.TrueColor, func(p *tea.Program, _ *lockedBuffer) error {
		p.Send(testutil.Key("x"))
		return nil
	})
	if !errors.Is(err, tea.ErrProgramPanic) {
		t.Errorf("err = %v, want tea.ErrProgramPanic", err)
	}
	endsWithReset(t, out)
}

func TestBackgroundOnAThemeChangeInSession(t *testing.T) {
	a := programRouter(t, &fakeView{body: "hello"})
	writeTheme(t, "light", "#ffffff")
	out, err := runRouter(t, a, colorprofile.TrueColor, func(p *tea.Program, out *lockedBuffer) error {
		cfg := testConfig()
		cfg.App.Theme = "light"
		p.Send(themeSaved(cfg))
		err := waitOut(out, "the light background", func(s string) bool { return strings.Contains(s, setOf("#ffffff")) })
		p.Quit()
		return err
	})
	if err != nil {
		t.Fatal(err)
	}
	seqs := bgSequences(out)
	want := []string{setOf("#1e1e2e"), setOf("#ffffff"), bgReset}
	if strings.Join(seqs, "|") != strings.Join(want, "|") {
		t.Errorf("bgSequences = %q, want %q", seqs, want)
	}
}

func TestBackgroundOnASCIIWritesNothing(t *testing.T) {
	a := programRouter(t, &fakeView{body: "hello"})
	out, err := runRouter(t, a, colorprofile.Ascii, func(p *tea.Program, _ *lockedBuffer) error {
		// Messages are handled in order: the router has recorded the ASCII
		// profile by the time it handles the quit.
		p.Send(tea.ColorProfileMsg{Profile: colorprofile.Ascii})
		p.Quit()
		return nil
	})
	if err != nil {
		t.Fatal(err)
	}
	if strings.Contains(out, "\x1b]11") {
		t.Errorf("ASCII profile wrote a background sequence: %q", bgSequences(out))
	}
}

func TestBackgroundOnANSI256IsTheConvertedColour(t *testing.T) {
	a := programRouter(t, &fakeView{body: "hello"})
	out, err := runRouter(t, a, colorprofile.ANSI256, func(p *tea.Program, _ *lockedBuffer) error { p.Quit(); return nil })
	if err != nil {
		t.Fatal(err)
	}
	seqs := bgSequences(out)
	// theme.Hex does not read an indexed colour; take its RGB like the terminal does.
	r, g, b, _ := colorprofile.ANSI256.Convert(theme.ColorBackground).RGBA()
	want := setOf(fmt.Sprintf("#%02x%02x%02x", r>>8, g>>8, b>>8))
	if len(seqs) < 2 || seqs[0] != want {
		t.Fatalf("bgSequences = %q, want first %q", seqs, want)
	}
	if seqs[0] == setOf("#1e1e2e") {
		t.Error("ANSI256 set the exact colour instead of the converted one")
	}
	endsWithReset(t, out)
}

// fakeExec stands in for the child of tea.Exec. During Run it snapshots what
// the program has written so far: the child must see the terminal's own
// background, so the last background sequence before Run is a reset.
type fakeExec struct {
	out *lockedBuffer
	err error

	mu       sync.Mutex
	captured string
}

func (f *fakeExec) Run() error {
	f.mu.Lock()
	f.captured = f.out.String()
	f.mu.Unlock()
	return f.err
}
func (f *fakeExec) SetStdin(io.Reader)  {}
func (f *fakeExec) SetStdout(io.Writer) {}
func (f *fakeExec) SetStderr(io.Writer) {}

func (f *fakeExec) snapshot() string {
	f.mu.Lock()
	defer f.mu.Unlock()
	return f.captured
}

// execView runs fe through tea.Exec on a key and reports cb's error.
type execView struct {
	fakeView
	fe *fakeExec
	cb chan error
}

func (v *execView) Update(msg tea.Msg) (tea.Model, tea.Cmd) {
	if _, ok := msg.(tea.KeyPressMsg); ok {
		return v, tea.Exec(v.fe, func(err error) tea.Msg { v.cb <- err; return nil })
	}
	return v, nil
}

func testExec(t *testing.T, runErr error) {
	t.Helper()
	out := &lockedBuffer{}
	fe := &fakeExec{out: out, err: runErr}
	v := &execView{fakeView: fakeView{body: "hello"}, fe: fe, cb: make(chan error, 1)}
	a := programRouter(t, v)

	got, err := runRouterTo(t, a, colorprofile.TrueColor, out, func(p *tea.Program, _ *lockedBuffer) error {
		p.Send(testutil.Key("x"))
		select {
		case cbErr := <-v.cb:
			if !errors.Is(cbErr, runErr) {
				return fmt.Errorf("exec callback got %v, want %v", cbErr, runErr)
			}
		case <-time.After(5 * time.Second):
			return errors.New("the exec callback never ran")
		}
		// v.cb fires once RestoreTerminal has finished: the RESET before the
		// child is written; the SET is buffered by renderer.start() and flushed
		// by the final flush before the RESET on close, which keeps the order
		// deterministic.
		//
		// Deliberately not asserted: "a SET is emitted while the program runs
		// again after the child". Bubble Tea v2.1.1 shares one renderer ticker
		// between startRenderer and stopRenderer; after ExecProcess the old
		// goroutine can call p.ticker.Stop() after the new one's Reset(), the
		// ticker stays dead, and the SET left in the buffer is only flushed at
		// shutdown. Waiting for it timed out about 1 run in 400-800
		// (DEVDESK-289). The production bug is tracked in DEVDESK-290; restore
		// that assertion once it is fixed upstream.
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
	before := bgSequences(captured)
	if len(before) < 2 || before[0] != setOf(defaultHex()) || before[len(before)-1] != bgReset {
		t.Errorf("before the child: bgSequences = %q, want [SET ... RESET]", before)
	}
	after := bgSequences(got[len(captured):])
	if len(after) < 2 || after[0] != setOf(defaultHex()) || after[len(after)-1] != bgReset {
		t.Errorf("after the child: bgSequences = %q, want [SET ... RESET]", after)
	}
}

func TestBackgroundAroundExecProcess(t *testing.T) { testExec(t, nil) }

func TestBackgroundAroundExecProcessThatFails(t *testing.T) {
	testExec(t, errors.New("child failed"))
}
