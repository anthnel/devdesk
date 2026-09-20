package app

import (
	"errors"
	"fmt"
	"io"
	"net"
	"strings"
	"testing"

	tea "github.com/charmbracelet/bubbletea"

	"github.com/anthnel/devdesk/internal/command"
	"github.com/anthnel/devdesk/internal/forward"
)

// echoServer starts a loopback server that writes back what it reads, so an
// OpenMsg has something real to probe. Torn down with the test.
func echoServer(t *testing.T) string {
	t.Helper()
	ln, err := net.Listen("tcp", "127.0.0.1:0")
	if err != nil {
		t.Fatalf("starting the echo server: %v", err)
	}
	t.Cleanup(func() { _ = ln.Close() })
	go func() {
		for {
			conn, err := ln.Accept()
			if err != nil {
				return
			}
			go func() {
				defer func() { _ = conn.Close() }()
				_, _ = io.Copy(conn, conn)
			}()
		}
	}()
	return ln.Addr().String()
}

func freePort(t *testing.T) int {
	t.Helper()
	ln, err := net.Listen("tcp", "127.0.0.1:0")
	if err != nil {
		t.Fatalf("reserving a port: %v", err)
	}
	port := ln.Addr().(*net.TCPAddr).Port
	if err := ln.Close(); err != nil {
		t.Fatalf("releasing the reserved port: %v", err)
	}
	return port
}

// runCmd drains a Cmd to the message it produces. An OpenMsg is answered with
// a Cmd rather than handled in Update, so this is how a test sees the result.
func runCmd(t *testing.T, cmd tea.Cmd) tea.Msg {
	t.Helper()
	if cmd == nil {
		t.Fatal("the router returned no command")
	}
	return cmd()
}

// The whole point of the round trip: a view asks, the router opens, and the
// port is live.
func TestAnOpenRequestOpensAForwardThatCarriesBytes(t *testing.T) {
	target := echoServer(t)
	a := newWithSize(testConfig(), 120, 40)
	t.Cleanup(a.sharedState.Forwards.CloseAll)

	port := freePort(t)
	_, cmd := a.Update(forward.OpenMsg{LocalPort: port, Target: target})
	msg, ok := runCmd(t, cmd).(forward.OpenedMsg)
	if !ok {
		t.Fatalf("the open produced a %T, want forward.OpenedMsg", msg)
	}
	if msg.Err != nil {
		t.Fatalf("OpenedMsg carried %v", msg.Err)
	}

	a.Update(msg)
	if got := len(a.sharedState.Forwards.List()); got != 1 {
		t.Fatalf("the registry holds %d forwards, want 1", got)
	}

	conn, err := net.Dial("tcp", msg.Forward.Addr())
	if err != nil {
		t.Fatalf("dialing the forward the router opened: %v", err)
	}
	defer func() { _ = conn.Close() }()
	if _, err := conn.Write([]byte("hello")); err != nil {
		t.Fatalf("writing through the forward: %v", err)
	}
	got := make([]byte, 5)
	if _, err := io.ReadFull(conn, got); err != nil {
		t.Fatalf("reading through the forward: %v", err)
	}
	if string(got) != "hello" {
		t.Errorf("the echo came back as %q, want %q", got, "hello")
	}
}

func TestACloseRequestStopsTheForward(t *testing.T) {
	target := echoServer(t)
	a := newWithSize(testConfig(), 120, 40)
	t.Cleanup(a.sharedState.Forwards.CloseAll)

	f, err := a.sharedState.Forwards.Open(freePort(t), target)
	if err != nil {
		t.Fatalf("Open: %v", err)
	}

	a.Update(forward.CloseMsg{ID: f.ID})
	if got := len(a.sharedState.Forwards.List()); got != 0 {
		t.Errorf("the registry still holds %d forwards, want 0", got)
	}
}

// The registry belongs to the router and not to a view, so it has to survive
// the rebuild that drops every view. This is the leak the placement exists to
// prevent, and the one nothing would report.
func TestForwardsSurviveAViewRebuild(t *testing.T) {
	target := echoServer(t)
	a := newWithSize(testConfig(), 120, 40)
	t.Cleanup(a.sharedState.Forwards.CloseAll)

	f, err := a.sharedState.Forwards.Open(freePort(t), target)
	if err != nil {
		t.Fatalf("Open: %v", err)
	}

	a.reinitializeViews()

	list := a.sharedState.Forwards.List()
	if len(list) != 1 || list[0].ID != f.ID {
		t.Fatalf("after reinitializeViews the registry holds %v, want the one forward %q", list, f.ID)
	}
	if _, err := net.Dial("tcp", f.Addr()); err != nil {
		t.Errorf("the forward stopped answering after the views were rebuilt: %v", err)
	}
}

// Each refusal has to reach the user as the thing they can act on. Relaying
// the operating system's own wording is what this mapping exists to avoid.
func TestEveryRefusalIsNamedRatherThanRelayed(t *testing.T) {
	cases := []struct {
		name string
		err  error
		want string
	}{
		{"privileged port", fmt.Errorf("%w: 80", forward.ErrPrivilegedPort), "below 1024"},
		{"port taken", fmt.Errorf("%w: 8080", forward.ErrPortInUse), "already taken"},
		{"target unreachable", fmt.Errorf("%w: x", forward.ErrTargetUnreachable), "did not answer"},
		{"anything else", errors.New("boom"), "check logs"},
	}
	for _, tc := range cases {
		t.Run(tc.name, func(t *testing.T) {
			got := forwardRefusal(tc.err)
			if !strings.Contains(got, tc.want) {
				t.Errorf("forwardRefusal(%v) = %q, want it to mention %q", tc.err, got, tc.want)
			}
			if strings.Contains(got, "bind:") || strings.Contains(got, "connect:") {
				t.Errorf("forwardRefusal(%v) relayed the OS wording: %q", tc.err, got)
			}
		})
	}
}

// A refused open must not be reported as a success, and must leave nothing
// behind in the registry.
func TestARefusedOpenReportsAndRegistersNothing(t *testing.T) {
	a := newWithSize(testConfig(), 120, 40)
	t.Cleanup(a.sharedState.Forwards.CloseAll)

	_, cmd := a.Update(forward.OpenMsg{LocalPort: 80, Target: "127.0.0.1:1"})
	msg, ok := runCmd(t, cmd).(forward.OpenedMsg)
	if !ok {
		t.Fatalf("the open produced a %T, want forward.OpenedMsg", msg)
	}
	if !errors.Is(msg.Err, forward.ErrPrivilegedPort) {
		t.Fatalf("OpenedMsg carried %v, want ErrPrivilegedPort", msg.Err)
	}

	if _, cmd := a.Update(msg); cmd == nil {
		t.Error("the refusal was swallowed — nothing was posted to the footer")
	}
	if got := len(a.sharedState.Forwards.List()); got != 0 {
		t.Errorf("a refused open left %d forwards behind, want 0", got)
	}
}

// recorder is a view that keeps the last forward snapshot it was handed. It
// exists because the broadcast's effect is the Update it performs on each held
// view, not the command it returns — a batch of nothing is nil, so the return
// value cannot stand in for the assertion.
type recorder struct{ got []forward.Forward }

func (r *recorder) Init() tea.Cmd { return nil }
func (r *recorder) Update(msg tea.Msg) (tea.Model, tea.Cmd) {
	if m, ok := msg.(forward.ChangedMsg); ok {
		r.got = m.Forwards
	}
	return r, nil
}
func (r *recorder) View() string { return "" }

func TestARefreshHandsEveryViewTheSnapshot(t *testing.T) {
	target := echoServer(t)
	a := newWithSize(testConfig(), 120, 40)
	t.Cleanup(a.sharedState.Forwards.CloseAll)

	f, err := a.sharedState.Forwards.Open(freePort(t), target)
	if err != nil {
		t.Fatalf("Open: %v", err)
	}

	spy := &recorder{}
	a.views[command.ViewStatus] = spy

	a.Update(forward.RefreshMsg{})

	if len(spy.got) != 1 {
		t.Fatalf("the view was handed %d forwards, want 1", len(spy.got))
	}
	if spy.got[0].ID != f.ID || spy.got[0].Target != target {
		t.Errorf("the view was handed %+v, want the forward %q to %s", spy.got[0], f.ID, target)
	}
}

// A view off screen is updated where it stands, so that coming back to it
// shows what happened while it was away — the property broadcastJobs has.
func TestABroadcastReachesAViewThatIsNotOnScreen(t *testing.T) {
	a := newWithSize(testConfig(), 120, 40)
	t.Cleanup(a.sharedState.Forwards.CloseAll)

	spy := &recorder{}
	a.views[command.ViewOCIResources] = spy
	a.currentView = command.ViewDashboard

	a.Update(forward.RefreshMsg{})

	if spy.got == nil {
		t.Error("a view that was not on screen never received the snapshot")
	}
}
