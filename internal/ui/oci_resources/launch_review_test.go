package ociresources

import (
	"strings"
	"testing"

	tea "charm.land/bubbletea/v2"
	"github.com/charmbracelet/x/ansi"

	"github.com/anthnel/devdesk/internal/engine"
	"github.com/anthnel/devdesk/internal/ui/testutil"
	"github.com/anthnel/devdesk/internal/ui/theme"
)

// Review of OC-21: what ctrl+y copies is pasted into a shell, so every typed
// value that holds a character a shell reads is quoted, the -p entries included,
// and a plain value is left as it is.

func TestShellQuoteQuotesWhatIsOutsideTheSafeList(t *testing.T) {
	tests := []struct{ in, want string }{
		{"web-1", "web-1"},
		{"./data:/d", "./data:/d"},
		{"127.0.0.1:8080:80/tcp", "127.0.0.1:8080:80/tcp"},
		{"type=bind,source=./d,target=/d", "type=bind,source=./d,target=/d"},
		{"user@host%1+x", "user@host%1+x"},
		{"8080:80;cmd", "'8080:80;cmd'"},
		{"8080:80$(cmd)", "'8080:80$(cmd)'"},
		{"#comment", "'#comment'"},
		{"~/d:/d", "'~/d:/d'"},
		{"a*b", "'a*b'"},
		{"a?b", "'a?b'"},
		{"[::1]:80:80", "'[::1]:80:80'"},
		{"a b", "'a b'"},
		{"a&b", "'a&b'"},
		{"a|b", "'a|b'"},
		{"a<b>c", "'a<b>c'"},
		{"(x)", "'(x)'"},
		{"it's", `'it'\''s'`},
		{"é", "'é'"},
		{"", "''"},
	}
	for _, tt := range tests {
		if got := shellQuote(tt.in); got != tt.want {
			t.Errorf("shellQuote(%q) = %q, want %q", tt.in, got, tt.want)
		}
	}
}

func TestTheCopiedCommandQuotesAPublishedPort(t *testing.T) {
	tests := []struct{ name, ports, want string }{
		{"a command separator", "8080:80;cmd", "-p '127.0.0.1:8080:80;cmd'"},
		{"a command substitution", "8080:80$(cmd)", "-p '127.0.0.1:8080:80$(cmd)'"},
		{"a plain port", "8080:80", "-p 127.0.0.1:8080:80 "},
	}
	for _, tt := range tests {
		t.Run(tt.name, func(t *testing.T) {
			f := NewLaunchForm("api:v1", nil, nil, 120)
			f.extraPortsInput.SetValue(tt.ports)

			cmd := f.buildCommandString() + " "

			if !strings.Contains(cmd, tt.want) {
				t.Errorf("the copied command lacks %q:\n%s", tt.want, cmd)
			}
			if strings.Contains(cmd, "\x1b") {
				t.Errorf("the copied command holds a terminal escape sequence:\n%q", cmd)
			}
		})
	}
}

func TestTheCopiedCommandQuotesAValueThatStartsWithAHash(t *testing.T) {
	f := NewLaunchForm("api:v1", nil, nil, 120)
	f.nameInput.SetValue("#web")
	f.volInput.SetValue("#d:/d")

	cmd := f.buildCommandString()

	for _, want := range []string{"--name '#web'", "-v '#d:/d'"} {
		if !strings.Contains(cmd, want) {
			t.Errorf("the copied command lacks %q:\n%s", want, cmd)
		}
	}
	if strings.Contains(cmd, "\x1b") {
		t.Errorf("the copied command holds a terminal escape sequence:\n%q", cmd)
	}
}

// Review of OC-21: a double submit. The form builds one LaunchFormSubmitMsg per
// enter, and both can be in flight before the first one is delivered.

func TestASecondSubmitWhileTheModalIsOpenIsIgnored(t *testing.T) {
	r := newNetworkRig(t, engine.Docker, "bridge", "host")
	f := r.m.launchForm
	f.networkIdx = 1 // host: the first submit is held
	focusLaunchField(f, f.fieldSubmit())
	_, first := f.Update(testutil.Key("enter"))
	f.networkIdx = 0 // bridge: the second one would launch at once
	_, second := f.Update(testutil.Key("enter"))

	r.m = settle(t, r.m, first)
	if !hostModalShown(r.screen()) {
		t.Fatalf("the first submit did not open the modal:\n%s", r.screen())
	}
	r.m = settle(t, r.m, second)

	if runs := r.runs(); len(runs) != 0 {
		t.Fatalf("the second submit launched under the modal: %v", runs)
	}
	if r.m.launchForm == nil || !hostModalShown(r.screen()) {
		t.Errorf("the modal or the form is gone (form %v):\n%s", r.m.launchForm != nil, r.screen())
	}
	r.press(t, "enter") // Cancel
	if r.m.launchForm == nil || r.m.mountModal != nil || len(r.runs()) != 0 {
		t.Errorf("cancel did not give the form back (runs %v)", r.runs())
	}
}

// The mirror order: a launch that started first must not leave a modal over a
// form that is gone when the host-network submit, already in flight, arrives
// before the launch has finished.
func TestASubmitAfterTheLaunchStartedOpensNoModal(t *testing.T) {
	r := newNetworkRig(t, engine.Docker, "bridge", "host")
	f := r.m.launchForm
	f.networkIdx = 0
	focusLaunchField(f, f.fieldSubmit())
	_, first := f.Update(testutil.Key("enter"))
	f.networkIdx = 1
	_, second := f.Update(testutil.Key("enter"))
	msg1, ok1 := testutil.MsgOf[LaunchFormSubmitMsg](first)
	msg2, ok2 := testutil.MsgOf[LaunchFormSubmitMsg](second)
	if !ok1 || !ok2 {
		t.Fatal("the two enters did not each produce a LaunchFormSubmitMsg")
	}

	var launch1, launch2 tea.Cmd
	r.m, launch1 = step(t, r.m, msg1)
	r.m, launch2 = step(t, r.m, msg2)
	r.m = settle(t, r.m, tea.Batch(launch1, launch2))

	if runs := r.runs(); len(runs) != 1 || !containsPair(runs[0], "--network", "bridge") {
		t.Fatalf("the engine saw %v, want the one run of the first submit", runs)
	}
	if r.m.mountModal != nil || r.m.mountPending != nil || hostModalShown(r.screen()) {
		t.Errorf("a modal is open over a form that is gone:\n%s", r.screen())
	}
}

// The warning line of the preview is the theme's style, not one built in the view.
func TestThePreviewWarnStyleIsTheThemesOne(t *testing.T) {
	var warned string
	for _, line := range hostForm().commandPreviewLines() {
		if strings.HasPrefix(strings.TrimSpace(ansi.Strip(line)), "--network host") {
			warned = line
		}
	}
	if warned == "" {
		t.Fatal("no --network host line in the preview")
	}
	if want := theme.PreviewWarnStyle.Render(ansi.Strip(warned)); warned != want {
		t.Errorf("the host network line is %q, want it rendered by theme.PreviewWarnStyle: %q", warned, want)
	}
}
