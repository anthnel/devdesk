package ociresources

import (
	"errors"
	"strings"
	"testing"

	"github.com/anthnel/devdesk/internal/cache"
	"github.com/anthnel/devdesk/internal/docker"
)

// The launch flow spans four messages that arrive out of order — the exposed
// ports, the cached options, the entrypoint verification and the launch result
// — so what matters is which of them are allowed to act on a form that has
// moved on.

func launchingModel(t *testing.T, ports ...string) Model {
	t.Helper()
	m := feed(t, loadedModel(t), ImageExposedPortsMsg{ImageName: "api:v1", Ports: ports})
	if m.launchForm == nil {
		t.Fatal("the exposed ports did not open the launch form")
	}
	return m
}

func TestTheExposedPortsOpenTheLaunchForm(t *testing.T) {
	m := launchingModel(t, "80/tcp", "443/tcp")

	if m.launchForm.image != "api:v1" {
		t.Errorf("the form is for %q, want the image whose ports arrived", m.launchForm.image)
	}
	if len(m.launchForm.ports) != 2 {
		t.Errorf("%d port rows, want one per EXPOSE", len(m.launchForm.ports))
	}
	// The networks the view already holds are offered, so the user does not have
	// to type a name they cannot see.
	if len(m.launchForm.networks) != len(networkFixtures()) {
		t.Errorf("%d networks offered, want the ones the view holds", len(m.launchForm.networks))
	}
}

// Failing to read the ports is not failing to launch: the form opens with none,
// and the extra-ports field is still there.
func TestTheFormStillOpensWhenThePortsCannotBeRead(t *testing.T) {
	m := feed(t, loadedModel(t), ImageExposedPortsMsg{
		ImageName: "api:v1", Err: errors.New("no such image"),
	})

	if m.launchForm == nil {
		t.Fatal("a failed port lookup left the user with no form")
	}
	if len(m.launchForm.ports) != 0 {
		t.Errorf("%d port rows, want none", len(m.launchForm.ports))
	}
}

func TestCachedOptionsAreAppliedToTheForm(t *testing.T) {
	m := launchingModel(t, "80/tcp")

	m = feed(t, m, LaunchOptionsCacheLoadedMsg{
		ImageName: "api:v1",
		Entry: &cache.LaunchOptionsEntry{
			Env:     "LOG_LEVEL=debug",
			Network: "devdesk",
			Detach:  true,
		},
	})

	if m.launchForm.envInput.Value() != "LOG_LEVEL=debug" {
		t.Errorf("env = %q, want the cached value", m.launchForm.envInput.Value())
	}
	if !m.launchForm.optionDetach {
		t.Error("the cached detach option was not applied")
	}
}

// The cache is read asynchronously, so its answer can arrive after the user has
// closed the form or opened another image's. Applying it then would rewrite a
// form the user is already filling in.
func TestStaleCachedOptionsAreDiscarded(t *testing.T) {
	m := launchingModel(t, "80/tcp")
	entry := &cache.LaunchOptionsEntry{Env: "FROM=elsewhere"}

	m = feed(t, m, LaunchOptionsCacheLoadedMsg{ImageName: "other:v9", Entry: entry})
	if m.launchForm.envInput.Value() != "" {
		t.Error("options cached for another image were applied")
	}

	closed := feed(t, m, LaunchFormCancelMsg{})
	if closed.launchForm != nil {
		t.Fatal("the form did not close")
	}
	// Nothing to assert beyond not panicking: the handler has to tolerate the
	// answer arriving after the form is gone.
	feed(t, closed, LaunchOptionsCacheLoadedMsg{ImageName: "api:v1", Entry: entry})
}

// A cached entrypoint has to be re-verified: the image may have been rebuilt
// since, and the form shows whether the binary is actually in it.
func TestACachedEntrypointIsVerifiedAgain(t *testing.T) {
	m := launchingModel(t, "80/tcp")

	next, cmd := step(t, m, LaunchOptionsCacheLoadedMsg{
		ImageName: "api:v1",
		Entry:     &cache.LaunchOptionsEntry{Entrypoint: "/bin/sh"},
	})

	if cmd == nil {
		t.Fatal("a cached entrypoint was applied without being verified")
	}
	if next.launchForm.verify != verifyChecking {
		t.Errorf("verify = %v, want the form to show a check in progress", next.launchForm.verify)
	}
	if next.launchForm.verifySeq == 0 {
		t.Error("the verification was not given a sequence number to discard stale answers with")
	}
}

func TestCachedOptionsWithNoEntrypointVerifyNothing(t *testing.T) {
	m := launchingModel(t, "80/tcp")

	_, cmd := step(t, m, LaunchOptionsCacheLoadedMsg{
		ImageName: "api:v1",
		Entry:     &cache.LaunchOptionsEntry{Network: "devdesk"},
	})

	if cmd != nil {
		t.Error("a verification was started with no entrypoint to verify")
	}
}

// ── Launching ────────────────────────────────────────────────────────────────

// The options are only worth remembering once the container actually started —
// caching a set that Docker refused would hand the same failure back next time.
func TestOptionsAreSavedOnlyOnceTheContainerStarts(t *testing.T) {
	m := launchingModel(t, "80/tcp")
	m = feed(t, m, LaunchFormSubmitMsg{Opts: docker.ContainerLaunchOptions{Image: "api:v1", Detach: true}})

	if m.lastLaunchOpts == nil || m.lastLaunchImage != "api:v1" {
		t.Fatal("the submitted options were not held for saving")
	}
	if m.launchForm != nil {
		t.Error("the form stayed open after submitting")
	}

	next, cmd := step(t, m, ContainerLaunchCompleteMsg{ContainerID: "9f2c"})
	if cmd == nil {
		t.Error("a successful launch saved nothing")
	}
	if next.lastLaunchOpts != nil || next.lastLaunchImage != "" {
		t.Error("the pending save was not cleared once it had been issued")
	}
}

func TestAFailedLaunchReportsAndForgetsTheOptions(t *testing.T) {
	m := launchingModel(t, "80/tcp")
	m = feed(t, m, LaunchFormSubmitMsg{Opts: docker.ContainerLaunchOptions{Image: "api:v1"}})

	next, cmd := step(t, m, ContainerLaunchCompleteMsg{Err: errors.New("port is already allocated")})

	if next.errorMsg == "" {
		t.Error("a failed launch reported nothing to the user")
	}
	// Rule 128: a footer message without a timer stays on screen forever.
	if cmd == nil {
		t.Error("the error was set with no clear timer")
	}
	if next.lastLaunchOpts != nil {
		t.Error("options Docker refused were kept for saving")
	}
}

// An interactive container needs a real terminal, so it is handed to the
// runtime rather than run in the background — the difference is invisible in
// the message and total in the behaviour.
func TestAnInteractiveContainerIsHandedToTheTerminal(t *testing.T) {
	installFakeDocker(t, fakeScript{"docker run": {Stdout: "9f2c\n"}})
	m := launchingModel(t, "80/tcp")

	// A detached launch runs the container itself and answers with the result.
	_, background := step(t, m, LaunchFormSubmitMsg{Opts: docker.ContainerLaunchOptions{
		Image: "api:v1", Detach: true,
	}})
	if _, ok := run(t, background).(ContainerLaunchCompleteMsg); !ok {
		t.Error("a detached launch did not run the container itself")
	}

	// An interactive one hands the process over instead: tea.ExecProcess wraps
	// it in a message for the runtime, which is what suspends the TUI.
	_, interactive := step(t, m, LaunchFormSubmitMsg{Opts: docker.ContainerLaunchOptions{
		Image: "api:v1", Interactive: true, TTY: true,
	}})
	if interactive == nil {
		t.Fatal("an interactive launch produced no command")
	}
	if _, ok := run(t, interactive).(ContainerLaunchCompleteMsg); ok {
		t.Error("an interactive launch ran the container in the background")
	}
}

// With no docker at all the hand-off cannot be built, and that has to be
// reported rather than silently doing nothing.
func TestAnInteractiveLaunchWithNoDockerIsReported(t *testing.T) {
	noDocker(t)
	m := launchingModel(t, "80/tcp")

	next, cmd := step(t, m, LaunchFormSubmitMsg{Opts: docker.ContainerLaunchOptions{
		Image: "api:v1", Interactive: true, TTY: true,
	}})

	if next.errorMsg == "" {
		t.Error("a launch that could not be built reported nothing")
	}
	if cmd == nil {
		t.Error("the error was set with no clear timer (Rule 128)")
	}
}

// ── The clipboard ────────────────────────────────────────────────────────────

// Both outcomes are footer messages, and Rule 128 caps both at three seconds.
func TestCopyingTheCommandReportsEitherWay(t *testing.T) {
	m := loadedModel(t)

	ok, okCmd := step(t, m, ClipboardCopyMsg{})
	if !strings.Contains(ok.infoMsg, "copied") {
		t.Errorf("infoMsg = %q, want confirmation of the copy", ok.infoMsg)
	}
	if okCmd == nil {
		t.Error("the confirmation was set with no clear timer")
	}

	failed, failedCmd := step(t, m, ClipboardCopyMsg{Err: errors.New("no clipboard on this system")})
	if failed.errorMsg == "" {
		t.Error("a failed copy reported nothing")
	}
	if failedCmd == nil {
		t.Error("the error was set with no clear timer")
	}
}

// ── Creating networks and volumes ────────────────────────────────────────────

// One form creates both, and the kind is the only thing that decides which
// Docker object comes out of it.
func TestTheResourceFormCreatesWhicheverKindItWasOpenedFor(t *testing.T) {
	installFakeDocker(t, fakeScript{})
	m := loadedModel(t)

	network, cmd := step(t, m, ResourceFormSubmitMsg{Kind: resourceFormNetwork, Name: "devdesk", Driver: "bridge"})
	if network.resourceForm != nil {
		t.Error("the form stayed open after submitting")
	}
	if _, ok := run(t, cmd).(NetworkActionMsg); !ok {
		t.Error("a network submission did not create a network")
	}

	_, cmd = step(t, m, ResourceFormSubmitMsg{Kind: resourceFormVolume, Name: "pgdata", Driver: "local"})
	if _, ok := run(t, cmd).(VolumeActionMsg); !ok {
		t.Error("a volume submission did not create a volume")
	}
}
