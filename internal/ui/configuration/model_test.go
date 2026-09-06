package configuration

import (
	"github.com/anthnel/devdesk/internal/command"
	"os"
	"path/filepath"
	"strings"
	"testing"

	tea "github.com/charmbracelet/bubbletea"
	"github.com/charmbracelet/lipgloss"
	"github.com/charmbracelet/x/ansi"

	"github.com/anthnel/devdesk/internal/config"
	sharedcomponents "github.com/anthnel/devdesk/internal/ui/components"
	"github.com/anthnel/devdesk/internal/ui/testutil"
	"github.com/anthnel/devdesk/internal/ui/theme"
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
	m := New(config.Default(), MCPFacts{})
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
	m.config.Forge.CloneMethod = "https"

	m = feed(t, m, testutil.Key("right"))

	if m.config.Forge.CloneMethod != "ssh" {
		t.Errorf("CloneMethod = %q after →, want ssh", m.config.Forge.CloneMethod)
	}
	reloaded, err := config.Load()
	if err != nil {
		t.Fatalf("reloading the saved config: %v", err)
	}
	if reloaded.Forge.CloneMethod != "ssh" {
		t.Errorf("the file holds %q; a cycle field persists as it changes", reloaded.Forge.CloneMethod)
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
	m := focusOn(t, newModel(t), "Parallel jobs")
	was := m.focusedField
	m.config.Forge.Pull.ParallelJobs = 4
	m.input.SetValue("not-a-number")

	m = feed(t, m, testutil.Key("down"))

	if m.focusedField != was {
		t.Errorf("focus moved to %d despite a refused value", m.focusedField)
	}
	if m.config.Forge.Pull.ParallelJobs != 4 {
		t.Errorf("ParallelJobs = %d, want the old value kept", m.config.Forge.Pull.ParallelJobs)
	}
	if !m.footer.IsSet() {
		t.Error("nothing was reported to the user")
	}
}

// A Trivy server address disables the two options its protocol cannot serve.
// A real constraint, carried over from the security form this view replaces.
func TestTheCheckboxAloneForcesOffTheOptionsItCannotServe(t *testing.T) {
	m := focusOn(t, newModel(t), "Use Trivy server")
	m.config.Scan.EnableMisconfig = true
	m.config.Scan.EnableLicense = true

	m = feed(t, m, testutil.Key(" "))

	if m.config.Scan.EnableMisconfig || m.config.Scan.EnableLicense {
		t.Errorf("server mode left incompatible options on: %+v", m.config.Scan)
	}
}

// Filling in the address alone must not turn client-server mode on — only the
// checkbox does.
func TestAnAddressWithoutTheCheckboxLeavesServerModeOff(t *testing.T) {
	m := focusOn(t, newModel(t), "Trivy server")
	m.config.Scan.EnableMisconfig = true
	m.config.Scan.EnableLicense = true
	m.input.SetValue("https://trivy:4954")

	m = feed(t, m, testutil.Key("down"))

	if !m.config.Scan.EnableMisconfig || !m.config.Scan.EnableLicense {
		t.Errorf("an address alone locked options that only the checkbox should: %+v", m.config.Scan)
	}
}

func TestADisabledOptionCannotBeToggledAndSaysWhy(t *testing.T) {
	m := newModel(t)
	m.config.Scan.UseTrivyServer = true
	m = focusOn(t, m, "Misconfiguration")

	m = feed(t, m, testutil.Key(" "))

	if m.config.Scan.EnableMisconfig {
		t.Error("a disabled option was toggled on")
	}
	if !strings.Contains(strings.ToLower(m.footer.Text()), "client-server") {
		t.Errorf("footer = %q, want it to say why", m.footer.Text())
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

// The header names the context, as every other view's does. Editing
// workspaces_dir in the wrong context is otherwise a silent mistake — the
// fields look identical in all of them.
func TestTheHeaderNamesTheContextItEdits(t *testing.T) {
	m := newModel(t)

	var got string
	for _, info := range m.GetHeaderInfo("dev") {
		if info.Key == "Context" {
			got = info.Value
		}
	}
	if got != "dev" {
		t.Errorf("the header's Context reads %q, want the context the router passed", got)
	}
}

// The title does not repeat it. The header says which context this is, so a
// title saying it too is the same fact twice on one screen.
func TestTheTitleDoesNotRepeatTheContext(t *testing.T) {
	m := newModel(t)
	if strings.Contains(m.GetTitle(), m.context) {
		t.Errorf("GetTitle() = %q, want the context left to the header", m.GetTitle())
	}
}

// The config file path moved out of the header and into the Paths group, where
// it sits beside the two paths it belongs with.
func TestTheHeaderNoLongerCarriesTheFilePath(t *testing.T) {
	m := newModel(t)
	for _, info := range m.GetHeaderInfo("default") {
		if info.Key == "File" {
			t.Errorf("the header still carries File = %q; it belongs under Paths", info.Value)
		}
	}

	m = focusOn(t, m, "Workspaces dir")
	m.activeTab = 0
	rendered := stripANSI(m.View())
	if !strings.Contains(rendered, "Config file") {
		t.Error("the app tab does not show the config file path")
	}
}

// ↓ onto a read-only row lands past it, and ↑ lands above it — a cursor that
// stopped there would show a focus indicator on a row no key acts upon.
func TestTheCursorStepsPastTheReadOnlyRow(t *testing.T) {
	m := focusOn(t, newModel(t), "Workspaces dir")

	m = feed(t, m, testutil.Key("down"))
	if got := m.current().Label; got != "Log file" {
		t.Errorf("↓ from the workspaces root landed on %q, want Log file", got)
	}

	m = feed(t, m, testutil.Key("up"))
	if got := m.current().Label; got != "Workspaces dir" {
		t.Errorf("↑ landed on %q, want Workspaces dir", got)
	}
}

// Each tab opens on a group heading, and every group is rendered once. A group
// interrupted by another would print its heading twice, which is what tagging a
// contiguous run rather than each field guards against.
func TestEachTabRendersItsGroupHeadingsOnceInOrder(t *testing.T) {
	m := newModel(t)

	for tab := range m.sections {
		m.activeTab = tab
		m.focusedField = 0
		m.bindInput()

		seen := map[string]bool{}
		order := []string{}
		for _, f := range m.fields() {
			if f.Group == "" {
				t.Fatalf("tab %q: %q belongs to no group", m.sections[tab].Title, f.Label)
			}
			if len(order) == 0 || order[len(order)-1] != f.Group {
				if seen[f.Group] {
					t.Errorf("tab %q: group %q resumes after another, so its heading renders twice",
						m.sections[tab].Title, f.Group)
				}
				seen[f.Group] = true
				order = append(order, f.Group)
			}
		}

		rendered := m.View()
		for _, g := range order {
			if strings.Count(rendered, g) < 1 {
				t.Errorf("tab %q: group heading %q is not rendered", m.sections[tab].Title, g)
			}
		}
	}
}

// A locked checkbox is the same row as an unlocked one, minus the interaction:
// theme.RenderCheckbox and theme.RenderCheckboxDisabled both emit the two-cell
// indent themselves, so the view adding one of its own put the disabled scan
// options two cells right of the rest of their group.
func TestEveryCheckboxStartsOnTheSameColumn(t *testing.T) {
	m := newModel(t)
	m.config.Scan.UseTrivyServer = true // locks Misconfiguration and Licenses

	for tab := range m.sections {
		m.activeTab = tab

		indents := map[int][]string{}
		locked := 0
		for _, f := range m.fields() {
			if f.Kind != kindToggle {
				continue
			}
			if m.isDisabled(f) {
				locked++
			}
			row := ansi.Strip(m.renderField(f, false))
			indent := len(row) - len(strings.TrimLeft(row, " "))
			indents[indent] = append(indents[indent], f.Label)
		}

		if len(indents) > 1 {
			t.Errorf("tab %q: checkboxes start on %d different columns: %v",
				m.sections[tab].Title, len(indents), indents)
		}
		if m.sections[tab].Title == "scan" && locked != len(serverModeFields) {
			t.Errorf("scan tab: %d locked checkboxes, want %d — the case this pins is not exercised",
				locked, len(serverModeFields))
		}
	}
}

// The scan tab is the one the user asked to be split by tool.
func TestTheScanTabSeparatesTrivyFromGitleaks(t *testing.T) {
	m := newModel(t)
	groups := map[string][]string{}
	for _, s := range m.sections {
		if s.Title != "scan" {
			continue
		}
		for _, f := range s.Fields {
			groups[f.Group] = append(groups[f.Group], f.Label)
		}
	}

	for _, want := range []string{"Trivy", "Gitleaks"} {
		if len(groups[want]) == 0 {
			t.Errorf("the scan tab has no %q group; it has %v", want, keysOfGroups(groups))
		}
	}
	for _, label := range groups["Trivy"] {
		if strings.Contains(strings.ToLower(label), "gitleaks") {
			t.Errorf("%q is in the Trivy group", label)
		}
	}
	for _, label := range groups["Gitleaks"] {
		if strings.Contains(strings.ToLower(label), "trivy") {
			t.Errorf("%q is in the Gitleaks group", label)
		}
	}
}

func keysOfGroups(m map[string][]string) []string {
	out := make([]string, 0, len(m))
	for k := range m {
		out = append(out, k)
	}
	return out
}

// A theme that cannot be listed still leaves the built-in one, so the cycle has
// something to offer rather than an empty set that ←→ cannot move through.
func TestAnUnreadableThemeDirectoryStillOffersTheDefault(t *testing.T) {
	t.Setenv("HOME", filepath.Join(t.TempDir(), "nonexistent"))
	t.Setenv("USERPROFILE", filepath.Join(t.TempDir(), "nonexistent"))

	m := New(config.Default(), MCPFacts{})

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

// stripANSI returns what the terminal actually shows, so a test can measure a
// column rather than a style.
func stripANSI(s string) string {
	var out strings.Builder
	inEsc := false
	for _, r := range s {
		switch {
		case r == 0x1b:
			inEsc = true
		case inEsc && r == 'm':
			inEsc = false
		case !inEsc:
			out.WriteRune(r)
		}
	}
	return out.String()
}

// chevronAt reports the cell the chevron lands on in a rendered field.
func chevronAt(rendered string) int {
	plain := []rune(stripANSI(rendered))
	for i, r := range plain {
		if string(r) == theme.IconChevronRight {
			return lipgloss.Width(string(plain[:i]))
		}
	}
	return -1
}

// Values line up on one column per tab. Twenty-nine settings whose values each
// start wherever their label happened to end reads as noise.
//
// Measured on the rendered line, not on the padding helper: asserting that
// padHead agrees with padHead is true whatever padHead pads, which is what the
// first version of this test did.
func TestValuesLineUpWithinATab(t *testing.T) {
	m := newModel(t)

	for tab, s := range m.sections {
		m.activeTab = tab
		m.focusedField = 0
		m.bindInput()

		column, from := -1, ""
		for _, f := range s.Fields {
			if f.Kind == kindToggle {
				continue // no chevron, no value
			}
			got := chevronAt(m.renderField(f, false))
			if got < 0 {
				t.Errorf("tab %q: %q renders no chevron", s.Title, f.Label)
				continue
			}
			if column == -1 {
				column, from = got, f.Label
				continue
			}
			if got != column {
				t.Errorf("tab %q: %q puts its chevron at cell %d, %q at %d",
					s.Title, f.Label, got, from, column)
			}
		}
	}
}

// A checkbox brings its own focus indicator (theme.RenderCheckbox), so the view
// must not add a second one.
//
// Asserted on where the row starts, not on focused-versus-blurred width: a
// prefix added to both branches keeps those equal while shifting the whole
// column, which is what the first version of this test missed.
func TestACheckboxIsNotIndentedTwice(t *testing.T) {
	m := focusOn(t, newModel(t), "Auto refresh")
	f := m.current()

	focused := stripANSI(m.renderField(f, true))
	if !strings.HasPrefix(focused, theme.IconCircleSmall) {
		t.Errorf("a focused checkbox renders %q; Rule 120 puts the indicator at column 0", firstCells(focused))
	}

	blurred := stripANSI(m.renderField(f, false))
	if !strings.HasPrefix(blurred, "  "+theme.IconCheckbox) && !strings.HasPrefix(blurred, "  "+theme.IconChecked) {
		t.Errorf("a blurred checkbox renders %q; want two spaces then the box", firstCells(blurred))
	}
}

func firstCells(s string) string {
	r := []rune(s)
	if len(r) > 12 {
		r = r[:12]
	}
	return string(r)
}

// Changing the GitLab URL invalidates a session established against the old
// host. The view says so and flags it for the router; it does not forbid the
// change, which is the same call as for the secret backend.
func TestChangingTheGitLabURLFlagsTheSession(t *testing.T) {
	// This test drains the Cmd, and the footer timer inside it really sleeps.
	testutil.FastTimers(t, &sharedcomponents.FooterMsgDuration)

	m := focusOn(t, newModel(t), "URL")
	m.config.Forge.URL = "https://old.example.com"
	m.input.SetValue("https://new.example.com")

	updated, cmd := m.Update(testutil.Key("down"))
	m = updated.(Model)

	if m.config.Forge.URL != "https://new.example.com" {
		t.Fatalf("URL = %q, want the typed value committed", m.config.Forge.URL)
	}
	// The command comes from internal/command rather than from a literal, so the
	// message follows a rename instead of quietly outliving it.
	if !strings.Contains(m.footer.Text(), ":"+string(command.ViewGitAuth)) {
		t.Errorf("footer = %q, want it to say where to sign in again", m.footer.Text())
	}

	var saw *ConfigSavedMsg
	for _, msg := range testutil.Msgs(cmd) {
		if s, ok := msg.(ConfigSavedMsg); ok {
			saw = &s
		}
	}
	if saw == nil {
		t.Fatal("no ConfigSavedMsg was emitted")
	}
	if !saw.ForgeChanged {
		t.Error("ForgeChanged is false, so the router would keep a session pointed at the old host")
	}
}

// Retyping the same URL is not a change, and must not close a working session.
func TestRetypingTheSameGitLabURLChangesNothing(t *testing.T) {
	m := focusOn(t, newModel(t), "URL")
	m.config.Forge.URL = "https://same.example.com"
	m.input.SetValue("https://same.example.com")

	updated, cmd := m.Update(testutil.Key("down"))
	m = updated.(Model)

	if m.footer.IsSet() {
		t.Errorf("footer = %q for an unchanged URL", m.footer.Text())
	}
	for _, msg := range testutil.Msgs(cmd) {
		if s, ok := msg.(ConfigSavedMsg); ok && s.ForgeChanged {
			t.Error("an unchanged URL was reported as changed")
		}
	}
}

// Only one view may write a setting. Both used to write gitlab.url, so neither
// was authoritative and editing it in one left the other stale.
func TestOnlyTheConfigurationViewOwnsTheForgeURL(t *testing.T) {
	if !strings.Contains(sourceOf(t, "../forge/auth/update.go"), "m.config.Forge.URL") {
		t.Skip("the auth view no longer reads the URL at all")
	}
	if strings.Contains(sourceOf(t, "../forge/auth/update.go"), "config.Forge.URL = ") {
		t.Error("the auth view assigns forge.url; the configuration view owns it")
	}
}

func sourceOf(t *testing.T, path string) string {
	t.Helper()
	data, err := os.ReadFile(path)
	if err != nil {
		t.Fatalf("reading %s: %v", path, err)
	}
	return string(data)
}
