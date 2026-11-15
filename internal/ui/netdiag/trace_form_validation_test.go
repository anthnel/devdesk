package netdiag

import (
	"strings"
	"testing"

	"github.com/anthnel/devdesk/internal/ui/components"
	"github.com/anthnel/devdesk/internal/ui/testutil"
)

// DEVDESK-470 task 470.3: Trace Path validates the target with parsePathTarget,
// the way the path trace does. A refusal emits nothing, underlines the target
// (Rule 140) and names the error in the footer, derived on every render.

const curlyUnderline = "4:3"

// pathIPv6Text is the path trace's own wording for an IPv6 literal.
var pathIPv6Text = capitalize(errPathIPv6.Error())

// traceFormWithTarget returns a supported model whose target field holds value.
func traceFormWithTarget(t *testing.T, value string) (*Model, *fakeEngine) {
	t.Helper()
	m, fe := traceFormModel(t, true)
	m.targetInput.SetValue(value)
	return m, fe
}

// pressTrace focuses the Trace Path button and presses enter.
func pressTrace(t *testing.T, m *Model) *Model {
	t.Helper()
	m.focusedField = fieldTraceButton
	return feed(t, m, testutil.Key("enter"))
}

// footerError is the status the Diagnostics tab derives on this frame.
func footerError(m *Model) (string, components.Level) {
	_, st := m.activeFooter()
	return st.Text, st.Level
}

func TestTraceFormValidationRefusesAnInvalidTarget(t *testing.T) {
	tests := []struct {
		name, target, want string
	}{
		{"empty", "", "Target is required"},
		{"ipv6 literal", "::1", pathIPv6Text},
		{"host with port", "host:8080", errPathPortText},
		{"port zero", "h:0", errPathPortText},
		{"ipv4 with port", "1.2.3.4:80", errPathPortText},
		{"bracketed ipv6 with port", "[::1]:443", errPathPortText},
		{"malformed host", "bad host!", "Target must be a hostname or IP address"},
	}
	for _, tc := range tests {
		t.Run(tc.name, func(t *testing.T) {
			m, fe := traceFormWithTarget(t, tc.target)
			gen := m.pathModel.gen
			m = pressTrace(t, m)

			if m.state != StateInput {
				t.Errorf("state = %v, want StateInput", m.state)
			}
			if m.pathModel.gen != gen || fe.traceCalls != 0 || fe.resolveCalls != 0 {
				t.Errorf("gen %d->%d trace=%d resolve=%d, nothing may be emitted",
					gen, m.pathModel.gen, fe.traceCalls, fe.resolveCalls)
			}
			if pm := m.pathModel; pm.state != pathReady || pm.result != nil || pm.errText != "" || pm.hopCount() != 0 {
				t.Errorf("path model state=%v result=%v errText=%q hops=%d, want a clean ready model: a refusal is no trace state",
					pm.state, pm.result, pm.errText, pm.hopCount())
			}
			if !m.traceValidation.Invalid(traceKeyTarget) {
				t.Error("the target is not marked invalid")
			}
			if !strings.Contains(m.targetInput.View(), curlyUnderline) {
				t.Errorf("the target is not underlined: %q", m.targetInput.View())
			}
			text, level := footerError(m)
			if text != tc.want || level != components.LevelError {
				t.Errorf("footer = {%q %v}, want {%q Error}", text, level, tc.want)
			}
			if !strings.Contains(m.RenderFooter(120), tc.want) {
				t.Errorf("RenderFooter does not show %q", tc.want)
			}
		})
	}
}

// Same sentence as the path trace gives for the same input.
func TestTraceFormValidationTextMatchesTheTraceParser(t *testing.T) {
	for _, target := range []string{"", "::1", "host:8080", "[::1]:443", "bad host!"} {
		_, err := parsePathTarget(target)
		if err == nil {
			t.Fatalf("parsePathTarget(%q) accepted the target", target)
		}
		m, _ := traceFormWithTarget(t, target)
		m = pressTrace(t, m)
		if text, _ := footerError(m); text != capitalize(err.Error()) {
			t.Errorf("target %q: footer %q, want %q", target, text, capitalize(err.Error()))
		}
	}
}

// Bare hosts and IPv6 literals are never read as host:port.
func TestTraceFormBareTargetsAreNotPortErrors(t *testing.T) {
	for _, in := range []string{"host", "1.2.3.4", "::1", "[::1]"} {
		_, err := parsePathTarget(in)
		if err != nil && err.Error() == errPathPortText {
			t.Errorf("parsePathTarget(%q) = port error, want none", in)
		}
		m, _ := traceFormWithTarget(t, in)
		m = pressTrace(t, m)
		if text, _ := footerError(m); text == errPathPortText {
			t.Errorf("target %q: the form says %q", in, text)
		}
	}
	for _, in := range []string{"host", "1.2.3.4"} {
		if _, err := parsePathTarget(in); err != nil {
			t.Errorf("parsePathTarget(%q) error = %v, want accepted", in, err)
		}
	}
}

func TestTraceFormValidationNothingIsMarkedBeforeTheFirstRefusal(t *testing.T) {
	m, _ := traceFormWithTarget(t, "host:8080")
	if strings.Contains(m.targetInput.View(), curlyUnderline) {
		t.Error("the target is underlined before any submit")
	}
	if text, _ := footerError(m); text != "" {
		t.Errorf("footer = %q before any submit, want empty", text)
	}
}

// The error is a status derived on every frame, not a 3 s message.
func TestTraceFormValidationErrorSurvivesTheFooterTimer(t *testing.T) {
	m, _ := traceFormWithTarget(t, "host:8080")
	m = pressTrace(t, m)
	m = feed(t, m, components.ClearFooterMsg{ID: m.footer.ID()})

	if text, level := footerError(m); text != errPathPortText || level != components.LevelError {
		t.Errorf("after the footer expiry: {%q %v}, want {%q Error}", text, level, errPathPortText)
	}
	if !strings.Contains(m.targetInput.View(), curlyUnderline) {
		t.Error("the underline went away with the footer timer")
	}
}

func TestTraceFormValidationCorrectionClearsTheMarks(t *testing.T) {
	corrections := map[string]func(*testing.T, *Model) *Model{
		"typing": func(t *testing.T, m *Model) *Model {
			m.targetInput.SetValue("example")
			m.targetInput.CursorEnd()
			return typeInto(t, m, ".com")
		},
		"paste": func(t *testing.T, m *Model) *Model {
			m.targetInput.SetValue("")
			m.focusedField = fieldTarget
			m.targetInput.Focus()
			return feed(t, m, testutil.Paste("example.com"))
		},
	}
	for name, correct := range corrections {
		t.Run(name, func(t *testing.T) {
			m, _ := traceFormWithTarget(t, "host:8080")
			m = pressTrace(t, m)
			if !m.traceValidation.Invalid(traceKeyTarget) {
				t.Fatal("precondition: the refused trace must mark the target")
			}

			m = correct(t, m)
			if m.traceValidation.Invalid(traceKeyTarget) {
				t.Error("the target is still marked invalid after the correction")
			}
			if strings.Contains(m.targetInput.View(), curlyUnderline) {
				t.Error("the target is still underlined after the correction")
			}
			if text, _ := footerError(m); text != "" {
				t.Errorf("footer = %q after the correction, want empty", text)
			}
		})
	}
}

// Revalidation stays live: breaking the target again shows at once.
func TestTraceFormValidationStaysLiveAfterARefusal(t *testing.T) {
	m, _ := traceFormWithTarget(t, "")
	m = pressTrace(t, m)
	m = typeInto(t, m, "host:80")
	if !m.traceValidation.Invalid(traceKeyTarget) {
		t.Error("typing a port after a refusal did not mark the target")
	}
	if text, _ := footerError(m); text != errPathPortText {
		t.Errorf("footer = %q, want %q", text, errPathPortText)
	}
}

func TestTraceFormValidationAcceptedRunClearsTheError(t *testing.T) {
	m, _ := traceFormWithTarget(t, "host:8080")
	m = pressTrace(t, m)
	if !m.traceValidation.Invalid(traceKeyTarget) {
		t.Fatal("precondition: the refused trace must mark the target")
	}

	m.targetInput.SetValue("example.com")
	m.focusedField = fieldRunButton
	m = feed(t, m, testutil.Key("enter"))

	if m.state != StateRunning {
		t.Fatalf("state = %v, want StateRunning", m.state)
	}
	if m.traceValidation.Invalid(traceKeyTarget) {
		t.Error("an accepted run left the target marked invalid")
	}
	if strings.Contains(m.targetInput.View(), curlyUnderline) {
		t.Error("an accepted run left the target underlined")
	}
	// A running diagnostic shows its own progress label: only the refusal must go.
	if text, level := footerError(m); level == components.LevelError || text == errPathPortText {
		t.Errorf("footer = {%q %v} after an accepted run, want the refusal gone", text, level)
	}
}

func TestTraceFormValidationValidTargetPassesTheGate(t *testing.T) {
	m, _ := traceFormWithTarget(t, "example.com")
	m = pressTrace(t, m)
	if m.traceValidation.Invalid(traceKeyTarget) || strings.Contains(m.targetInput.View(), curlyUnderline) {
		t.Error("a valid target is marked invalid")
	}
	if m.state != StateTrace {
		t.Errorf("state = %v, want StateTrace", m.state)
	}
	// The trace sub-level opens, so the footer shows its progress; only a refusal is wrong.
	text, level := footerError(m)
	if level == components.LevelError || text == errPathPortText {
		t.Errorf("footer = {%q %v} for a valid target, want no refusal", text, level)
	}
}

// The validation and its marks must be the shared components' types, so a
// restyle only happens on a change of state (Rule 140).
func TestTraceFormValidationFieldTypes(t *testing.T) {
	m := &Model{}
	if _, ok := any(&m.traceValidation).(*components.Validation); !ok {
		t.Errorf("traceValidation is %T, want components.Validation", m.traceValidation)
	}
	if _, ok := any(&m.traceMarks).(*components.InvalidMarks); !ok {
		t.Errorf("traceMarks is %T, want components.InvalidMarks", m.traceMarks)
	}
}
