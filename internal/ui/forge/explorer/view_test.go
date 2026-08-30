package explorer

import (
	"errors"
	"strings"
	"testing"

	tea "github.com/charmbracelet/bubbletea"
	"github.com/charmbracelet/lipgloss"

	"github.com/anthnel/devdesk/internal/command"
	"github.com/anthnel/devdesk/internal/config"
	"github.com/anthnel/devdesk/internal/forge"
	"github.com/anthnel/devdesk/internal/shared"
	"github.com/anthnel/devdesk/internal/ui/datatable"
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
// saying anything. The load belongs to the footer alone, and an empty table
// stays a table (Rule 139) — header, no rows, with the count in GetHeaderInfo.
func TestTheLoadIsReportedInTheFooterUntilTheFirstLoadLands(t *testing.T) {
	m := newTestModel(t)

	if footer := m.RenderFooter(160); !strings.Contains(footer, "Loading GitLab groups") {
		t.Errorf("a model that has never loaded does not report it in the footer:\n%s", footer)
	}
	if view := m.View(); strings.Contains(view, "Loading GitLab groups") {
		t.Errorf("the body reports the load; it belongs in the footer alone:\n%s", view)
	}
	if view, table := m.View(), m.renderTable(); view != table {
		t.Errorf("the body before the first load is %q, want the plain table view", view)
	}

	empty := feed(t, m, RootGroupsLoadedMsg{})
	if view, table := empty.View(), empty.renderTable(); view != table {
		t.Errorf("an empty result is %q, want the plain table view", view)
	}
	if info := empty.GetHeaderInfo("work"); len(info) == 0 || info[len(info)-1].Value != "0" {
		t.Errorf("GetHeaderInfo() = %+v, want the group count at 0", info)
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

	// The kind is a glyph now, not the forge's word: the Type column became the
	// untitled icon column (§3.56). The words survive in the help legend, which
	// TestTheHelpNamesTheRowGlyphs checks.
	for _, want := range []string{"sub", "api", "legacy", theme.IconNamespace, theme.IconRepository} {
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
	if len(info) != 2 || info[0].Value != "work" {
		t.Errorf("GetHeaderInfo() = %+v with no user, want the context and the row count", info)
	}

	m.shared.CurrentUser = newUser("anthnel")
	info = m.GetHeaderInfo("work")
	if len(info) != 3 || info[1].Value != "@anthnel" {
		t.Errorf("GetHeaderInfo() = %+v, want the context, the @username and the row count", info)
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
		// `created` and `scheduled` are GitLab's, and they used to fall through
		// verbatim — this line asserted the defect (D66). They join the four
		// other ways a pipeline has not started yet.
		{"created", theme.IconPending},
		{"scheduled", theme.IconPending},
		{"", ""},
		// A backend that breaks the contract still renders something, which is
		// the only reason the default branch survives.
		{"not_a_status", "not_a_status"},
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

// ── §3.56 : the icon columns ─────────────────────────────────────────────────

// The first column carries the kind, and the two kinds are two glyphs.
func TestTheFirstColumnSaysWhatTheRowIs(t *testing.T) {
	group := explorerRow{node: &TreeNode{Type: NodeTypeGroup}}
	project := explorerRow{node: &TreeNode{Type: NodeTypeProject}}

	if got := iconCell(group); got != theme.IconNamespace {
		t.Errorf("iconCell(group) = %q, want the namespace glyph", got)
	}
	if got := iconCell(project); got != theme.IconRepository {
		t.Errorf("iconCell(project) = %q, want the repository glyph", got)
	}
}

// None of the explorer's glyphs is one the workspaces view uses. The two lists
// answer different questions — what the forge holds, what is on disk — and a
// row that looked the same in both would claim they are the same object.
func TestTheExplorerGlyphsAreNotTheWorkspaceOnes(t *testing.T) {
	ws := []string{theme.IconGitBranch, theme.IconDirectory, theme.IconDirectoryOpen, theme.IconFile}
	for _, mine := range []string{theme.IconNamespace, theme.IconRepository} {
		for _, theirs := range ws {
			if mine == theirs {
				t.Errorf("the explorer and workspaces share the glyph %q", mine)
			}
		}
	}
}

// In the clone selection the glyph becomes a checkbox — Rule 125 fixes the
// column at two cells, so the two cannot sit side by side.
func TestTheSelectionReplacesTheGlyphWithACheckbox(t *testing.T) {
	row := explorerRow{node: &TreeNode{Type: NodeTypeProject}, selecting: true, check: theme.CheckAll}

	if got := iconCell(row); got != theme.IconChecked {
		t.Errorf("iconCell(ticked) = %q, want the ticked box", got)
	}
}

// …and the colour still says which kind it is, which is what makes sharing the
// column honest rather than lossy.
func TestTheKindSurvivesTheSelectionAsAColour(t *testing.T) {
	group := explorerRow{node: &TreeNode{Type: NodeTypeGroup}, selecting: true}
	project := explorerRow{node: &TreeNode{Type: NodeTypeProject}, selecting: true}

	if iconCell(group) != iconCell(project) {
		t.Fatal("the two kinds show different boxes — this test is about the colour")
	}
	if iconStyle(group).GetForeground() == iconStyle(project).GetForeground() {
		t.Error("a ticked group and a ticked project are the same colour — the kind is lost")
	}
}

// Rule 122: what a Cell returns is measured, so it must carry no escape.
func TestTheIconCellsCarryNoEscapeSequence(t *testing.T) {
	rows := []explorerRow{
		{node: &TreeNode{Type: NodeTypeGroup, Visibility: "public"}},
		{node: &TreeNode{Type: NodeTypeProject, Visibility: "private"}, selecting: true, check: theme.CheckSome},
	}
	for _, row := range rows {
		if strings.Contains(iconCell(row), "\x1b") {
			t.Errorf("iconCell carries an escape sequence: %q", iconCell(row))
		}
		if strings.Contains(visibilityIcon(row.node), "\x1b") {
			t.Errorf("visibilityIcon carries an escape sequence: %q", visibilityIcon(row.node))
		}
	}
}

// The three visibilities are three glyphs, and anything else is an empty cell —
// a placeholder would be a wrong answer where nothing is a true one.
func TestVisibilityIsAGlyphOrNothing(t *testing.T) {
	want := map[string]string{
		"public":   theme.IconVisibilityPublic,
		"internal": theme.IconVisibilityInternal,
		"private":  theme.IconLock,
		"":         "",
		"nonsense": "",
	}
	for visibility, glyph := range want {
		if got := visibilityIcon(&TreeNode{Visibility: visibility}); got != glyph {
			t.Errorf("visibilityIcon(%q) = %q, want %q", visibility, got, glyph)
		}
	}
	if got := visibilityRole(&TreeNode{Visibility: "nonsense"}); got != "" {
		t.Errorf("visibilityRole(nonsense) = %q, want no role", got)
	}
}

// The order the user asked for, read off the declaration rather than off a
// rendered row: a header can be truncated, a declaration cannot.
func TestTheColumnsAreInTheDeclaredOrder(t *testing.T) {
	want := []string{"", "Name", "Slug", "Visibility", "Role", "CI", "Created", "Activity"}

	cols := explorerColumns()
	if len(cols) != len(want) {
		t.Fatalf("the table has %d columns, want %d", len(cols), len(want))
	}
	for i, title := range want {
		if cols[i].Title != title {
			t.Errorf("column %d is %q, want %q", i, cols[i].Title, title)
		}
	}
}

// Rule 125: the icon column is untitled, two cells, and adds nothing to the
// filter or the sort. The width is checked by datatable's own source walk; what
// is checked here is that this table's first column is the one it walks.
func TestTheIconColumnAddsNoTextAndNoOrder(t *testing.T) {
	icon := explorerColumns()[0]

	if icon.Title != "" {
		t.Errorf("the icon column is titled %q", icon.Title)
	}
	if icon.MinWidth != datatable.IconColumnWidth {
		t.Errorf("the icon column is %d cells wide, want %d", icon.MinWidth, datatable.IconColumnWidth)
	}
	if icon.Less != nil {
		t.Error("the icon column declares a comparator")
	}
	if icon.Search != nil {
		t.Error("the icon column declares a search key")
	}
}

// Visibility lost its comparator with §3.56, and the header is what it costs.
// Three values in an order nobody would agree on are not a sort; dropping it is
// what lets the whole word fit in ten cells instead of twelve.
func TestVisibilityShowsTheWordAndDoesNotSort(t *testing.T) {
	for _, col := range explorerColumns() {
		if col.Title != "Visibility" {
			continue
		}
		if col.Less != nil {
			t.Error("the Visibility column sorts again — the header no longer fits")
		}
		if col.MinWidth != lipgloss.Width("Visibility") {
			t.Errorf("the Visibility column is %d cells, want the width of its own header", col.MinWidth)
		}
		return
	}
	t.Fatal("no column titled Visibility")
}

// Rule 114: the forge's words left the table with the Type column, so the help
// is where they now live. A legend naming neither glyph would leave a GitHub
// user with no way to learn what the row icons mean.
func TestTheHelpNamesTheRowGlyphs(t *testing.T) {
	m := newTestModel(t)

	var legend string
	for _, section := range m.GetHelpContent().Sections {
		if section.Title == "Row Icons" {
			legend = section.Body
		}
	}
	if legend == "" {
		t.Fatal("the help has no Row Icons section")
	}
	for _, want := range []string{
		theme.IconNamespace, theme.IconRepository,
		theme.IconVisibilityPublic, theme.IconVisibilityInternal, theme.IconLock,
		"Group", "Project", "Public", "Internal", "Private",
	} {
		if !strings.Contains(legend, want) {
			t.Errorf("the Row Icons legend does not mention %q:\n%s", want, legend)
		}
	}
}

// ── D66 : every status a backend may report reaches a glyph ──────────────────

// The guard D66 did not have. A value missing from pipelineStatusLabel's switch
// falls through to its default and prints its own name, truncated to the six
// cells the CI column has — which is how `in_progress` reached the screen as
// `in_pr…`.
//
// It walks forge.CIStatuses() rather than a list written here, so a status
// added to the interface fails this test until the view has an answer for it.
func TestEveryDeclaredCIStatusHasAnIcon(t *testing.T) {
	for _, status := range forge.CIStatuses() {
		node := &TreeNode{Type: NodeTypeProject, PipelineStatus: status}
		got := pipelineStatusLabel(node)

		if got == status {
			t.Errorf("status %q falls through to the default branch and prints itself", status)
		}
		if got == "" {
			t.Errorf("status %q renders an empty cell, which means no pipeline at all", status)
		}
		if lipgloss.Width(got) > 2 {
			t.Errorf("status %q renders %q, which is wider than a glyph", status, got)
		}
	}
}

// Rule 122: the CI cell is measured before it is styled, so it carries no
// escape sequence — the colour is pipelineStatusStyle's job.
func TestTheCIStatusCellsCarryNoEscapeSequence(t *testing.T) {
	for _, status := range forge.CIStatuses() {
		cell := pipelineStatusLabel(&TreeNode{Type: NodeTypeProject, PipelineStatus: status})
		if strings.Contains(cell, "\x1b") {
			t.Errorf("the CI cell for %q carries an escape sequence: %q", status, cell)
		}
	}
}

// A failed pipeline is red, and nothing else in the vocabulary is. This is the
// half of D66 that made it worth fixing rather than tidying: a GitHub `failure`
// used to reach the default branch and be painted with the *warning* style, so
// a broken build looked like a caution.
func TestOnlyAFailedPipelineIsRed(t *testing.T) {
	for _, status := range forge.CIStatuses() {
		style := pipelineStatusStyle(&TreeNode{Type: NodeTypeProject, PipelineStatus: status})
		isError := style.GetForeground() == theme.StatusErrorStyle.GetForeground()

		if want := status == forge.CIStatusFailed; isError != want {
			t.Errorf("status %q renders red=%v, want %v", status, isError, want)
		}
	}
}
