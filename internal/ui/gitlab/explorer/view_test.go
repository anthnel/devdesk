package explorer

import (
	"errors"
	"strings"
	"testing"

	tea "github.com/charmbracelet/bubbletea"

	"github.com/anthnel/devdesk/internal/shared"
	"github.com/anthnel/devdesk/internal/ui/components"
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
	if !strings.Contains(view, "gitlab-auth") {
		t.Error("the view does not name the command that authenticates")
	}
}

// Before the first load completes the view shows the spinner rather than
// "No groups found", which would be a lie while the request is in flight.
func TestViewShowsTheSpinnerUntilTheFirstLoadLands(t *testing.T) {
	m := newTestModel(t)

	if view := m.View(); !strings.Contains(view, "Loading GitLab groups") {
		t.Errorf("a model that has never loaded does not show the spinner:\n%s", view)
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

// The three long-running modes each take over the viewport.
func TestViewShowsTheModalForEachMode(t *testing.T) {
	tests := []struct {
		name string
		open func(*testing.T) Model
		want string
	}{
		{"pulling", func(t *testing.T) Model {
			m := feed(t, drilledModel(t), testutil.Key("p"))
			return feed(t, m, PullDestinationSelectedMsg{Path: t.TempDir()})
		}, "Pulling"},
		{"loading templates", func(t *testing.T) Model {
			return feed(t, drilledModel(t), testutil.Key("ctrl+n"))
		}, "Loading templates"},
		{"report", func(t *testing.T) Model {
			return feed(t, drilledModel(t), PullCompleteMsg{Report: components.PullReport{Cloned: []string{"alpha/api"}}})
		}, "Pull Complete"},
		{"delete confirmation", func(t *testing.T) Model {
			return feed(t, drilledModel(t), testutil.Key("ctrl+d"))
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
	m := feed(t, drilledModel(t), testutil.Key("ctrl+n"), TemplatesLoadedMsg{})

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

	for _, row := range drilledModel(t).table.Rows() {
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
			return feed(t, drilledModel(t), testutil.Key("ctrl+d"))
		}},
		{"creating", func(t *testing.T) Model {
			return feed(t, drilledModel(t), testutil.Key("ctrl+n"), TemplatesLoadedMsg{})
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

	creating := feed(t, drilledModel(t), testutil.Key("ctrl+n"), TemplatesLoadedMsg{})
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
			notWant: []string{"ctrl+n", "ctrl+d", "p"},
		},
		{
			name: "browsing",
			open: func(t *testing.T) Model { return drilledModel(t) },
			want: []string{"ctrl+n", "ctrl+d", "p", "ctrl+w", ".", "/", "ctrl+r"},
		},
		{
			name: "pulling",
			open: func(t *testing.T) Model {
				m := drilledModel(t)
				m.mode = ModePulling
				return m
			},
			notWant: []string{"ctrl+n", "ctrl+d", "p", "esc"},
		},
		{
			name: "report open",
			open: func(t *testing.T) Model {
				return feed(t, drilledModel(t), PullCompleteMsg{})
			},
			want:    []string{"enter/esc"},
			notWant: []string{"ctrl+n"},
		},
		{
			name: "confirming a delete",
			open: func(t *testing.T) Model {
				return feed(t, drilledModel(t), testutil.Key("ctrl+d"))
			},
			want:    []string{"space", "esc"},
			notWant: []string{"ctrl+d"},
		},
		{
			name: "creating",
			open: func(t *testing.T) Model {
				return feed(t, drilledModel(t), testutil.Key("ctrl+n"), TemplatesLoadedMsg{})
			},
			want:    []string{"enter", "esc"},
			notWant: []string{"ctrl+n"},
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

// ctrl+w opens the highlighted node in a browser, so it must not be advertised
// for a node GitLab gave no URL for.
func TestBrowserShortcutNeedsAURL(t *testing.T) {
	m := drilledModel(t)
	for _, child := range m.currentGroupNode.Children {
		child.WebURL = ""
	}

	for _, s := range m.GetShortcuts() {
		if s.Key == "ctrl+w" {
			t.Error("ctrl+w is advertised for a node with no web URL")
		}
	}
}

// Rule 137: descriptions read as imperative actions, capitalised.
func TestShortcutDescriptionsAreImperative(t *testing.T) {
	models := []Model{
		drilledModel(t),
		feed(t, drilledModel(t), testutil.Key("ctrl+d")),
		feed(t, drilledModel(t), PullCompleteMsg{}),
		feed(t, drilledModel(t), testutil.Key("ctrl+n"), TemplatesLoadedMsg{}),
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
	if got := nodeTypeLabel(&TreeNode{Type: NodeTypeGroup}); got != "Group" {
		t.Errorf("nodeTypeLabel(group) = %q", got)
	}
	if got := nodeTypeLabel(&TreeNode{Type: NodeTypeProject}); got != "Project" {
		t.Errorf("nodeTypeLabel(project) = %q", got)
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
