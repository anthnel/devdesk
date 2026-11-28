package app

import (
	"fmt"
	"strings"
	"testing"

	tea "charm.land/bubbletea/v2"
	"github.com/charmbracelet/x/ansi"

	"github.com/anthnel/devdesk/internal/command"
	"github.com/anthnel/devdesk/internal/docker"
	"github.com/anthnel/devdesk/internal/engine"
	"github.com/anthnel/devdesk/internal/testutil/canary"
	"github.com/anthnel/devdesk/internal/testutil/fakecli"
	ociresources "github.com/anthnel/devdesk/internal/ui/oci_resources"
)

// OC-23 (DEVDESK-821, S8), the router's half: a launch whose environment holds
// SECRET=canary goes through the real router and the real OCI view, the engine
// refuses it and repeats the value on its stderr, and the surfaces only the router
// owns are read: the whole screen, the window title, the terminal progress bar, the
// jobs registry and its failure memory, the `:jobs` view, and every file under HOME.
// The form, the preview, ctrl+y, the cache and the inspect are in
// internal/ui/oci_resources and internal/ui/viewer (their seams are unexported).
//
// Not covered, because nothing a launch does reaches it: there is no command
// history (the palette keeps none), a launch is not a job (jobs.Kind has no launch
// kind), and the MCP server exposes no tool that reads an environment or a launch
// (`containers_list` is a `docker ps` listing).
func TestCanaryLaunchedThroughTheRouterReachesNoRouterSurface(t *testing.T) {
	for _, name := range engine.Names() {
		t.Run(name, func(t *testing.T) {
			previous := engine.Current()
			engine.SetCurrent(engine.ShapeFor(name))
			t.Cleanup(func() { engine.SetCurrent(previous) })

			a := ociRouter(t) // isolateHome inside; PATH emptied
			home, homeFiles := canary.Home(t)
			canary.AssertWired(t, home, homeFiles)
			logs := canary.CaptureLog(t, "DevDesk log")
			fake := fakecli.Install(t, fakecli.Scenario{
				{Tool: name, Args: []string{"run"}, Prefix: true, Reply: fakecli.Reply{
					Stderr: "Error: invalid environment " + canary.Entry + "\n", Exit: 125,
				}},
			}, name)

			var surfaces canary.Surfaces
			surfaces.Add(
				homeFiles, logs, canary.Environ("DevDesk process environment"),
				canary.Text("router screen", func() string { return ansi.Strip(a.Render()) }),
				canary.Text("window title", func() string { return a.View().WindowTitle }),
				canary.Text("terminal progress bar", func() string { return fmt.Sprintf("%+v", a.View().ProgressBar) }),
				canary.Text("jobs registry", func() string { return fmt.Sprintf("%+v %+v", a.jobs.Snapshot(), a.jobs.Failures()) }),
				canary.Text("router footer", func() string { return ansi.Strip(a.renderViewFooter()) }),
			)

			// The form opens (a Msg the view sends itself) and the launch is submitted.
			a.Update(ociresources.ImageExposedPortsMsg{ImageName: "nginx:1.27"})
			opts := docker.ContainerLaunchOptions{Image: "nginx:1.27", Env: []string{canary.Entry}, Detach: true}
			_, cmd := a.Update(ociresources.LaunchFormSubmitMsg{Opts: opts})
			if cmd == nil {
				t.Fatal("the router returned no command for the submit")
			}
			done, found := firstMsgOf[ociresources.ContainerLaunchCompleteMsg](cmd)
			if !found || done.Err == nil {
				t.Fatalf("the submit produced %+v (found %v), want a refused launch", done, found)
			}
			a.Update(done)

			calls := fake.Invocations()
			if len(calls) != 1 {
				t.Fatalf("the engine was started %d times, want one run", len(calls))
			}
			canary.AssertPresent(t, canary.Lines("child environment", func() []string { return calls[0].Env }))
			surfaces.Add(canary.Lines("engine argv", func() []string { return calls[0].Args }))
			// The failure is on screen, so the surfaces below are read after something
			// was shown, and what is shown is the fixed sentence.
			if screen := ansi.Strip(a.Render()); !strings.Contains(screen, "Failed to launch container") {
				t.Fatalf("the refused launch left no sentence on the router's screen:\n%s", screen)
			}
			surfaces.AssertAbsent(t)

			// The :jobs view, the screen that lists what ran this session.
			_ = a.switchView(command.ViewJobs)
			a.resize(a.width, a.height)
			canary.AssertAbsent(t, surfaces...)
		})
	}
}

// firstMsgOf runs cmd, and the members of a batch in order, until one returns a
// message of type T. A member that is a timer (a footer expiry) would block for its
// whole duration: the submit of the OCI view returns the launch command alone, so
// none is reached here, and a test that changed that would be slow, not wrong.
func firstMsgOf[T tea.Msg](cmd tea.Cmd) (T, bool) {
	var zero T
	if cmd == nil {
		return zero, false
	}
	switch msg := cmd().(type) {
	case T:
		return msg, true
	case tea.BatchMsg:
		for _, c := range msg {
			if got, ok := firstMsgOf[T](c); ok {
				return got, true
			}
		}
	}
	return zero, false
}
