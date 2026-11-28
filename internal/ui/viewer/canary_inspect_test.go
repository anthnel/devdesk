package viewer_test

import (
	"strings"
	"testing"

	tea "charm.land/bubbletea/v2"
	"github.com/charmbracelet/x/ansi"

	"github.com/anthnel/devdesk/internal/config"
	"github.com/anthnel/devdesk/internal/docker"
	"github.com/anthnel/devdesk/internal/engine"
	"github.com/anthnel/devdesk/internal/testutil/canary"
	"github.com/anthnel/devdesk/internal/testutil/fakecli"
	"github.com/anthnel/devdesk/internal/ui/containers"
	"github.com/anthnel/devdesk/internal/ui/keymap"
	"github.com/anthnel/devdesk/internal/ui/testutil"
	"github.com/anthnel/devdesk/internal/ui/viewer"
)

// OC-23 (DEVDESK-821, S8), the inspect half: the container the launch started
// has SECRET=canary in its Config.Env and, under Podman, in
// Config.CreateCommand. The inspect is opened the way a user opens it (the
// containers view, enter, the document viewer, the real docker.InspectContainer
// against the fake CLI), and the test follows the value through the display, Y,
// `m` and the close.
//
// What is persistent here: every file under HOME (and the XDG directories, which
// canary.Home points inside it), the log, and the launch cache. The clipboard is
// not: it belongs to the operating system, the user asked for the copy, and what
// it receives is asserted on its own (masked by default, readable after `m`, which
// is the point of `m`). The screen is not persistent either, and is asserted per
// state.

const canaryContainerID = "0123456789ab"

// canaryInspect is the document the engine returns: docker's shape, plus
// Podman's CreateCommand with the three spellings of an environment option.
// Labels and Cmd carry no value: they are out of the mask's scope (OC-13).
func canaryInspect(engineName string) string {
	createCommand := ""
	if engineName == engine.Podman {
		createCommand = `,
            "CreateCommand": ["podman", "run", "-d", "--name", "web",
                "-e", "` + canary.Entry + `",
                "--env=` + canary.Entry + `",
                "-e` + canary.Entry + `",
                "--env-file", "/home/u/app.env",
                "nginx:1.27"]`
	}
	return `[
    {
        "Id": "` + canaryContainerID + `",
        "Name": "/web",
        "Config": {
            "Env": [
                "` + canary.Entry + `",
                "LOG_LEVEL=info",
                "PATH=/usr/local/bin:/usr/bin"
            ],
            "Cmd": ["nginx", "-g", "daemon off;"],
            "Labels": {"app": "web"}` + createCommand + `
        }
    }
]
`
}

// openedInspect drives the containers view to the inspect source and the viewer
// to the loaded document.
func openedInspect(t *testing.T) viewer.Model {
	t.Helper()
	cm := containers.New(config.Default())
	next, _ := cm.Update(tea.WindowSizeMsg{Width: 160, Height: 30})
	cm = next.(containers.Model)
	next, _ = cm.Update(containers.ContainersListMsg{Containers: []docker.Container{
		{ID: canaryContainerID, Name: "web", Image: "nginx:1.27", State: "running"},
	}})
	cm = next.(containers.Model)
	_, cmd := cm.Update(testutil.Key("enter"))
	if cmd == nil {
		t.Fatal("enter on a container issued no command")
	}
	open, ok := cmd().(viewer.OpenRequestMsg)
	if !ok || open.Source == nil {
		t.Fatalf("enter produced %T, want a viewer.OpenRequestMsg", cmd())
	}

	m := viewer.NewWithSource(config.Default(), open.Source)
	m = feedViewer(t, m, tea.WindowSizeMsg{Width: 100, Height: 40})
	init := m.Init()
	if init == nil {
		t.Fatal("the viewer's Init loads nothing")
	}
	return feedViewer(t, m, resolveCmd(t, init)...)
}

func resolveCmd(t *testing.T, cmd tea.Cmd) []tea.Msg {
	t.Helper()
	if cmd == nil {
		return nil
	}
	switch msg := cmd().(type) {
	case tea.BatchMsg:
		var out []tea.Msg
		for _, c := range msg {
			out = append(out, resolveCmd(t, c)...)
		}
		return out
	case nil:
		return nil
	default:
		return []tea.Msg{msg}
	}
}

func feedViewer(t *testing.T, m viewer.Model, msgs ...tea.Msg) viewer.Model {
	t.Helper()
	for _, msg := range msgs {
		next, _ := m.Update(msg)
		m = next.(viewer.Model)
	}
	return m
}

// pressY presses Y and delivers the result of the copy to the viewer.
func pressY(t *testing.T, m viewer.Model) viewer.Model {
	t.Helper()
	next, cmd := m.Update(testutil.Key(keymap.Copy))
	m = next.(viewer.Model)
	if cmd == nil {
		t.Fatal("Y issued no command")
	}
	return feedViewer(t, m, resolveCmd(t, cmd)...)
}

// screenSurfaces are what the viewer shows in its current state: the body in the
// display now on screen, the footer, and the header values (the "Env" field says
// masked or revealed and a count, never a value).
func screenSurfaces(prefix string, current func() viewer.Model) canary.Surfaces {
	var s canary.Surfaces
	s.Add(
		canary.Text(prefix+": body", func() string { return ansi.Strip(current().Render()) }),
		canary.Text(prefix+": footer", func() string { return ansi.Strip(current().RenderFooter(100)) }),
		canary.Lines(prefix+": header", func() []string {
			var out []string
			for _, h := range current().GetHeaderInfo("default") {
				out = append(out, h.Key+": "+h.Value)
			}
			return out
		}),
	)
	return s
}

func TestCanaryInTheInspectIsMaskedUntilMIsPressed(t *testing.T) {
	for _, name := range engine.Names() {
		t.Run(name, func(t *testing.T) {
			previous := engine.Current()
			engine.SetCurrent(engine.ShapeFor(name))
			t.Cleanup(func() { engine.SetCurrent(previous) })

			home, homeFiles := canary.Home(t)
			canary.AssertWired(t, home, homeFiles)
			logs := canary.CaptureLog(t, "DevDesk log")
			var clipboard, revealedClipboard canary.Recorder
			target := &clipboard
			restore := viewer.SwapClipboard(func(text string) error { target.Record(text); return nil })
			t.Cleanup(restore)
			fake := fakecli.Install(t, fakecli.Scenario{
				{Tool: name, Args: []string{"inspect"}, Prefix: true, Reply: fakecli.Reply{Stdout: canaryInspect(name)}},
			}, name)

			// The document as the engine gives it holds the value: the test can see it.
			canary.AssertPresent(t, canary.Static("the engine's inspect", canaryInspect(name)))

			m := openedInspect(t)
			current := func() viewer.Model { return m }
			// The inspect opens on the tree: masked there too, before `f`.
			canary.AssertAbsent(t, screenSurfaces("masked inspect (tree)", current)...)
			// `f` leaves the tree for the text display, where every line (the whole
			// CreateCommand included) is on screen.
			m = feedViewer(t, m, testutil.Key("f"))

			// 1. Masked: every surface, screen and persistent, is clean.
			var surfaces canary.Surfaces
			environ := canary.Environ("DevDesk process environment") // built once: it compares with what it saw now
			surfaces.Add(homeFiles, logs, clipboard.Surface("clipboard"), environ)
			surfaces.Add(screenSurfaces("masked inspect", current)...)
			surfaces.AssertAbsent(t)
			if body := ansi.Strip(m.Render()); !strings.Contains(body, canary.Name+"=********") {
				t.Fatalf("the masked inspect does not show %s=********:\n%s", canary.Name, body)
			}

			// 2. Y copies exactly what is shown: masked.
			m = pressY(t, m)
			if got := clipboard.Items(); len(got) != 1 || !strings.Contains(got[0], canary.Name+"=********") {
				t.Fatalf("Y copied %q, want the masked document", got)
			}
			surfaces.AssertAbsent(t)

			// 3. Negative control: m reveals, and the surfaces CAN see the value.
			m = feedViewer(t, m, testutil.Key("m"))
			canary.AssertPresent(t, canary.Text("revealed inspect: body", func() string { return ansi.Strip(m.Render()) }))
			if name == engine.Podman {
				// Both the Env entry and the three spellings of CreateCommand.
				if n := strings.Count(ansi.Strip(m.Render()), canary.Entry); n < 4 {
					t.Errorf("the revealed Podman inspect shows %d copies of %s, want 4 (Env and three CreateCommand spellings)", n, canary.Entry)
				}
			}
			target = &revealedClipboard
			m = pressY(t, m)
			canary.AssertPresent(t, revealedClipboard.Surface("clipboard after m"))

			// 4. A refresh while revealed, then the close. The reveal is state of the
			// model only: nothing persistent holds the value.
			next, reload := m.Update(testutil.Key("ctrl+r"))
			m = next.(viewer.Model)
			m = feedViewer(t, m, resolveCmd(t, reload)...)
			canary.AssertPresent(t, canary.Text("revealed inspect after ctrl+r", func() string { return ansi.Strip(m.Render()) }))
			next, _ = m.Update(testutil.Key("esc"))
			m = next.(viewer.Model)

			var persistent canary.Surfaces
			persistent.Add(homeFiles, logs)
			persistent.AssertAbsent(t)
			canary.AssertAbsent(t, environ)

			// 5. The next inspect of the same container opens masked again.
			reopened := openedInspect(t)
			reopened = feedViewer(t, reopened, testutil.Key("f"))
			canary.AssertAbsent(t, screenSurfaces("reopened inspect", func() viewer.Model { return reopened })...)
			canary.AssertAbsent(t, clipboard.Surface("clipboard before m"))

			// The fake engine saw one inspect per load and nothing else.
			for _, call := range fake.Invocations() {
				if len(call.Args) == 0 || call.Args[0] != "inspect" {
					t.Errorf("the engine was asked %q, want inspect only", call.Args)
				}
				canary.AssertAbsent(t, canary.Lines("engine argv", func() []string { return call.Args }))
			}
		})
	}
}
