package setup

import (
	"fmt"
	"reflect"
	"runtime"
	"strings"
	"testing"

	tea "charm.land/bubbletea/v2"
	"github.com/charmbracelet/x/ansi"

	"github.com/anthnel/devdesk/internal/ui/testutil"
)

// DEVDESK-376: dk setup has no router, so no jobs registry and no blink: a
// verification in flight shows a fixed "●" followed by its text, and nothing
// ticks. The wizard no longer owns a braille spinner.

func hasBraille(s string) bool {
	for _, r := range s {
		if r >= 0x2800 && r <= 0x28FF {
			return true
		}
	}
	return false
}

// requireNoTick fails when cmd is not one of the wizard's own verification
// Cmds. It never runs cmd (a verification touches the keyring, the engine
// daemon and a global): it reads the name of the function behind it. A
// tea.Batch or a spinner tick lives in another package, so a tick added next
// to a verification is caught without executing anything.
func requireNoTick(t *testing.T, what string, cmd tea.Cmd) {
	t.Helper()
	if cmd == nil {
		return
	}
	fn := runtime.FuncForPC(reflect.ValueOf(cmd).Pointer())
	if fn == nil {
		t.Errorf("%s: cannot resolve the Cmd's function", what)
		return
	}
	own := reflect.TypeOf(Model{}).PkgPath() + "."
	// textinput.Blink is the text cursor's blink (Init), not a loading tick.
	if name := fn.Name(); !strings.HasPrefix(name, own) && name != "charm.land/bubbles/v2/textinput.Blink" {
		t.Errorf("%s: Cmd comes from %s, want a single verification of package setup (no batch, no tick)", what, name)
	}
}

func TestSecretBackendCheckShowsTheFixedDotAndText(t *testing.T) {
	m := New()
	m.width, m.height = 100, 30
	m = past(t, m, "staging")
	if m.secretBackendCheck != secretBackendChecking {
		t.Fatalf("secretBackendCheck = %v, want checking", m.secretBackendCheck)
	}
	out := ansi.Strip(m.Render())
	if !strings.Contains(out, "● Checking whether this is reachable...") {
		t.Errorf("secret backend question lacks %q:\n%s", "● Checking whether this is reachable...", out)
	}
	if hasBraille(out) {
		t.Errorf("render holds a braille glyph:\n%s", out)
	}
}

func TestContainerEngineCheckShowsTheFixedDotAndText(t *testing.T) {
	m := New()
	m.width, m.height = 100, 30
	m = past(t, m, "staging")
	m = feed(t, m, SecretBackendCheckedMsg{Persists: true, Detail: "ok"})
	m = feed(t, m, testutil.Key("enter"))
	if m.engineCheck != engineChecking {
		t.Fatalf("engineCheck = %v, want checking", m.engineCheck)
	}
	out := ansi.Strip(m.Render())
	if !strings.Contains(out, "● Checking whether it's running...") {
		t.Errorf("container engine question lacks %q:\n%s", "● Checking whether it's running...", out)
	}
	if hasBraille(out) {
		t.Errorf("render holds a braille glyph:\n%s", out)
	}
}

func TestContextExistenceCheckShowsTheFixedDotInTheFooterLine(t *testing.T) {
	isolateHome(t)
	m := New()
	m.width, m.height = 100, 30
	m = feed(t, m, testutil.Key("enter")) // font check -> context name
	m.contextNameInput.SetValue("staging")
	m = feed(t, m, testutil.Key("enter"))
	if m.pending == "" {
		t.Fatal("the existence check did not start")
	}
	line := ansi.Strip(m.renderFooterLine())
	if want := "● " + m.pending; line != want {
		t.Errorf("footer line = %q, want %q", line, want)
	}
	if hasBraille(ansi.Strip(m.Render())) {
		t.Error("render holds a braille glyph")
	}
}

func TestVerificationEndRemovesTheIndicatorAndAsksTheNextQuestion(t *testing.T) {
	isolateHome(t)
	m := New()
	m.width, m.height = 100, 30
	m = feed(t, m, testutil.Key("enter"))
	m.contextNameInput.SetValue("staging")
	m = feed(t, m, testutil.Key("enter"))
	m = feed(t, m, ContextExistsMsg{Exists: false})
	if m.pending != "" {
		t.Errorf("pending = %q after the check ended", m.pending)
	}
	if got := ansi.Strip(m.renderFooterLine()); strings.Contains(got, "●") {
		t.Errorf("footer line still shows the dot: %q", got)
	}
	if m.step != stepSecretBackend {
		t.Fatalf("step = %d, want stepSecretBackend (next question)", m.step)
	}

	m = feed(t, m, SecretBackendCheckedMsg{Persists: true, Detail: "Stored in the OS keyring."})
	out := ansi.Strip(m.Render())
	if strings.Contains(out, "Checking whether this is reachable") || strings.Contains(out, "●") {
		t.Errorf("indicator still shown after the secret backend check:\n%s", out)
	}
	if !strings.Contains(out, "Stored in the OS keyring.") {
		t.Errorf("check result missing:\n%s", out)
	}

	m = feed(t, m, testutil.Key("enter"))
	m = feed(t, m, ContainerEngineCheckedMsg{Name: "docker"})
	if out := ansi.Strip(m.Render()); strings.Contains(out, "Checking whether it's running") || strings.Contains(out, "●") {
		t.Errorf("indicator still shown after the engine check:\n%s", out)
	}
}

func TestNoTickAnywhereInTheWizard(t *testing.T) {
	isolateHome(t)

	t.Run("the model owns no spinner", func(t *testing.T) {
		if _, ok := reflect.TypeOf(Model{}).FieldByName("spinner"); ok {
			t.Error("Model still has a spinner field")
		}
	})

	t.Run("Init", func(t *testing.T) {
		requireNoTick(t, "Init", New().Init())
	})

	t.Run("idle updates", func(t *testing.T) {
		m := New()
		for _, msg := range []tea.Msg{tea.WindowSizeMsg{Width: 80, Height: 24}, testutil.Key("down")} {
			_, cmd := m.Update(msg)
			requireNoTick(t, fmt.Sprintf("%T when idle", msg), cmd)
		}
	})

	t.Run("secret backend check", func(t *testing.T) {
		m := New()
		m = feed(t, m, testutil.Key("enter"))
		m.contextNameInput.SetValue("staging")
		_, cmd := m.Update(ContextExistsMsg{Exists: false})
		requireNoTick(t, "arriving at the secret backend step", cmd)
	})

	t.Run("container engine check", func(t *testing.T) {
		m := New()
		m = past(t, m, "staging")
		m = feed(t, m, SecretBackendCheckedMsg{Persists: true, Detail: "ok"})
		_, cmd := m.Update(testutil.Key("enter"))
		requireNoTick(t, "arriving at the container engine step", cmd)
	})

	t.Run("context name check", func(t *testing.T) {
		m := New()
		m = feed(t, m, testutil.Key("enter"))
		m.contextNameInput.SetValue("staging")
		_, cmd := m.Update(testutil.Key("enter"))
		requireNoTick(t, "submitting the context name", cmd)
	})

	t.Run("after a verification ended", func(t *testing.T) {
		m := New()
		m = past(t, m, "staging")
		_, cmd := m.Update(SecretBackendCheckedMsg{Persists: true, Detail: "ok"})
		requireNoTick(t, "SecretBackendCheckedMsg", cmd)
		_, cmd = m.Update(ContainerEngineCheckedMsg{Name: "docker"})
		requireNoTick(t, "ContainerEngineCheckedMsg", cmd)
	})
}
