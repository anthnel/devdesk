package components

import (
	"strings"
	"testing"

	tea "charm.land/bubbletea/v2"
	"github.com/charmbracelet/x/ansi"

	"github.com/anthnel/devdesk/internal/config"
	sharedcomponents "github.com/anthnel/devdesk/internal/ui/components"
	"github.com/anthnel/devdesk/internal/ui/testutil"
	"github.com/anthnel/devdesk/internal/ui/theme"
)

// DEVDESK-292 step 4a: invalid fields are underlined after a refused submit.

const curly = "4:3"

func downTo(f *ComponentForm, n int) *ComponentForm {
	for i := 0; i < n; i++ {
		f = feedForm(f, testutil.Key("down"))
	}
	return f
}

// submitRefusable moves to the last field of an https form and presses enter.
func submitHTTPS(f *ComponentForm) (*ComponentForm, tea.Cmd) {
	f = downTo(f, 4)
	return f.Update(testutil.Key("enter"))
}

// inputLine returns the raw View() line holding the input under label.
func inputLine(t *testing.T, f *ComponentForm, label string) string {
	t.Helper()
	lines := strings.Split(f.View(), "\n")
	for i, l := range lines {
		if strings.Contains(ansi.Strip(l), label+" "+theme.IconChevronRight) && i+1 < len(lines) {
			return lines[i+1]
		}
	}
	t.Fatalf("no %q label in the form:\n%s", label, ansi.Strip(f.View()))
	return ""
}

func labelLine(t *testing.T, f *ComponentForm, label string) string {
	t.Helper()
	for _, l := range strings.Split(f.View(), "\n") {
		if strings.Contains(ansi.Strip(l), label+" "+theme.IconChevronRight) {
			return l
		}
	}
	t.Fatalf("no %q label", label)
	return ""
}

func TestNothingIsUnderlinedBeforeTheFirstSubmit(t *testing.T) {
	f := NewComponentForm(nil)
	f = typeInto(f, "x")
	f = downTo(f, 1)
	if strings.Contains(f.View(), curly) {
		t.Error("a curly underline is rendered before any submit")
	}
	if st := f.ValidationStatus(); st != (sharedcomponents.Status{}) {
		t.Errorf("ValidationStatus() = %+v before any submit, want zero", st)
	}
}

func TestARefusedSubmitUnderlinesEmptyRequiredPlaceholders(t *testing.T) {
	f := NewComponentForm(nil)
	f, cmd := submitHTTPS(f)

	if cmd != nil {
		t.Fatal("a form with empty Name and Target produced a command")
	}
	for _, label := range []string{"Name", "Target"} {
		if line := inputLine(t, f, label); !strings.Contains(line, curly) {
			t.Errorf("empty required %s placeholder is not underlined: %q", label, line)
		}
	}
	if line := inputLine(t, f, "Timeout (seconds)"); strings.Contains(line, curly) {
		t.Error("the valid Timeout field is underlined")
	}
	st := f.ValidationStatus()
	if st.Level != sharedcomponents.LevelError || !strings.HasPrefix(st.Text, "Name is required") {
		t.Errorf("ValidationStatus() = %+v, want an Error naming Name first", st)
	}
	if !strings.Contains(st.Text, "(+1 more)") {
		t.Errorf("status %q does not announce the Target error", st.Text)
	}
}

func TestOnlyTheFaultyFieldIsUnderlined(t *testing.T) {
	f := NewComponentForm(nil)
	f = typeInto(f, "api")
	f, _ = submitHTTPS(f)

	if strings.Contains(inputLine(t, f, "Name"), curly) {
		t.Error("the filled Name is underlined")
	}
	if !strings.Contains(inputLine(t, f, "Target"), curly) {
		t.Error("the empty Target is not underlined")
	}
	if got := f.ValidationStatus().Text; !strings.HasPrefix(got, "Target is required") {
		t.Errorf("status = %q, want it to name Target", got)
	}
}

func TestCorrectingAFieldRemovesItsUnderline(t *testing.T) {
	f := NewComponentForm(nil)
	f, _ = submitHTTPS(f)
	f = feedForm(f, testutil.Key("up"), testutil.Key("up"), testutil.Key("up"), testutil.Key("up"))
	f = typeInto(f, "api")

	if strings.Contains(inputLine(t, f, "Name"), curly) {
		t.Error("Name is still underlined after typing a value")
	}
	if !strings.Contains(inputLine(t, f, "Target"), curly) {
		t.Error("Target lost its underline although still empty")
	}
	if got := f.ValidationStatus().Text; !strings.HasPrefix(got, "Target is required") {
		t.Errorf("status = %q, want it to move on to Target", got)
	}

	f = feedForm(f, testutil.Key("down"))
	f = typeInto(f, "example.com")
	if strings.Contains(f.View(), curly) || f.ValidationStatus() != (sharedcomponents.Status{}) {
		t.Error("a fully corrected form still shows an error")
	}
}

func TestInvalidNumbersAreRefusedNotReplacedByTheDefault(t *testing.T) {
	cases := []struct {
		name, typ, value, label, msg string
		field                        int
		ok                           bool
	}{
		{"timeout letters", "https", "abc", "Timeout (seconds)", "Timeout must be a number between 1 and 999", fieldTimeout, false},
		{"timeout zero", "https", "0", "Timeout (seconds)", "Timeout must be a number between 1 and 999", fieldTimeout, false},
		{"timeout negative", "https", "-5", "Timeout (seconds)", "Timeout must be a number between 1 and 999", fieldTimeout, false},
		{"timeout min", "https", "1", "Timeout (seconds)", "", fieldTimeout, true},
		{"timeout max", "https", "999", "Timeout (seconds)", "", fieldTimeout, true},
		{"timeout empty", "https", "", "Timeout (seconds)", "", fieldTimeout, true},
		{"count letters", "icmp", "abc", "Ping count", "Ping count must be a number between 1 and 99", fieldExtra, false},
		{"count zero", "icmp", "0", "Ping count", "Ping count must be a number between 1 and 99", fieldExtra, false},
		{"count max", "icmp", "99", "Ping count", "", fieldExtra, true},
		{"count empty", "icmp", "", "Ping count", "", fieldExtra, true},
	}
	for _, tc := range cases {
		t.Run(tc.name, func(t *testing.T) {
			f := NewComponentForm(&config.ComponentConfig{Name: "api", Type: tc.typ, Target: "example.com"})
			f = downTo(f, tc.field)
			f = typeInto(f, tc.value)
			f = downTo(f, f.maxFieldIndex()-tc.field)
			f, cmd := f.Update(testutil.Key("enter"))

			if tc.ok {
				if cmd == nil {
					t.Fatalf("%q was refused", tc.value)
				}
				return
			}
			if cmd != nil {
				t.Fatalf("%q was accepted (silently replaced by the default)", tc.value)
			}
			if !strings.Contains(inputLine(t, f, tc.label), curly) {
				t.Errorf("the %s field is not underlined", tc.label)
			}
			if got := f.ValidationStatus().Text; !strings.HasPrefix(got, tc.msg) {
				t.Errorf("status = %q, want prefix %q", got, tc.msg)
			}
		})
	}
}

func TestEmptyTimeoutAndCountKeepTheirDefaults(t *testing.T) {
	f := NewComponentForm(&config.ComponentConfig{Name: "p", Type: "icmp", Target: "1.1.1.1"})
	f = downTo(f, f.maxFieldIndex())
	_, cmd := f.Update(testutil.Key("enter"))
	if cmd == nil {
		t.Fatal("empty Timeout and Ping count were refused")
	}
	got := cmd().(ComponentFormSubmitMsg).Component
	if got.Timeout != 10 || got.Count != 4 {
		t.Errorf("defaults = timeout %d count %d, want 10 and 4", got.Timeout, got.Count)
	}
}

func TestSwitchingAwayFromICMPClearsThePingCountError(t *testing.T) {
	f := NewComponentForm(&config.ComponentConfig{Name: "p", Type: "icmp", Target: "1.1.1.1"})
	f = downTo(f, fieldExtra)
	f = typeInto(f, "abc")
	f = downTo(f, 1)
	f, _ = f.Update(testutil.Key("enter"))
	if !strings.HasPrefix(f.ValidationStatus().Text, "Ping count must") {
		t.Fatalf("precondition: status = %q", f.ValidationStatus().Text)
	}

	f = downTo(f, 4) // wraps to the Type field (index 3) from submit (5)
	f = feedForm(f, testutil.Key("left"))
	if f.compType != "https" {
		t.Fatalf("compType = %q, want https", f.compType)
	}
	if st := f.ValidationStatus(); st != (sharedcomponents.Status{}) {
		t.Errorf("status = %+v after the Ping count field was hidden, want zero", st)
	}
}

func TestInvalidStyleKeepsPrefixAndWidths(t *testing.T) {
	valid := NewComponentForm(&config.ComponentConfig{Name: "api", Type: "https", Target: "x.io"})
	invalid := NewComponentForm(nil)
	invalid, _ = submitHTTPS(invalid)
	invalid = feedForm(invalid, testutil.Key("down")) // focus wraps to Name

	for _, label := range []string{"Name", "Target", "Timeout (seconds)"} {
		if a, b := labelLine(t, valid, label), labelLine(t, invalid, label); a != b && !strings.Contains(a, theme.IconCircleSmall) {
			// Unfocused prefixes must be byte-identical.
			t.Errorf("%s label differs between valid and invalid forms:\n%q\n%q", label, a, b)
		}
	}
	if line := labelLine(t, invalid, "Name"); !strings.Contains(ansi.Strip(line), theme.IconCircleSmall+" Name "+theme.IconChevronRight) {
		t.Errorf("focused invalid Name label = %q, want the focus prefix", ansi.Strip(line))
	}
	if line := labelLine(t, invalid, "Target"); !strings.HasPrefix(ansi.Strip(line), "  Target "+theme.IconChevronRight) {
		t.Errorf("unfocused invalid Target label = %q, want two-space prefix", ansi.Strip(line))
	}

	// Same shape as a form with the same fields filled: compare against the
	// same form, only underline differing, by width per line.
	before := NewComponentForm(nil)
	vl, il := strings.Split(ansi.Strip(before.View()), "\n"), strings.Split(ansi.Strip(invalid.View()), "\n")
	if len(vl) != len(il) {
		t.Fatalf("line count %d (valid) vs %d (invalid)", len(vl), len(il))
	}
	for i := range vl {
		if ansi.StringWidth(vl[i]) != ansi.StringWidth(il[i]) {
			t.Errorf("line %d width %d vs %d", i, ansi.StringWidth(vl[i]), ansi.StringWidth(il[i]))
		}
	}
	if !strings.Contains(invalid.View(), curly) {
		t.Error("precondition: the invalid form shows no underline")
	}
}
