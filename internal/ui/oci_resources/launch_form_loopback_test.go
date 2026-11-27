package ociresources

import (
	"slices"
	"strings"
	"testing"

	"github.com/anthnel/devdesk/internal/cache"
	"github.com/anthnel/devdesk/internal/docker"
	sharedcomponents "github.com/anthnel/devdesk/internal/ui/components"
	"github.com/anthnel/devdesk/internal/ui/testutil"
)

// OC-19 (S3): the launch form publishes ports on 127.0.0.1 unless the user
// wrote an address. The preview and the ctrl+y copy show what will really run,
// so both are read through buildCommandString (what ctrl+y copies) and
// buildCommandLines (what the preview draws). The clipboard itself is never
// touched.

const allInterfacesWarning = "all interfaces"

func newLoopbackForm() *LaunchForm {
	return NewLaunchForm("api:v1", []string{"80/tcp", "443/tcp"}, nil, 120)
}

// previewHas reports whether the preview and the copied command both carry
// the flag exactly (as a whole argument, not a prefix of a longer one).
func previewHas(f *LaunchForm, flag string) bool {
	hasLine := false
	for _, line := range f.buildCommandLines() {
		if strings.TrimSpace(strings.TrimSuffix(strings.TrimSpace(line), "\\")) == flag {
			hasLine = true
		}
	}
	return hasLine && strings.Contains(f.buildCommandString()+" ", " "+flag+" ")
}

// submitLaunchMsg presses enter on the Launch button and returns the
// submission it produced, if any.
func submitLaunchMsg(f *LaunchForm) (*LaunchForm, LaunchFormSubmitMsg, bool) {
	focusLaunchField(f, f.fieldSubmit())
	f, cmd := f.Update(testutil.Key("enter"))
	if cmd == nil {
		return f, LaunchFormSubmitMsg{}, false
	}
	msg, ok := testutil.MsgOf[LaunchFormSubmitMsg](cmd)
	return f, msg, ok
}

func TestLaunchFormPreviewShowsLoopbackForAnAdditionalPortWithoutAddress(t *testing.T) {
	tests := []struct {
		name  string
		extra string
		flags []string
	}{
		{"host:container", "8080:80", []string{"-p 127.0.0.1:8080:80"}},
		{"protocol", "8080:80/udp", []string{"-p 127.0.0.1:8080:80/udp"}},
		{"range", "8000-8010:8000-8010", []string{"-p 127.0.0.1:8000-8010:8000-8010"}},
		{"container port only", "80", []string{"-p 127.0.0.1::80"}},
		{"several", "8080:80, 9090:90/tcp", []string{"-p 127.0.0.1:8080:80", "-p 127.0.0.1:9090:90/tcp"}},
		{"explicit loopback is kept", "127.0.0.1:8080:80", []string{"-p 127.0.0.1:8080:80"}},
		{"explicit IPv6 loopback is kept", "[::1]:8080:80", []string{"-p [::1]:8080:80"}},
		{"explicit LAN address is kept", "192.168.1.10:8080:80", []string{"-p 192.168.1.10:8080:80"}},
	}
	for _, tt := range tests {
		t.Run(tt.name, func(t *testing.T) {
			f := newLoopbackForm()
			f.ports[0].enabled, f.ports[1].enabled = false, false
			f.extraPortsInput.SetValue(tt.extra)

			for _, flag := range tt.flags {
				if !previewHas(f, flag) {
					t.Errorf("preview/copy lacks %q:\n%s", flag, f.buildCommandString())
				}
			}
			cmd := f.buildCommandString()
			for _, bare := range strings.Split(tt.extra, ",") {
				bare = strings.TrimSpace(bare)
				if strings.Contains(" "+cmd+" ", " -p "+bare+" ") && !isAddressed(bare) {
					t.Errorf("the bare mapping %q is still previewed:\n%s", bare, cmd)
				}
			}
		})
	}
}

// isAddressed reports whether a -p spec names an address of its own.
func isAddressed(spec string) bool {
	return docker.LoopbackPort(spec) == spec
}

// Criterion 2: the ports pre-filled from EXPOSE, launched untouched, are on 127.0.0.1.
func TestLaunchFormPrefilledExposePortsArePublishedOnLoopback(t *testing.T) {
	f := newLoopbackForm()

	for _, flag := range []string{"-p 127.0.0.1:80:80", "-p 127.0.0.1:443:443"} {
		if !previewHas(f, flag) {
			t.Errorf("preview/copy lacks %q:\n%s", flag, f.buildCommandString())
		}
	}
	if strings.Contains(" "+f.buildCommandString()+" ", " -p 80:80 ") {
		t.Errorf("the bare EXPOSE mapping is still previewed:\n%s", f.buildCommandString())
	}
}

func TestLaunchFormEditedExposeHostPortIsPublishedOnLoopback(t *testing.T) {
	f := newLoopbackForm()
	f.ports[0].hostPortInput.SetValue("8080")

	if !previewHas(f, "-p 127.0.0.1:8080:80") {
		t.Errorf("preview/copy lacks the loopback mapping of the edited port:\n%s", f.buildCommandString())
	}
	f.ports[1].enabled = false
	if strings.Contains(f.buildCommandString(), "443") {
		t.Errorf("an unticked port is previewed:\n%s", f.buildCommandString())
	}
}

// Whatever the form hands to the launcher must come out on 127.0.0.1 once the
// launcher has done its part; this is the form half of criterion 2.
func TestLaunchFormSubmitHandsOverPortsThatEndOnLoopback(t *testing.T) {
	f := newLoopbackForm()
	f.extraPortsInput.SetValue("9090:90, 7000")

	_, msg, ok := submitLaunchMsg(f)
	if !ok {
		t.Fatal("the launch was refused")
	}
	if len(msg.Opts.Ports) != 4 {
		t.Fatalf("Opts.Ports = %q, want the two EXPOSE ports and the two additional ones", msg.Opts.Ports)
	}
	for _, p := range msg.Opts.Ports {
		got := docker.LoopbackPort(p)
		if !strings.HasPrefix(got, "127.0.0.1:") {
			t.Errorf("port %q ends as %q, want an address-prefixed 127.0.0.1 mapping", p, got)
		}
		if docker.PortExposedOnAllInterfaces(p) {
			t.Errorf("port %q is exposed on all interfaces without being asked", p)
		}
	}
}

// Criterion 3: an explicit 0.0.0.0 is allowed, previewed as typed, and warned
// about in the footer at level Warning.
func TestLaunchFormExplicitAllInterfacesIsAllowedPreviewedAndWarned(t *testing.T) {
	f := newLoopbackForm()
	f.ports[0].enabled, f.ports[1].enabled = false, false
	f.extraPortsInput.SetValue("0.0.0.0:8080:80")

	if !previewHas(f, "-p 0.0.0.0:8080:80") {
		t.Errorf("preview/copy does not show the explicit address:\n%s", f.buildCommandString())
	}
	if strings.Contains(f.buildCommandString(), "127.0.0.1") {
		t.Errorf("an explicit 0.0.0.0 was rewritten to loopback:\n%s", f.buildCommandString())
	}

	st := footerStatus(f)
	if st.Level != sharedcomponents.LevelWarning {
		t.Errorf("footer level = %v, want LevelWarning (text %q)", st.Level, st.Text)
	}
	if !strings.Contains(strings.ToLower(st.Text), allInterfacesWarning) {
		t.Errorf("footer text = %q, want it to say the port is exposed on %q", st.Text, allInterfacesWarning)
	}

	f, msg, ok := submitLaunchMsg(f)
	if !ok {
		t.Fatal("an explicit 0.0.0.0 was refused, it must be allowed")
	}
	if !slices.Contains(msg.Opts.Ports, "0.0.0.0:8080:80") {
		t.Errorf("Opts.Ports = %q, want the explicit mapping kept as typed", msg.Opts.Ports)
	}
	if st := footerStatus(f); st.Level != sharedcomponents.LevelWarning || !strings.Contains(strings.ToLower(st.Text), allInterfacesWarning) {
		t.Errorf("after the submit the footer is {%q, %v}, want the Warning to stay", st.Text, st.Level)
	}
}

func TestLaunchFormNoWarningWithoutAnAllInterfacesPort(t *testing.T) {
	tests := []struct {
		name  string
		extra string
	}{
		{"defaults only", ""},
		{"no address", "8080:80"},
		{"explicit loopback", "127.0.0.1:8080:80"},
		{"explicit IPv6 loopback", "[::1]:8080:80"},
	}
	for _, tt := range tests {
		t.Run(tt.name, func(t *testing.T) {
			f := newLoopbackForm()
			f.extraPortsInput.SetValue(tt.extra)
			if st := footerStatus(f); st.Text != "" {
				t.Errorf("footer = {%q, %v}, want nothing", st.Text, st.Level)
			}
		})
	}
}

// Nothing is warned about a port that will not be launched.
func TestLaunchFormNoWarningWhenAllInterfacesPortIsGone(t *testing.T) {
	f := newLoopbackForm()
	f.extraPortsInput.SetValue("0.0.0.0:8080:80")
	if footerStatus(f).Text == "" {
		t.Fatal("precondition: the explicit 0.0.0.0 must warn")
	}

	f.extraPortsInput.SetValue("8080:80")

	if st := footerStatus(f); st.Text != "" {
		t.Errorf("footer = {%q, %v} after the address was removed, want nothing", st.Text, st.Level)
	}
}

// The Warning is a lower priority than a refusal: an invalid host port keeps
// the footer at Error until it is fixed.
func TestLaunchFormErrorOutranksTheAllInterfacesWarning(t *testing.T) {
	f := newLoopbackForm()
	f.extraPortsInput.SetValue("0.0.0.0:8080:80")
	setHostPort(f, 0, "80a0", true)

	f, submitted := submitLaunch(f)
	if submitted {
		t.Fatal("an invalid host port was accepted")
	}
	if st := footerStatus(f); st.Level != sharedcomponents.LevelError || st.Text != portMessage {
		t.Errorf("footer = {%q, %v}, want the host-port error {%q, LevelError}", st.Text, st.Level, portMessage)
	}
}

// Criterion 4: a cached entry written before the address existed carries no
// address, and the form applies 127.0.0.1 to it.
func TestLaunchFormLegacyCachedPortsAreAppliedOnLoopback(t *testing.T) {
	f := newLoopbackForm()
	f.ApplyCached(cache.LaunchOptionsEntry{
		ExtraPorts: "9090:90, 7000",
		PortMappings: map[string]cache.PortMappingEntry{
			"80/tcp":  {HostPort: "8081", Enabled: true},
			"443/tcp": {HostPort: "443", Enabled: false},
		},
	})

	for _, flag := range []string{"-p 127.0.0.1:8081:80", "-p 127.0.0.1:9090:90", "-p 127.0.0.1::7000"} {
		if !previewHas(f, flag) {
			t.Errorf("preview/copy lacks %q:\n%s", flag, f.buildCommandString())
		}
	}
	for _, bare := range []string{" -p 8081:80 ", " -p 9090:90 ", " -p 7000 "} {
		if strings.Contains(" "+f.buildCommandString()+" ", bare) {
			t.Errorf("the bare cached mapping %q is still previewed:\n%s", strings.TrimSpace(bare), f.buildCommandString())
		}
	}
	if st := footerStatus(f); st.Text != "" {
		t.Errorf("footer = {%q, %v}, a legacy cached value must not warn", st.Text, st.Level)
	}
}

// A cached value that did carry an explicit all-interfaces address warns again
// when it is restored: the user must be told each time, not only when they typed it.
func TestLaunchFormCachedAllInterfacesPortWarnsAgain(t *testing.T) {
	f := newLoopbackForm()
	f.ApplyCached(cache.LaunchOptionsEntry{ExtraPorts: "0.0.0.0:8080:80"})

	st := footerStatus(f)
	if st.Level != sharedcomponents.LevelWarning || !strings.Contains(strings.ToLower(st.Text), allInterfacesWarning) {
		t.Errorf("footer = {%q, %v}, want the all-interfaces Warning", st.Text, st.Level)
	}
	if !previewHas(f, "-p 0.0.0.0:8080:80") {
		t.Errorf("preview/copy does not show the cached explicit address:\n%s", f.buildCommandString())
	}
}
