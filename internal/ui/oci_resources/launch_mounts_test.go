package ociresources

import (
	"os"
	"path/filepath"
	"runtime"
	"slices"
	"strings"
	"testing"

	tea "charm.land/bubbletea/v2"
	"github.com/charmbracelet/x/ansi"

	"github.com/anthnel/devdesk/internal/docker"
	"github.com/anthnel/devdesk/internal/engine"
	"github.com/anthnel/devdesk/internal/testutil/fakecli"
	sharedcomponents "github.com/anthnel/devdesk/internal/ui/components"
	"github.com/anthnel/devdesk/internal/ui/testutil"
)

// OC-20 (DEVDESK-818): a launch that mounts something sensitive of the host asks
// first. The classification is docker.SensitiveMounts (internal/docker, see
// launch_mounts_test.go for its API); this file is the form, the modal and the
// launch that follows.
//
// Expected production behavior, pinned below:
//
//   - The check runs when the launch is submitted (enter on the Launch button),
//     on the volumes of the form (-v) and on its --mount entries, before anything
//     is started or handed to the terminal, interactive launches included.
//   - If docker.SensitiveMounts(opts) is not empty, a modal opens and the form
//     stays: Model.launchForm is not nil while it is open (the form, its fields
//     and its focus come back as they were when the modal closes), the modal has
//     the keyboard, and Render() draws the modal and not the form. RenderFooter
//     keeps its form-mode shape, and Model.formStatus() is where the footer line
//     is derived (Rule 128: a state, no timer, it does not vanish after 3 s).
//   - The modal names every sensitive mount: its resolved Path and its Reason, as
//     docker.SensitiveMounts returns them. It has one action and Cancel, Cancel
//     is the default: enter, esc and a closed modal all cancel. Plain sensitive
//     mounts: the action is "Mount anyway". An engine socket in the request (even
//     among others): the action is "Mount the engine socket", the text says the
//     container gets control of the engine, the footer says it too at level Error,
//     and no letter answers for the user (y does nothing). The way to the action
//     is left or right, then enter, on both kinds of modal.
//   - Cancel: nothing is started, the form is back with its values and the focus
//     on Launch, and a second enter asks again. Confirm: the launch goes on as
//     it would have, with the argv the form built.
//   - No sensitive mount: no modal, the launch goes on at once, as before.
//   - The Volumes field takes --mount values too (see the form-level tests): the
//     entries of the field are split on commas, except that a --mount value (it
//     starts with `type=`) runs on over its own `key=value` tokens and the bare
//     `readonly` and `ro`, until a token starting with `type=` (the next value)
//     or the first other token (a -v entry). They land in
//     ContainerLaunchOptions.Mounts, the others in Volumes. A --mount value that
//     does not start with `type=` is not recognised by the form (docker's own
//     documented form starts there, and a bind needs `type=bind` anyway).
//
// Linux paths only, like the classification itself.

// mountHost is the private HOME and working directory of a test.
type mountHost struct{ home, cwd string }

func isolateMountHost(t *testing.T) mountHost {
	t.Helper()
	if runtime.GOOS != "linux" {
		t.Skip("these cases name Linux host paths (/etc, /var/run/docker.sock)")
	}
	resolve := func(p string) string {
		r, err := filepath.EvalSymlinks(p)
		if err != nil {
			t.Fatal(err)
		}
		return r
	}
	h := mountHost{home: resolve(t.TempDir()), cwd: resolve(t.TempDir())}
	for _, d := range []string{".ssh", ".devdesk", "projects"} {
		if err := os.MkdirAll(filepath.Join(h.home, d), 0o700); err != nil {
			t.Fatal(err)
		}
	}
	t.Setenv("HOME", h.home)
	t.Setenv("USERPROFILE", h.home)
	t.Setenv("XDG_RUNTIME_DIR", resolve(t.TempDir()))
	t.Setenv("DOCKER_HOST", "")
	t.Setenv("CONTAINER_HOST", "")
	t.Chdir(h.cwd)
	return h
}

func (h mountHost) expand(s string) string {
	return strings.NewReplacer("{home}", h.home, "{cwd}", h.cwd).Replace(s)
}

// mountRig is a launch form open on an image, a fake engine that answers every
// `run`, and a private HOME. The footer timer is shortened before anything builds
// a command (Rule 128).
type mountRig struct {
	m    Model
	fake *fakecli.Fake
	host mountHost
}

func newMountRig(t *testing.T, engineName string) *mountRig {
	t.Helper()
	testutil.FastTimers(t, &sharedcomponents.FooterMsgDuration)
	host := isolateMountHost(t)
	useEngineNamed(t, engineName)
	fake := installFakeTools(t, fakeScript{engineName + " run": {Stdout: "9f2c1d4e5b6a\n"}}, engineName)
	return &mountRig{m: launchingModel(t), fake: fake, host: host}
}

// settle feeds back every message a command produces, and those of the commands
// they produce in turn.
func settle(t *testing.T, m Model, cmd tea.Cmd) Model {
	t.Helper()
	for range 6 {
		if cmd == nil {
			return m
		}
		var next []tea.Cmd
		for _, msg := range testutil.Msgs(cmd) {
			var c tea.Cmd
			m, c = step(t, m, msg)
			next = append(next, c)
		}
		cmd = tea.Batch(next...)
	}
	return m
}

// press sends the keys one at a time and settles after each.
func (r *mountRig) press(t *testing.T, keys ...string) {
	t.Helper()
	for _, k := range keys {
		var cmd tea.Cmd
		r.m, cmd = step(t, r.m, testutil.Key(k))
		r.m = settle(t, r.m, cmd)
	}
}

// launch types the Volumes field, goes to the Launch button and presses enter.
func (r *mountRig) launch(t *testing.T, volumes string) {
	t.Helper()
	r.m.launchForm.volInput.SetValue(r.host.expand(volumes))
	focusLaunchField(r.m.launchForm, r.m.launchForm.fieldSubmit())
	r.press(t, "enter")
}

// confirm is the gesture that answers the action of either modal: a move off
// Cancel, then enter.
func (r *mountRig) confirm(t *testing.T) { t.Helper(); r.press(t, "left", "enter") }

func (r *mountRig) screen() string { return ansi.Strip(r.m.Render()) }

func (r *mountRig) footerLine() string {
	lines := strings.Split(ansi.Strip(r.m.RenderFooter(160)), "\n")
	return strings.TrimSpace(lines[len(lines)-1])
}

// formShown reports whether the launch form is what Render() draws.
func formShown(screen string) bool {
	return strings.Contains(screen, "Environment") && strings.Contains(screen, "Additional ports")
}

// runs returns the argv of every engine start, without the tool name.
func (r *mountRig) runs() [][]string {
	var out [][]string
	for _, c := range r.fake.Invocations() {
		out = append(out, c.Args)
	}
	return out
}

// wantSensitive classifies what the form would hand over, through the real
// function, so the modal is checked against the words of the classification.
func wantSensitive(t *testing.T, volumes string) []docker.SensitiveMount {
	t.Helper()
	var opts docker.ContainerLaunchOptions
	for _, e := range splitLaunchEntries(volumes) {
		if strings.HasPrefix(e, "type=") {
			opts.Mounts = append(opts.Mounts, e)
		} else {
			opts.Volumes = append(opts.Volumes, e)
		}
	}
	got := docker.SensitiveMounts(opts)
	if len(got) == 0 {
		t.Fatalf("the test case %q is not sensitive: fix the case", volumes)
	}
	return got
}

// splitLaunchEntries is the test's own, simple split for cases that hold one
// entry per comma-free value.
func splitLaunchEntries(s string) []string {
	if strings.HasPrefix(s, "type=") {
		return []string{s}
	}
	var out []string
	for e := range strings.SplitSeq(s, ",") {
		if e = strings.TrimSpace(e); e != "" {
			out = append(out, e)
		}
	}
	return out
}

func forEachLaunchEngine(t *testing.T, fn func(t *testing.T, name string)) {
	t.Helper()
	for _, name := range engine.Names() {
		t.Run(name, func(t *testing.T) { fn(t, name) })
	}
}

// ── Criterion 1: a sensitive path asks first ─────────────────────────────────

var sensitivePathCases = []struct{ name, volumes string }{
	{"the root", "/:/host:ro"},
	{"/etc", "/etc:/host-etc:ro"},
	{"under /etc", "/etc/ssl/certs:/certs"},
	{"HOME", "{home}:/h"},
	{"~/.ssh", "{home}/.ssh:/root/.ssh:ro"},
	{"~/.ssh typed with a tilde", "~/.ssh:/root/.ssh"},
	{"~/.devdesk", "{home}/.devdesk:/d"},
	{"a dot-dot that ends in /etc", "/tmp/../etc:/x"},
	{"a --mount of /etc", "type=bind,source=/etc,target=/x,readonly"},
	{"a --mount with src and dst", "type=bind,src={home}/.ssh,dst=/root/.ssh"},
}

func TestASensitiveMountOpensAModalThatNamesThePathAndTheReason(t *testing.T) {
	for _, tt := range sensitivePathCases {
		t.Run(tt.name, func(t *testing.T) {
			r := newMountRig(t, engine.Docker)
			want := wantSensitive(t, r.host.expand(tt.volumes))

			r.launch(t, tt.volumes)

			screen := r.screen()
			if formShown(screen) {
				t.Errorf("the form is still on screen, want the modal:\n%s", screen)
			}
			for _, w := range want {
				if !strings.Contains(screen, w.Path) {
					t.Errorf("the modal does not name %q:\n%s", w.Path, screen)
				}
				if !strings.Contains(screen, w.Reason) {
					t.Errorf("the modal does not give the reason %q:\n%s", w.Reason, screen)
				}
			}
			for _, label := range []string{"Mount anyway", "Cancel"} {
				if !strings.Contains(screen, label) {
					t.Errorf("the modal has no %q button:\n%s", label, screen)
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

// Every sensitive mount of the request is named, in one modal.
func TestOneModalNamesEverySensitiveMountOfTheRequest(t *testing.T) {
	r := newMountRig(t, engine.Docker)
	volumes := "/etc:/a, data:/b, {home}/.ssh:/c"
	want := wantSensitive(t, r.host.expand(volumes))
	if len(want) != 2 {
		t.Fatalf("the case holds %d sensitive mounts, want 2", len(want))
	}

	r.launch(t, volumes)

	screen := r.screen()
	for _, w := range want {
		if !strings.Contains(screen, w.Path) || !strings.Contains(screen, w.Reason) {
			t.Errorf("the modal lacks %q or its reason %q:\n%s", w.Path, w.Reason, screen)
		}
	}
	if strings.Contains(screen, "data:/b") {
		t.Errorf("the modal names a mount that is not sensitive:\n%s", screen)
	}
}

// Criterion 1: the default is Cancel. enter does nothing but go back to the form.
func TestTheDefaultOfTheModalIsCancel(t *testing.T) {
	r := newMountRig(t, engine.Docker)
	r.launch(t, "/etc:/host-etc:ro")

	r.press(t, "enter")

	if runs := r.runs(); len(runs) != 0 {
		t.Fatalf("enter on the default launched: %v", runs)
	}
	assertBackOnTheForm(t, r, "/etc:/host-etc:ro")
	// The form is usable again: the same enter asks again.
	r.press(t, "enter")
	if screen := r.screen(); formShown(screen) || !strings.Contains(screen, "/etc") {
		t.Errorf("a second launch did not ask again:\n%s", screen)
	}
}

func TestEscapeOnTheModalGoesBackToTheForm(t *testing.T) {
	r := newMountRig(t, engine.Docker)
	r.launch(t, "/etc:/host-etc:ro")

	r.press(t, "esc")

	if runs := r.runs(); len(runs) != 0 {
		t.Fatalf("esc launched: %v", runs)
	}
	assertBackOnTheForm(t, r, "/etc:/host-etc:ro")
}

// Moving to the action and back to Cancel cancels as well: the traversal is
// bounded, one press too many does not loop onto the action.
func TestAnExtraPressOnCancelDoesNotReachTheAction(t *testing.T) {
	r := newMountRig(t, engine.Docker)
	r.launch(t, "/etc:/host-etc:ro")

	r.press(t, "right", "right", "enter")

	if runs := r.runs(); len(runs) != 0 {
		t.Fatalf("two presses to the right launched: %v", runs)
	}
	assertBackOnTheForm(t, r, "/etc:/host-etc:ro")
}

func assertBackOnTheForm(t *testing.T, r *mountRig, volumes string) {
	t.Helper()
	if r.m.launchForm == nil {
		t.Fatal("the form was closed by the cancel")
	}
	if got := r.m.launchForm.volInput.Value(); got != r.host.expand(volumes) {
		t.Errorf("Volumes = %q, want what was typed, %q", got, r.host.expand(volumes))
	}
	if r.m.launchForm.focusedField != r.m.launchForm.fieldSubmit() {
		t.Errorf("the focus is on field %d, want the Launch button", r.m.launchForm.focusedField)
	}
	if screen := r.screen(); !formShown(screen) {
		t.Errorf("the form is not what is on screen:\n%s", screen)
	}
}

// Criterion 1: confirming launches, with the argv the form built, on both engines.
func TestConfirmingASensitiveMountLaunchesIt(t *testing.T) {
	tests := []struct {
		name    string
		volumes string
		flag    string // "-v" or "--mount"
		value   string
	}{
		{"-v", "/etc:/host-etc:ro", "-v", "/etc:/host-etc:ro"},
		{"--mount", "type=bind,source=/etc,target=/host-etc,readonly", "--mount", "type=bind,source=/etc,target=/host-etc,readonly"},
	}
	forEachLaunchEngine(t, func(t *testing.T, name string) {
		for _, tt := range tests {
			t.Run(tt.name, func(t *testing.T) {
				r := newMountRig(t, name)
				r.launch(t, tt.volumes)
				if len(r.runs()) != 0 {
					t.Fatalf("launched before the answer: %v", r.runs())
				}

				r.confirm(t)

				runs := r.runs()
				if len(runs) != 1 {
					t.Fatalf("the engine was started %d times, want one run: %v", len(runs), runs)
				}
				argv := runs[0]
				if argv[0] != "run" || argv[len(argv)-1] != "api:v1" {
					t.Errorf("argv = %q, want a run of api:v1", argv)
				}
				if !containsPair(argv, tt.flag, tt.value) {
					t.Errorf("argv = %q, want %s %s", argv, tt.flag, tt.value)
				}
				other := map[string]string{"-v": "--mount", "--mount": "-v"}[tt.flag]
				if slices.Contains(argv, other) {
					t.Errorf("argv = %q holds %s, which nothing asked for", argv, other)
				}
				if r.m.launchForm != nil {
					t.Error("the form is still open after the launch")
				}
				if screen := r.screen(); strings.Contains(screen, "Mount anyway") {
					t.Errorf("the modal is still on screen:\n%s", screen)
				}
			})
		}
	})
}

// ── Criterion 4: nothing to say, nothing asked ───────────────────────────────

func TestAMountWithNoLinkToASensitivePathLaunchesAtOnce(t *testing.T) {
	tests := []struct{ name, volumes string }{
		{"./data", "./data:/data"},
		{"a named volume", "myvolume:/app/data"},
		{"a path under HOME", "{home}/projects/app:/app"},
		{"~/projects", "~/projects:/p"},
		{"/etcetera", "/etcetera:/x"},
		{"a harmless --mount", "type=bind,source=./data,target=/data"},
		{"a named volume through --mount", "type=volume,source=pgdata,target=/var/lib/postgresql"},
		{"a tmpfs over /etc", "type=tmpfs,target=/etc"},
		{"the container side is /etc", "type=bind,source={home}/projects,target=/etc"},
		{"an anonymous volume", "/var/lib/data"},
		{"no volume at all", ""},
	}
	forEachLaunchEngine(t, func(t *testing.T, name string) {
		for _, tt := range tests {
			t.Run(tt.name, func(t *testing.T) {
				r := newMountRig(t, name)

				r.launch(t, tt.volumes)

				if runs := r.runs(); len(runs) != 1 || runs[0][0] != "run" {
					t.Fatalf("the engine saw %v, want the one run, at once", runs)
				}
				if r.m.launchForm != nil {
					t.Error("the form is still open: a modal stood in the way of a harmless launch")
				}
			})
		}
	})
}

// ── Criterion 2: the engine socket ───────────────────────────────────────────

var engineSocketCases = []struct {
	name       string
	volumes    string
	dockerHost string
}{
	{"docker.sock", "/var/run/docker.sock:/var/run/docker.sock", ""},
	{"docker.sock, read-only", "/var/run/docker.sock:/var/run/docker.sock:ro", ""},
	{"the podman socket", "/run/podman/podman.sock:/run/podman/podman.sock", ""},
	{"the rootless podman socket", "{xdg}/podman/podman.sock:/x", ""},
	{"the socket of DOCKER_HOST", "{host}/engine.sock:/x", "unix://{host}/engine.sock"},
	{"docker.sock through --mount", "type=bind,source=/var/run/docker.sock,target=/var/run/docker.sock", ""},
}

// socketRig also resolves the {xdg} and {host} placeholders of a socket case.
func newSocketRig(t *testing.T, engineName string, volumes, dockerHost string) (*mountRig, string) {
	t.Helper()
	r := newMountRig(t, engineName)
	xdg := os.Getenv("XDG_RUNTIME_DIR")
	hostDir := t.TempDir()
	fill := strings.NewReplacer("{xdg}", xdg, "{host}", hostDir)
	t.Setenv("DOCKER_HOST", fill.Replace(dockerHost))
	return r, fill.Replace(volumes)
}

func TestAnEngineSocketGetsAModalAndAFooterAtLevelError(t *testing.T) {
	for _, tt := range engineSocketCases {
		t.Run(tt.name, func(t *testing.T) {
			r, volumes := newSocketRig(t, engine.Docker, tt.volumes, tt.dockerHost)
			want := wantSensitive(t, volumes)
			if len(want) != 1 || !want[0].EngineSocket {
				t.Fatalf("the case is not one engine socket: %+v", want)
			}

			r.launch(t, volumes)

			screen := r.screen()
			if formShown(screen) {
				t.Errorf("the form is still on screen, want the modal:\n%s", screen)
			}
			for _, text := range []string{want[0].Path, want[0].Reason, "Mount the engine socket", "Cancel"} {
				if !strings.Contains(screen, text) {
					t.Errorf("the modal lacks %q:\n%s", text, screen)
				}
			}
			lower := strings.ToLower(screen)
			if !strings.Contains(lower, "control") || !strings.Contains(lower, "engine") {
				t.Errorf("the modal does not say the container gets control of the engine:\n%s", screen)
			}
			for _, bare := range []string{"Yes", "No"} {
				if strings.Contains(screen, " "+bare+" ") {
					t.Errorf("the modal offers a plain %q:\n%s", bare, screen)
				}
			}

			status := r.m.formStatus()
			if status.Level != sharedcomponents.LevelError {
				t.Errorf("footer level = %v, want LevelError (text %q)", status.Level, status.Text)
			}
			text := strings.ToLower(status.Text)
			if !strings.Contains(text, "control") || !strings.Contains(text, "engine") {
				t.Errorf("footer text = %q, want it to say the container gets control of the engine", status.Text)
			}
			if line := r.footerLine(); !strings.Contains(line, strings.TrimSpace(status.Text)) {
				t.Errorf("the rendered footer line is %q, want the status text %q", line, status.Text)
			}
			if runs := r.runs(); len(runs) != 0 {
				t.Errorf("the engine was started before the answer: %v", runs)
			}
		})
	}
}

// The default is Cancel, and nothing a user types by reflex answers for them.
func TestNoKeyButTheNamedChoiceMountsTheEngineSocket(t *testing.T) {
	const socket = "/var/run/docker.sock:/var/run/docker.sock"
	t.Run("enter", func(t *testing.T) {
		r := newMountRig(t, engine.Docker)
		r.launch(t, socket)
		r.press(t, "enter")
		if len(r.runs()) != 0 {
			t.Fatalf("enter on the default mounted the socket: %v", r.runs())
		}
		assertBackOnTheForm(t, r, socket)
	})
	t.Run("esc", func(t *testing.T) {
		r := newMountRig(t, engine.Docker)
		r.launch(t, socket)
		r.press(t, "esc")
		if len(r.runs()) != 0 {
			t.Fatalf("esc mounted the socket: %v", r.runs())
		}
		assertBackOnTheForm(t, r, socket)
	})
	for _, key := range []string{"y", "Y", "space"} {
		t.Run(key, func(t *testing.T) {
			r := newMountRig(t, engine.Docker)
			r.launch(t, socket)

			r.press(t, key)

			if len(r.runs()) != 0 {
				t.Fatalf("%q mounted the socket: %v", key, r.runs())
			}
			if formShown(r.screen()) {
				t.Errorf("%q closed the modal: it is the named choice or Cancel, nothing else", key)
			}
			// ...and the modal is still the one that answers: a move and enter confirm.
			r.press(t, "left", "enter")
			if len(r.runs()) != 1 {
				t.Errorf("after %q, left and enter did not mount the socket: %v", key, r.runs())
			}
		})
	}
}

func TestChoosingMountTheEngineSocketLaunchesOnEveryEngine(t *testing.T) {
	tests := []struct{ name, volumes, flag, value string }{
		{"-v", "/var/run/docker.sock:/var/run/docker.sock", "-v", "/var/run/docker.sock:/var/run/docker.sock"},
		{"--mount", "type=bind,source=/run/podman/podman.sock,target=/run/podman/podman.sock",
			"--mount", "type=bind,source=/run/podman/podman.sock,target=/run/podman/podman.sock"},
	}
	forEachLaunchEngine(t, func(t *testing.T, name string) {
		for _, tt := range tests {
			t.Run(tt.name, func(t *testing.T) {
				r := newMountRig(t, name)
				r.launch(t, tt.volumes)

				r.confirm(t)

				runs := r.runs()
				if len(runs) != 1 || !containsPair(runs[0], tt.flag, tt.value) {
					t.Fatalf("the engine saw %v, want one run with %s %s", runs, tt.flag, tt.value)
				}
				if r.m.launchForm != nil {
					t.Error("the form is still open after the launch")
				}
				if st := r.m.formStatus(); st.Level == sharedcomponents.LevelError && st.Text != "" {
					t.Errorf("the footer still holds an Error after the launch: %q", st.Text)
				}
			})
		}
	})
}

// A socket among other sensitive mounts: one modal, every path, the named choice.
func TestASocketAmongOtherSensitiveMountsKeepsTheNamedChoice(t *testing.T) {
	r := newMountRig(t, engine.Docker)
	volumes := "/etc:/e, /var/run/docker.sock:/var/run/docker.sock"
	want := wantSensitive(t, volumes)

	r.launch(t, volumes)

	screen := r.screen()
	for _, w := range want {
		if !strings.Contains(screen, w.Path) {
			t.Errorf("the modal does not name %q:\n%s", w.Path, screen)
		}
	}
	if !strings.Contains(screen, "Mount the engine socket") {
		t.Errorf("a socket among the mounts did not get its named choice:\n%s", screen)
	}
	if st := r.m.formStatus(); st.Level != sharedcomponents.LevelError {
		t.Errorf("footer level = %v, want LevelError", st.Level)
	}
	r.press(t, "enter")
	if len(r.runs()) != 0 {
		t.Errorf("the default launched: %v", r.runs())
	}
}

// Rule 130 and the header: while the socket modal is open the keys it lists are
// the keys it answers to. y/n is not among them.
func TestTheHeaderOfTheEngineSocketModalDoesNotAdvertiseY(t *testing.T) {
	r := newMountRig(t, engine.Docker)
	r.launch(t, "/var/run/docker.sock:/var/run/docker.sock")

	sc := r.m.GetShortcuts()

	if testutil.HasShortcut(sc, "y/n") {
		t.Errorf("the header lists y/n: %v", testutil.ShortcutKeys(sc))
	}
	if !testutil.HasShortcut(sc, "esc") {
		t.Errorf("the header does not list esc: %v", testutil.ShortcutKeys(sc))
	}
}

// ── The launch that is not detached ──────────────────────────────────────────

// An interactive launch (-it) is handed to the terminal, which would run the
// container with the mount before anyone answered: it is held like any other.
func TestAnInteractiveLaunchIsHeldToo(t *testing.T) {
	r := newMountRig(t, engine.Docker)
	r.m.launchForm.optionInteractiveTTY, r.m.launchForm.optionDetach = true, false

	r.launch(t, "/etc:/host-etc:ro")

	if screen := r.screen(); formShown(screen) || !strings.Contains(screen, "Mount anyway") {
		t.Fatalf("an interactive launch was not held:\n%s", screen)
	}
	r.press(t, "enter")
	if runs := r.runs(); len(runs) != 0 {
		t.Errorf("cancelling still started the engine: %v", runs)
	}
	assertBackOnTheForm(t, r, "/etc:/host-etc:ro")
	if !r.m.launchForm.optionInteractiveTTY {
		t.Error("the options of the form did not survive the cancel")
	}

	r.press(t, "enter")
	r.confirm(t)
	if r.m.launchForm != nil {
		t.Error("confirming did not close the form for the hand-off")
	}
}

// ── The form: the Volumes field takes --mount values ─────────────────────────

func TestTheVolumesFieldSplitsVolumesFromMountValues(t *testing.T) {
	tests := []struct {
		name    string
		field   string
		volumes []string
		mounts  []string
	}{
		{"volumes only", "myvol:/data, other:/o", []string{"myvol:/data", "other:/o"}, nil},
		{"one mount", "type=bind,source=./data,target=/d", nil, []string{"type=bind,source=./data,target=/d"}},
		{"a mount among volumes", "myvol:/data, type=bind,source=./data,target=/d, other:/o",
			[]string{"myvol:/data", "other:/o"}, []string{"type=bind,source=./data,target=/d"}},
		{"two mounts", "type=bind,source=/a,target=/b,readonly, type=tmpfs,target=/t",
			nil, []string{"type=bind,source=/a,target=/b,readonly", "type=tmpfs,target=/t"}},
		{"a bare flag inside a mount, then a volume", "type=bind,source=/a,target=/b,ro,myvol:/x",
			[]string{"myvol:/x"}, []string{"type=bind,source=/a,target=/b,ro"}},
		{"blank entries", " , myvol:/data ,, ", []string{"myvol:/data"}, nil},
		{"a volume with options", "data:/d:ro", []string{"data:/d:ro"}, nil},
	}
	for _, tt := range tests {
		t.Run(tt.name, func(t *testing.T) {
			f := NewLaunchForm("api:v1", nil, nil, 120)
			f.volInput.SetValue(tt.field)

			opts, ok := submitOpts(f)

			if !ok {
				t.Fatalf("the form refused %q", tt.field)
			}
			if !slices.Equal(opts.Volumes, tt.volumes) {
				t.Errorf("Opts.Volumes = %q, want %q", opts.Volumes, tt.volumes)
			}
			if !slices.Equal(opts.Mounts, tt.mounts) {
				t.Errorf("Opts.Mounts = %q, want %q", opts.Mounts, tt.mounts)
			}
		})
	}
}

// The preview and ctrl+y show what will run, the --mount form included.
func TestThePreviewShowsAMountValueAsAMountFlag(t *testing.T) {
	f := NewLaunchForm("api:v1", nil, nil, 120)
	f.volInput.SetValue("myvol:/data, type=bind,source=/etc,target=/x,readonly")

	cmd := f.buildCommandString()

	if !strings.Contains(cmd, "--mount type=bind,source=/etc,target=/x,readonly") {
		t.Errorf("the preview lacks the --mount flag:\n%s", cmd)
	}
	if !strings.Contains(cmd, "-v myvol:/data") {
		t.Errorf("the preview lost the volume:\n%s", cmd)
	}
	if strings.Contains(cmd, "-v type=") || strings.Contains(cmd, "-v source=") {
		t.Errorf("the preview shows a mount value as a volume:\n%s", cmd)
	}
}

// The submit message carries what the modal must classify: the form does not
// check anything itself, and typing a sensitive path is not refused or flagged
// before the launch (Rule 140 is for invalid fields; this one is valid).
func TestTypingASensitivePathIsNotAnErrorOfTheForm(t *testing.T) {
	f := NewLaunchForm("api:v1", nil, nil, 120)
	f.volInput.SetValue("/etc:/host-etc:ro")

	opts, ok := submitOpts(f)

	if !ok {
		t.Fatalf("the form refused a valid volume: %q", footerStatus(f).Text)
	}
	if !slices.Equal(opts.Volumes, []string{"/etc:/host-etc:ro"}) {
		t.Errorf("Opts.Volumes = %q, want the entry as typed", opts.Volumes)
	}
	if st := footerStatus(f); st.Level == sharedcomponents.LevelError {
		t.Errorf("the form shows an Error before the launch for a plain sensitive path: %q", st.Text)
	}
}
