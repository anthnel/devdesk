package ociresources

import (
	"reflect"
	"strings"
	"testing"

	tea "charm.land/bubbletea/v2"
	"github.com/charmbracelet/x/ansi"

	sharedcomponents "github.com/anthnel/devdesk/internal/ui/components"
	"github.com/anthnel/devdesk/internal/ui/testutil"
	"github.com/anthnel/devdesk/internal/ui/theme"
)

// DEVDESK-292 step 4c: the launch form refuses an invalid host port of a
// ticked EXPOSE port, underlines it after the first refused submit only, and
// names it in the footer (Error level, derived on every frame).

const curlyUnderline = "4:3"

func newValidationForm() *LaunchForm {
	return NewLaunchForm("api:v1", []string{"80/tcp", "443/tcp"}, nil, 120)
}

// setHostPort sets the host port of port i and ticks or unticks it.
func setHostPort(f *LaunchForm, i int, value string, enabled bool) {
	f.ports[i].hostPortInput.SetValue(value)
	f.ports[i].enabled = enabled
}

// submitLaunch presses enter on the Launch button.
func submitLaunch(f *LaunchForm) (*LaunchForm, bool) {
	focusLaunchField(f, f.fieldSubmit())
	f, cmd := f.Update(testutil.Key("enter"))
	if cmd == nil {
		return f, false
	}
	_, ok := testutil.MsgOf[LaunchFormSubmitMsg](cmd)
	return f, ok
}

func footerStatus(f *LaunchForm) sharedcomponents.Status {
	return Model{launchForm: f}.formStatus()
}

// portLine returns the raw rendered line of the port whose container label is label.
func portLine(t *testing.T, f *LaunchForm, label string) string {
	t.Helper()
	for _, line := range strings.Split(f.View(), "\n") {
		if strings.Contains(ansi.Strip(line), ":"+label) && strings.Contains(ansi.Strip(line), "tcp") {
			return line
		}
	}
	t.Fatalf("no line for port %s in:\n%s", label, ansi.Strip(f.View()))
	return ""
}

const portMessage = "Host port for 80/tcp must be a number between 1 and 65535"

func TestLaunchFormRefusesAnInvalidHostPortOfATickedPort(t *testing.T) {
	f := newValidationForm()
	setHostPort(f, 0, "80a0", true)

	f, submitted := submitLaunch(f)
	if submitted {
		t.Fatal("submit with host port 80a0 on a ticked 80/tcp produced LaunchFormSubmitMsg, want a refusal")
	}
	if !strings.Contains(portLine(t, f, "80/tcp"), curlyUnderline) {
		t.Errorf("port 80/tcp row is not underlined after the refused submit:\n%q", portLine(t, f, "80/tcp"))
	}
	if strings.Contains(portLine(t, f, "443/tcp"), curlyUnderline) {
		t.Error("the valid 443/tcp row is underlined, only the faulty field should be")
	}
	st := footerStatus(f)
	if st.Text != portMessage || st.Level != sharedcomponents.LevelError {
		t.Errorf("formStatus() = {%q, level %v}, want {%q, LevelError}", st.Text, st.Level, portMessage)
	}
}

func TestLaunchFormNothingIsUnderlinedBeforeTheFirstSubmit(t *testing.T) {
	f := newValidationForm()
	setHostPort(f, 0, "80a0", true)
	f.Update(testutil.Key("x")) // a change before any submit must not arm validation

	if strings.Contains(f.View(), curlyUnderline) {
		t.Error("an invalid port is underlined before any submit")
	}
	if st := footerStatus(f); st.Text != "" {
		t.Errorf("formStatus().Text = %q before any submit, want empty", st.Text)
	}
}

func TestLaunchFormHostPortBounds(t *testing.T) {
	tests := []struct {
		value string
		valid bool
	}{
		{"0", false}, {"65536", false}, {"-1", false}, {"80a0", false}, {" 8 0", false},
		{"1", true}, {"65535", true}, {"8080", true}, {"", true},
		{"+80", false}, {"٣٣", false}, {"99999999999999999999", false}, {"  ", true}, {" 80 ", true}, {"0080", true}, {"0000", false},
	}
	for _, tc := range tests {
		t.Run("ticked/"+tc.value, func(t *testing.T) {
			f := newValidationForm()
			setHostPort(f, 0, tc.value, true)
			f, submitted := submitLaunch(f)
			if submitted != tc.valid {
				t.Errorf("host port %q ticked: submitted = %v, want %v", tc.value, submitted, tc.valid)
			}
			if got := strings.Contains(f.View(), curlyUnderline); got == tc.valid {
				t.Errorf("host port %q ticked: underlined = %v, want %v", tc.value, got, !tc.valid)
			}
		})
		t.Run("unticked/"+tc.value, func(t *testing.T) {
			f := newValidationForm()
			setHostPort(f, 0, tc.value, false)
			f, submitted := submitLaunch(f)
			if !submitted {
				t.Errorf("host port %q unticked: refused, an unticked port is never invalid", tc.value)
			}
			if strings.Contains(f.View(), curlyUnderline) || footerStatus(f).Text != "" {
				t.Errorf("host port %q unticked: shows an error", tc.value)
			}
		})
	}
}

func TestLaunchFormTickingAnInvalidPortRevalidates(t *testing.T) {
	f := newValidationForm()
	setHostPort(f, 0, "80a0", true)
	f, _ = submitLaunch(f)

	focusLaunchField(f, f.firstPortField())
	f, _ = f.Update(testutil.Key("space")) // untick
	if f.ports[0].enabled {
		t.Fatal("space did not untick the port")
	}
	if strings.Contains(f.View(), curlyUnderline) || footerStatus(f).Text != "" {
		t.Error("unticking the invalid port left the error in place")
	}
}

func TestLaunchFormAdditionalPortsAndEnvironmentAreNeverRefused(t *testing.T) {
	f := newValidationForm()
	f.extraPortsInput.SetValue("not a port, 99999:abc")
	f.envInput.SetValue("%%%, =, 1BAD")
	f, submitted := submitLaunch(f)
	if !submitted {
		t.Error("free-form Additional ports / Environment values were refused")
	}
	if strings.Contains(f.View(), curlyUnderline) || footerStatus(f).Text != "" {
		t.Error("Additional ports / Environment are flagged invalid")
	}
}

func TestLaunchFormCorrectionClearsTheErrorOnTheNextRender(t *testing.T) {
	f := newValidationForm()
	setHostPort(f, 0, "80a0", true)
	f, _ = submitLaunch(f)
	if footerStatus(f).Text == "" {
		t.Fatal("precondition: the refused submit must set a footer error")
	}

	focusLaunchField(f, f.firstPortField())
	f.ports[0].hostPortInput.CursorEnd() // SetValue while blurred leaves the cursor at 0
	for _, m := range append(testutil.Keys("backspace", "backspace", "backspace", "backspace"), testutil.Type("8080")...) {
		f, _ = f.Update(m)
	}
	if got := f.ports[0].hostPortInput.Value(); got != "8080" {
		t.Fatalf("host port = %q after correction, want 8080", got)
	}
	if strings.Contains(f.View(), curlyUnderline) {
		t.Error("the port is still underlined after the correction")
	}
	if st := footerStatus(f); st.Text != "" {
		t.Errorf("formStatus().Text = %q after the correction, want empty", st.Text)
	}
	if _, ok := submitLaunch(f); !ok {
		t.Error("submit refused after the correction")
	}
}

func TestLaunchFormErrorFollowsAnEditAfterARefusal(t *testing.T) {
	f := newValidationForm()
	setHostPort(f, 0, "80", true)
	setHostPort(f, 1, "", true)
	f, _ = submitLaunch(f) // accepted: nothing armed yet beyond a clean submit
	setHostPort(f, 0, "80a0", true)
	f.ports[0].hostPortInput.SetValue("0")
	f, submitted := submitLaunch(f)
	if submitted {
		t.Fatal("host port 0 was accepted")
	}
	// Break the other port: revalidation is live after a refusal.
	focusLaunchField(f, f.firstPortField()+1)
	f.ports[1].enabled = true
	f, _ = f.Update(testutil.Key("x"))
	if !strings.Contains(portLine(t, f, "443/tcp"), curlyUnderline) {
		t.Error("typing an invalid host port after a refusal did not underline it at once")
	}
	if st := footerStatus(f); !strings.Contains(st.Text, "80/tcp") {
		t.Errorf("formStatus().Text = %q, want the first invalid field (80/tcp) named", st.Text)
	}
}

func TestLaunchFormInvalidPortKeepsPrefixAndWidth(t *testing.T) {
	valid := newValidationForm()
	setHostPort(valid, 0, "8080", true)
	invalid := newValidationForm()
	setHostPort(invalid, 0, "80a0", true)
	invalid, _ = submitLaunch(invalid)
	valid, _ = submitLaunch(valid)

	for _, focus := range []bool{false, true} {
		if focus {
			focusLaunchField(valid, valid.firstPortField())
			focusLaunchField(invalid, invalid.firstPortField())
		}
		v, i := valid.renderPortEntry(0), invalid.renderPortEntry(0)
		if ansi.StringWidth(v) != ansi.StringWidth(i) {
			t.Errorf("focus=%v: port row width %d valid vs %d invalid", focus, ansi.StringWidth(v), ansi.StringWidth(i))
		}
		stripV, stripI := ansi.Strip(v), ansi.Strip(i)
		if focus && (!strings.HasPrefix(stripI, theme.IconCircleSmall+" ") || !strings.HasPrefix(stripV, theme.IconCircleSmall+" ")) {
			t.Errorf("focused row lost its focus marker prefix: %q / %q", stripV, stripI)
		}
		if !focus && (strings.HasPrefix(stripI, theme.IconCircleSmall) || !strings.HasPrefix(stripI, "  ")) {
			t.Errorf("unfocused row prefix changed: %q", stripI)
		}
		if !strings.Contains(i, curlyUnderline) {
			t.Errorf("focus=%v: invalid row is not underlined", focus)
		}
	}
}

func TestLaunchFormHasNoDeadErrField(t *testing.T) {
	if _, ok := reflect.TypeOf(LaunchForm{}).FieldByName("err"); ok {
		t.Error("LaunchForm.err is never set; validation state lives in components.Validation")
	}
}

// DEVDESK-292: a message to the focused host port must not restyle it unless
// its invalid state changed, or the cursor stops blinking.
func TestLaunchFormHostPortCursorStillBlinks(t *testing.T) {
	f := newValidationForm()
	setHostPort(f, 0, "80", true)
	focusLaunchField(f, f.firstPortField())
	testutil.RequireBlinks(t, func(m tea.Msg) tea.Cmd {
		_, cmd := f.Update(m)
		return cmd
	}, func() string { return f.ports[0].hostPortInput.View() })
}

func TestMappingArgNormalizesTheHostPort(t *testing.T) {
	tests := map[string]string{"0080": "80:80", " 8080 ": "8080:80", "": "80:80", "   ": "80:80"}
	for value, want := range tests {
		p := portEntry{containerPort: "80/tcp"}
		p.hostPortInput.SetValue(value)
		if got := p.mappingArg(); got != want {
			t.Errorf("mappingArg(%q) = %q, want %q", value, got, want)
		}
	}
}
