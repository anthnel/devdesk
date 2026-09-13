package setup

import (
	"errors"
	"io"
	"log"
	"os"
	"testing"

	tea "github.com/charmbracelet/bubbletea"

	"github.com/anthnel/devdesk/internal/config"
	"github.com/anthnel/devdesk/internal/ui/components"
	"github.com/anthnel/devdesk/internal/ui/testutil"
)

// checkContextExistsCmd, saveContextCmd and setCurrentContextCmd all read or
// write under the home directory config resolves against, so it is
// redirected for the whole package — the same reason internal/app does this.
func TestMain(m *testing.M) {
	log.SetOutput(io.Discard)

	home, err := os.MkdirTemp("", "devdesk-setup-test")
	if err != nil {
		log.SetOutput(os.Stderr)
		panic(err)
	}
	_ = os.Setenv("HOME", home)
	_ = os.Setenv("USERPROFILE", home)

	code := m.Run()

	_ = os.RemoveAll(home)
	log.SetOutput(os.Stderr)
	os.Exit(code)
}

// feed applies messages to the model in order, matching feedForm's shape in
// internal/ui/components' own tests.
func feed(t *testing.T, m Model, msgs ...tea.Msg) Model {
	t.Helper()
	for _, msg := range msgs {
		next, _ := m.Update(msg)
		m = next.(Model)
	}
	return m
}

func TestSubmitWithAnEmptyNameShowsAFooterError(t *testing.T) {
	m := New()

	m = feed(t, m, testutil.Keys("down", "down", "down", "down", "down", "down", "down", "down", "down", "down")...)
	if m.focusedField != fieldSubmit {
		t.Fatalf("focusedField = %d, want fieldSubmit (%d)", m.focusedField, fieldSubmit)
	}

	m = feed(t, m, testutil.Key("enter"))

	if !m.footer.IsSet() {
		t.Error("submitting an empty context name did not set a footer error")
	}
	if m.footer.Level() != components.LevelError {
		t.Errorf("footer level = %v, want LevelError", m.footer.Level())
	}
	if m.stage != stageForm {
		t.Errorf("stage = %v, want stageForm — an invalid name must not start the async chain", m.stage)
	}
}

func TestContextExistsOffersOverwriteAndNoReturnsToTheNameField(t *testing.T) {
	m := New()
	m.contextNameInput.SetValue("staging")

	m = feed(t, m, ContextExistsMsg{Exists: true})

	if m.stage != stageConfirmOverwrite {
		t.Fatalf("stage = %v, want stageConfirmOverwrite", m.stage)
	}
	if m.confirmModal == nil {
		t.Fatal("confirmModal is nil while stageConfirmOverwrite is active")
	}

	m = feed(t, m, components.ConfirmModalNoMsg{})

	if m.stage != stageForm {
		t.Errorf("stage = %v, want stageForm after declining the overwrite", m.stage)
	}
	if m.focusedField != fieldContextName {
		t.Errorf("focusedField = %d, want fieldContextName so the user can pick another name", m.focusedField)
	}
}

func TestContextExistsYesProceedsToSaveWhenNoTokenWasEntered(t *testing.T) {
	m := New()
	m.contextNameInput.SetValue("staging")

	m = feed(t, m, ContextExistsMsg{Exists: true})
	m = feed(t, m, components.ConfirmModalYesMsg{})

	if m.pending != "Saving context..." {
		t.Errorf("pending = %q, want the saving message (no token means validation is skipped)", m.pending)
	}
}

func TestSecretBackendCyclesAndWrapsBothDirections(t *testing.T) {
	m := New()
	m.focusedField = fieldSecretBackend

	m = feed(t, m, testutil.Key("right"))
	if got, want := secretBackendOptions[m.secretBackendIdx], "keyring"; got != want {
		t.Errorf("after one right, secretBackend = %q, want %q", got, want)
	}

	m = feed(t, m, testutil.Key("right"), testutil.Key("right"))
	if got, want := secretBackendOptions[m.secretBackendIdx], "auto"; got != want {
		t.Errorf("after wrapping all the way around (3 rights total), secretBackend = %q, want %q", got, want)
	}

	m = feed(t, m, testutil.Key("left"))
	if got, want := secretBackendOptions[m.secretBackendIdx], "git-credential"; got != want {
		t.Errorf("after left from auto, secretBackend = %q, want %q (wraps to the last option)", got, want)
	}
}

func TestSwitchingToGitHubCoercesAnInternalVisibilityToPrivate(t *testing.T) {
	m := New()
	m.focusedField = fieldForgeVisibility
	m = feed(t, m, testutil.Key("right")) // private -> internal
	if m.visibilityValue != "internal" {
		t.Fatalf("visibilityValue = %q, want internal", m.visibilityValue)
	}

	m.focusedField = fieldForgeType
	m = feed(t, m, testutil.Key("right")) // gitlab -> github

	if m.visibilityValue != "private" {
		t.Errorf("visibilityValue = %q after switching to GitHub, want private (most-private coercion, not a positional clamp)", m.visibilityValue)
	}
}

func TestForgeURLIsPrefilledButNotOverwrittenOnceTouched(t *testing.T) {
	m := New()
	if m.forgeURLInput.Value() == "" {
		t.Fatal("forgeURLInput is empty on a new model; expected the GitLab example URL")
	}

	m.forgeURLInput.SetValue("")

	// Navigate for real (down five times from the name field) so the input's
	// own Focus() is set — Update() ignores keystrokes on a blurred field.
	m = feed(t, m, testutil.Keys("down", "down", "down", "down", "down")...)
	if m.focusedField != fieldForgeURL {
		t.Fatalf("focusedField = %d, want fieldForgeURL (%d)", m.focusedField, fieldForgeURL)
	}
	m = feed(t, m, testutil.Type("https://git.internal")...)
	if !m.forgeURLTouched {
		t.Fatal("forgeURLTouched was not set after typing into the field")
	}

	m = feed(t, m, testutil.Key("up"))
	if m.focusedField != fieldForgeType {
		t.Fatalf("focusedField = %d, want fieldForgeType (%d)", m.focusedField, fieldForgeType)
	}
	m = feed(t, m, testutil.Key("right")) // gitlab -> github

	if m.forgeURLInput.Value() != "https://git.internal" {
		t.Errorf("forgeURLInput.Value() = %q, want the user's own value preserved", m.forgeURLInput.Value())
	}
}

func TestTokenValidationFailureKeepsFocusOnTheTokenFieldWithAFooterError(t *testing.T) {
	m := New()
	m.pending = "Validating token..."

	m = feed(t, m, TokenValidatedMsg{Err: errors.New("401 unauthorized")})

	if m.pending != "" {
		t.Errorf("pending = %q, want empty once validation returns", m.pending)
	}
	if m.focusedField != fieldForgeToken {
		t.Errorf("focusedField = %d, want fieldForgeToken so the user can retype it", m.focusedField)
	}
	if !m.footer.IsSet() || m.footer.Level() != components.LevelError {
		t.Error("a failed token validation must leave a footer error set")
	}
}

func TestSaveContextCmdWritesTheFileAndSetCurrentContextCmdPointsAtIt(t *testing.T) {
	m := New()
	m.contextNameInput.SetValue("integration-test")

	cfg := buildConfig(m)
	if msg, ok := testutil.MsgOf[ContextSavedMsg](saveContextCmd(cfg, "integration-test")); !ok || msg.Err != nil {
		t.Fatalf("saveContextCmd: ok=%v err=%v", ok, msg.Err)
	}

	exists, err := config.ContextExists("integration-test")
	if err != nil || !exists {
		t.Fatalf("ContextExists(integration-test) = %v, %v; want true, nil", exists, err)
	}

	if msg, ok := testutil.MsgOf[CurrentContextSetMsg](setCurrentContextCmd("integration-test")); !ok || msg.Err != nil {
		t.Fatalf("setCurrentContextCmd: ok=%v err=%v", ok, msg.Err)
	}

	current, err := config.GetCurrentContext()
	if err != nil || current != "integration-test" {
		t.Fatalf("GetCurrentContext() = %v, %v; want integration-test, nil", current, err)
	}
}

func TestCheckContextExistsCmdReportsFalseForAFreshName(t *testing.T) {
	msg, ok := testutil.MsgOf[ContextExistsMsg](checkContextExistsCmd("never-created"))
	if !ok {
		t.Fatal("checkContextExistsCmd returned no ContextExistsMsg")
	}
	if msg.Err != nil {
		t.Fatalf("Err = %v, want nil", msg.Err)
	}
	if msg.Exists {
		t.Error("Exists = true for a context that was never created")
	}
}
