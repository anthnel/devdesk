package setup

import (
	"errors"
	"io"
	"log"
	"net/http"
	"net/http/httptest"
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

// feed applies messages to the model in order.
func feed(t *testing.T, m Model, msgs ...tea.Msg) Model {
	t.Helper()
	for _, msg := range msgs {
		next, _ := m.Update(msg)
		m = next.(Model)
	}
	return m
}

// past advances the model past the font check and, optionally, sets a
// context name and answers it — the two steps almost every other test needs
// out of the way before it can exercise the step it actually cares about.
func past(t *testing.T, m Model, name string) Model {
	t.Helper()
	m = feed(t, m, testutil.Key("enter")) // font check -> context name
	if name != "" {
		m.contextNameInput.SetValue(name)
	}
	m = feed(t, m, ContextExistsMsg{Exists: false}) // as if submit+check already ran
	return m
}

func TestWizardStartsOnTheFontCheck(t *testing.T) {
	m := New()
	if m.step != stepFontCheck {
		t.Errorf("step = %d, want stepFontCheck (%d)", m.step, stepFontCheck)
	}
}

func TestEscOnTheFirstStepQuits(t *testing.T) {
	m := New()
	_, cmd := m.Update(testutil.Key("esc"))
	if _, ok := cmd().(tea.QuitMsg); !ok {
		t.Errorf("Esc on the font check step did not quit")
	}
}

func TestEnterWalksThroughTheFontCheckAndNameSteps(t *testing.T) {
	m := New()
	m = feed(t, m, testutil.Key("enter"))
	if m.step != stepContextName {
		t.Fatalf("step = %d, want stepContextName (%d)", m.step, stepContextName)
	}

	m = past(t, m, "staging")
	if m.step != stepSecretBackend {
		t.Errorf("step = %d, want stepSecretBackend (%d) once the name is free", m.step, stepSecretBackend)
	}
}

func TestEscGoesBackOneStepAtATime(t *testing.T) {
	m := New()
	m = past(t, m, "staging")
	if m.step != stepSecretBackend {
		t.Fatalf("setup: step = %d, want stepSecretBackend", m.step)
	}

	m = feed(t, m, testutil.Key("esc"))
	if m.step != stepContextName {
		t.Errorf("step = %d, want stepContextName after one esc", m.step)
	}
}

func TestSubmitWithAnEmptyNameShowsAFooterErrorAndDoesNotAdvance(t *testing.T) {
	m := New()
	m = feed(t, m, testutil.Key("enter")) // font check -> context name

	m = feed(t, m, testutil.Key("enter")) // submit the (empty) name

	if !m.footer.IsSet() || m.footer.Level() != components.LevelError {
		t.Error("submitting an empty context name did not set a footer error")
	}
	if m.step != stepContextName {
		t.Errorf("step = %d, want stepContextName — an invalid name must not advance", m.step)
	}
}

func TestContextExistsOffersOverwriteAndNoReturnsToTheNameStep(t *testing.T) {
	m := New()
	m = feed(t, m, testutil.Key("enter"))
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
	if m.step != stepContextName {
		t.Errorf("step = %d, want stepContextName so the user can pick another name", m.step)
	}
}

func TestContextExistsYesAdvancesToTheNextStep(t *testing.T) {
	m := New()
	m = feed(t, m, testutil.Key("enter"))
	m.contextNameInput.SetValue("staging")

	m = feed(t, m, ContextExistsMsg{Exists: true})
	m = feed(t, m, components.ConfirmModalYesMsg{})

	if m.step != stepSecretBackend {
		t.Errorf("step = %d, want stepSecretBackend after confirming the overwrite", m.step)
	}
}

func TestSecretBackendCyclesAndWrapsBothDirections(t *testing.T) {
	m := New()
	m = past(t, m, "staging")
	if m.step != stepSecretBackend {
		t.Fatalf("setup: step = %d, want stepSecretBackend", m.step)
	}

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
	m = past(t, m, "staging")
	m.step = stepForgeVisibility
	m = feed(t, m, testutil.Key("right")) // private -> internal
	if m.visibilityValue != "internal" {
		t.Fatalf("visibilityValue = %q, want internal", m.visibilityValue)
	}

	m.step = stepForgeType
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

	m = past(t, m, "staging")
	m.step = stepForgeURL
	m.updateFocus()

	m = feed(t, m, testutil.Type("https://git.internal")...)
	if !m.forgeURLTouched {
		t.Fatal("forgeURLTouched was not set after typing into the field")
	}

	m = feed(t, m, testutil.Key("esc")) // forgeURL -> forgeType
	if m.step != stepForgeType {
		t.Fatalf("step = %d, want stepForgeType", m.step)
	}
	m = feed(t, m, testutil.Key("right")) // gitlab -> github

	if m.forgeURLInput.Value() != "https://git.internal" {
		t.Errorf("forgeURLInput.Value() = %q, want the user's own value preserved", m.forgeURLInput.Value())
	}
}

func TestTokenValidationFailureReturnsToTheTokenStepWithAFooterError(t *testing.T) {
	m := New()
	m.step = stepForgeToken
	m.pending = "Validating token..."

	m = feed(t, m, TokenValidatedMsg{Err: errors.New("401 unauthorized")})

	if m.pending != "" {
		t.Errorf("pending = %q, want empty once validation returns", m.pending)
	}
	if m.step != stepForgeToken {
		t.Errorf("step = %d, want stepForgeToken so the user can retype it", m.step)
	}
	if !m.footer.IsSet() || m.footer.Level() != components.LevelError {
		t.Error("a failed token validation must leave a footer error set")
	}
}

func TestEnterOnTheLastStepWithNoTokenGoesStraightToSaving(t *testing.T) {
	m := New()
	m.step = stepForgeToken

	m = feed(t, m, testutil.Key("enter"))

	if m.pending != "Saving context..." {
		t.Errorf("pending = %q, want the saving message (no token means validation is skipped)", m.pending)
	}
}

func TestThemeFetchAddsNewThemesAndTheyBecomeCyclable(t *testing.T) {
	srv := httptest.NewServer(http.HandlerFunc(func(w http.ResponseWriter, r *http.Request) {
		_, _ = w.Write([]byte(`{"name":"test","color_ok":"#000000","color_error":"#000000","color_warn":"#000000","color_primary":"#000000","color_secondary":"#000000","color_border":"#000000","color_text":"#000000","color_dim":"#000000","color_highlight":"#000000","color_white":"#000000","color_black":"#000000","color_background":"#000000"}`))
	}))
	defer srv.Close()

	restore := remoteThemeBaseURL
	remoteThemeBaseURL = srv.URL + "/"
	t.Cleanup(func() { remoteThemeBaseURL = restore })

	before := New()
	beforeCount := len(before.themeOptions)

	msg, ok := testutil.MsgOf[RemoteThemesFetchedMsg](fetchRemoteThemesCmd())
	if !ok {
		t.Fatal("fetchRemoteThemesCmd returned no RemoteThemesFetchedMsg")
	}
	if msg.Unreachable {
		t.Fatal("Unreachable = true against a working test server")
	}
	if len(msg.Added) != len(knownRemoteThemes) {
		t.Fatalf("Added = %d themes, want %d", len(msg.Added), len(knownRemoteThemes))
	}

	m := feed(t, before, msg)
	if m.themeCheckState != themeCheckDone {
		t.Errorf("themeCheckState = %v, want themeCheckDone", m.themeCheckState)
	}
	if len(m.themeOptions) != beforeCount+len(knownRemoteThemes) {
		t.Errorf("themeOptions grew by %d, want %d", len(m.themeOptions)-beforeCount, len(knownRemoteThemes))
	}
}

func TestThemeFetchUnreachableFallsBackToTheDocsPointer(t *testing.T) {
	restore := remoteThemeBaseURL
	// A closed local port refuses the connection immediately — no need to
	// wait out remoteThemeFetchTimeout for this to fail.
	remoteThemeBaseURL = "http://127.0.0.1:1/"
	t.Cleanup(func() { remoteThemeBaseURL = restore })

	m := New()
	m = feed(t, m, RemoteThemesFetchedMsg{Unreachable: true})

	if m.themeCheckState != themeCheckUnreachable {
		t.Errorf("themeCheckState = %v, want themeCheckUnreachable", m.themeCheckState)
	}
	_, body, _ := m.currentQuestionAt(stepTheme)
	if body == "" {
		t.Fatal("theme question body is empty")
	}
}

// currentQuestionAt is a small test helper: currentQuestion always reads
// m.step, so this pins it long enough to read another step's content.
func (m Model) currentQuestionAt(step int) (string, string, string) {
	m.step = step
	return m.currentQuestion()
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
