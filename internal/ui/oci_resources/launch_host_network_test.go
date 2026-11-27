package ociresources

import (
	"fmt"
	"image/color"
	"slices"
	"strings"
	"testing"

	"github.com/anthnel/devdesk/internal/cache"
	"github.com/anthnel/devdesk/internal/engine"
	sharedcomponents "github.com/anthnel/devdesk/internal/ui/components"
	"github.com/anthnel/devdesk/internal/ui/theme"
	"github.com/charmbracelet/x/ansi"
)

// OC-21 (DEVDESK-819, requirement S4): the host network asks first, through the
// modal OC-20 opened for the sensitive mounts (components.ChoiceModal, Cancel
// the default, one modal however many reasons).
//
// Finding on the current code: the Network field is NOT a free text. It is a ←→
// cycle field over the networks of the engine (plus `bridge` when there are
// none), and the engines list `host` and `none` among them. The launcher takes a
// free string though (ContainerLaunchOptions.Network), and a cached choice comes
// back by name (ApplyCached). So the check is on the value the form submits.
//
// Expected production behavior, pinned below:
//
//   - A network whose name is "host" once trimmed and compared without regard to
//     case (` HOST `, `Host`) is the host network. Suggested API:
//     `docker.SharesHostNetwork(network string) bool`; the form calls it at the
//     submit, next to docker.SensitiveMounts, and holds the launch behind the one
//     modal. Nothing is started before the answer. Interactive launches included.
//   - Host network only: title "Host network", the sentence hostNetworkReason, one
//     action hostNetworkAction and Cancel (the default). enter, esc and a closed
//     modal cancel; y, Y, n and space answer nothing; left then enter confirms.
//   - Host network and sensitive mounts: ONE modal that names every mount (path and
//     reason, as OC-20) and the sentence hostNetworkReason, with the action
//     "Mount and use the host network", or "Mount the engine socket and use the
//     host network" when a socket is among the mounts (the footer is then the
//     socket's Error, as in OC-20). Never two modals in a row.
//   - The argv after the confirmation is today's form: `--network` and `host` as two
//     elements, the network as typed in the list, nothing else added: no --pid,
//     --ipc, --uts, --userns, --privileged.
//   - The form footer, derived on every render (Rule 128, no timer), is
//     hostNetworkFooter at level Warning while the network field holds host, in
//     the form and while the modal is open; a validation Error wins over it.
//   - The command preview (right column) shows the `--network host` line in the
//     warning colour of the theme (ColorFooterWarn), and no other line. The text
//     copied by ctrl+y is plain, `--network host` included.
//   - `container:<id>` is not handled by OC-21 (follow-up): no modal, `--network
//     container:<id>` as typed, and never a --pid or --ipc with it.
//   - Any other network name (bridge, none, my-host, host1...) asks nothing.

const (
	hostNetworkReason = "The container shares the network of this host, loopback included"
	hostNetworkAction = "Use the host network"
	hostNetworkFooter = "This container will share the network of this host"
)

var hostSpellings = []string{"host", "HOST", "Host", " host ", " HOST", "host "}

// newNetworkRig is a mountRig whose launch form offers the named networks.
func newNetworkRig(t *testing.T, engineName string, names ...string) *mountRig {
	t.Helper()
	r := newMountRig(t, engineName)
	m := feed(t, loadedModel(t), NetworksListMsg{Networks: networksNamed(names)})
	r.m = feed(t, m, ImageExposedPortsMsg{ImageName: "api:v1"})
	if r.m.launchForm == nil {
		t.Fatal("the exposed ports did not open the launch form")
	}
	return r
}

// selectNetwork cycles the Network field with the right arrow until it holds name.
func (r *mountRig) selectNetwork(t *testing.T, name string) {
	t.Helper()
	f := r.m.launchForm
	focusLaunchField(f, f.fieldNetwork())
	for range len(f.networks) {
		if f.networks[f.networkIdx] == name {
			return
		}
		r.press(t, "right")
	}
	t.Fatalf("the network %q is not offered: %q", name, f.networks)
}

func hostModalShown(screen string) bool { return strings.Contains(screen, hostNetworkReason) }

// ── A host network asks first ────────────────────────────────────────────────

func TestTheHostNetworkOpensAModalWhateverItsSpelling(t *testing.T) {
	for _, spelling := range hostSpellings {
		t.Run(fmt.Sprintf("%q", spelling), func(t *testing.T) {
			r := newNetworkRig(t, engine.Docker, "bridge", spelling)
			r.selectNetwork(t, spelling)

			r.submit(t)

			screen := r.screen()
			if formShown(screen) {
				t.Errorf("the form is still on screen, want the modal:\n%s", screen)
			}
			for _, text := range []string{"Host network", hostNetworkReason, hostNetworkAction, "Cancel"} {
				if !strings.Contains(screen, text) {
					t.Errorf("the modal lacks %q:\n%s", text, screen)
				}
			}
			for _, bare := range []string{"Yes", "No", "Mount anyway"} {
				if strings.Contains(screen, " "+bare+" ") {
					t.Errorf("the modal offers %q:\n%s", bare, screen)
				}
			}
			if r.m.launchForm == nil {
				t.Error("the form is gone: cancelling could not bring it back")
			}
			if runs := r.runs(); len(runs) != 0 {
				t.Errorf("the engine was started before the answer: %v", runs)
			}
		})
	}
}

// Criterion 2: Cancel is the default, and it gives the form back as it was.
func TestCancellingTheHostNetworkGivesTheFormBackIntact(t *testing.T) {
	for _, key := range []string{"enter", "esc"} {
		t.Run(key, func(t *testing.T) {
			r := newNetworkRig(t, engine.Docker, "bridge", "host", "none")
			r.selectNetwork(t, "host")
			r.m.launchForm.nameInput.SetValue("web")
			r.m.launchForm.userInput.SetValue("1000:1000")
			r.submit(t)

			r.press(t, key)

			if runs := r.runs(); len(runs) != 0 {
				t.Fatalf("%s launched: %v", key, runs)
			}
			f := r.m.launchForm
			if f == nil {
				t.Fatal("the form was closed by the cancel")
			}
			if f.networks[f.networkIdx] != "host" || f.nameInput.Value() != "web" || f.userInput.Value() != "1000:1000" {
				t.Errorf("the form came back changed: network %q, name %q, user %q", f.networks[f.networkIdx], f.nameInput.Value(), f.userInput.Value())
			}
			if f.focusedField != f.fieldSubmit() {
				t.Errorf("the focus is on field %d, want the Launch button", f.focusedField)
			}
			if screen := r.screen(); !formShown(screen) || hostModalShown(screen) {
				t.Errorf("the form is not what is on screen:\n%s", screen)
			}
			// The same enter asks again.
			r.press(t, "enter")
			if !hostModalShown(r.screen()) {
				t.Error("a second launch did not ask again")
			}
		})
	}
}

// An extra press toward Cancel does not wrap onto the action, and no letter of
// habit answers: only the named action, reached with left, then enter.
func TestNoKeyButTheNamedChoiceUsesTheHostNetwork(t *testing.T) {
	for _, key := range []string{"y", "Y", "n", "space", "right"} {
		t.Run(key, func(t *testing.T) {
			r := newNetworkRig(t, engine.Docker, "bridge", "host")
			r.selectNetwork(t, "host")
			r.submit(t)

			r.press(t, key)
			if key == "right" {
				r.press(t, "enter") // already on Cancel: stays there
				if len(r.runs()) != 0 || r.m.launchForm == nil {
					t.Fatalf("right then enter launched: %v", r.runs())
				}
				return
			}

			if len(r.runs()) != 0 {
				t.Fatalf("%q launched: %v", key, r.runs())
			}
			if !hostModalShown(r.screen()) {
				t.Errorf("%q closed the modal: it is the named choice or Cancel, nothing else", key)
			}
			r.press(t, "left", "enter")
			if len(r.runs()) != 1 {
				t.Errorf("after %q, left and enter did not launch: %v", key, r.runs())
			}
		})
	}
}

// Confirming launches with `--network host` as two elements, on both engines,
// and nothing else is added.
func TestConfirmingTheHostNetworkLaunchesIt(t *testing.T) {
	forEachLaunchEngine(t, func(t *testing.T, name string) {
		r := newNetworkRig(t, name, "bridge", "host")
		r.selectNetwork(t, "host")
		r.submit(t)
		if len(r.runs()) != 0 {
			t.Fatalf("launched before the answer: %v", r.runs())
		}

		r.confirm(t)

		runs := r.runs()
		if len(runs) != 1 {
			t.Fatalf("the engine was started %d times, want one run: %v", len(runs), runs)
		}
		argv := runs[0]
		if !containsPair(argv, "--network", "host") {
			t.Errorf("argv = %q, want --network host", argv)
		}
		if n := countOf(argv, "--network"); n != 1 {
			t.Errorf("argv = %q holds --network %d times, want once", argv, n)
		}
		for _, e := range argv {
			for _, p := range []string{"--pid", "--ipc", "--uts", "--userns", "--privileged", "--cap-add", "--security-opt", "--device"} {
				if strings.HasPrefix(e, p) {
					t.Errorf("argv = %q holds %q: nothing but the network was asked", argv, e)
				}
			}
		}
		if r.m.launchForm != nil {
			t.Error("the form is still open after the launch")
		}
		if screen := r.screen(); hostModalShown(screen) {
			t.Errorf("the modal is still on screen:\n%s", screen)
		}
	})
}

func countOf(argv []string, word string) int {
	n := 0
	for _, e := range argv {
		if e == word {
			n++
		}
	}
	return n
}

// Every spelling is held; confirming launches once.
func TestAnyHostSpellingLaunchesOnceConfirmed(t *testing.T) {
	for _, spelling := range hostSpellings {
		t.Run(fmt.Sprintf("%q", spelling), func(t *testing.T) {
			r := newNetworkRig(t, engine.Docker, "bridge", spelling)
			r.selectNetwork(t, spelling)
			r.submit(t)
			if !hostModalShown(r.screen()) || len(r.runs()) != 0 {
				t.Fatalf("the launch was not held (runs %v):\n%s", r.runs(), r.screen())
			}

			r.confirm(t)

			if runs := r.runs(); len(runs) != 1 || runs[0][0] != "run" {
				t.Errorf("the engine saw %v, want the one run", runs)
			}
		})
	}
}

// An interactive launch is handed to the terminal: it is held like any other.
func TestAnInteractiveLaunchOnTheHostNetworkIsHeldToo(t *testing.T) {
	r := newNetworkRig(t, engine.Docker, "bridge", "host")
	r.selectNetwork(t, "host")
	r.m.launchForm.optionInteractiveTTY, r.m.launchForm.optionDetach = true, false

	r.submit(t)

	if !hostModalShown(r.screen()) {
		t.Fatalf("an interactive launch was not held:\n%s", r.screen())
	}
	r.press(t, "enter")
	if len(r.runs()) != 0 || r.m.launchForm == nil || !r.m.launchForm.optionInteractiveTTY {
		t.Errorf("cancelling did not give the form back as it was (runs %v)", r.runs())
	}
}

// The cache keeps the network by name: a host network that comes back from it is
// asked about like one the user picked.
func TestAHostNetworkRestoredFromTheCacheIsAskedAbout(t *testing.T) {
	r := newNetworkRig(t, engine.Docker, "bridge", "host")
	r.m = feed(t, r.m, LaunchOptionsCacheLoadedMsg{ImageName: "api:v1", Entry: &cache.LaunchOptionsEntry{Network: "host"}})
	if f := r.m.launchForm; f.networks[f.networkIdx] != "host" {
		t.Fatalf("the cache did not select host: %q", f.networks[f.networkIdx])
	}

	r.submit(t)

	if !hostModalShown(r.screen()) || len(r.runs()) != 0 {
		t.Errorf("a cached host network launched without asking (runs %v):\n%s", r.runs(), r.screen())
	}
}

// ── Nothing else asks ────────────────────────────────────────────────────────

func TestAnotherNetworkLaunchesAtOnce(t *testing.T) {
	names := []string{"bridge", "none", "overlay-prod", "devdesk", "hostnet", "my-host", "host1", "ghost", "podman"}
	forEachLaunchEngine(t, func(t *testing.T, engineName string) {
		for _, name := range names {
			t.Run(name, func(t *testing.T) {
				r := newNetworkRig(t, engineName, append([]string{"host"}, names...)...)
				r.selectNetwork(t, name)

				r.submit(t)

				runs := r.runs()
				if len(runs) != 1 || !containsPair(runs[0], "--network", name) {
					t.Fatalf("the engine saw %v, want the one run on --network %s, at once", runs, name)
				}
				if hostModalShown(r.screen()) || r.m.launchForm != nil {
					t.Error("a modal or the form stood in the way of a harmless launch")
				}
			})
		}
	})
}

// Characterization of what OC-21 leaves alone: joining another container's
// network namespace is not asked about (follow-up), and the argv carries the
// network as typed and no namespace option with it.
func TestJoiningAnotherContainersNetworkIsNotHandledHere(t *testing.T) {
	const net = "container:0123456789ab"
	r := newNetworkRig(t, engine.Docker, "bridge", net)
	r.selectNetwork(t, net)

	r.submit(t)

	runs := r.runs()
	if len(runs) != 1 || !containsPair(runs[0], "--network", net) {
		t.Fatalf("the engine saw %v, want the one run on --network %s, at once", runs, net)
	}
	for _, e := range runs[0] {
		for _, p := range []string{"--pid", "--ipc", "--uts", "--userns", "--privileged"} {
			if strings.HasPrefix(e, p) {
				t.Errorf("argv = %q holds %q: a network never brings a namespace option with it", runs[0], e)
			}
		}
	}
	if hostModalShown(r.screen()) {
		t.Error("a host-network modal opened for a container: network")
	}
}

// ── One modal for every reason ───────────────────────────────────────────────

func TestAHostNetworkWithASensitiveMountAsksOnce(t *testing.T) {
	const volumes = "/etc:/host-etc:ro"
	r := newNetworkRig(t, engine.Docker, "bridge", "host")
	r.selectNetwork(t, "host")
	want := wantSensitive(t, volumes)

	r.launch(t, volumes)

	screen := r.screen()
	if formShown(screen) {
		t.Fatalf("the form is still on screen, want the modal:\n%s", screen)
	}
	for _, w := range want {
		if !strings.Contains(screen, w.Path) || !strings.Contains(screen, w.Reason) {
			t.Errorf("the modal lacks the mount %q or its reason %q:\n%s", w.Path, w.Reason, screen)
		}
	}
	for _, text := range []string{hostNetworkReason, "Mount and use the host network", "Cancel"} {
		if !strings.Contains(screen, text) {
			t.Errorf("the modal lacks %q:\n%s", text, screen)
		}
	}
	if len(r.runs()) != 0 {
		t.Fatalf("launched before the answer: %v", r.runs())
	}

	r.press(t, "enter") // Cancel
	if len(r.runs()) != 0 || r.m.launchForm == nil || !formShown(r.screen()) {
		t.Fatalf("cancelling did not give the form back (runs %v)", r.runs())
	}

	r.press(t, "enter") // ask again, then one confirmation
	r.confirm(t)
	runs := r.runs()
	if len(runs) != 1 {
		t.Fatalf("one confirmation started the engine %d times, want one run: %v", len(runs), runs)
	}
	if !containsPair(runs[0], "-v", volumes) || !containsPair(runs[0], "--network", "host") {
		t.Errorf("argv = %q, want -v %s and --network host", runs[0], volumes)
	}
	if hostModalShown(r.screen()) || r.m.launchForm != nil {
		t.Error("a second modal or the form is on screen after the one confirmation")
	}
}

// With a socket among the mounts the named choice keeps its name, the Error
// footer stays the socket's, and the host network is in the same modal.
func TestAHostNetworkWithTheEngineSocketKeepsTheNamedChoiceAndTheErrorFooter(t *testing.T) {
	const volumes = "/var/run/docker.sock:/var/run/docker.sock"
	r := newNetworkRig(t, engine.Docker, "bridge", "host")
	r.selectNetwork(t, "host")

	r.launch(t, volumes)

	screen := r.screen()
	for _, text := range []string{hostNetworkReason, "Mount the engine socket and use the host network", "Cancel"} {
		if !strings.Contains(screen, text) {
			t.Errorf("the modal lacks %q:\n%s", text, screen)
		}
	}
	if st := r.m.formStatus(); st.Level != sharedcomponents.LevelError {
		t.Errorf("footer level = %v (%q), want the socket's LevelError", st.Level, st.Text)
	}
	r.press(t, "y", "enter") // enter is on Cancel: the modal closes and the form is back
	if len(r.runs()) != 0 {
		t.Fatalf("y or enter launched: %v", r.runs())
	}
	r.press(t, "enter") // ask again
	r.press(t, "left", "enter")
	if runs := r.runs(); len(runs) != 1 || !containsPair(runs[0], "--network", "host") {
		t.Errorf("the named choice did not launch with --network host: %v", runs)
	}
}

// The mount modal of OC-20 does not change when the network is not the host's.
func TestASensitiveMountOnAnotherNetworkKeepsTheMountAnywayModal(t *testing.T) {
	r := newNetworkRig(t, engine.Docker, "bridge", "host")
	r.selectNetwork(t, "bridge")

	r.launch(t, "/etc:/host-etc:ro")

	screen := r.screen()
	if !strings.Contains(screen, mountAnywayLabel) || hostModalShown(screen) || strings.Contains(screen, "host network") {
		t.Errorf("the OC-20 modal changed:\n%s", screen)
	}
}

// ── The footer and the preview ───────────────────────────────────────────────

func hostForm() *LaunchForm {
	f := NewLaunchForm("api:v1", []string{"80/tcp"}, networksNamed([]string{"bridge", "host"}), 120)
	f.networkIdx = 1
	return f
}

func TestTheFooterWarnsWhileTheNetworkIsTheHost(t *testing.T) {
	f := hostForm()

	st := footerStatus(f)
	if st.Text != hostNetworkFooter || st.Level != sharedcomponents.LevelWarning {
		t.Errorf("formStatus() = {%q, level %v}, want {%q, LevelWarning}", st.Text, st.Level, hostNetworkFooter)
	}

	f.networkIdx = 0
	if st := footerStatus(f); st.Text != "" {
		t.Errorf("formStatus().Text = %q on bridge, want empty", st.Text)
	}
}

func TestAnErrorOfTheFormWinsOverTheHostNetworkWarning(t *testing.T) {
	f := hostForm()
	f.ports[0].hostPortInput.SetValue("80a0")
	f, _ = submitLaunch(f)

	if st := footerStatus(f); st.Level != sharedcomponents.LevelError {
		t.Errorf("formStatus() = {%q, level %v}, want the Error of the invalid port", st.Text, st.Level)
	}
}

func TestTheFooterWarnsWhileTheHostNetworkModalIsOpen(t *testing.T) {
	r := newNetworkRig(t, engine.Docker, "bridge", "host")
	r.selectNetwork(t, "host")
	r.submit(t)

	st := r.m.formStatus()
	if st.Text != hostNetworkFooter || st.Level != sharedcomponents.LevelWarning {
		t.Errorf("formStatus() = {%q, level %v}, want {%q, LevelWarning}", st.Text, st.Level, hostNetworkFooter)
	}
	if line := r.footerLine(); !strings.Contains(line, hostNetworkFooter) {
		t.Errorf("the rendered footer line is %q, want %q", line, hostNetworkFooter)
	}
}

// sgrForeground is the truecolor foreground sequence Lip Gloss v2 writes for c.
func sgrForeground(c color.Color) string {
	r, g, b, _ := c.RGBA()
	return fmt.Sprintf("38;2;%d;%d;%d", r>>8, g>>8, b>>8)
}

func TestThePreviewShowsTheHostNetworkLineAtLevelWarn(t *testing.T) {
	warn := sgrForeground(theme.ColorFooterWarn)
	if dim := theme.DimStyle.Render("x"); strings.Contains(dim, warn) {
		t.Skip("the dim preview colour and the warning colour are the same: nothing to tell them apart")
	}

	f := hostForm()
	var warned []string
	for _, line := range f.commandPreviewLines() {
		if strings.Contains(line, warn) {
			warned = append(warned, strings.TrimSpace(ansi.Strip(line)))
		}
	}
	if len(warned) != 1 || !strings.HasPrefix(warned[0], "--network host") {
		t.Errorf("warning-coloured preview lines = %q, want exactly the --network host line", warned)
	}

	f.networkIdx = 0
	for _, line := range f.commandPreviewLines() {
		if strings.Contains(line, warn) {
			t.Errorf("a preview line is in the warning colour on bridge: %q", ansi.Strip(line))
		}
	}
}

// ctrl+y copies a plain command: the colour is the screen's, not the clipboard's,
// and the form of the flag is the one the launcher builds.
func TestTheCopiedCommandNamesTheHostNetworkInPlainText(t *testing.T) {
	cmd := hostForm().buildCommandString()

	if !strings.Contains(cmd, "--network host") {
		t.Errorf("the copied command lacks --network host:\n%s", cmd)
	}
	if strings.Contains(cmd, "\x1b") {
		t.Errorf("the copied command holds a terminal escape sequence:\n%q", cmd)
	}
	for _, p := range []string{"--pid", "--ipc", "--uts", "--privileged", "--userns"} {
		if strings.Contains(cmd, p) {
			t.Errorf("the copied command holds %s:\n%s", p, cmd)
		}
	}
}

// The submit message still carries the network as listed: the launcher gets it
// the way it always did.
func TestTheHostNetworkReachesTheOptionsAsListed(t *testing.T) {
	opts, ok := submitOpts(hostForm())

	if !ok || opts.Network != "host" {
		t.Errorf("submit = %#v, %v, want Network host", opts, ok)
	}
	if !slices.Equal(slices.DeleteFunc(slices.Clone(opts.Ports), func(p string) bool { return p == "" }), []string{"80:80"}) {
		t.Errorf("Opts.Ports = %q, want only the ticked EXPOSE port", opts.Ports)
	}
}
