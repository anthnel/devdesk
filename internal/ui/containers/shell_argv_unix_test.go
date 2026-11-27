//go:build unix

package containers

import (
	"path/filepath"
	"slices"
	"strings"
	"testing"
	"time"

	tea "charm.land/bubbletea/v2"

	"github.com/anthnel/devdesk/internal/docker"
	"github.com/anthnel/devdesk/internal/engine"
	"github.com/anthnel/devdesk/internal/testutil/fakecli"
	sharedcomponents "github.com/anthnel/devdesk/internal/ui/components"
	"github.com/anthnel/devdesk/internal/ui/keymap"
	"github.com/anthnel/devdesk/internal/ui/testutil"
)

// OC-18 (DEVDESK-816): T opens `<engine> exec -it <id> <shell>` as an argv array,
// never through an interpreter, and a container ID that is not one is refused
// before anything is started, with a footer message at level Error.
//
// The engine and the terminal emulator are both fakecli tools (one Install: it
// owns PATH), so the journal tells exactly what was started and with which argv.

// shellTools installs a fake docker and a fake kitty, announces kitty as the
// terminal, and pins the docker engine. docker answers `exec <id> which bash`
// when id is not empty; kitty answers anything.
func shellTools(t *testing.T, id string, bashPresent bool) *fakecli.Fake {
	t.Helper()
	scenario := fakecli.Scenario{{Tool: "kitty", Prefix: true}}
	if id != "" {
		exit := 0
		if !bashPresent {
			exit = 1
		}
		scenario = append(scenario, fakecli.Exchange{
			Tool: "docker", Args: []string{"exec", id, "which", "bash"}, Reply: fakecli.Reply{Exit: exit},
		})
	}
	fake := fakecli.Install(t, scenario, "docker", "kitty")
	t.Setenv("WT_SESSION", "")
	t.Setenv("KITTY_WINDOW_ID", "1")

	previous := engine.Current()
	engine.SetCurrent(engine.ShapeFor("docker"))
	t.Cleanup(func() { engine.SetCurrent(previous) })
	return fake
}

// journalOf returns the invocations of one tool.
func journalOf(f *fakecli.Fake, tool string) []fakecli.Invocation {
	var out []fakecli.Invocation
	for _, inv := range f.Invocations() {
		if inv.Tool == tool {
			out = append(out, inv)
		}
	}
	return out
}

// waitForTool polls the journal until the tool has run, within d.
func waitForTool(f *fakecli.Fake, tool string, d time.Duration) []fakecli.Invocation {
	deadline := time.Now().Add(d)
	for {
		if got := journalOf(f, tool); len(got) > 0 || time.Now().After(deadline) {
			return got
		}
		time.Sleep(10 * time.Millisecond)
	}
}

// AC1, AC3, AC4: in a new window the emulator receives `-- <engine> exec -it
// <id> <shell>`, each of them its own argv element, with the container's real ID,
// bash when the container has it and sh when it does not. No `sh -c`, no joined
// string.
func TestShellWindowIsAnArgvArray(t *testing.T) {
	for _, tt := range []struct {
		name  string
		bash  bool
		shell string
	}{
		{"bash available", true, "/bin/bash"},
		{"bash missing falls back to sh", false, "/bin/sh"},
	} {
		t.Run(tt.name, func(t *testing.T) {
			fake := shellTools(t, webID, tt.bash)
			m := runningWeb(t, true)

			m, cmd := updateWithin(t, m, testutil.Key(keymap.Terminal), 5*time.Second)
			for i := 0; i < 3 && cmd != nil; i++ {
				msg := cmd()
				if _, ok := msg.(ShellWindowOpenedMsg); ok {
					break
				}
				m, cmd = step(t, m, msg)
			}

			ran := waitForTool(fake, "kitty", 3*time.Second)
			if len(ran) != 1 {
				t.Fatalf("the terminal was started %d time(s), want once", len(ran))
			}
			args := ran[0].Args
			if len(args) != 6 || args[0] != "--" || filepath.Base(args[1]) != "docker" ||
				!slices.Equal(args[2:], []string{"exec", "-it", webID, tt.shell}) {
				t.Errorf("terminal argv = %q, want [-- <engine> exec -it %s %s], one element each", args, webID, tt.shell)
			}
			if !docker.IsContainerID(webID) {
				t.Fatalf("setup: %q is not a container ID", webID)
			}
			for _, w := range args {
				if w == "-c" || w == "sh -c" || strings.Contains(w, " ") {
					t.Errorf("terminal argv %q holds %q: the session goes through an interpreter or a joined string", args, w)
				}
			}
		})
	}
}

// hostileIDs are what a container's ID may never be: shell text, an option, an
// empty value, a value too short. They are put in the list the view shows, as a
// drifting parser would.
var hostileIDs = []struct{ name, id string }{
	{"shell metacharacters", "abc; rm -rf ~"},
	{"a leading dash", "-0123456789ab"},
	{"empty", ""},
	{"too short", "0123456789a"},
}

// modelWithRunningID is a model whose only, selected, running container has the
// given ID.
func modelWithRunningID(t *testing.T, id string, newWindow bool) Model {
	t.Helper()
	m := feed(t, newTestModel(t), ContainersListMsg{Containers: []docker.Container{
		{ID: id, Name: "evil", Image: "busybox", State: "running"},
	}})
	m.config.App.TerminalNewWindow = newWindow
	if c := m.getSelectedContainer(); c == nil || c.ID != id {
		t.Fatalf("setup: the selected container is %+v, want the one with ID %q", c, id)
	}
	return m
}

// settleRefusal drains what a refused shell leaves behind: the footer timer and
// anything the view asks for next. It fails on every message that would mean a
// process was handed over, and returns the last Error the footer showed.
func settleRefusal(t *testing.T, m Model, cmd tea.Cmd) (Model, string) {
	t.Helper()
	var shown string
	note := func(m Model) {
		if m.footer.Level() == sharedcomponents.LevelError && m.footer.Text() != "" {
			shown = m.footer.Text()
		}
	}
	note(m)
	for depth := 0; depth < 4 && cmd != nil; depth++ {
		var next []tea.Cmd
		for _, msg := range testutil.Msgs(cmd) {
			switch msg.(type) {
			case sharedcomponents.ClearFooterMsg:
				continue // the timer; feeding it would clear the message under test
			case ShellDetectedMsg, ShellWindowOpenedMsg, PagerExitMsg:
			default:
				t.Errorf("a refused shell produced %T: something was handed to a process", msg)
				continue
			}
			var c tea.Cmd
			m, c = step(t, m, msg)
			note(m)
			next = append(next, c)
		}
		cmd = tea.Batch(next...)
	}
	return m, shown
}

// expectRefused checks the outcome shared by every refusal: an Error in the footer that says the
// identifier is refused without echoing it, no pending wait, nothing started.
func expectRefused(t *testing.T, m Model, shown, id string, fake *fakecli.Fake) {
	t.Helper()
	if shown == "" {
		t.Fatal("the footer never showed an Error: the refusal is silent")
	}
	if !strings.Contains(strings.ToLower(shown), "refused") {
		t.Errorf("footer = %q, want it to say the container ID is refused", shown)
	}
	if len(id) > 3 && strings.Contains(shown, id) {
		t.Errorf("footer = %q echoes the refused value", shown)
	}
	if m.WantsBlink() {
		t.Error("the view still waits on a shell that was refused")
	}
	if got := fake.Invocations(); len(got) != 0 {
		t.Errorf("a refused container ID started %d process(es): %+v", len(got), got)
	}
}

// AC2: T on a container whose ID is not a container ID, in place and in a window,
// starts nothing (no `which bash`, no exec, no terminal) and tells the user, at
// level Error, that the identifier is refused.
func TestShellKeyRefusesAnInvalidContainerID(t *testing.T) {
	for _, newWindow := range []bool{false, true} {
		mode := map[bool]string{false: "in place", true: "in a new window"}[newWindow]
		for _, bad := range hostileIDs {
			t.Run(mode+"/"+bad.name, func(t *testing.T) {
				testutil.FastTimers(t, &sharedcomponents.FooterMsgDuration)
				fake := shellTools(t, "", false) // no `exec` declared: any engine call fails the test
				m := modelWithRunningID(t, bad.id, newWindow)

				m, cmd := step(t, m, testutil.Key(keymap.Terminal))
				m, shown := settleRefusal(t, m, cmd)

				expectRefused(t, m, shown, bad.id, fake)
			})
		}
	}
}

// AC2, defence in depth: the answer of the detection names the container too. A
// message carrying a malformed ID (a stale or forged one) starts no shell, in
// place or in a window.
func TestShellDetectedRefusesAnInvalidContainerID(t *testing.T) {
	for _, newWindow := range []bool{false, true} {
		mode := map[bool]string{false: "in place", true: "in a new window"}[newWindow]
		for _, bad := range hostileIDs {
			t.Run(mode+"/"+bad.name, func(t *testing.T) {
				testutil.FastTimers(t, &sharedcomponents.FooterMsgDuration)
				fake := shellTools(t, "", false)
				m := runningWeb(t, newWindow)

				m, cmd := step(t, m, ShellDetectedMsg{ID: bad.id, Shell: "/bin/sh"})
				m, shown := settleRefusal(t, m, cmd)

				expectRefused(t, m, shown, bad.id, fake)
			})
		}
	}
}
