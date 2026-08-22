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

	"github.com/anthnel/devdesk/internal/config"
	"github.com/anthnel/devdesk/internal/credentials"
	"github.com/anthnel/devdesk/internal/forge"
	"github.com/anthnel/devdesk/internal/ui/testutil"
)

// The only Cmds here — authenticate() and logout() — reach the GitLab API and
// the secret store, so no test executes one. loadSavedCredentials() is the
// exception: it only reads the injected storage, so it is run directly.

// This view logs its whole flow at INFO level.
func TestMain(m *testing.M) {
	log.SetOutput(io.Discard)
	code := m.Run()
	log.SetOutput(os.Stderr)
	os.Exit(code)
}

// The logged-in view has a single button, on the field the login form uses for
// the URL.
const fieldLogoutButton = fieldToken

// fakeStorage records what the view asks of the secret store.
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

// persisted wraps a storage in a Selection that claims to survive the session,
// which is what every test but the memory-fallback ones assumes.
func persisted(storage credentials.Storage) credentials.Selection {
	return credentials.Selection{
		Storage: storage,
		Backend: credentials.BackendKeyring,
		Detail:  "Secrets are stored in the Test Keyring.",
	}
}

func testConfig() *config.Config {
	cfg := config.Default()
	cfg.GitLab.URL = ""
	return cfg
}

func newTestModel(t *testing.T, cfg *config.Config, storage *fakeStorage) *Model {
	t.Helper()
	m := New(cfg, persisted(storage), nil)
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

func testUser() forge.User {
	return forge.User{ID: "7", Username: "anthoni", Name: "Anthoni D"}
}

// ── Construction ─────────────────────────────────────────────────────────────

// The URL is read from the configuration, never edited here: this view owns the
// token and the act of logging in, and the configuration view owns the setting.
// Both used to write gitlab.url, so neither was authoritative.
func TestTheURLIsReadFromTheConfiguration(t *testing.T) {
	cfg := testConfig()
	cfg.GitLab.URL = "https://gitlab.example.com"

	m := newTestModel(t, cfg, newFakeStorage())

	if !strings.Contains(m.View(), "https://gitlab.example.com") {
		t.Error("the configured URL is not shown")
	}
	if m.tokenInput.Value() != "" {
		t.Error("the token input was prefilled; it must be typed or loaded, never guessed")
	}
}

// Without a URL there is nothing to log into, and saying "token is required"
// would name the wrong problem.
func TestNoConfiguredURLIsReportedAsSuch(t *testing.T) {
	cfg := testConfig()
	cfg.GitLab.URL = ""
	m := newTestModel(t, cfg, newFakeStorage())
	m.tokenInput.SetValue("glpat-x")

	if cmd := m.authenticate(); cmd != nil {
		t.Error("authenticate() ran with no URL configured")
	}
	if !strings.Contains(m.error, ":config") {
		t.Errorf("error = %q, want it to say where the URL is set", m.error)
	}
}

func TestNewStartsOnTheTokenField(t *testing.T) {
	m := newTestModel(t, testConfig(), newFakeStorage())

	if m.currentField != fieldToken {
		t.Errorf("currentField = %d on a new form, want the token field", m.currentField)
	}
	if !m.tokenInput.Focused() {
		t.Error("the token input is not focused on a new form")
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
	if cmd := New(testConfig(), persisted(newFakeStorage()), nil).Init(); cmd == nil {
		t.Fatal("Init() returned no command, so saved credentials are never loaded")
	}
}

// ── Credential loading ───────────────────────────────────────────────────────

// There is one source. The configuration file no longer carries a token, and a
// build that left one there had it migrated into the store at startup (§3.9).
func TestLoadSavedCredentialsReadsTheStore(t *testing.T) {
	cfg := testConfig()
	cfg.GitLab.URL = "https://gitlab.example.com"
	storage := newFakeStorage()
	storage.token = "glpat-from-store"

	msg := New(cfg, persisted(storage), nil).loadSavedCredentials()().(CredentialsLoadedMsg)

	if msg.Token != "glpat-from-store" {
		t.Errorf("Token = %q, want the stored one", msg.Token)
	}
	if msg.URL != "https://gitlab.example.com" {
		t.Errorf("URL = %q, want the configured one", msg.URL)
	}
	if len(storage.loadCalls) != 1 || storage.loadCalls[0] != "https://gitlab.example.com" {
		t.Errorf("the store was queried with %v, want the configured URL once", storage.loadCalls)
	}
}

func TestLoadSavedCredentialsReportsNothingFound(t *testing.T) {
	tests := []struct {
		name    string
		url     string
		storage credentials.Storage
	}{
		{"no URL configured", "", newFakeStorage()},
		{"the store holds no token", "https://gitlab.example.com", newFakeStorage()},
		{"the store errors", "https://gitlab.example.com", &fakeStorage{loadErr: errors.New("store unavailable"), saved: map[string]string{}}},
		{"no store at all", "https://gitlab.example.com", nil},
	}

	for _, tc := range tests {
		t.Run(tc.name, func(t *testing.T) {
			cfg := testConfig()
			cfg.GitLab.URL = tc.url

			m := New(cfg, credentials.Selection{Storage: tc.storage}, nil)

			msg := m.loadSavedCredentials()().(CredentialsLoadedMsg)

			if msg.Token != "" || msg.URL != "" {
				t.Errorf("got %+v, want an empty result", msg)
			}
		})
	}
}

// Loaded credentials trigger an auto-login rather than waiting for the user to
// press Login on a prefilled form.
func TestCredentialsLoadedStartsAnAutoLogin(t *testing.T) {
	m := newTestModel(t, testConfig(), newFakeStorage())

	m, cmd := step(t, m, CredentialsLoadedMsg{URL: "https://gitlab.example.com", Token: "glpat-x"})

	if !m.authenticating {
		t.Error("authenticating = false after credentials were loaded")
	}
	if cmd == nil {
		t.Error("no command issued, so the auto-login never runs")
	}
	if m.tokenInput.Value() != "glpat-x" {
		t.Error("the loaded token was not written into the form")
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

	for range 10 {
		m = feed(t, m, testutil.Key("down"))
	}
	if m.currentField != fieldSubmit {
		t.Errorf("currentField = %d after repeated down, want %d (Login)", m.currentField, fieldSubmit)
	}

	for range 10 {
		m = feed(t, m, testutil.Key("up"))
	}
	if m.currentField != fieldToken {
		t.Errorf("currentField = %d after repeated up, want %d (URL)", m.currentField, fieldToken)
	}
}

func TestFocusFollowsTheCurrentField(t *testing.T) {
	m := newTestModel(t, testConfig(), newFakeStorage())

	if !m.tokenInput.Focused() {
		t.Error("the token input does not hold focus on the first field")
	}

	// The submit button holds no text input.
	m = feed(t, m, testutil.Key("down"))
	if m.tokenInput.Focused() {
		t.Error("an input kept focus on the Login button")
	}
}

func TestTypingReachesTheFocusedInput(t *testing.T) {
	cfg := testConfig()
	cfg.GitLab.URL = "https://git.example.com"
	m := newTestModel(t, cfg, newFakeStorage())

	m = feed(t, m, testutil.Type("glpat-abc")...)

	if got := m.tokenInput.Value(); got != "glpat-abc" {
		t.Errorf("token input = %q after typing, want the typed value", got)
	}
	if m.config.GitLab.URL != "https://git.example.com" {
		t.Error("typing into the token field changed the configured URL")
	}
}

// Space used to be intercepted for the save-option radio buttons, which meant a
// space could not be typed into either text field. With the radios gone it is
// an ordinary character again.
func TestSpaceIsAnOrdinaryCharacter(t *testing.T) {
	m := newTestModel(t, testConfig(), newFakeStorage())

	// Type rather than Key: a terminal delivers a space as a rune, which is what
	// the removed `case " "` used to intercept.
	m = feed(t, m, testutil.Type(" ")...)

	if got := m.tokenInput.Value(); got != " " {
		t.Errorf("token input = %q after a space, want it typed through", got)
	}
}

// ── Enter ────────────────────────────────────────────────────────────────────

func TestEnterAdvancesThroughTheTextFields(t *testing.T) {
	m := newTestModel(t, testConfig(), newFakeStorage())

	m, cmd := step(t, m, testutil.Key("enter"))
	if m.currentField != fieldSubmit {
		t.Errorf("currentField = %d after enter on the token, want the Login button", m.currentField)
	}
	if cmd != nil {
		t.Error("enter on the token field submitted the form")
	}

	m, _ = step(t, m, testutil.Key("enter"))
	if m.currentField != fieldSubmit {
		t.Errorf("currentField = %d after enter on the token, want the Login button", m.currentField)
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
			setConfigURL(m, tc.url)
			m.tokenInput.SetValue(tc.token)
			m.currentField = fieldSubmit

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
	if m.user.Username != "anthoni" {
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

// A failed secret save is not a failed login: the session is live, the warning
// is informational.
func TestSaveWarningIsCarriedWithoutFailingTheLogin(t *testing.T) {
	m := newTestModel(t, testConfig(), newFakeStorage())

	m = feed(t, m, AuthResultMsg{User: testUser(), SaveWarning: "could not reach the secret store"})

	if !m.authenticated {
		t.Error("a save warning was treated as a failed login")
	}
	if m.warning != "could not reach the secret store" {
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

// The token goes to the store; nothing about it may reach the file (§3.9). And
// now that the configuration view owns gitlab.url, this view writes no setting
// at all — the URL it authenticates against is the one it was handed.
func TestAuthenticateWritesNoConfiguration(t *testing.T) {
	cfg := testConfig()
	cfg.GitLab.URL = "https://gitlab.example.com"
	m := newTestModel(t, cfg, newFakeStorage())
	m.tokenInput.SetValue("glpat-secret")

	if cmd := m.authenticate(); cmd == nil {
		t.Fatal("authenticate() returned no command")
	}

	// The Cmd itself reaches the network, so assert on the config it was
	// handed: it is the same pointer, and nothing may have been set on it.
	if m.config.GitLab.URL != "https://gitlab.example.com" {
		t.Errorf("URL = %q; authenticate() must not rewrite the setting it read", m.config.GitLab.URL)
	}
	if m.config.Scan.GitleaksConfig != "" || m.config.App.Theme != cfg.App.Theme {
		t.Error("authenticate() wrote to the config outside Update() (Rule 110)")
	}
}

// The app router authenticates globally at startup; the view has to accept that
// session rather than prompting again.
func TestGitLabAuthSuccessFromTheRouter(t *testing.T) {
	m := newTestModel(t, testConfig(), newFakeStorage())

	m = feed(t, m, GitLabAuthSuccessMsg{User: testUser()})

	if !m.authenticated || m.user.Username == "" {
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
	m := newTestModel(t, testConfig(), newFakeStorage())
	m = feed(t, m, AuthResultMsg{User: testUser(), SaveWarning: "stale"})
	m.tokenInput.SetValue("glpat-persisted")

	m = feed(t, m, LogoutCompleteMsg{})

	if m.authenticated || m.user.Username != "" {
		t.Error("the session survived the logout")
	}
	if m.tokenInput.Value() != "" {
		t.Error("the token stayed in the input after logging out")
	}
	if m.warning != "" || m.error != "" {
		t.Error("a stale warning or error survived the logout")
	}
	if !strings.Contains(m.success, "Logged out") {
		t.Errorf("success message = %q, want the logout confirmation", m.success)
	}
	if !m.tokenInput.Focused() {
		t.Error("the token input is not focused after logging out")
	}
}

// Logout has to reach the store. Anything left there would be picked up by the
// next auto-login and silently sign the user back in.
func TestLogoutDeletesFromTheStore(t *testing.T) {
	storage := newFakeStorage()
	m := newTestModel(t, testConfig(), storage)
	setConfigURL(m, "https://gitlab.example.com")

	cmd := m.logout()
	if cmd == nil {
		t.Fatal("logout() returned no command")
	}
	cmd()

	if len(storage.deleted) != 1 || storage.deleted[0] != "https://gitlab.example.com" {
		t.Errorf("deleted = %v, want the configured URL once", storage.deleted)
	}
}

// ── Input lockout ────────────────────────────────────────────────────────────

// While a login is in flight every key is ignored: a second submission would
// race the first.
func TestKeysAreIgnoredWhileAuthenticating(t *testing.T) {
	m := newTestModel(t, testConfig(), newFakeStorage())
	setConfigURL(m, "https://gitlab.example.com")
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

// setConfigURL is where the URL lives now: the configuration view owns it, this
// view reads it. Tests that used to type into a URL input set it here instead.
func setConfigURL(m *Model, url string) {
	m.config.GitLab.URL = url
}
