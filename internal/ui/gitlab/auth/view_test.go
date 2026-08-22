package auth

import (
	"errors"
	"strings"
	"testing"

	"github.com/anthnel/devdesk/internal/credentials"
	"github.com/anthnel/devdesk/internal/ui/shortcut"
	"github.com/anthnel/devdesk/internal/ui/testutil"
	"github.com/anthnel/devdesk/internal/ui/theme"
)

// ── Edit mode ────────────────────────────────────────────────────────────────

// InEditMode keeps the app router from stealing ":" while the user types a URL
// or a token — both of which can legitimately contain one.
func TestInEditModeOnlyOnTheTextFields(t *testing.T) {
	m := newTestModel(t, testConfig(), newFakeStorage())

	for _, field := range []int{fieldToken, fieldToken} {
		m.currentField = field
		if !m.InEditMode() {
			t.Errorf("InEditMode() is false on field %d, so ':' would not reach the input", field)
		}
	}

	m.currentField = fieldSubmit
	if m.InEditMode() {
		t.Error("InEditMode() is true on the Login button, which holds no text input")
	}
}

func TestInEditModeIsFalseWhileBusyOrSignedIn(t *testing.T) {
	m := newTestModel(t, testConfig(), newFakeStorage())
	m.currentField = fieldToken

	m.authenticating = true
	if m.InEditMode() {
		t.Error("InEditMode() is true while authentication is in flight, where keys are ignored anyway")
	}

	m.authenticating = false
	m.authenticated = true
	if m.InEditMode() {
		t.Error("InEditMode() is true once signed in, where the form is replaced by a logout button")
	}
}

// ── Form rendering ───────────────────────────────────────────────────────────

func TestViewRendersTheLoginForm(t *testing.T) {
	m := newTestModel(t, testConfig(), newFakeStorage())

	out := m.View()

	for _, want := range []string{"GitLab URL", "Personal Access Token", "Login"} {
		if !strings.Contains(out, want) {
			t.Errorf("View() is missing %q", want)
		}
	}

	// The store the token goes to is named on the form. There is no choice to
	// present any more, but the user still has to be able to read where their
	// secret ends up (§3.9).
	if !strings.Contains(out, "Test Keyring") {
		t.Errorf("View() does not name the secret store:\n%s", out)
	}
	for _, gone := range []string{"Save options", "(•)", "( )"} {
		if strings.Contains(out, gone) {
			t.Errorf("View() still renders %q — the save-option radios were removed", gone)
		}
	}
}

// A session-only fallback has to be visible without reading the source: a
// fallback that silently fails to persist is how the old "secure" option came
// to write plaintext.
func TestViewWarnsWhenNothingIsPersisted(t *testing.T) {
	m := New(testConfig(), credentials.SessionOnly("no store under test"), nil)
	m = feed(t, m, testutil.Resize(100, 30))

	out := m.View()
	if !strings.Contains(out, "Nothing is saved") {
		t.Errorf("View() does not warn that the token will not be kept:\n%s", out)
	}
	if !strings.Contains(out, theme.IconWarning) {
		t.Error("the session-only notice is not marked as a warning")
	}
}

// What the migration off plaintext configuration did is reported once, on the
// view that owns the token.
func TestViewReportsTheMigrationNotices(t *testing.T) {
	m := New(testConfig(), persisted(newFakeStorage()), []string{"The GitLab token moved out of the configuration file."})
	m = feed(t, m, testutil.Resize(100, 30))

	if !strings.Contains(m.View(), "moved out of the configuration file") {
		t.Error("View() does not report what the migration did")
	}
}

func TestViewIsSilentWithoutMigrationNotices(t *testing.T) {
	m := newTestModel(t, testConfig(), newFakeStorage())

	if strings.Contains(m.View(), theme.IconLock) {
		t.Error("View() rendered a migration notice block with nothing to report")
	}
}

// The rendered form must never contain the token, whichever field has focus.
func TestViewNeverRendersTheTokenInClear(t *testing.T) {
	m := newTestModel(t, testConfig(), newFakeStorage())
	m.tokenInput.SetValue("glpat-supersecret")

	for field := fieldToken; field <= fieldSubmit; field++ {
		m.currentField = field
		if strings.Contains(m.View(), "supersecret") {
			t.Fatalf("View() rendered the token in clear with field %d focused", field)
		}
	}
}

func TestViewShowsTheSpinnerWhileAuthenticating(t *testing.T) {
	m := newTestModel(t, testConfig(), newFakeStorage())
	m.authenticating = true

	out := m.View()
	if !strings.Contains(out, "Authenticating") {
		t.Error("View() does not report an in-flight login")
	}
	if strings.Contains(out, "Personal Access Token") {
		t.Error("the form is still rendered under the spinner")
	}
}

func TestViewShowsTheLoggedInStateInsteadOfTheForm(t *testing.T) {
	cfg := testConfig()
	cfg.Forge.URL = "https://gitlab.example.com"
	m := newTestModel(t, cfg, newFakeStorage())

	m = feed(t, m, AuthResultMsg{User: testUser()})

	out := m.View()
	if !strings.Contains(out, "Authenticated as") || !strings.Contains(out, "anthoni") {
		t.Error("the logged-in view does not name the user")
	}
	if !strings.Contains(out, "Anthoni D") {
		t.Error("the logged-in view does not show the user's full name")
	}
	if !strings.Contains(out, "https://gitlab.example.com") {
		t.Error("the logged-in view does not show the GitLab URL")
	}
	if !strings.Contains(out, "Logout") {
		t.Error("the logged-in view offers no way out")
	}
	if strings.Contains(out, "Personal Access Token") {
		t.Error("the login form is still rendered while signed in")
	}
}

// The success banner is part of the logged-in card, so rendering it above as
// well would say the same thing twice.
func TestSuccessBannerIsNotRepeatedWhileSignedIn(t *testing.T) {
	m := newTestModel(t, testConfig(), newFakeStorage())
	m = feed(t, m, AuthResultMsg{User: testUser()})

	if count := strings.Count(m.View(), "anthoni"); count != 1 {
		t.Errorf("the username appears %d times in the signed-in view, want once", count)
	}
}

func TestSuccessBannerShowsAfterLogout(t *testing.T) {
	m := newTestModel(t, testConfig(), newFakeStorage())
	m = feed(t, m, AuthResultMsg{User: testUser()}, LogoutCompleteMsg{})

	out := m.View()
	if !strings.Contains(out, "Logged out") {
		t.Error("the logout confirmation is not shown")
	}
	if !strings.Contains(out, "Personal Access Token") {
		t.Error("the login form did not come back after logging out")
	}
}

func TestViewRendersTheError(t *testing.T) {
	m := newTestModel(t, testConfig(), newFakeStorage())

	m = feed(t, m, AuthResultMsg{Error: errors.New("401 unauthorized")})

	if !strings.Contains(m.View(), "401 unauthorized") {
		t.Error("View() does not surface the authentication error")
	}
}

func TestViewRendersTheSaveWarning(t *testing.T) {
	m := newTestModel(t, testConfig(), newFakeStorage())

	m = feed(t, m, AuthResultMsg{User: testUser(), SaveWarning: "the secret store refused the write"})

	if !strings.Contains(m.View(), "the secret store refused the write") {
		t.Error("View() does not surface the save warning")
	}
}

// The warning box is width-constrained, so it must survive a terminal narrower
// than its padding.
func TestViewSurvivesANarrowTerminal(t *testing.T) {
	m := newTestModel(t, testConfig(), newFakeStorage())
	m = feed(t, m, AuthResultMsg{User: testUser(), SaveWarning: "the secret store refused the write"})
	m = feed(t, m, testutil.Resize(10, 10))

	if out := m.View(); out == "" {
		t.Error("View() returned nothing on a narrow terminal")
	}
}

// ── Header, footer and help ──────────────────────────────────────────────────

func TestGetTitleAndIcon(t *testing.T) {
	m := newTestModel(t, testConfig(), newFakeStorage())

	if !strings.Contains(m.GetTitle(), "Gitlab Authentication") {
		t.Errorf("GetTitle() = %q, want it to name the view", m.GetTitle())
	}
	if m.GetIcon() != "" {
		t.Errorf("GetIcon() = %q, want empty — the title carries the icon", m.GetIcon())
	}
}

func TestGetHeaderInfoCarriesTheContext(t *testing.T) {
	info := newTestModel(t, testConfig(), newFakeStorage()).GetHeaderInfo("work")

	if len(info) != 1 || info[0].Key != "Context" || info[0].Value != "work" {
		t.Errorf("GetHeaderInfo() = %+v, want the active context", info)
	}
}

func TestFooterHeightMatchesWhatRenderFooterEmits(t *testing.T) {
	m := newTestModel(t, testConfig(), newFakeStorage())

	want := m.GetFooterHeight()
	got := strings.Count(m.RenderFooter(100), "\n") + 1
	if got != want {
		t.Errorf("RenderFooter() emitted %d lines, GetFooterHeight() promised %d", got, want)
	}
}

// Rule 137: descriptions are capitalised imperatives.
func TestShortcutDescriptionsAreCapitalised(t *testing.T) {
	for _, s := range newTestModel(t, testConfig(), newFakeStorage()).GetShortcuts() {
		if s.Description == "" {
			t.Errorf("shortcut %q has no description", s.Key)
			continue
		}
		if first := s.Description[0]; first < 'A' || first > 'Z' {
			t.Errorf("shortcut %q description %q does not start with a capital", s.Key, s.Description)
		}
	}
}

func TestGetHelpContentIsPopulated(t *testing.T) {
	content := newTestModel(t, testConfig(), newFakeStorage()).GetHelpContent()

	if content.Title == "" || content.Description == "" {
		t.Error("the help content has no title or description")
	}
	if len(content.KeyBindings) == 0 || len(content.Sections) == 0 {
		t.Error("the help content has no key bindings or sections")
	}
}

func TestHelpDocumentsTheAdvertisedShortcuts(t *testing.T) {
	m := newTestModel(t, testConfig(), newFakeStorage())
	documented := map[string]bool{}
	for _, kb := range m.GetHelpContent().KeyBindings {
		documented[kb.Key] = true
	}

	for _, s := range m.GetShortcuts() {
		if s.Key == "?" {
			continue
		}
		if !documented[s.Key] {
			t.Errorf("shortcut %q is advertised in the header but absent from the help", s.Key)
		}
	}
}

func TestShortcutsAreStable(t *testing.T) {
	// This view has no state-dependent shortcuts, so the set must not change
	// between signed-out and signed-in — if that ever stops being true, Rule 130
	// applies and this test should be replaced rather than deleted.
	m := newTestModel(t, testConfig(), newFakeStorage())
	before := keysOf(m.GetShortcuts())

	m = feed(t, m, AuthResultMsg{User: testUser()})

	if after := keysOf(m.GetShortcuts()); after != before {
		t.Errorf("shortcuts changed from %q to %q without a Rule 130 branch", before, after)
	}
}

func keysOf(shortcuts shortcut.Shortcuts) string {
	keys := make([]string, 0, len(shortcuts))
	for _, s := range shortcuts {
		keys = append(keys, s.Key)
	}
	return strings.Join(keys, ",")
}
