package auth

import (
	"errors"
	"io"
	"log"
	"os"
	"strings"
	"testing"

	"github.com/charmbracelet/bubbles/spinner"
	tea "github.com/charmbracelet/bubbletea"
	gitlabclient "gitlab.com/gitlab-org/api/client-go"

	"github.com/anthnel/devdesk/internal/config"
	"github.com/anthnel/devdesk/internal/ui/testutil"
)

// The only Cmds here — authenticate() and logout() — reach the GitLab API and
// the git credential helper, so no test executes one. loadSavedCredentials()
// is the exception: it only reads the config and the injected storage, so it is
// run directly.

// This view logs its whole flow at INFO level.
func TestMain(m *testing.M) {
	log.SetOutput(io.Discard)
	code := m.Run()
	log.SetOutput(os.Stderr)
	os.Exit(code)
}

// Field indices in the login form.
const (
	fieldURL          = 0
	fieldToken        = 1
	fieldSaveToHelper = 2
	fieldSaveToConfig = 3
	fieldLoginButton  = 4
	fieldLogoutButton = 0 // the logged-in view has a single button
)

// fakeStorage records what the view asks of the credential store.
type fakeStorage struct {
	token     string
	loadErr   error
	saved     map[string]string
	deleted   []string
	loadCalls []string
}

func newFakeStorage() *fakeStorage {
	return &fakeStorage{saved: map[string]string{}}
}

func (f *fakeStorage) Save(url, token string) error {
	f.saved[url] = token
	return nil
}

func (f *fakeStorage) Load(url string) (string, error) {
	f.loadCalls = append(f.loadCalls, url)
	return f.token, f.loadErr
}

func (f *fakeStorage) Delete(url string) error {
	f.deleted = append(f.deleted, url)
	return nil
}

func testConfig() *config.Config {
	cfg := config.Default()
	cfg.GitLab.URL = ""
	cfg.GitLab.Token = ""
	return cfg
}

func newTestModel(t *testing.T, cfg *config.Config, storage *fakeStorage) *Model {
	t.Helper()
	m := New(cfg, storage)
	return feed(t, m, tea.WindowSizeMsg{Width: 100, Height: 30})
}

func feed(t *testing.T, m *Model, msgs ...tea.Msg) *Model {
	t.Helper()
	for _, msg := range msgs {
		m, _ = step(t, m, msg)
	}
	return m
}

func step(t *testing.T, m *Model, msg tea.Msg) (*Model, tea.Cmd) {
	t.Helper()
	next, cmd := m.Update(msg)
	updated, ok := next.(*Model)
	if !ok {
		t.Fatalf("Update() returned %T, want *auth.Model", next)
	}
	return updated, cmd
}

func testUser() *gitlabclient.User {
	return &gitlabclient.User{ID: 7, Username: "anthoni", Name: "Anthoni D"}
}

// ── Construction ─────────────────────────────────────────────────────────────

func TestNewPrefillsTheURLFromConfig(t *testing.T) {
	cfg := testConfig()
	cfg.GitLab.URL = "https://gitlab.example.com"

	m := newTestModel(t, cfg, newFakeStorage())

	if got := m.urlInput.Value(); got != "https://gitlab.example.com" {
		t.Errorf("URL input = %q, want it prefilled from the config", got)
	}
	if m.tokenInput.Value() != "" {
		t.Error("the token input was prefilled; it must be typed or loaded, never guessed")
	}
}

func TestNewDefaultsToTheSecureSaveOption(t *testing.T) {
	m := newTestModel(t, testConfig(), newFakeStorage())

	if m.saveOption != SaveToHelper {
		t.Error("the default save option is the config file; the credential helper is the safe default")
	}
	if m.currentField != fieldURL {
		t.Errorf("currentField = %d on a new form, want the URL field", m.currentField)
	}
	if !m.urlInput.Focused() {
		t.Error("the URL input is not focused on a new form")
	}
}

// The token must never be echoed: this view is often on a shared screen.
func TestTokenInputIsMasked(t *testing.T) {
	m := newTestModel(t, testConfig(), newFakeStorage())

	m.tokenInput.SetValue("glpat-supersecret")

	if rendered := m.tokenInput.View(); strings.Contains(rendered, "supersecret") {
		t.Errorf("the token input rendered its value in clear: %q", rendered)
	}
}

func TestInitLoadsSavedCredentials(t *testing.T) {
	if cmd := New(testConfig(), newFakeStorage()).Init(); cmd == nil {
		t.Fatal("Init() returned no command, so saved credentials are never loaded")
	}
}

// ── Credential loading ───────────────────────────────────────────────────────

// The config wins over the credential helper: a token written there is an
// explicit choice by the user.
func TestLoadSavedCredentialsPrefersTheConfig(t *testing.T) {
	cfg := testConfig()
	cfg.GitLab.URL = "https://gitlab.example.com"
	cfg.GitLab.Token = "glpat-from-config"
	storage := newFakeStorage()
	storage.token = "glpat-from-storage"

	msg := New(cfg, storage).loadSavedCredentials()().(CredentialsLoadedMsg)

	if msg.Source != "config" {
		t.Errorf("Source = %q, want \"config\"", msg.Source)
	}
	if msg.Token != "glpat-from-config" {
		t.Errorf("Token = %q, want the config's", msg.Token)
	}
	if len(storage.loadCalls) != 0 {
		t.Error("the credential helper was queried even though the config had a token")
	}
}

func TestLoadSavedCredentialsFallsBackToStorage(t *testing.T) {
	cfg := testConfig()
	cfg.GitLab.URL = "https://gitlab.example.com"
	storage := newFakeStorage()
	storage.token = "glpat-from-storage"

	msg := New(cfg, storage).loadSavedCredentials()().(CredentialsLoadedMsg)

	if msg.Source != "storage" {
		t.Errorf("Source = %q, want \"storage\"", msg.Source)
	}
	if msg.Token != "glpat-from-storage" {
		t.Errorf("Token = %q, want the stored one", msg.Token)
	}
	if len(storage.loadCalls) != 1 || storage.loadCalls[0] != "https://gitlab.example.com" {
		t.Errorf("the helper was queried with %v, want the configured URL once", storage.loadCalls)
	}
}

func TestLoadSavedCredentialsReportsNothingFound(t *testing.T) {
	tests := []struct {
		name    string
		url     string
		storage *fakeStorage
	}{
		{"no URL configured", "", newFakeStorage()},
		{"helper has no token", "https://gitlab.example.com", newFakeStorage()},
		{"helper errors", "https://gitlab.example.com", &fakeStorage{loadErr: errors.New("helper unavailable"), saved: map[string]string{}}},
		{"no storage at all", "https://gitlab.example.com", nil},
	}

	for _, tc := range tests {
		t.Run(tc.name, func(t *testing.T) {
			cfg := testConfig()
			cfg.GitLab.URL = tc.url

			var m *Model
			if tc.storage == nil {
				m = New(cfg, nil)
			} else {
				m = New(cfg, tc.storage)
			}

			msg := m.loadSavedCredentials()().(CredentialsLoadedMsg)

			if msg.Token != "" || msg.Source != "" {
				t.Errorf("got %+v, want an empty result", msg)
			}
		})
	}
}

// Loaded credentials trigger an auto-login rather than waiting for the user to
// press Login on a prefilled form.
func TestCredentialsLoadedStartsAnAutoLogin(t *testing.T) {
	m := newTestModel(t, testConfig(), newFakeStorage())

	m, cmd := step(t, m, CredentialsLoadedMsg{URL: "https://gitlab.example.com", Token: "glpat-x", Source: "storage"})

	if !m.authenticating {
		t.Error("authenticating = false after credentials were loaded")
	}
	if cmd == nil {
		t.Error("no command issued, so the auto-login never runs")
	}
	if m.urlInput.Value() != "https://gitlab.example.com" || m.tokenInput.Value() != "glpat-x" {
		t.Error("the loaded credentials were not written into the form")
	}
}

func TestIncompleteCredentialsDoNotAutoLogin(t *testing.T) {
	for _, tc := range []struct {
		name string
		msg  CredentialsLoadedMsg
	}{
		{"nothing found", CredentialsLoadedMsg{}},
		{"url only", CredentialsLoadedMsg{URL: "https://gitlab.example.com"}},
		{"token only", CredentialsLoadedMsg{Token: "glpat-x"}},
	} {
		t.Run(tc.name, func(t *testing.T) {
			m := newTestModel(t, testConfig(), newFakeStorage())

			m, cmd := step(t, m, tc.msg)

			if m.authenticating {
				t.Error("an auto-login started from incomplete credentials")
			}
			if cmd != nil {
				t.Error("a command was issued for incomplete credentials")
			}
		})
	}
}

// ── Navigation ───────────────────────────────────────────────────────────────

func TestVerticalNavigationClampsAtBothEnds(t *testing.T) {
	m := newTestModel(t, testConfig(), newFakeStorage())

	for i := 0; i < 10; i++ {
		m = feed(t, m, testutil.Key("down"))
	}
	if m.currentField != fieldLoginButton {
		t.Errorf("currentField = %d after repeated down, want %d (Login)", m.currentField, fieldLoginButton)
	}

	for i := 0; i < 10; i++ {
		m = feed(t, m, testutil.Key("up"))
	}
	if m.currentField != fieldURL {
		t.Errorf("currentField = %d after repeated up, want %d (URL)", m.currentField, fieldURL)
	}
}

func TestFocusFollowsTheCurrentField(t *testing.T) {
	m := newTestModel(t, testConfig(), newFakeStorage())

	m = feed(t, m, testutil.Key("down"))
	if !m.tokenInput.Focused() || m.urlInput.Focused() {
		t.Error("focus did not move from the URL to the token input")
	}

	// The radio buttons and the submit button hold no text input.
	m = feed(t, m, testutil.Key("down"))
	if m.urlInput.Focused() || m.tokenInput.Focused() {
		t.Error("an input kept focus on the save-option radio buttons")
	}
}

func TestTypingReachesTheFocusedInput(t *testing.T) {
	m := newTestModel(t, testConfig(), newFakeStorage())

	m = feed(t, m, testutil.Type("https://git.example.com")...)
	if got := m.urlInput.Value(); got != "https://git.example.com" {
		t.Errorf("URL input = %q after typing, want the typed value", got)
	}

	m = feed(t, m, testutil.Key("down"))
	m = feed(t, m, testutil.Type("glpat-abc")...)
	if got := m.tokenInput.Value(); got != "glpat-abc" {
		t.Errorf("token input = %q after typing, want the typed value", got)
	}
	if m.urlInput.Value() != "https://git.example.com" {
		t.Error("typing into the token field also changed the URL")
	}
}

// ── Save options ─────────────────────────────────────────────────────────────

func TestSaveOptionShortcuts(t *testing.T) {
	m := newTestModel(t, testConfig(), newFakeStorage())

	m = feed(t, m, testutil.Key("ctrl+f"))
	if m.saveOption != SaveToConfig {
		t.Error("ctrl+f did not select the config file")
	}

	m = feed(t, m, testutil.Key("ctrl+s"))
	if m.saveOption != SaveToHelper {
		t.Error("ctrl+s did not select the credential helper")
	}
}

// Rule 135: space selects, on the radio button that has focus.
func TestSpaceSelectsTheFocusedSaveOption(t *testing.T) {
	m := newTestModel(t, testConfig(), newFakeStorage())
	m.currentField = fieldSaveToConfig

	m = feed(t, m, testutil.Key(" "))
	if m.saveOption != SaveToConfig {
		t.Error("space on the config radio did not select it")
	}

	m.currentField = fieldSaveToHelper
	m = feed(t, m, testutil.Key(" "))
	if m.saveOption != SaveToHelper {
		t.Error("space on the helper radio did not select it")
	}
}

func TestSpaceIsInertOnTheOtherFields(t *testing.T) {
	for _, field := range []int{fieldURL, fieldToken, fieldLoginButton} {
		m := newTestModel(t, testConfig(), newFakeStorage())
		m.currentField = field
		m.saveOption = SaveToConfig

		m = feed(t, m, testutil.Key(" "))

		if m.saveOption != SaveToConfig {
			t.Errorf("space on field %d changed the save option", field)
		}
	}
}

// ── Enter ────────────────────────────────────────────────────────────────────

func TestEnterAdvancesThroughTheTextFields(t *testing.T) {
	m := newTestModel(t, testConfig(), newFakeStorage())

	m, cmd := step(t, m, testutil.Key("enter"))
	if m.currentField != fieldToken {
		t.Errorf("currentField = %d after enter on the URL, want the token field", m.currentField)
	}
	if cmd != nil {
		t.Error("enter on the URL field submitted the form")
	}

	m, _ = step(t, m, testutil.Key("enter"))
	if m.currentField != fieldSaveToHelper {
		t.Errorf("currentField = %d after enter on the token, want the first radio", m.currentField)
	}
}

func TestEnterSelectsOnTheRadioButtons(t *testing.T) {
	m := newTestModel(t, testConfig(), newFakeStorage())
	m.currentField = fieldSaveToConfig

	m, cmd := step(t, m, testutil.Key("enter"))

	if m.saveOption != SaveToConfig {
		t.Error("enter on the config radio did not select it")
	}
	if cmd != nil {
		t.Error("enter on a radio button submitted the form")
	}
}

func TestEnterOnTheButtonRequiresBothFields(t *testing.T) {
	tests := []struct {
		name  string
		url   string
		token string
		want  bool
	}{
		{"both present", "https://gitlab.example.com", "glpat-x", true},
		{"no token", "https://gitlab.example.com", "", false},
		{"no url", "", "glpat-x", false},
		{"neither", "", "", false},
	}

	for _, tc := range tests {
		t.Run(tc.name, func(t *testing.T) {
			m := newTestModel(t, testConfig(), newFakeStorage())
			m.urlInput.SetValue(tc.url)
			m.tokenInput.SetValue(tc.token)
			m.currentField = fieldLoginButton

			m, cmd := step(t, m, testutil.Key("enter"))

			if tc.want {
				if cmd == nil {
					t.Error("Login issued no command with both fields filled")
				}
				if m.error != "" {
					t.Errorf("error = %q on a valid submission", m.error)
				}
				return
			}
			if m.error == "" {
				t.Error("an incomplete submission produced no error message")
			}
		})
	}
}

// ── Authentication result ────────────────────────────────────────────────────

func TestAuthStartShowsTheSpinner(t *testing.T) {
	m := newTestModel(t, testConfig(), newFakeStorage())

	m, cmd := step(t, m, AuthStartMsg{})

	if !m.authenticating {
		t.Error("authenticating = false after AuthStartMsg")
	}
	if cmd == nil {
		t.Error("the spinner was not started")
	}
}

func TestSpinnerTicksOnlyWhileAuthenticating(t *testing.T) {
	m := newTestModel(t, testConfig(), newFakeStorage())

	_, cmd := step(t, m, spinner.TickMsg{})
	if cmd != nil {
		t.Error("an idle model kept the spinner running")
	}

	m.authenticating = true
	_, cmd = step(t, m, spinner.TickMsg{})
	if cmd == nil {
		t.Error("the spinner stopped while authentication was in flight")
	}
}

func TestSuccessfulAuthResultRecordsTheUser(t *testing.T) {
	m := newTestModel(t, testConfig(), newFakeStorage())
	m.authenticating = true

	m = feed(t, m, AuthResultMsg{User: testUser()})

	if !m.authenticated {
		t.Error("authenticated = false after a successful result")
	}
	if m.authenticating {
		t.Error("authenticating = true after the result arrived")
	}
	if m.user == nil || m.user.Username != "anthoni" {
		t.Errorf("user = %+v, want the authenticated user", m.user)
	}
	if !strings.Contains(m.success, "anthoni") {
		t.Errorf("success message = %q, want it to name the user", m.success)
	}
	if m.error != "" {
		t.Errorf("error = %q after a success", m.error)
	}
	if m.currentField != fieldLogoutButton {
		t.Errorf("currentField = %d after signing in, want the logout button", m.currentField)
	}
}

// A failed credential save is not a failed login: the session is live, the
// warning is informational.
func TestSaveWarningIsCarriedWithoutFailingTheLogin(t *testing.T) {
	m := newTestModel(t, testConfig(), newFakeStorage())

	m = feed(t, m, AuthResultMsg{User: testUser(), SaveWarning: "could not reach the credential helper"})

	if !m.authenticated {
		t.Error("a save warning was treated as a failed login")
	}
	if m.warning != "could not reach the credential helper" {
		t.Errorf("warning = %q, want the save warning", m.warning)
	}
}

func TestFailedAuthResultSurfacesTheError(t *testing.T) {
	m := newTestModel(t, testConfig(), newFakeStorage())
	m.authenticating = true
	m.success = "stale success"
	m.warning = "stale warning"

	m = feed(t, m, AuthResultMsg{Error: errors.New("401 unauthorized")})

	if m.authenticated {
		t.Error("authenticated = true after a failed result")
	}
	if m.error != "401 unauthorized" {
		t.Errorf("error = %q, want the failure surfaced", m.error)
	}
	if m.success != "" || m.warning != "" {
		t.Error("a failure left the previous success or warning on screen")
	}
	if m.authenticating {
		t.Error("authenticating = true after the result arrived, so the spinner never stops")
	}
}

// The app router authenticates globally at startup; the view has to accept that
// session rather than prompting again.
func TestGitLabAuthSuccessFromTheRouter(t *testing.T) {
	m := newTestModel(t, testConfig(), newFakeStorage())

	m = feed(t, m, GitLabAuthSuccessMsg{User: testUser()})

	if !m.authenticated || m.user == nil {
		t.Error("the view ignored a session established by the router")
	}
	if !strings.Contains(m.success, "Already authenticated") {
		t.Errorf("success message = %q, want it to say the session already existed", m.success)
	}
}

func TestSetAuthMirrorsTheRouterMessage(t *testing.T) {
	m := newTestModel(t, testConfig(), newFakeStorage())

	m.SetAuth(nil, testUser())

	if !m.authenticated || m.authenticating {
		t.Error("SetAuth did not put the view in the authenticated state")
	}
	if !strings.Contains(m.success, "anthoni") {
		t.Errorf("success message = %q, want it to name the user", m.success)
	}
}

// ── Logout ───────────────────────────────────────────────────────────────────

func TestEnterLogsOutWhenAuthenticated(t *testing.T) {
	m := newTestModel(t, testConfig(), newFakeStorage())
	m = feed(t, m, AuthResultMsg{User: testUser()})

	_, cmd := step(t, m, testutil.Key("enter"))

	if cmd == nil {
		t.Error("enter on the logout button issued no command")
	}
}

func TestLogoutCompleteClearsTheSession(t *testing.T) {
	cfg := testConfig()
	cfg.GitLab.Token = "glpat-persisted"
	m := newTestModel(t, cfg, newFakeStorage())
	m = feed(t, m, AuthResultMsg{User: testUser(), SaveWarning: "stale"})
	m.tokenInput.SetValue("glpat-persisted")

	m = feed(t, m, LogoutCompleteMsg{})

	if m.authenticated || m.user != nil {
		t.Error("the session survived the logout")
	}
	if m.tokenInput.Value() != "" {
		t.Error("the token stayed in the input after logging out")
	}
	if cfg.GitLab.Token != "" {
		t.Error("the token stayed in the config after logging out")
	}
	if m.warning != "" || m.error != "" {
		t.Error("a stale warning or error survived the logout")
	}
	if !strings.Contains(m.success, "Logged out") {
		t.Errorf("success message = %q, want the logout confirmation", m.success)
	}
	if !m.urlInput.Focused() {
		t.Error("the URL input is not focused after logging out")
	}
}

// ── Input lockout ────────────────────────────────────────────────────────────

// While a login is in flight every key is ignored: a second submission would
// race the first.
func TestKeysAreIgnoredWhileAuthenticating(t *testing.T) {
	m := newTestModel(t, testConfig(), newFakeStorage())
	m.urlInput.SetValue("https://gitlab.example.com")
	m.tokenInput.SetValue("glpat-x")
	m.authenticating = true
	field := m.currentField

	m, cmd := step(t, m, testutil.Key("down"))

	if m.currentField != field {
		t.Error("navigation worked while authentication was in flight")
	}
	if cmd != nil {
		t.Error("a key issued a command while authentication was in flight")
	}
}

func TestWindowSizeIsStored(t *testing.T) {
	m := newTestModel(t, testConfig(), newFakeStorage())

	m = feed(t, m, tea.WindowSizeMsg{Width: 140, Height: 50})

	if m.width != 140 || m.height != 50 {
		t.Errorf("window size = %dx%d, want 140x50", m.width, m.height)
	}
}
