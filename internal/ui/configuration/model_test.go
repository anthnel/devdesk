package configuration

import (
	"os"
	"path/filepath"
	"strings"
	"testing"

	tea "github.com/charmbracelet/bubbletea"

	"github.com/anthnel/devdesk/internal/config"
	sharedcomponents "github.com/anthnel/devdesk/internal/ui/components"
	"github.com/anthnel/devdesk/internal/ui/testutil"
)

// Every field in this view calls config.Save on change, so the tests must not
// be pointed at the developer's own ~/.devdesk.
func TestMain(m *testing.M) {
	dir, err := os.MkdirTemp("", "devdesk-configuration-test")
	if err != nil {
		panic(err)
	}
	_ = os.Setenv("HOME", dir)
	_ = os.Setenv("USERPROFILE", dir)
	code := m.Run()
	_ = os.RemoveAll(dir)
	os.Exit(code)
}

func newModel(t *testing.T) Model {
	t.Helper()
	m := New(config.Default())
	updated, _ := m.Update(tea.WindowSizeMsg{Width: 120, Height: 30})
	return updated.(Model)
}

func feed(t *testing.T, m Model, msgs ...tea.Msg) Model {
	t.Helper()
	for _, msg := range msgs {
		updated, _ := m.Update(msg)
		m = updated.(Model)
	}
	return m
}

// focusOn puts the cursor on a named field in the section that holds it.
func focusOn(t *testing.T, m Model, label string) Model {
	t.Helper()
	for tab, s := range m.sections {
		for i, f := range s.Fields {
			if f.Label == label {
				m.activeTab = tab
				m.focusedField = i
				m.bindInput()
				return m
			}
		}
	}
	t.Fatalf("no field labelled %q", label)
	return m
}

// Rule 135 gives each key one job. Tab is reserved for tabs, which is what
// makes a tabbed form the only layout that can use ↑↓ for fields.
func TestTabSwitchesSectionAndArrowsMoveBetweenFields(t *testing.T) {
	m := newModel(t)

	m = feed(t, m, testutil.Key("down"))
	if m.activeTab != 0 {
		t.Errorf("down changed the tab to %d; ↑↓ moves between fields", m.activeTab)
	}
	if m.focusedField != 1 {
		t.Errorf("focusedField = %d after down, want 1", m.focusedField)
	}

	m = feed(t, m, testutil.Key("tab"))
	if m.activeTab != 1 {
		t.Errorf("activeTab = %d after tab, want 1", m.activeTab)
	}
	if m.focusedField != 0 {
		t.Errorf("focusedField = %d after switching section, want the first field", m.focusedField)
	}
}

// Space is the only key that may toggle a checkbox (Rule 135), and enter must
// not.
func TestOnlySpaceTogglesACheckbox(t *testing.T) {
	m := focusOn(t, newModel(t), "Auto refresh")
	before := m.config.Status.AutoRefresh

	m = feed(t, m, testutil.Key("enter"))
	if m.config.Status.AutoRefresh != before {
		t.Error("enter toggled a checkbox")
	}

	m = feed(t, m, testutil.Key(" "))
	if m.config.Status.AutoRefresh == before {
		t.Error("space did not toggle the checkbox")
	}
}

func TestArrowsCycleAClosedSetAndPersist(t *testing.T) {
	m := focusOn(t, newModel(t), "Clone method")
	m.config.GitLab.CloneMethod = "https"

	m = feed(t, m, testutil.Key("right"))

	if m.config.GitLab.CloneMethod != "ssh" {
		t.Errorf("CloneMethod = %q after →, want ssh", m.config.GitLab.CloneMethod)
	}
	reloaded, err := config.Load()
	if err != nil {
		t.Fatalf("reloading the saved config: %v", err)
	}
	if reloaded.GitLab.CloneMethod != "ssh" {
		t.Errorf("the file holds %q; a cycle field persists as it changes", reloaded.GitLab.CloneMethod)
	}
}

// A typed value is written when focus leaves the field, so moving on is what
// commits — there is no save step.
func TestLeavingATextFieldCommitsIt(t *testing.T) {
	m := focusOn(t, newModel(t), "IDE command")
	m.input.SetValue("nvim")

	m = feed(t, m, testutil.Key("down"))

	if m.config.App.IDECommand != "nvim" {
		t.Errorf("IDECommand = %q, want the typed value committed on blur", m.config.App.IDECommand)
	}
}

// A refused value keeps the cursor where it is. Letting focus move would leave
// the rejected text on screen while the config quietly held something else.
func TestARefusedValueKeepsTheCursorOnItsField(t *testing.T) {
	m := focusOn(t, newModel(t), "Pull parallel jobs")
	was := m.focusedField
	m.config.GitLab.Pull.ParallelJobs = 4
	m.input.SetValue("not-a-number")

	m = feed(t, m, testutil.Key("down"))

	if m.focusedField != was {
		t.Errorf("focus moved to %d despite a refused value", m.focusedField)
	}
	if m.config.GitLab.Pull.ParallelJobs != 4 {
		t.Errorf("ParallelJobs = %d, want the old value kept", m.config.GitLab.Pull.ParallelJobs)
	}
	if m.footerError == "" {
		t.Error("nothing was reported to the user")
	}
}

// A Trivy server address disables the three options its protocol cannot serve.
// A real constraint, carried over from the security form this view replaces.
func TestAServerAddressForcesOffTheOptionsItCannotServe(t *testing.T) {
	m := focusOn(t, newModel(t), "Trivy server")
	m.config.Scan.EnableMisconfig = true
	m.config.Scan.EnableLicense = true
	m.config.Scan.GenerateSBOM = true
	m.input.SetValue("https://trivy:4954")

	m = feed(t, m, testutil.Key("down"))

	if m.config.Scan.EnableMisconfig || m.config.Scan.EnableLicense || m.config.Scan.GenerateSBOM {
		t.Errorf("server mode left incompatible options on: %+v", m.config.Scan)
	}
}

func TestADisabledOptionCannotBeToggledAndSaysWhy(t *testing.T) {
	m := newModel(t)
	m.config.Scan.TrivyServer = "https://trivy:4954"
	m = focusOn(t, m, "Misconfiguration")

	m = feed(t, m, testutil.Key(" "))

	if m.config.Scan.EnableMisconfig {
		t.Error("a disabled option was toggled on")
	}
	if !strings.Contains(strings.ToLower(m.footerInfo), "client-server") {
		t.Errorf("footerInfo = %q, want it to say why", m.footerInfo)
	}
}

// ── the secret backend ──────────────────────────────────────────────────────

// Changing where secrets live is confirmed on the way out, not on every ←→:
// cycling through three values would otherwise raise two modals.
func TestCyclingTheSecretBackendAsksOnlyWhenLeavingTheField(t *testing.T) {
	m := focusOn(t, newModel(t), "Secret backend")

	m = feed(t, m, testutil.Key("right"))
	if m.confirmModal != nil {
		t.Fatal("a modal opened while the value was still being cycled")
	}

	m = feed(t, m, testutil.Key("down"))
	if m.confirmModal == nil {
		t.Fatal("leaving the field with a changed backend did not ask")
	}
}

func TestDecliningTheBackendChangePutsItBack(t *testing.T) {
	m := focusOn(t, newModel(t), "Secret backend")
	original := m.config.App.SecretBackend

	m = feed(t, m, testutil.Key("right"), testutil.Key("down"), sharedcomponents.ConfirmModalNoMsg{})

	if m.config.App.SecretBackend != original {
		t.Errorf("SecretBackend = %q after declining, want %q", m.config.App.SecretBackend, original)
	}
	if m.confirmModal != nil {
		t.Error("the modal is still open")
	}
}

// Confirming tells the router, which is the only thing that can resolve a new
// credentials.Selection — no view can rebuild itself into one.
func TestConfirmingTheBackendChangeTellsTheRouter(t *testing.T) {
	m := focusOn(t, newModel(t), "Secret backend")
	m = feed(t, m, testutil.Key("right"), testutil.Key("down"))

	msgs := testutil.Msgs(func() tea.Cmd {
		_, cmd := m.Update(sharedcomponents.ConfirmModalYesMsg{})
		return cmd
	}())

	var saved *ConfigSavedMsg
	for _, msg := range msgs {
		if s, ok := msg.(ConfigSavedMsg); ok {
			saved = &s
		}
	}
	if saved == nil {
		t.Fatalf("no ConfigSavedMsg among %v", msgs)
	}
	if !saved.BackendChanged {
		t.Error("BackendChanged is false, so the router would not re-resolve the store")
	}
}

// ── the view is not allowed to lie about itself ─────────────────────────────

// A text field takes every printable key, so the router must not claim ":" or
// "q" while one has focus.
func TestATextFieldHoldsTheKeyboard(t *testing.T) {
	m := focusOn(t, newModel(t), "IDE command")
	if !m.InEditMode() {
		t.Error("InEditMode() is false on a text field; the router would eat printable keys")
	}

	m = focusOn(t, m, "Auto refresh")
	if m.InEditMode() {
		t.Error("InEditMode() is true on a checkbox, which takes no text")
	}
}

// Rule 124: tab bar, blank line, info line.
func TestTheFooterIsThreeLines(t *testing.T) {
	m := newModel(t)
	if got := m.GetFooterHeight(); got != 3 {
		t.Errorf("GetFooterHeight() = %d, want 3", got)
	}
	if got := strings.Count(m.RenderFooter(120), "\n") + 1; got != 3 {
		t.Errorf("RenderFooter rendered %d lines, want 3", got)
	}
}

// Rule 138: obvious navigation stays out of the header.
func TestShortcutsOmitTheObviousOnes(t *testing.T) {
	m := newModel(t)
	for _, s := range m.GetShortcuts() {
		if s.Key == "tab" || strings.Contains(s.Key, "pgup") {
			t.Errorf("shortcut %q is self-evident and should not be listed", s.Key)
		}
		if s.Description == "" || strings.ToUpper(s.Description[:1]) != s.Description[:1] {
			t.Errorf("description %q must start with a capital (Rule 137)", s.Description)
		}
	}
}

// Rule 131: exactly one blank line above the form.
func TestTheFormOpensWithOneBlankLine(t *testing.T) {
	lines := strings.Split(newModel(t).View(), "\n")
	if len(lines) < 3 {
		t.Fatalf("the view rendered %d lines", len(lines))
	}
	if strings.TrimSpace(lines[0]) != "" {
		t.Errorf("line 1 = %q, want a blank padding line", lines[0])
	}
	if strings.TrimSpace(lines[1]) == "" {
		t.Error("line 2 is blank too; Rule 131 allows exactly one")
	}
}

// The view says which context it edits. Editing workspaces_dir in the wrong
// context is otherwise a silent mistake — the field looks identical in all of
// them.
func TestTheViewNamesTheContextItEdits(t *testing.T) {
	m := newModel(t)
	if !strings.Contains(m.View(), m.context) {
		t.Errorf("the view does not name its context (%q)", m.context)
	}
}

// A theme that cannot be listed still leaves the built-in one, so the cycle has
// something to offer rather than an empty set that ←→ cannot move through.
func TestAnUnreadableThemeDirectoryStillOffersTheDefault(t *testing.T) {
	t.Setenv("HOME", filepath.Join(t.TempDir(), "nonexistent"))
	t.Setenv("USERPROFILE", filepath.Join(t.TempDir(), "nonexistent"))

	m := New(config.Default())

	f := fieldNamed(t, "Theme")
	_ = f
	for _, s := range m.sections {
		for _, field := range s.Fields {
			if field.Label == "Theme" && len(field.Options) == 0 {
				t.Error("the theme cycle has no options")
			}
		}
	}
}
