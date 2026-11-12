package auth

import (
	"errors"
	"strings"
	"testing"

	"github.com/charmbracelet/x/ansi"

	"github.com/anthnel/devdesk/internal/jobs"
	"github.com/anthnel/devdesk/internal/ui/indicator"
	"github.com/anthnel/devdesk/internal/ui/testutil"
)

// DEVDESK-375: "Authenticating..." is a footer status with the shared activity
// dot, driven by the router frame; the body keeps its form (Rule 128).

func authFooterPlain(m *Model) string { return ansi.Strip(m.RenderFooter(160)) }

func authDots(s string) (on, off int) {
	return strings.Count(s, indicator.Glyph(indicator.Running, true)),
		strings.Count(s, indicator.Glyph(indicator.Running, false))
}

// blinker is the contract the router reads to arm its animation chain. It is
// asserted at run time so a missing method is a failed assertion, not a build error.
type blinker interface{ WantsBlink() bool }

func wantsBlink(t *testing.T, m *Model) bool {
	t.Helper()
	b, ok := any(m).(blinker)
	if !ok {
		t.Error("Model has no WantsBlink() bool: the router cannot keep the dot blinking")
		return false
	}
	return b.WantsBlink()
}

func authenticatingModel(t *testing.T) *Model {
	t.Helper()
	m := newTestModel(t, testConfig(), newFakeStorage())
	m, _ = step(t, m, AuthStartMsg{})
	return m
}

func TestAuthStartShowsTheDotAndTheTextInTheFooter(t *testing.T) {
	m := authenticatingModel(t)

	plain := authFooterPlain(m)
	if !strings.Contains(plain, "Authenticating...") {
		t.Errorf("the footer does not say Authenticating...\n%s", plain)
	}
	if on, off := authDots(plain); on+off != 1 {
		t.Errorf("want exactly one dot in the footer, got on=%d off=%d\n%s", on, off, plain)
	}
	if !wantsBlink(t, m) {
		t.Error("WantsBlink() = false while authenticating")
	}
}

func TestTheBodyKeepsTheFormWhileAuthenticating(t *testing.T) {
	m := authenticatingModel(t)

	out := ansi.Strip(m.Render())
	if strings.Contains(out, "Authenticating...") {
		t.Error("the body still carries Authenticating...: it belongs to the footer")
	}
	if !strings.Contains(out, "Personal Access Token") || !strings.Contains(out, "Login") {
		t.Errorf("the form is gone from the body while authenticating\n%s", out)
	}
}

func TestNoBrailleWhileAuthenticating(t *testing.T) {
	m := authenticatingModel(t)

	for name, out := range map[string]string{"body": m.Render(), "footer": m.RenderFooter(160)} {
		for _, r := range out {
			if r >= 0x2800 && r <= 0x28FF {
				t.Errorf("%s carries a braille glyph %q", name, r)
				break
			}
		}
	}
}

func TestTheFrameMovesTheFooterDot(t *testing.T) {
	m := authenticatingModel(t)
	m, _ = step(t, m, jobs.FrameMsg{Blink: true})
	on, _ := authDots(authFooterPlain(m))
	m, _ = step(t, m, jobs.FrameMsg{Blink: false})
	_, off := authDots(authFooterPlain(m))
	if on != 1 || off != 1 {
		t.Errorf("the dot did not follow the frame: on in phase true = %d, off in phase false = %d", on, off)
	}
}

func TestAuthResultClearsTheDot(t *testing.T) {
	m := authenticatingModel(t)

	m, _ = step(t, m, AuthResultMsg{Error: errors.New("401 Unauthorized")})

	plain := authFooterPlain(m)
	if on, off := authDots(plain); on+off != 0 {
		t.Errorf("the dot outlived the result: on=%d off=%d\n%s", on, off, plain)
	}
	if strings.Contains(plain, "Authenticating...") {
		t.Error("Authenticating... outlived the result")
	}
	if wantsBlink(t, m) {
		t.Error("WantsBlink() = true after the result")
	}
	if !strings.Contains(ansi.Strip(m.Render()), "401") {
		t.Error("the error is no longer rendered after a failed login")
	}
}

func TestIdleModelHasNoDotAndNoBlink(t *testing.T) {
	m := newTestModel(t, testConfig(), newFakeStorage())
	if on, off := authDots(authFooterPlain(m)); on+off != 0 {
		t.Errorf("an idle form shows a dot: on=%d off=%d", on, off)
	}
	if wantsBlink(t, m) {
		t.Error("an idle model asks for the blink")
	}
}

func TestEnterIsIgnoredWhileAuthenticating(t *testing.T) {
	m := authenticatingModel(t)
	m.tokenInput.SetValue("glpat-x")
	setConfigURL(m, "https://gitlab.example.com")

	m, cmd := step(t, m, testutil.Key("enter"))

	if cmd != nil {
		t.Error("enter issued a command while authentication was in flight")
	}
	if !m.authenticating {
		t.Error("enter stopped the authentication")
	}
}

func TestARefusedSubmitKeepsTheValidationErrorAndNoDot(t *testing.T) {
	m := newTestModel(t, testConfig(), newFakeStorage())
	setConfigURL(m, "https://gitlab.example.com")
	m.tokenInput.SetValue("")
	m.currentField = fieldSubmit

	m, _ = step(t, m, testutil.Key("enter"))

	plain := authFooterPlain(m)
	if on, off := authDots(plain); on+off != 0 {
		t.Errorf("a refused submit shows a dot: on=%d off=%d", on, off)
	}
	if strings.TrimSpace(plain) == "" || strings.Contains(plain, "Authenticating") {
		t.Errorf("the footer lost the validation error\n%s", plain)
	}
}
