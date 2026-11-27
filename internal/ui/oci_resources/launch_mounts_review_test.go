package ociresources

import (
	"slices"
	"strings"
	"testing"

	tea "charm.land/bubbletea/v2"
	"github.com/charmbracelet/x/ansi"

	"github.com/anthnel/devdesk/internal/docker"
	"github.com/anthnel/devdesk/internal/engine"
	sharedcomponents "github.com/anthnel/devdesk/internal/ui/components"
)

// OC-20 review, the form half (see internal/docker/launch_mounts_review_test.go).

// Review 1: the Volumes field splits a --mount value whatever the case of its keys.
func TestTheVolumesFieldReadsMountValuesInAnyCase(t *testing.T) {
	tests := []struct {
		name    string
		field   string
		volumes []string
		mounts  []string
	}{
		{"type in capitals", "TYPE=bind,source=/a,target=/b", nil, []string{"TYPE=bind,source=/a,target=/b"}},
		{"type value in capitals", "type=BIND,SOURCE=/a,TARGET=/b,READONLY", nil, []string{"type=BIND,SOURCE=/a,TARGET=/b,READONLY"}},
		{"a capitalized flag, then a volume", "Type=bind,Src=/a,dst=/b,RO,myvol:/x", []string{"myvol:/x"}, []string{"Type=bind,Src=/a,dst=/b,RO"}},
		{"two values, mixed", "TYPE=tmpfs,target=/t, type=BIND,source=/a,target=/b", nil, []string{"TYPE=tmpfs,target=/t", "type=BIND,source=/a,target=/b"}},
	}
	for _, tt := range tests {
		t.Run(tt.name, func(t *testing.T) {
			volumes, mounts := splitVolumeField(tt.field)
			if !slices.Equal(volumes, tt.volumes) || !slices.Equal(mounts, tt.mounts) {
				t.Errorf("splitVolumeField(%q) = %q, %q; want %q, %q", tt.field, volumes, mounts, tt.volumes, tt.mounts)
			}
		})
	}
}

// Review 5: no sensitive launch starts without an answer, whatever the state of
// the form. A submit that arrives after esc closed the form (the message was in
// flight) is held like any other, and Cancel copes with a form that is gone.
func TestASubmitThatOutlivesTheFormIsStillHeld(t *testing.T) {
	r := newMountRig(t, engine.Docker)
	opts := docker.ContainerLaunchOptions{Image: "api:v1", Detach: true, Volumes: []string{"/etc:/host-etc:ro"}}

	r.m, _ = step(t, r.m, LaunchFormCancelMsg{})
	if r.m.launchForm != nil {
		t.Fatal("esc did not close the form")
	}
	r.m, _ = step(t, r.m, LaunchFormSubmitMsg{Opts: opts})

	if r.m.mountModal == nil {
		t.Fatal("a sensitive launch with no form on screen was not held")
	}
	if runs := r.runs(); len(runs) != 0 {
		t.Fatalf("the engine was started before any answer: %v", runs)
	}
	if !strings.Contains(r.screen(), "Mount anyway") {
		t.Errorf("the modal is not on screen:\n%s", r.screen())
	}

	r.press(t, "esc")

	if r.m.mountModal != nil || r.m.mountPending != nil {
		t.Error("Cancel left the modal or the held launch behind")
	}
	if runs := r.runs(); len(runs) != 0 {
		t.Errorf("Cancel started the engine: %v", runs)
	}
}

// The same held launch, answered: an explicit confirmation is the only way out.
func TestAHeldSubmitWithNoFormLaunchesOnlyWhenConfirmed(t *testing.T) {
	r := newMountRig(t, engine.Docker)
	r.m, _ = step(t, r.m, LaunchFormCancelMsg{})
	r.m, _ = step(t, r.m, LaunchFormSubmitMsg{Opts: docker.ContainerLaunchOptions{
		Image: "api:v1", Detach: true, Volumes: []string{"/etc:/host-etc:ro"},
	}})
	r.press(t, "enter") // the default
	if len(r.runs()) != 0 {
		t.Fatalf("enter on the default launched: %v", r.runs())
	}

	r.m, _ = step(t, r.m, LaunchFormSubmitMsg{Opts: docker.ContainerLaunchOptions{
		Image: "api:v1", Detach: true, Volumes: []string{"/etc:/host-etc:ro"},
	}})
	r.confirm(t)

	if runs := r.runs(); len(runs) != 1 || !containsPair(runs[0], "-v", "/etc:/host-etc:ro") {
		t.Errorf("the engine saw %v, want the one confirmed run", runs)
	}
}

// Review 8: an ancestor of the engine socket gets the named modal.
func TestAnAncestorOfTheEngineSocketGetsTheNamedModal(t *testing.T) {
	r := newMountRig(t, engine.Docker)

	r.launch(t, "/run:/host-run")

	screen := r.screen()
	if !strings.Contains(screen, "Mount the engine socket") {
		t.Errorf("the modal lacks the named choice:\n%s", screen)
	}
	if st := r.m.formStatus(); st.Level != sharedcomponents.LevelError {
		t.Errorf("footer level = %v, want LevelError", st.Level)
	}
	if len(r.runs()) != 0 {
		t.Errorf("launched before the answer: %v", r.runs())
	}
}

// Review 11: on a narrow terminal the modal wraps instead of overflowing; the
// whole path stays readable (wrapped, never cut) and so do both buttons.
func TestTheModalWrapsToANarrowTerminal(t *testing.T) {
	r := newMountRig(t, engine.Docker)
	r.m, _ = step(t, r.m, tea.WindowSizeMsg{Width: 40, Height: 30})
	r.launch(t, "{home}/.ssh:/root/.ssh:ro")
	want := wantSensitive(t, r.host.expand("{home}/.ssh:/root/.ssh:ro"))[0]

	r.assertFitsAndReadable(t, 40, want)

	// A resize while the modal is open wraps it again.
	r.m, _ = step(t, r.m, tea.WindowSizeMsg{Width: 180, Height: 30})
	if !strings.Contains(r.screen(), want.Path) {
		t.Errorf("back at 180 columns the path is not on one line:\n%s", r.screen())
	}
	r.m, _ = step(t, r.m, tea.WindowSizeMsg{Width: 36, Height: 30})
	r.assertFitsAndReadable(t, 36, want)
}

func (r *mountRig) assertFitsAndReadable(t *testing.T, width int, want docker.SensitiveMount) {
	t.Helper()
	screen := r.screen()
	var body strings.Builder
	for line := range strings.SplitSeq(screen, "\n") {
		if w := ansi.StringWidth(line); w > width {
			t.Errorf("a line is %d columns wide on a %d column terminal: %q", w, width, line)
		}
		body.WriteString(strings.Trim(strings.TrimSpace(line), "│┃ "))
	}
	if !strings.Contains(body.String(), want.Path) {
		t.Errorf("the path %q is cut or missing at %d columns:\n%s", want.Path, width, screen)
	}
	for _, label := range []string{"Mount anyway", "Cancel"} {
		if !strings.Contains(screen, label) {
			t.Errorf("the button %q is not visible at %d columns:\n%s", label, width, screen)
		}
	}
	if !strings.Contains(strings.Join(strings.Fields(body.String()), ""), strings.Join(strings.Fields(want.Reason), "")) {
		t.Errorf("the reason is cut at %d columns:\n%s", width, screen)
	}
}

// OC-20 re-review, B4: a typed path is drawn without what could pass for the
// layout of the modal.
func TestPrintableReplacesEveryLineSeparator(t *testing.T) {
	got := printable("/a\u2028b\u2029c\x1b[31md\ne\u200ff")
	for _, r := range got {
		if r == '\u2028' || r == '\u2029' || r == '\x1b' || r == '\n' || r == '\u200f' {
			t.Errorf("printable left %U in %q", r, got)
		}
	}
	if got != "/a?b?c?[31md?e?f" {
		t.Errorf("printable = %q, want every separator, control and format character replaced by ?", got)
	}
}

// A real space of a path at the start of a wrapped line must not be read as the
// indentation of the modal: the continuation lines carry nothing but the path.
func TestAWrappedPathCarriesNothingButItself(t *testing.T) {
	const p = "/srv/with some  spaces/in it/.ssh"
	for _, limit := range []int{8, 10, 16} {
		out := wrapPath(p, limit)
		if got := strings.Join(strings.Split(out, "\n"), ""); got != p {
			t.Errorf("limit %d: the wrapped path joined is %q, want %q", limit, got, p)
		}
		for _, line := range strings.Split(out, "\n") {
			if ansi.StringWidth(line) > limit {
				t.Errorf("limit %d: line %q is wider", limit, line)
			}
		}
	}
	if got := wrapPath(p, 0); got != p {
		t.Errorf("no limit: wrapPath = %q, want the path untouched", got)
	}
}
