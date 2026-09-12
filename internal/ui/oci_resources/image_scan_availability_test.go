package ociresources

import (
	"strings"
	"testing"

	"github.com/anthnel/devdesk/internal/config"
	"github.com/anthnel/devdesk/internal/docker"
	"github.com/anthnel/devdesk/internal/engine"
	"github.com/anthnel/devdesk/internal/scan"
	"github.com/anthnel/devdesk/internal/ui/keymap"
	"github.com/anthnel/devdesk/internal/ui/testutil"
)

// Scanning an *image* from a container means inspecting one the host engine
// holds, which needs that engine's socket mounted into Trivy. Rootless podman
// has none unless `podman system service` is running (§3.67), and that is
// knowable before the keypress — so S is greyed with its reason rather than
// launching a container that fails on startup (Rule 130).

// modelWithImage returns a view showing one image, with deps set as given.
func modelWithImage(t *testing.T, deps *scan.DependencyStatus) Model {
	t.Helper()
	m := New(config.Default())
	m.images = []docker.Image{{ID: "sha256:abc", Repository: "nginx", Tag: "latest"}}
	m.activeTab = tabImages
	m.updateImageTable()
	m.deps = deps
	return m
}

func TestSIsRefusedWhenThePodmanSocketIsAbsent(t *testing.T) {
	previous := engine.Current()
	t.Cleanup(func() { engine.SetCurrent(previous) })
	engine.SetCurrent(engine.ShapeFor(engine.Podman))

	m := modelWithImage(t, &scan.DependencyStatus{
		TrivyAvailable:  true,
		TrivySource:     scan.ToolSourceContainer,
		EngineAvailable: true,
		ImageScanSocket: "", // no `podman system service` running
	})

	act := m.imageScan()

	if act.Enabled() {
		t.Fatal("S is offered although there is no socket to inspect the image with")
	}
	// The reason names the engine and what to do about it: a refusal the user
	// cannot act on is barely better than a silent one.
	if !strings.Contains(act.Reason, "podman") {
		t.Errorf("Reason = %q, want it to name podman", act.Reason)
	}
	if !strings.Contains(act.Reason, "system service") {
		t.Errorf("Reason = %q, want it to say how to fix it", act.Reason)
	}
}

// The socket is S's problem alone. Launching and deleting go through the
// engine's CLI rather than through a container, so greying them for a missing
// socket would refuse actions that work.
func TestAMissingSocketDoesNotRefuseLaunchOrDelete(t *testing.T) {
	previous := engine.Current()
	t.Cleanup(func() { engine.SetCurrent(previous) })
	engine.SetCurrent(engine.ShapeFor(engine.Podman))

	m := modelWithImage(t, &scan.DependencyStatus{
		TrivyAvailable:  true,
		TrivySource:     scan.ToolSourceContainer,
		EngineAvailable: true,
	})

	if act := m.imageActions(); !act.Enabled() {
		t.Errorf("N and D are refused (%q) although only the image scan needs a socket", act.Reason)
	}
}

func TestSIsOfferedWhenTheSocketIsThere(t *testing.T) {
	m := modelWithImage(t, &scan.DependencyStatus{
		TrivyAvailable:  true,
		TrivySource:     scan.ToolSourceContainer,
		EngineAvailable: true,
		ImageScanSocket: "/var/run/docker.sock",
	})

	if act := m.imageScan(); !act.Enabled() {
		t.Errorf("S is refused (%q) although the socket is there", act.Reason)
	}
}

// A Trivy binary reads the image through the engine itself, so no socket of its
// own is mounted and the question does not arise.
func TestATrivyBinaryNeedsNoSocket(t *testing.T) {
	m := modelWithImage(t, &scan.DependencyStatus{
		TrivyAvailable: true,
		TrivySource:    scan.ToolSourceBinary,
		TrivyBinary:    "/usr/bin/trivy",
	})

	if act := m.imageScan(); !act.Enabled() {
		t.Errorf("S is refused (%q) although Trivy runs as a binary", act.Reason)
	}
}

// Not knowing is not knowing it is a no: while the check has not come back, S
// stays offered. Greying it for three frames and un-greying it afterwards reads
// as a fault.
func TestSStaysOfferedUntilTheCheckComesBack(t *testing.T) {
	m := modelWithImage(t, nil)

	if act := m.imageScan(); !act.Enabled() {
		t.Errorf("S is refused (%q) before anything has looked", act.Reason)
	}
}

// Rule 130: the set of keys does not change from one state to another within
// the same screen — only whether they are greyed.
func TestTheImageShortcutsDoNotChangeWithTheSocket(t *testing.T) {
	withSocket := modelWithImage(t, &scan.DependencyStatus{
		TrivyAvailable: true, TrivySource: scan.ToolSourceContainer,
		EngineAvailable: true, ImageScanSocket: "/var/run/docker.sock",
	})
	without := modelWithImage(t, &scan.DependencyStatus{
		TrivyAvailable: true, TrivySource: scan.ToolSourceContainer,
		EngineAvailable: true,
	})

	got := testutil.ShortcutKeys(without.GetShortcuts())
	want := testutil.ShortcutKeys(withSocket.GetShortcuts())
	if len(got) != len(want) {
		t.Fatalf("the shortcut set changed with the socket:\n with: %v\nwithout: %v", want, got)
	}
	for i := range want {
		if got[i] != want[i] {
			t.Errorf("shortcut %d = %q, want %q", i, got[i], want[i])
		}
	}

	if !testutil.ShortcutDisabled(without.GetShortcuts(), keymap.Scan) {
		t.Error("S is not greyed although there is no socket")
	}
	if testutil.ShortcutDisabled(withSocket.GetShortcuts(), keymap.Scan) {
		t.Error("S is greyed although the socket is there")
	}
}
