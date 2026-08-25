package explorer

import (
	"errors"
	"strings"
	"testing"

	tea "github.com/charmbracelet/bubbletea"

	"github.com/anthnel/devdesk/internal/command"
	"github.com/anthnel/devdesk/internal/config"
	"github.com/anthnel/devdesk/internal/forge"
	"github.com/anthnel/devdesk/internal/shared"
	"github.com/anthnel/devdesk/internal/ui/keymap"
	"github.com/anthnel/devdesk/internal/ui/testutil"
	"github.com/anthnel/devdesk/internal/ui/theme"
)

// ── View branches ────────────────────────────────────────────────────────────

// Without a client the view has nothing to browse, and says which command
// fixes that rather than showing an empty table.
func TestViewWithoutAClientPointsAtTheAuthView(t *testing.T) {
	m := feed(t, New(testConfig(), &shared.State{}), tea.WindowSizeMsg{Width: 160, Height: 30})

	view := m.View()

	if !strings.Contains(view, "not authenticated") {
		t.Errorf("the view does not say it is unauthenticated:\n%s", view)
	}
	// The command comes from internal/command, so this follows the rename
	// rather than outliving it — which is what §3.6 step 5 moved it there for.
	if !strings.Contains(view, string(command.ViewGitAuth)) {
		t.Error("the view does not name the command that authenticates")
	}
}

// Before the first load completes the footer reports it, rather than the body
// saying "No groups found" — which would be a lie while the request is in
// flight. The load belongs to the footer alone, so the tree keeps its place.
func TestTheLoadIsReportedInTheFooterUntilTheFirstLoadLands(t *testing.T) {
	m := newTestModel(t)

	if footer := m.RenderFooter(160); !strings.Contains(footer, "Loading GitLab groups") {
		t.Errorf("a model that has never loaded does not report it in the footer:\n%s", footer)
	}
	if view := m.View(); strings.Contains(view, "Loading GitLab groups") {
		t.Errorf("the body reports the load; it belongs in the footer alone:\n%s", view)
	}
	if view := m.View(); strings.Contains(view, "No groups found") {
		t.Errorf("the body says there is nothing while the load is in flight:\n%s", view)
	}

	empty := feed(t, m, RootGroupsLoadedMsg{})
	if view := empty.View(); !strings.Contains(view, "No groups found") {
		t.Errorf("an empty result does not show the empty state:\n%s", view)
	}
}

func TestViewShowsTheLoadError(t *testing.T) {
	m := feed(t, newTestModel(t), LoadErrorMsg{Error: errors.New("403 forbidden")})

	if view := m.View(); !strings.Contains(view, "403 forbidden") {
		t.Errorf("the view does not show the load error:\n%s", view)
	}
}

func TestViewShowsTheRows(t *testing.T) {
	view := drilledModel(t).View()

	for _, want := range []string{"sub", "api", "legacy", "Group", "Project"} {
		if !strings.Contains(view, want) {
			t.Errorf("the table does not show %q:\n%s", want, view)
		}
	}
}

// The modes that take over the viewport.
func TestViewShowsTheModalForEachMode(t *testing.T) {
	tests := []struct {
		name string
		open func(*testing.T) Model
		want string
	}{
		{"loading templates", func(t *testing.T) Model {
			return feed(t, drilledModel(t), testutil.Key(keymap.New))
		}, "Loading templates"},
		{"delete confirmation", func(t *testing.T) Model {
			return feed(t, drilledModel(t), testutil.Key(keymap.Delete))
		}, "Delete Group"},
	}

	for _, tt := range tests {
		t.Run(tt.name, func(t *testing.T) {
			if view := tt.open(t).View(); !strings.Contains(view, tt.want) {
				t.Errorf("the %s view does not show %q:\n%s", tt.name, tt.want, view)
			}
		})
	}
}

// Rule 112: the creation form takes the whole viewport rather than opening as a
// modal.
func TestViewShowsTheCreationFormFullWidth(t *testing.T) {
	m := feed(t, drilledModel(t), testutil.Key(keymap.New), TemplatesLoadedMsg{})

	view := m.View()

	if !strings.Contains(view, "Name") || !strings.Contains(view, "Visibility") {
		t.Errorf("the creation form is not rendered:\n%s", view)
	}
	if strings.Contains(view, "legacy") {
		t.Error("the table is still visible behind the form")
	}
}

// Rule 122: a styled cell is truncated mid-escape by bubbles/table and bleeds
// into every row below it. Colour has to be forced first — under go test
// lipgloss finds no TTY, strips every escape, and the check passes vacuously.
func TestTableCellsCarryNoEscapeSequences(t *testing.T) {
	withTrueColor(t)

	m := drilledModel(t)
	for _, row := range m.table.Table().Rows() {
		for i, cell := range row {
			if strings.Contains(cell, "\x1b") {
				t.Errorf("cell %d of row %v carries an escape sequence", i, row)
			}
		}
	}
}

// ── Footer ───────────────────────────────────────────────────────────────────

// Rule 124: the router sizes the viewport from GetFooterHeight(), so it has to
// match what RenderFooter() actually emits.
func TestFooterHeightMatchesWhatIsRendered(t *testing.T) {
	tests := []struct {
		name string
		open func(*testing.T) Model
	}{
		{"unauthenticated", func(t *testing.T) Model {
			return feed(t, New(testConfig(), &shared.State{}), tea.WindowSizeMsg{Width: 160, Height: 30})
		}},
		{"loaded", func(t *testing.T) Model { return loadedModel(t) }},
		{"drilled", func(t *testing.T) Model { return drilledModel(t) }},
		{"filtering", func(t *testing.T) Model { return feed(t, drilledModel(t), testutil.Key("/")) }},
		{"empty", func(t *testing.T) Model { return feed(t, newTestModel(t), RootGroupsLoadedMsg{}) }},
		{"confirming a delete", func(t *testing.T) Model {
			return feed(t, drilledModel(t), testutil.Key(keymap.Delete))
		}},
		{"creating", func(t *testing.T) Model {
			return feed(t, drilledModel(t), testutil.Key(keymap.New), TemplatesLoadedMsg{})
		}},
	}

	for _, tt := range tests {
		t.Run(tt.name, func(t *testing.T) {
			m := tt.open(t)

			lines := strings.Count(m.RenderFooter(160), "\n") + 1

			if got := m.GetFooterHeight(); got != lines {
				t.Errorf("GetFooterHeight() = %d, RenderFooter() emitted %d lines", got, lines)
			}
		})
	}
}

// The tab bar is a breadcrumb of the path, always visible below the table
// (Rule 123).
func TestFooterShowsTheBreadcrumb(t *testing.T) {
	footer := loadedModel(t).RenderFooter(160)
	if !strings.Contains(footer, "home") {
		t.Errorf("the root footer has no home tab:\n%s", footer)
	}

	footer = drilledModel(t).RenderFooter(160)
	if !strings.Contains(footer, "alpha") {
		t.Errorf("the footer does not name the group being browsed:\n%s", footer)
	}
}

func TestFooterShowsTheErrorMessage(t *testing.T) {
	m := feed(t, drilledModel(t), DeleteCompleteMsg{Error: errors.New("boom")})

	if !strings.Contains(m.RenderFooter(160), "Delete failed") {
		t.Error("the footer does not show the delete failure")
	}
}

// The bar is hidden until a filter exists, and the router keys the viewport
// corners off FilterBarVisible() (Rule 136).
func TestFilterBarVisibility(t *testing.T) {
	if loadedModel(t).FilterBarVisible() {
		t.Error("FilterBarVisible() is true with no filter")
	}

	searching := feed(t, loadedModel(t), testutil.Key("/"))
	if !searching.FilterBarVisible() {
		t.Error("FilterBarVisible() is false while the search box has focus")
	}

	// Nothing to filter, so the bar has nothing to sit under.
	empty := feed(t, newTestModel(t), RootGroupsLoadedMsg{}, testutil.Key("/"))
	if empty.FilterBarVisible() {
		t.Error("FilterBarVisible() is true with no rows")
	}

	modal := feed(t, drilledModel(t), testutil.Key("/"))
	modal.mode = ModeConfirmingDelete
	if modal.FilterBarVisible() {
		t.Error("FilterBarVisible() is true behind a modal")
	}
}

// ── Header ───────────────────────────────────────────────────────────────────

func TestTitleFollowsTheCreationForm(t *testing.T) {
	if got := loadedModel(t).GetTitle(); !strings.Contains(got, "GitLab Explorer") {
		t.Errorf("GetTitle() = %q", got)
	}

	creating := feed(t, drilledModel(t), testutil.Key(keymap.New), TemplatesLoadedMsg{})
	if got := creating.GetTitle(); !strings.Contains(got, theme.IconChevronRight) {
		t.Errorf("GetTitle() = %q while creating, want the form appended", got)
	}
}

func TestHeaderInfoCarriesTheContextAndUser(t *testing.T) {
	m := loadedModel(t)

	info := m.GetHeaderInfo("work")
	if len(info) != 1 || info[0].Value != "work" {
		t.Errorf("GetHeaderInfo() = %+v with no user, want the context alone", info)
	}

	m.shared.CurrentUser = newUser("anthnel")
	info = m.GetHeaderInfo("work")
	if len(info) != 2 || info[1].Value != "@anthnel" {
		t.Errorf("GetHeaderInfo() = %+v, want the context and the @username", info)
	}
}

// Rule 130: a shortcut for an action that cannot run must not be advertised.
func TestShortcutsFollowTheState(t *testing.T) {
	tests := []struct {
		name    string
		open    func(*testing.T) Model
		want    []string
		notWant []string
	}{
		{
			name: "unauthenticated",
			open: func(t *testing.T) Model {
				return feed(t, New(testConfig(), &shared.State{}), tea.WindowSizeMsg{Width: 160, Height: 30})
			},
			notWant: []string{keymap.New, keymap.Delete, "p"},
		},
		{
			name: "browsing",
			open: func(t *testing.T) Model { return drilledModel(t) },
			want: []string{keymap.New, keymap.Delete, keymap.Clone, keymap.Web, ".", "/", "ctrl+r"},
		},
		{
			name: "selecting what to clone",
			open: func(t *testing.T) Model {
				return feed(t, drilledModel(t), testutil.Key(keymap.Clone))
			},
			want:    []string{"space", "enter", "esc"},
			notWant: []string{keymap.New, keymap.Delete},
		},
		{
			name:    "cloning",
			open:    func(t *testing.T) Model { return cloningModel(t) },
			want:    []string{"esc", "/"},
			notWant: []string{keymap.New, keymap.Delete, "space"},
		},
		{
			name: "confirming a delete",
			open: func(t *testing.T) Model {
				return feed(t, drilledModel(t), testutil.Key(keymap.Delete))
			},
			want:    []string{"space", "esc"},
			notWant: []string{keymap.Delete},
		},
		{
			name: "creating",
			open: func(t *testing.T) Model {
				return feed(t, drilledModel(t), testutil.Key(keymap.New), TemplatesLoadedMsg{})
			},
			want:    []string{"enter", "esc"},
			notWant: []string{keymap.New},
		},
	}

	for _, tt := range tests {
		t.Run(tt.name, func(t *testing.T) {
			keys := map[string]bool{}
			for _, s := range tt.open(t).GetShortcuts() {
				keys[s.Key] = true
			}
			for _, key := range tt.want {
				if !keys[key] {
					t.Errorf("%q is not advertised", key)
				}
			}
			for _, key := range tt.notWant {
				if keys[key] {
					t.Errorf("%q is advertised but cannot run here", key)
				}
			}
		})
	}
}

// W opens the highlighted node in a browser, so it is greyed — never
// dropped — for a node the forge gave no URL for.
func TestBrowserShortcutNeedsAURL(t *testing.T) {
	m := drilledModel(t)
	for _, child := range m.currentGroupNode.Children {
		child.WebURL = ""
	}

	if !testutil.HasShortcut(m.GetShortcuts(), keymap.Web) {
		t.Fatal("W disappeared for a node with no web URL instead of being greyed")
	}
	if !testutil.ShortcutDisabled(m.GetShortcuts(), keymap.Web) {
		t.Error("W is offered for a node with no web URL")
	}
}

// A refresh must not empty the column and refill it: the tree is the same
// screen either side of a load, so the keys are greyed in place.
func TestALoadGreysTheKeysInsteadOfRemovingThem(t *testing.T) {
	m := drilledModel(t)
	settled := testutil.ShortcutKeys(m.GetShortcuts())

	m.loading = true

	if got := testutil.ShortcutKeys(m.GetShortcuts()); strings.Join(got, " ") != strings.Join(settled, " ") {
		t.Errorf("a load advertises %v, want the same keys as a settled tree %v", got, settled)
	}
	if !testutil.ShortcutDisabled(m.GetShortcuts(), keymap.Clone) {
		t.Error("C is offered while the tree is loading")
	}
	if !testutil.ShortcutEnabled(m.GetShortcuts(), "ctrl+r") {
		t.Error("ctrl+r is greyed while loading; a refresh is exactly what still applies")
	}
}

// Rule 137: descriptions read as imperative actions, capitalised.
func TestShortcutDescriptionsAreImperative(t *testing.T) {
	models := []Model{
		drilledModel(t),
		feed(t, drilledModel(t), testutil.Key(keymap.Delete)),
		feed(t, drilledModel(t), testutil.Key(keymap.Clone)),
		cloningModel(t),
		feed(t, cloningModel(t), CloneRunFinishedMsg{}),
		feed(t, drilledModel(t), testutil.Key(keymap.New), TemplatesLoadedMsg{}),
	}

	for _, m := range models {
		for _, s := range m.GetShortcuts() {
			if s.Description == "" {
				t.Errorf("shortcut %q has no description", s.Key)
				continue
			}
			if first := s.Description[:1]; first != strings.ToUpper(first) {
				t.Errorf("description %q does not start with a capital", s.Description)
			}
		}
	}
}

// ── Help ─────────────────────────────────────────────────────────────────────

func TestHelpContentIsPopulated(t *testing.T) {
	content := loadedModel(t).GetHelpContent()

	if content.Title == "" || content.Description == "" {
		t.Error("the help has no title or description")
	}
	if len(content.KeyBindings) == 0 || len(content.Sections) == 0 {
		t.Error("the help lists no key bindings or sections")
	}
}

// Every key the header advertises should be explained in the help — the check
// that caught the drift in the status, containers and workspaces views.
func TestHelpDocumentsTheAdvertisedShortcuts(t *testing.T) {
	m := drilledModel(t)

	documented := map[string]bool{}
	for _, kb := range m.GetHelpContent().KeyBindings {
		documented[strings.ToLower(kb.Key)] = true
		for _, key := range strings.Split(kb.Key, "/") {
			if trimmed := strings.ToLower(strings.TrimSpace(key)); trimmed != "" {
				documented[trimmed] = true
			}
		}
	}

	for _, s := range m.GetShortcuts() {
		key := strings.ToLower(s.Key)
		if key == "←→" {
			continue // documented as separate → and ← entries
		}
		if !documented[key] {
			t.Errorf("shortcut %q is advertised in the header but absent from the help", s.Key)
		}
	}
}

// The CI column is icons only, so the legend is the only place their meaning is
// written down.
func TestHelpExplainsEveryPipelineIcon(t *testing.T) {
	var legend string
	for _, section := range loadedModel(t).GetHelpContent().Sections {
		if strings.Contains(section.Title, "CI") {
			legend = section.Body
		}
	}
	if legend == "" {
		t.Fatal("the help has no CI legend")
	}

	for _, status := range []string{"success", "failed", "running", "pending", "canceled", "skipped", "manual"} {
		icon := pipelineStatusLabel(&TreeNode{Type: NodeTypeProject, PipelineStatus: status})
		if !strings.Contains(legend, icon) {
			t.Errorf("the legend does not explain the %q icon %q", status, icon)
		}
	}
}

// ── Cell labels ──────────────────────────────────────────────────────────────

func TestPipelineStatusLabels(t *testing.T) {
	tests := []struct {
		status string
		want   string
	}{
		{"success", theme.IconOK},
		{"failed", theme.IconError},
		{"running", theme.IconRunning},
		{"pending", theme.IconPending},
		{"waiting_for_resource", theme.IconPending},
		{"preparing", theme.IconPending},
		{"canceled", theme.IconCanceled},
		{"skipped", theme.IconSkipped},
		{"manual", theme.IconManual},
		{"scheduled", "scheduled"}, // unknown statuses fall through verbatim
		{"", ""},
	}

	for _, tt := range tests {
		node := &TreeNode{Type: NodeTypeProject, PipelineStatus: tt.status}
		if got := pipelineStatusLabel(node); got != tt.want {
			t.Errorf("pipelineStatusLabel(%q) = %q, want %q", tt.status, got, tt.want)
		}
	}
}

// Groups have no pipelines, so the column stays empty for them whatever the
// field holds.
func TestGroupsShowNoPipelineStatus(t *testing.T) {
	node := &TreeNode{Type: NodeTypeGroup, PipelineStatus: "success"}

	if got := pipelineStatusLabel(node); got != "" {
		t.Errorf("pipelineStatusLabel() = %q for a group", got)
	}
}

func TestTypeAndVisibilityLabels(t *testing.T) {
	gitlab := forge.VocabularyFor(config.ForgeGitLab)
	if got := nodeTypeLabel(gitlab, &TreeNode{Type: NodeTypeGroup}); got != "Group" {
		t.Errorf("nodeTypeLabel(group) = %q", got)
	}
	if got := nodeTypeLabel(gitlab, &TreeNode{Type: NodeTypeProject}); got != "Project" {
		t.Errorf("nodeTypeLabel(project) = %q", got)
	}

	// The same two rows in a GitHub context read differently, which is the
	// whole point of the vocabulary.
	github := forge.VocabularyFor(config.ForgeGitHub)
	if got := nodeTypeLabel(github, &TreeNode{Type: NodeTypeGroup}); got != "Organization" {
		t.Errorf("nodeTypeLabel(github group) = %q, want Organization", got)
	}
	if got := nodeTypeLabel(github, &TreeNode{Type: NodeTypeProject}); got != "Repository" {
		t.Errorf("nodeTypeLabel(github project) = %q, want Repository", got)
	}

	labels := map[string]string{
		"public": "Public", "internal": "Internal", "private": "Private",
		"": "", "nonsense": "",
	}
	for visibility, want := range labels {
		if got := visibilityLabel(&TreeNode{Visibility: visibility}); got != want {
			t.Errorf("visibilityLabel(%q) = %q, want %q", visibility, got, want)
		}
	}
}

// Rule 127: relative times come from the shared helper, and a missing date
// renders as an empty cell rather than a zero time.
func TestTimeAgoOfAMissingDateIsEmpty(t *testing.T) {
	if got := timeAgo(nil); got != "" {
		t.Errorf("timeAgo(nil) = %q", got)
	}
	if got := timeAgo(at(1)); got == "" {
		t.Error("timeAgo() of a real date is empty")
	}
}

// TestAGitHubContextSpeaksGitHub is what the vocabulary is for, seen from the
// outside: the same view, the same rows, a different set of nouns.
//
// It is the test the grep guard cannot be — vocabtest refuses a *literal*, and
// a screen can be free of literals and still read the wrong forge's words
// because a site resolved the vocabulary for the wrong context.
func TestAGitHubContextSpeaksGitHub(t *testing.T) {
	cfg := testConfig()
	cfg.Forge.Type = config.ForgeGitHub
	cfg.Forge.URL = "https://github.com"

	m := feed(t, New(cfg, &shared.State{}), tea.WindowSizeMsg{Width: 160, Height: 30})

	if got := m.GetTitle(); !strings.Contains(got, "GitHub Explorer") {
		t.Errorf("GetTitle() = %q, want it to name GitHub", got)
	}
	if strings.Contains(m.GetTitle(), "GitLab") {
		t.Errorf("GetTitle() = %q still names GitLab", m.GetTitle())
	}

	// Signed out is the screen that needs the words before any session exists.
	// No colour profile is forced, so lipgloss emits no escapes under go test.
	out := m.View()
	if !strings.Contains(out, "GitHub not authenticated") {
		t.Errorf("the signed-out screen does not name GitHub:\n%s", out)
	}

	help := m.GetHelpContent()
	if !strings.Contains(help.Description, "organizations") {
		t.Errorf("the help still describes groups rather than organizations: %q", help.Description)
	}
	if strings.Contains(help.Title, "GitLab") {
		t.Errorf("the help title still names GitLab: %q", help.Title)
	}
}
