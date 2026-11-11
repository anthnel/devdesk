package auth

import (
	tea "charm.land/bubbletea/v2"
	"strings"
	"testing"

	"github.com/charmbracelet/x/ansi"

	"github.com/anthnel/devdesk/internal/ui/components"
	"github.com/anthnel/devdesk/internal/ui/testutil"
	"github.com/anthnel/devdesk/internal/ui/theme"
)

// DEVDESK-292 step 4d: an empty token refused at submit underlines the field
// (its placeholder) and puts "Token is required" in the footer at the Error
// level. Nothing shows before the first refused submit.

const (
	tokenRequired = "Token is required"
	curlyMark     = "4:3" // SGR parameter of a curly underline
)

// submitEmpty drives the form to a refused submit: the URL is configured, the
// token is empty, focus goes to Login and enter is pressed.
func submitEmpty(t *testing.T) (*Model, tea.Cmd) {
	t.Helper()
	m := newTestModel(t, testConfig(), newFakeStorage())
	setConfigURL(m, "https://gitlab.example.com")
	m = feed(t, m, testutil.Keys("down")...)
	if m.currentField != fieldSubmit {
		t.Fatalf("currentField = %d, want the Login button", m.currentField)
	}
	return step(t, m, testutil.Key("enter"))
}

// errorSGR is the escape prefix FooterErrorStyle emits.
func errorSGR() string {
	out := theme.FooterErrorStyle.Render("x")
	return out[:strings.Index(out, "x")]
}

func TestEmptyTokenSubmitUnderlinesThePlaceholderAndWarnsInTheFooter(t *testing.T) {
	m, _ := submitEmpty(t)

	if !strings.Contains(m.Render(), curlyMark) {
		t.Error("Render() has no curly underline on the empty token after a refused submit")
	}
	line := footerInfoLine(m, 100)
	if !strings.Contains(ansi.Strip(line), tokenRequired) {
		t.Fatalf("footer info line = %q, want %q", ansi.Strip(line), tokenRequired)
	}
	if !strings.Contains(line, errorSGR()) {
		t.Errorf("footer message is not at the Error level: %q", line)
	}
	if strings.Contains(m.Render(), tokenRequired) {
		t.Error("the message is in the body, it belongs to the footer")
	}
}

// Regression: authenticating used to turn true before authenticate() returned
// nil, so the "Authenticating..." spinner stayed up for ever.
func TestEmptyTokenSubmitDoesNotStartAnAuthentication(t *testing.T) {
	m, cmd := submitEmpty(t)

	if m.authenticating {
		t.Error("authenticating = true after a refused submit: the spinner never stops")
	}
	if strings.Contains(m.Render(), "Authenticating") {
		t.Error("Render() shows the Authenticating spinner after a refused submit")
	}
	if cmd != nil {
		t.Errorf("a refused submit returned a command (%v messages), want none", len(testutil.Msgs(cmd)))
	}
	if !strings.Contains(m.Render(), "Personal Access Token") {
		t.Error("the form is gone after a refused submit")
	}
	// The keyboard still works: the spinner state would have swallowed keys.
	m = feed(t, m, testutil.Keys("up")...)
	if m.currentField != fieldToken {
		t.Errorf("currentField = %d after up, keys are being ignored", m.currentField)
	}
}

func TestNothingIsInvalidBeforeTheFirstSubmit(t *testing.T) {
	m := newTestModel(t, testConfig(), newFakeStorage())
	setConfigURL(m, "https://gitlab.example.com")

	check := func(step string) {
		t.Helper()
		if strings.Contains(m.Render(), curlyMark) {
			t.Errorf("%s: curly underline before any submit", step)
		}
		if strings.TrimSpace(ansi.Strip(m.RenderFooter(100))) != "" {
			t.Errorf("%s: footer text before any submit: %q", step, ansi.Strip(m.RenderFooter(100)))
		}
	}
	check("fresh form")

	// Typing then clearing the token is not a submit.
	m = feed(t, m, testutil.Type("ab")...)
	m = feed(t, m, testutil.Keys("backspace", "backspace")...)
	check("token typed then cleared")

	// Enter on the token field only advances.
	m = feed(t, m, testutil.Key("enter"))
	check("enter on the token field")
}

func TestTypingATokenClearsTheMark(t *testing.T) {
	m, _ := submitEmpty(t)
	m = feed(t, m, testutil.Keys("up")...)
	m = feed(t, m, testutil.Type("g")...)

	if strings.Contains(m.Render(), curlyMark) {
		t.Error("curly underline still there after typing a token")
	}
	if strings.TrimSpace(ansi.Strip(m.RenderFooter(100))) != "" {
		t.Errorf("footer = %q after typing a token, want empty", ansi.Strip(m.RenderFooter(100)))
	}
}

func TestPastingATokenClearsTheMark(t *testing.T) {
	m, _ := submitEmpty(t)
	m = feed(t, m, testutil.Keys("up")...)
	m = feed(t, m, testutil.Paste("glpat-abc"))

	if strings.Contains(m.Render(), curlyMark) {
		t.Error("curly underline still there after pasting a token")
	}
	if strings.Contains(ansi.Strip(m.RenderFooter(100)), tokenRequired) {
		t.Error("footer still says the token is required after a paste")
	}
}

// Emptying the field again after a refusal brings the mark back at once: the
// form stays live once a submit was refused.
func TestClearingTheTokenAfterARefusalMarksItAgain(t *testing.T) {
	m, _ := submitEmpty(t)
	m = feed(t, m, testutil.Keys("up")...)
	m = feed(t, m, testutil.Type("g")...)
	m = feed(t, m, testutil.Key("backspace"))

	if !strings.Contains(m.Render(), curlyMark) {
		t.Error("no curly underline after emptying the token again")
	}
	if !strings.Contains(ansi.Strip(m.RenderFooter(100)), tokenRequired) {
		t.Error("footer lost the message after emptying the token again")
	}
}

// The message is derived at every render: it does not expire like a timed
// footer message.
func TestTheValidationMessageDoesNotExpire(t *testing.T) {
	m, _ := submitEmpty(t)
	m = feed(t, m, clearFooterFor(m))

	if !strings.Contains(ansi.Strip(m.RenderFooter(100)), tokenRequired) {
		t.Error("the validation message expired with the footer timer")
	}
}

// A masked, whitespace-only token is empty once trimmed, so it is refused; the
// underline lands on the bullets and never on the clear text.
func TestWhitespaceOnlyTokenIsRefusedAndStaysMasked(t *testing.T) {
	m := newTestModel(t, testConfig(), newFakeStorage())
	setConfigURL(m, "https://gitlab.example.com")
	m = feed(t, m, testutil.Type("   ")...)
	m = feed(t, m, testutil.Keys("down")...)
	m, _ = step(t, m, testutil.Key("enter"))

	if m.authenticating {
		t.Error("a blank token started an authentication")
	}
	out := m.Render()
	if !strings.Contains(out, curlyMark) {
		t.Error("no curly underline on a blank token after a refused submit")
	}
	if !strings.Contains(ansi.Strip(m.RenderFooter(100)), tokenRequired) {
		t.Error("footer does not name the token")
	}
}

func TestInvalidTokenKeepsTheFooterHeight(t *testing.T) {
	m, _ := submitEmpty(t)

	if got, want := strings.Count(m.RenderFooter(100), "\n")+1, m.GetFooterHeight(); got != want {
		t.Errorf("RenderFooter() emitted %d lines, GetFooterHeight() = %d", got, want)
	}
	if got := ansi.StringWidth(footerInfoLine(m, 100)); got != 100 {
		t.Errorf("info line width = %d, want 100", got)
	}
}

// The mark changes colour only: the text and every line width stay the same.
func TestInvalidMarkAddsNoCell(t *testing.T) {
	valid := newTestModel(t, testConfig(), newFakeStorage())
	setConfigURL(valid, "https://gitlab.example.com")
	invalid, _ := submitEmpty(t)

	if a, b := ansi.Strip(valid.Render()), ansi.Strip(invalid.Render()); a != b {
		t.Errorf("rendered text differs between valid and invalid:\n%q\n%q", a, b)
	}
}

// The missing-URL check stays in the body (m.error): it is not a field error.
func TestMissingURLStillReportsInTheBody(t *testing.T) {
	m := newTestModel(t, testConfig(), newFakeStorage())
	m.tokenInput.SetValue("glpat-x")
	m.currentField = fieldSubmit

	m, _ = step(t, m, testutil.Key("enter"))
	if m.error == "" {
		t.Error("no error for a missing URL")
	}
	if strings.Contains(m.Render(), curlyMark) {
		t.Error("a missing URL underlined the token")
	}
}

// clearFooterFor builds the expiry of the timed footer message instead of
// sleeping on its timer.
func clearFooterFor(m *Model) tea.Msg { return components.ClearFooterMsg{ID: m.footer.ID()} }

// DEVDESK-292: typing in the token field must not restyle it unless its
// invalid state changed (a restyle resets the cursor). The italic placeholder
// is a marker a restyle would wipe.
func TestTypingDoesNotRestyleATokenWhoseStateDidNotChange(t *testing.T) {
	m := newTestModel(t, testConfig(), newFakeStorage())
	styles := m.tokenInput.Styles()
	styles.Focused.Placeholder = styles.Focused.Placeholder.Italic(true)
	m.tokenInput.SetStyles(styles)

	m = feed(t, m, testutil.Type("abc")...)
	if !m.tokenInput.Styles().Focused.Placeholder.GetItalic() {
		t.Error("the token field was restyled although its invalid state did not change")
	}
}
