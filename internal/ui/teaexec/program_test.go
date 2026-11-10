package teaexec

import (
	"errors"
	"io"
	"runtime"
	"strings"
	"sync"
	"testing"
	"time"

	tea "charm.land/bubbletea/v2"

	"github.com/anthnel/devdesk/internal/ui/testutil"
)

// These tests run a real tea.Program. Bubble Tea v2.1.1 can leave its renderer
// ticker stopped after an exec whose child returns at once (DEVDESK-290); with
// GOMAXPROCS(1) the loss is deterministic. Note that tea.WithInput(nil) makes
// every exec wait 500 ms before the child starts, so waits below are generous.

const (
	viewBefore = "before"
	viewAfter  = "after-child"
	redrawWait = 3 * time.Second
	runGuard   = 10 * time.Second
)

type lockedBuffer struct {
	mu sync.Mutex
	b  strings.Builder
}

func (l *lockedBuffer) Write(p []byte) (int, error) {
	l.mu.Lock()
	defer l.mu.Unlock()
	return l.b.Write(p)
}

func (l *lockedBuffer) String() string {
	l.mu.Lock()
	defer l.mu.Unlock()
	return l.b.String()
}

// instantChild returns from Run at once, with err.
type instantChild struct{ err error }

func (c instantChild) Run() error        { return c.err }
func (instantChild) SetStdin(io.Reader)  {}
func (instantChild) SetStdout(io.Writer) {}
func (instantChild) SetStderr(io.Writer) {}

type childDoneMsg struct{}

// execModel runs start() on a key and shows viewAfter once the callback's
// message arrives.
type execModel struct {
	start func(cb tea.ExecCallback) tea.Cmd
	cb    tea.ExecCallback
	view  string
}

func (m execModel) Init() tea.Cmd { return nil }

func (m execModel) Update(msg tea.Msg) (tea.Model, tea.Cmd) {
	switch msg.(type) {
	case tea.KeyPressMsg:
		return m, m.start(m.cb)
	case childDoneMsg:
		m.view = viewAfter
	}
	return m, nil
}

func (m execModel) View() tea.View { return tea.NewView(m.view) }

func waitFor(buf *lockedBuffer, want string, d time.Duration) bool {
	deadline := time.Now().Add(d)
	for time.Now().Before(deadline) {
		if strings.Contains(buf.String(), want) {
			return true
		}
		time.Sleep(10 * time.Millisecond)
	}
	return strings.Contains(buf.String(), want)
}

// runExec starts a program, triggers one exec through start, and reports the
// callback's error, whether viewAfter was drawn within wait of the callback
// (program still running), and any harness error.
func runExec(t *testing.T, start func(cb tea.ExecCallback) tea.Cmd, wait time.Duration) (cbErr error, redrawn bool) {
	t.Helper()
	prev := runtime.GOMAXPROCS(1)
	t.Cleanup(func() { runtime.GOMAXPROCS(prev) })

	buf := &lockedBuffer{}
	cbCh := make(chan error, 1)
	m := execModel{start: start, view: viewBefore}
	m.cb = func(err error) tea.Msg { cbCh <- err; return childDoneMsg{} }

	p := tea.NewProgram(m, tea.WithInput(nil), tea.WithOutput(buf),
		tea.WithWindowSize(80, 10), tea.WithoutSignalHandler())
	done := make(chan struct{})
	go func() { _, _ = p.Run(); close(done) }()
	t.Cleanup(func() {
		p.Kill()
		select {
		case <-done:
		case <-time.After(runGuard):
			t.Error("the program did not stop after Kill")
		}
	})

	if !waitFor(buf, viewBefore, 5*time.Second) {
		t.Fatal("the first frame never appeared")
	}
	p.Send(testutil.Key("x"))
	select {
	case cbErr = <-cbCh:
	case <-time.After(5 * time.Second):
		t.Fatal("the exec callback never ran")
	}
	return cbErr, waitFor(buf, viewAfter, wait)
}

func TestARedrawFollowsAnInstantChild(t *testing.T) {
	for i := range 20 {
		_, redrawn := runExec(t, func(cb tea.ExecCallback) tea.Cmd {
			return Command(instantChild{}, cb)
		}, redrawWait)
		if !redrawn {
			t.Fatalf("run %d: %q never drawn after the child returned: the renderer ticker is stopped", i, viewAfter)
		}
	}
}

func TestARedrawFollowsAChildThatFailsAtOnce(t *testing.T) {
	errChild := errors.New("exec: not found")
	cbErr, redrawn := runExec(t, func(cb tea.ExecCallback) tea.Cmd {
		return Command(instantChild{err: errChild}, cb)
	}, redrawWait)
	if !errors.Is(cbErr, errChild) {
		t.Errorf("callback error = %v, want %v", cbErr, errChild)
	}
	if !redrawn {
		t.Errorf("%q never drawn after a child that failed at once", viewAfter)
	}
}

// TestUpstreamTickerRaceStillExists pins the upstream defect: a bare tea.Exec
// still loses the ticker. When Bubble Tea fixes it, this fails and the settle
// delay can go.
func TestUpstreamTickerRaceStillExists(t *testing.T) {
	_, redrawn := runExec(t, func(cb tea.ExecCallback) tea.Cmd {
		return tea.Exec(instantChild{}, cb)
	}, time.Second)
	if redrawn {
		t.Fatal("Bubble Tea no longer loses the renderer ticker after Exec (DEVDESK-290): " +
			"internal/ui/teaexec's settle delay can go")
	}
}
