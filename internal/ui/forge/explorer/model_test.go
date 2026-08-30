package explorer

import (
	"errors"
	"strings"
	"testing"
	"time"

	"github.com/charmbracelet/bubbles/spinner"
	tea "github.com/charmbracelet/bubbletea"

	"github.com/anthnel/devdesk/internal/command"
	"github.com/anthnel/devdesk/internal/jobs"
	"github.com/anthnel/devdesk/internal/oci"
	"github.com/anthnel/devdesk/internal/shared"
	"github.com/anthnel/devdesk/internal/ui/components"
	"github.com/anthnel/devdesk/internal/ui/keymap"
	"github.com/anthnel/devdesk/internal/ui/testutil"
	"github.com/anthnel/devdesk/internal/ui/theme"
)

// ── Construction and loading ─────────────────────────────────────────────────

func TestNewStartsEmptyAndInTheForgesOwnOrder(t *testing.T) {
	m := New(testConfig(), &shared.State{})

	if len(m.nodes) != 0 {
		t.Errorf("a new model holds %d nodes", len(m.nodes))
	}
	if m.mode != ModeNormal {
		t.Errorf("mode = %v on a new model, want ModeNormal", m.mode)
	}
	if column, desc := m.table.SortState(); column != -1 || desc {
		t.Errorf("sort = (column %d, desc=%v), want the forge's own order", column, desc)
	}
	if m.firstLoadDone {
		t.Error("firstLoadDone is true before any load")
	}
}

// Init only issues the load when there is a client to load through; without one
// the view shows its "authenticate first" message rather than failing later.
func TestInitLoadsOnlyWhenAuthenticated(t *testing.T) {
	if cmd := New(testConfig(), &shared.State{}).Init(); cmd != nil {
		t.Error("Init() returned a command with no GitLab client")
	}
	if cmd := New(testConfig(), authenticatedState(t)).Init(); cmd == nil {
		t.Error("Init() returned no command while authenticated")
	}
}

func TestRootGroupsLoadedFillsTheTable(t *testing.T) {
	m := loadedModel(t)

	if m.loading {
		t.Error("still loading after the groups arrived")
	}
	if !m.firstLoadDone {
		t.Error("firstLoadDone is false after a successful load")
	}
	if got := rowNames(m.table.Table().Rows()); len(got) != 3 {
		t.Errorf("table holds %v, want the three root groups", got)
	}
}

// A load failure has to clear the spinner as well as record the message;
// firstLoadDone is what stops the view showing "Loading..." forever.
func TestLoadErrorStopsTheSpinner(t *testing.T) {
	m := feed(t, newTestModel(t), LoadErrorMsg{Error: errors.New("401 unauthorized")})

	if m.loading {
		t.Error("still loading after the error")
	}
	if !m.firstLoadDone {
		t.Error("firstLoadDone is false after a failed load")
	}
	if !strings.Contains(m.error, "401") {
		t.Errorf("error = %q, want it to carry the API message", m.error)
	}
}

// A failure while drilling into a group must also release that group's own
// spinner, or the row keeps a permanent loading marker.
func TestLoadErrorReleasesTheParentNode(t *testing.T) {
	parent := &TreeNode{ID: "1", Name: "alpha", Type: NodeTypeGroup, Loading: true}

	feed(t, newTestModel(t), LoadErrorMsg{Error: errors.New("boom"), ParentNode: parent})

	if parent.Loading {
		t.Error("the parent node is still marked loading after the error")
	}
}

// ── Drill-down ───────────────────────────────────────────────────────────────

func TestDrillDownEntersAGroupAndLoadsItLazily(t *testing.T) {
	m := loadedModel(t)

	m, cmd := step(t, m, testutil.Key("right"))

	if m.currentGroupNode == nil || m.currentGroupNode.Name != "alpha" {
		t.Fatalf("currentGroupNode = %v, want alpha", m.currentGroupNode)
	}
	if !m.loading {
		t.Error("drilling into an unloaded group did not start loading")
	}
	if cmd == nil {
		t.Error("drilling into an unloaded group issued no command")
	}
}

func TestDrillDownIgnoresProjects(t *testing.T) {
	m := drilledModel(t)
	// Sorted by type ascending: the subgroup first, then the two projects.
	m = feed(t, m, testutil.Key("down"))

	before := m.currentGroupNode
	m = feed(t, m, testutil.Key("right"))

	if m.currentGroupNode != before {
		t.Errorf("drilling into a project moved to %v", m.currentGroupNode)
	}
}

func TestDrillUpRestoresTheCursor(t *testing.T) {
	m := loadedModel(t)
	m = feed(t, m, testutil.Key("down")) // select beta
	cursorBefore := m.table.Cursor()

	m = feed(t, m, testutil.Key("right"))
	m = feed(t, m, ChildrenLoadedMsg{ParentNode: m.currentGroupNode, Children: childFixtures(m.currentGroupNode)})
	m = feed(t, m, testutil.Key("left"))

	if m.currentGroupNode != nil {
		t.Errorf("drilling up left currentGroupNode = %v, want the root", m.currentGroupNode)
	}
	if got := m.table.Cursor(); got != cursorBefore {
		t.Errorf("cursor = %d after drilling back up, want the %d it was left at", got, cursorBefore)
	}
}

func TestDrillUpAtTheRootDoesNothing(t *testing.T) {
	m := loadedModel(t)

	for _, key := range []string{"left", "esc"} {
		next := feed(t, m, testutil.Key(key))
		if next.currentGroupNode != nil || len(next.navigationStack) != 0 {
			t.Errorf("%q at the root moved to %v", key, next.currentGroupNode)
		}
	}
}

// Esc is the second way back up, alongside ←. The h/l aliases went with
// every other bare-letter navigation (§3.26).
func TestEveryDrillKeyWorks(t *testing.T) {
	for _, down := range []string{"right"} {
		for _, up := range []string{"left", "esc"} {
			t.Run(down+"/"+up, func(t *testing.T) {
				m := feed(t, loadedModel(t), testutil.Key(down))
				if m.currentGroupNode == nil {
					t.Fatalf("%q did not drill down", down)
				}
				m = feed(t, m, ChildrenLoadedMsg{ParentNode: m.currentGroupNode, Children: nil})
				if m = feed(t, m, testutil.Key(up)); m.currentGroupNode != nil {
					t.Errorf("%q did not drill up", up)
				}
			})
		}
	}
}

func TestChildrenLoadedShowsThemAndMarksTheParentExpanded(t *testing.T) {
	m := drilledModel(t)

	if m.loading {
		t.Error("still loading after the children arrived")
	}
	alpha := m.nodes[0]
	if !alpha.Expanded || alpha.Loading {
		t.Errorf("alpha expanded=%v loading=%v after its children arrived", alpha.Expanded, alpha.Loading)
	}
	if got := rowNames(m.table.Table().Rows()); len(got) != 3 {
		t.Errorf("table holds %v, want alpha's three children", got)
	}
}

// ── Tabs ─────────────────────────────────────────────────────────────────────

// The tab bar is a breadcrumb: home, then one tab per level, with the deepest
// active (Rule 123).
func TestTabsTrackTheDrillDepth(t *testing.T) {
	m := loadedModel(t)
	if got := m.tabCount(); got != 1 {
		t.Errorf("tabCount() = %d at the root, want the home tab alone", got)
	}

	m = drilledModel(t)
	if got := m.tabCount(); got != 2 {
		t.Errorf("tabCount() = %d one level down, want 2", got)
	}
	if m.activeTabIndex != m.tabCount()-1 {
		t.Errorf("activeTabIndex = %d, want the deepest tab %d", m.activeTabIndex, m.tabCount()-1)
	}

	m = feed(t, m, testutil.Key("left"))
	if m.activeTabIndex != 0 {
		t.Errorf("activeTabIndex = %d back at the root, want 0", m.activeTabIndex)
	}
}

// ── Sorting ──────────────────────────────────────────────────────────────────

// '.' cycles each column ascending then descending before moving on (Rule 111).
func TestSortCyclesDirectionThenColumn(t *testing.T) {
	m := loadedModel(t)

	m = feed(t, m, testutil.Key("."))
	if column, desc := m.table.SortState(); column != columnName || desc {
		t.Errorf("after one '.', sort = (column %d, desc=%v), want Name ascending", column, desc)
	}

	m = feed(t, m, testutil.Key("."))
	if column, desc := m.table.SortState(); column != columnName || !desc {
		t.Errorf("after two '.', sort = (column %d, desc=%v), want the same column descending", column, desc)
	}
}

// The cycle comes back to no sort at all, which is the state the table opens
// in — so the forge's own order is reachable again after `.` has moved away
// from it. That is what SortColumn: -1 buys and what a sort by node type could
// never express.
func TestSortCycleComesBackToTheForgesOwnOrder(t *testing.T) {
	m := loadedModel(t)
	// Name, Created and Activity sort; the icon column, Slug, Visibility, Role
	// and CI do not.
	const sortable = 3
	for range sortable*2 + 1 {
		m = feed(t, m, testutil.Key("."))
	}

	if column, desc := m.table.SortState(); column != -1 || desc {
		t.Errorf("sort = (column %d, desc=%v) after a full cycle, want the starting state", column, desc)
	}
}

func TestSortReordersTheRows(t *testing.T) {
	m := drilledModel(t)

	byName := feed(t, m, testutil.Key(".")) // the first sortable column is Name
	if got := rowNames(byName.table.Table().Rows()); !equal(got, []string{"api", "legacy", "sub"}) {
		t.Errorf("sorted by name ascending = %v", got)
	}

	byNameDesc := feed(t, byName, testutil.Key("."))
	if got := rowNames(byNameDesc.table.Table().Rows()); !equal(got, []string{"sub", "legacy", "api"}) {
		t.Errorf("sorted by name descending = %v", got)
	}
}

// A header arrow is the only thing telling the user which column is active.
func TestSortIndicatorFollowsTheActiveColumn(t *testing.T) {
	m := loadedModel(t)

	for i, col := range m.table.Table().Columns() {
		if strings.ContainsAny(col.Title, "▲▼") {
			t.Errorf("column %d header = %q on open, want no arrow at all", i, col.Title)
		}
	}

	m = feed(t, m, testutil.Key("."))
	if got := m.table.Table().Columns()[columnName].Title; !strings.Contains(got, "▲") {
		t.Errorf("Name header = %q, want an ascending arrow", got)
	}

	m = feed(t, m, testutil.Key("."))
	if got := m.table.Table().Columns()[columnName].Title; !strings.Contains(got, "▼") {
		t.Errorf("Name header = %q after reversing, want a descending arrow", got)
	}

	m = feed(t, m, testutil.Key("."))
	if got := m.table.Table().Columns()[columnName].Title; strings.ContainsAny(got, "▲▼") {
		t.Errorf("Name header = %q once the sort moved on, want no arrow", got)
	}
	if got := m.table.Table().Columns()[columnCreated].Title; !strings.Contains(got, "▲") {
		t.Errorf("Created header = %q, want the arrow to have moved here", got)
	}
}

// Nodes with no date sort as oldest rather than crashing the comparison.
func TestSortHandlesMissingDates(t *testing.T) {
	m := loadedModel(t)
	m.nodes[1].CreatedAt = nil

	m = feed(t, m, testutil.Keys(".", ".", ".")...) // Name asc, Name desc, Created asc

	if got := rowNames(m.table.Table().Rows()); got[0] != "beta" {
		t.Errorf("rows sorted by creation date = %v, want the undated node first", got)
	}
}

// ── Filtering ────────────────────────────────────────────────────────────────

func TestFilterNarrowsTheRows(t *testing.T) {
	m := drilledModel(t)

	m = feed(t, m, testutil.Key("/"))
	m = feed(t, m, testutil.Type("api")...)

	if got := rowNames(m.table.Table().Rows()); !equal(got, []string{"api"}) {
		t.Errorf("rows while filtering on \"api\" = %v", got)
	}
}

// The filter matches the full path too, so a subgroup can be found by its
// parent's name.
func TestFilterMatchesTheFullPath(t *testing.T) {
	m := drilledModel(t)

	m = feed(t, m, testutil.Key("/"))
	m = feed(t, m, testutil.Type("alpha/leg")...)

	if got := rowNames(m.table.Table().Rows()); !equal(got, []string{"legacy"}) {
		t.Errorf("rows while filtering on a path fragment = %v", got)
	}
}

func TestFilterIsCaseInsensitive(t *testing.T) {
	m := feed(t, drilledModel(t), testutil.Key("/"))
	m = feed(t, m, testutil.Type("API")...)

	if got := rowNames(m.table.Table().Rows()); !equal(got, []string{"api"}) {
		t.Errorf("rows while filtering on \"API\" = %v", got)
	}
}

// Every action resolves the highlighted row through the same list the table is
// built from. Filtering used to leave the two out of step — the table showed the
// matches, the actions indexed the unfiltered list — so ctrl+d on the only
// visible row deleted a different resource entirely.
func TestActionsResolveTheRowTheUserCanSee(t *testing.T) {
	// "legacy" sorts last unfiltered; filtered to itself it is row 0.
	filtered := func(t *testing.T) Model {
		t.Helper()
		m := feed(t, drilledModel(t), testutil.Key("/"))
		m = feed(t, m, testutil.Type("legacy")...)
		m = feed(t, m, testutil.Key("enter"))
		if got := rowNames(m.table.Table().Rows()); !equal(got, []string{"legacy"}) {
			t.Fatalf("the filter left %v, want just legacy", got)
		}
		return m
	}

	t.Run("delete", func(t *testing.T) {
		m := feed(t, filtered(t), testutil.Key(keymap.Delete))

		if m.deleteTargetNode == nil {
			t.Fatal("ctrl+d selected nothing")
		}
		if m.deleteTargetNode.Name != "legacy" {
			t.Errorf("ctrl+d targeted %q, want the only visible row", m.deleteTargetNode.Name)
		}
	})

	t.Run("clone selection", func(t *testing.T) {
		m := feed(t, filtered(t), testutil.Key(keymap.Clone), testutil.Key(" "))

		roots := m.selection.rootPaths()
		if len(roots) != 1 {
			t.Fatalf("rootPaths() = %v, want the one visible row", roots)
		}
		if roots[0] != "alpha/legacy" {
			t.Errorf("space ticked %q, want the only visible row", roots[0])
		}
	})

	// gamma is the last of the three root groups, so a filter that leaves only
	// gamma puts it at row 0 — a different index in each list.
	t.Run("drill down", func(t *testing.T) {
		m := feed(t, loadedModel(t), testutil.Key("/"))
		m = feed(t, m, testutil.Type("gamma")...)
		m = feed(t, m, testutil.Key("enter"), testutil.Key("right"))

		if m.currentGroupNode == nil || m.currentGroupNode.Name != "gamma" {
			t.Errorf("→ entered %v, want the only visible row", m.currentGroupNode)
		}
	})
}

// bubbles does not clamp the cursor when the row count shrinks, so a filter that
// narrows the list leaves it past the end: nothing highlighted, and every action
// that resolves the selection silently does nothing.
func TestFilteringClampsTheCursor(t *testing.T) {
	m := drilledModel(t)
	m = feed(t, m, testutil.Key("down"), testutil.Key("down")) // last row

	m = feed(t, m, testutil.Key("/"))
	m = feed(t, m, testutil.Type("api")...)

	if got, rows := m.table.Cursor(), len(m.table.Table().Rows()); got >= rows {
		t.Fatalf("cursor = %d with %d rows left; nothing is highlighted", got, rows)
	}

	m = feed(t, m, testutil.Key("enter"), testutil.Key(keymap.Delete))
	if m.deleteTargetNode == nil {
		t.Error("ctrl+d after narrowing the filter selected nothing")
	}
}

// Drilling into a smaller group is the same shape as narrowing the filter.
func TestDrillingIntoASmallerGroupClampsTheCursor(t *testing.T) {
	m := loadedModel(t)
	m = feed(t, m, testutil.Key("down"), testutil.Key("down")) // gamma, row 2

	m = feed(t, m, testutil.Key("right"))
	m = feed(t, m, ChildrenLoadedMsg{
		ParentNode: m.currentGroupNode,
		Children:   []*TreeNode{{ID: "90", Name: "only", FullPath: "gamma/only", Type: NodeTypeProject}},
	})

	if got := m.table.Cursor(); got >= len(m.table.Table().Rows()) {
		t.Errorf("cursor = %d with %d rows", got, len(m.table.Table().Rows()))
	}
}

// While the search box has focus it owns every key, so view shortcuts must not
// fire from inside it.
func TestSearchModeSwallowsViewShortcuts(t *testing.T) {
	m := feed(t, drilledModel(t), testutil.Key("/"))

	m = feed(t, m, testutil.Key(keymap.Clone))

	if m.mode != ModeNormal {
		t.Error("C started a clone selection while the search box had focus")
	}
	if !strings.Contains(m.table.FilterBar().SearchQuery(), keymap.Clone) {
		t.Errorf("C did not reach the search box; query = %q", m.table.FilterBar().SearchQuery())
	}
}

// ── Create ───────────────────────────────────────────────────────────────────

// ctrl+n parents the new resource on the group being browsed, not on the
// highlighted row — creating inside a group you are looking at is the common
// case, and the highlighted row may be a project.
func TestCreateParentsOnTheBrowsedGroup(t *testing.T) {
	m := feed(t, loadedModel(t), testutil.Key(keymap.New))
	if m.creationParentID != "" || m.creationParentName != "" {
		t.Errorf("at the root, parent = (%q, %q), want none", m.creationParentID, m.creationParentName)
	}
	if m.mode != ModeLoadingTemplates {
		t.Errorf("mode = %v after ctrl+n, want ModeLoadingTemplates", m.mode)
	}

	m = feed(t, drilledModel(t), testutil.Key("down"), testutil.Key(keymap.New))
	if m.creationParentID != "1" || m.creationParentName != "alpha" {
		t.Errorf("inside alpha, parent = (%q, %q), want alpha regardless of the cursor", m.creationParentID, m.creationParentName)
	}
}

func TestTemplatesLoadedOpensTheForm(t *testing.T) {
	m := feed(t, loadedModel(t), testutil.Key(keymap.New))

	m = feed(t, m, TemplatesLoadedMsg{Templates: []oci.TemplateEntry{
		{Name: "go-service", Repository: "templates/go", Tag: "v1"},
	}})

	if m.mode != ModeCreatingProject {
		t.Errorf("mode = %v once the templates arrived, want ModeCreatingProject", m.mode)
	}
	if m.creationForm == nil {
		t.Fatal("no creation form after the templates arrived")
	}
	if len(m.templateEntries) != 1 {
		t.Errorf("templateEntries = %v, want the one that arrived", m.templateEntries)
	}
}

// A registry that is unreachable must not block creation: the form opens
// anyway, and says why the template list is empty as soon as the list is on
// screen — not only once the field takes focus, which is what D10 fixed.
func TestTemplateFailureStillOpensTheForm(t *testing.T) {
	m := feed(t, loadedModel(t), testutil.Key(keymap.New))

	m = feed(t, m, TemplatesLoadedMsg{Error: errors.New("registry unreachable")})

	if m.creationForm == nil {
		t.Fatal("a registry error suppressed the creation form")
	}
	if len(m.templateEntries) != 0 {
		t.Errorf("templateEntries = %v after a registry error, want none", m.templateEntries)
	}

	m = feed(t, m, testutil.Key("right")) // Group -> Project
	if view := m.creationForm.View(); !strings.Contains(view, "Registry error") {
		t.Errorf("the unfocused Template field does not mention the registry failure:\n%s", view)
	}

	m = feed(t, m, testutil.Keys("down", "down", "down", "down")...) // onto Template
	if view := m.creationForm.View(); !strings.Contains(view, "Registry error") {
		t.Errorf("the focused Template field does not mention the registry failure:\n%s", view)
	}
}

func TestCancellingCreationReturnsToNormal(t *testing.T) {
	m := feed(t, loadedModel(t), testutil.Key(keymap.New), TemplatesLoadedMsg{})

	m = feed(t, m, components.CreationFormCancelMsg{})

	if m.mode != ModeNormal || m.creationForm != nil {
		t.Errorf("mode = %v, form = %v after cancelling", m.mode, m.creationForm)
	}
}

// The row goes on screen when the request goes out, and settles in place when
// the forge answers. The tree is never emptied — which is what a refresh used
// to do for the whole of a network call.
func TestCreationPutsThePlaceholderRowOnScreenAndSettlesIt(t *testing.T) {
	m := loadedModel(t)
	before := len(m.table.Visible())

	m, cmd := step(t, m, components.CreationFormSubmitMsg{FormType: components.FormTypeGroup, Name: "new"})

	run, started := startedRun(cmd)
	if !started {
		t.Fatal("submitting registered no run")
	}
	if run.Kind != jobs.KindCreate {
		t.Errorf("run kind = %q, want %q", run.Kind, jobs.KindCreate)
	}
	if got := len(m.table.Visible()); got != before+1 {
		t.Fatalf("visible rows = %d, want the placeholder to make it %d", got, before+1)
	}
	placeholder := m.table.Visible()[before].node
	if !placeholder.Creating || placeholder.FullPath != "new" {
		t.Errorf("placeholder = %+v, want a creating node at %q", placeholder, "new")
	}

	m = feed(t, m, GroupCreatedMsg{Namespace: newGroup(7, "alpha/new"), Target: "new"})

	if got := len(m.table.Visible()); got != before+1 {
		t.Errorf("visible rows = %d after settling, want the placeholder replaced not duplicated", got)
	}
	settled := m.table.Visible()[before].node
	if settled.Creating {
		t.Error("the row is still marked as being created")
	}
	if settled.FullPath != "alpha/new" || settled.ID != "7" {
		t.Errorf("settled node = %+v, want the one the forge answered with", settled)
	}
	if m.loading {
		t.Error("settling a creation triggered a refresh")
	}
	// The property the four removed TestPendingSelection* tests protected: the
	// user is left on what they just made.
	if node, ok := m.selectedNode(); !ok || node.FullPath != "alpha/new" {
		t.Errorf("cursor is on %+v, want the created group", node)
	}
}

// A failed creation takes its row away again and says so in the footer. Not in
// m.error: that replaces the tree with an error screen, which is the one place
// the row's disappearance cannot be seen.
func TestCreationFailureRemovesTheRowAndReportsInTheFooter(t *testing.T) {
	m := loadedModel(t)
	before := len(m.table.Visible())

	m, _ = step(t, m, components.CreationFormSubmitMsg{FormType: components.FormTypeGroup, Name: "new"})
	m = feed(t, m, GroupCreatedMsg{Target: "new", Error: errors.New("name has already been taken")})

	if got := len(m.table.Visible()); got != before {
		t.Errorf("visible rows = %d, want the placeholder gone (%d)", got, before)
	}
	if m.error != "" {
		t.Errorf("error = %q, want the tree left alone", m.error)
	}
	if m.footer.Level() != components.LevelError {
		t.Errorf("footer level = %v, want an error", m.footer.Level())
	}
	if !strings.Contains(m.footer.Text(), "new") {
		t.Errorf("footer = %q, want it to name what failed", m.footer.Text())
	}
}

// A project whose template failed still exists, so the row settles; only the
// message differs, and it is a warning rather than an error — nothing failed
// that the user asked for first.
func TestProjectCreatedWithAFailedTemplateStillSettles(t *testing.T) {
	m := loadedModel(t)
	m, _ = step(t, m, components.CreationFormSubmitMsg{FormType: components.FormTypeProject, Name: "svc"})

	m = feed(t, m, ProjectCreatedMsg{
		Repository:    newProject(9, "alpha/svc"),
		Target:        "svc",
		TemplateError: errors.New("download template: 404"),
	})

	node, ok := m.selectedNode()
	if !ok || node.FullPath != "alpha/svc" || node.Creating {
		t.Errorf("selected node = %+v, want the settled project", node)
	}
	if m.loading {
		t.Error("a template failure triggered a refresh")
	}
	if m.footer.Level() != components.LevelWarning {
		t.Errorf("footer level = %v, want a warning", m.footer.Level())
	}
	if !strings.Contains(m.footer.Text(), "template") {
		t.Errorf("footer = %q, want it to name the template", m.footer.Text())
	}
}

// The spinner on a working row comes from the broadcast, not from this view's
// own chain — that one stops when the tree settles, so a frame taken from it
// would freeze on frame zero for the whole of the call.
func TestAWorkingRowTakesItsFrameFromTheBroadcast(t *testing.T) {
	m := loadedModel(t)
	m, _ = step(t, m, components.CreationFormSubmitMsg{FormType: components.FormTypeGroup, Name: "new"})

	m = feed(t, m, jobs.ChangedMsg{
		Runs:  []jobs.Run{createRun("new", "new")},
		Frame: "⣾",
	})

	row, ok := rowFor(m, "new")
	if !ok {
		t.Fatal("the placeholder row is gone")
	}
	if row.frame != "⣾" {
		t.Errorf("row frame = %q, want the broadcast's", row.frame)
	}
	if got := iconCell(row); got != "⣾" {
		t.Errorf("icon cell = %q, want the spinner", got)
	}
}

// A reload replaces the level wholesale, and a create in flight has nothing yet
// to be replaced by. Dropped, its row would vanish mid-request while the
// registry went on tracking the run.
func TestARefreshKeepsACreationInFlightOnScreen(t *testing.T) {
	m := loadedModel(t)
	m, _ = step(t, m, components.CreationFormSubmitMsg{FormType: components.FormTypeGroup, Name: "new"})

	m, _ = step(t, m, testutil.Key("ctrl+r"))
	if _, ok := rowFor(m, "new"); !ok {
		t.Fatal("ctrl+r took the in-flight row off screen")
	}

	m = feed(t, m, RootGroupsLoadedMsg{Nodes: rootFixtures()})
	if _, ok := rowFor(m, "new"); !ok {
		t.Fatal("the reload dropped the in-flight row")
	}

	// And it still settles onto the row the refresh carried over.
	m = feed(t, m, GroupCreatedMsg{Namespace: newGroup(7, "alpha/new"), Target: "new"})
	if _, ok := rowFor(m, "new"); ok {
		t.Error("the placeholder outlived the answer")
	}
	if _, ok := rowFor(m, "alpha/new"); !ok {
		t.Error("the created group is not on screen after settling")
	}
}

// Rule 130 forbids greying a key that acts anyway: N is greyed while the level
// loads, and the parent ID it reads is not there yet either.
func TestCreatingIsRefusedWhileTheLevelLoads(t *testing.T) {
	m := newTestModel(t)
	m.loading = true

	m, cmd := step(t, m, testutil.Key(keymap.New))

	if m.mode == ModeLoadingTemplates || m.creationForm != nil {
		t.Error("N opened the creation form while the level was still loading")
	}
	if cmd == nil || m.footer.Text() != reasonStillLoading {
		t.Errorf("footer = %q, want %q", m.footer.Text(), reasonStillLoading)
	}
	if !testutil.ShortcutDisabled(m.GetShortcuts(), keymap.New) {
		t.Error("N is not greyed while loading")
	}
}

// Rule 130: a row the forge has not confirmed offers nothing that needs an
// identifier, and pressing the key anyway says why.
func TestAPlaceholderRowRefusesTheActionsThatNeedAnIdentifier(t *testing.T) {
	m := loadedModel(t)
	m, _ = step(t, m, components.CreationFormSubmitMsg{FormType: components.FormTypeGroup, Name: "new"})
	m.table.SetCursor(len(m.table.Visible()) - 1)

	if node, _ := m.selectedNode(); !node.Creating {
		t.Fatal("the cursor is not on the placeholder")
	}
	if m.actionable().Enabled() {
		t.Error("a placeholder row reports itself as actionable")
	}
	if !testutil.ShortcutDisabled(m.GetShortcuts(), keymap.Delete) {
		t.Error("D is not greyed on a row that does not exist yet")
	}

	m, _ = step(t, m, testutil.Key(keymap.Delete))
	if m.deleteConfirmModal != nil {
		t.Error("D opened a confirmation on a row that does not exist yet")
	}
	if m.footer.Text() != reasonNotCreatedYet {
		t.Errorf("footer = %q, want %q", m.footer.Text(), reasonNotCreatedYet)
	}
}

// The form decides which API call the submit becomes; the view routes on
// FormType alone.
func TestCreationSubmitRoutesOnTheFormType(t *testing.T) {
	for _, formType := range []components.FormType{components.FormTypeGroup, components.FormTypeProject} {
		m, cmd := step(t, loadedModel(t), components.CreationFormSubmitMsg{
			FormType: formType, Name: "new thing", Visibility: "private",
		})

		if cmd == nil {
			t.Errorf("submitting form type %v issued no command", formType)
		}
		if m.mode != ModeNormal || m.creationForm != nil {
			t.Errorf("submitting left mode=%v form=%v", m.mode, m.creationForm)
		}
	}
}

// Submitting after the session expired must say so rather than panic on a nil
// client inside the command.
func TestCreationSubmitWithoutAClientReports(t *testing.T) {
	m := feed(t, New(testConfig(), &shared.State{}), tea.WindowSizeMsg{Width: 160, Height: 30})

	m, cmd := step(t, m, components.CreationFormSubmitMsg{FormType: components.FormTypeGroup, Name: "x"})

	if cmd != nil {
		t.Errorf("submitting with no forge issued %T", testutil.Msg(cmd))
	}
	if !strings.Contains(m.error, "Not connected") {
		t.Errorf("error = %q, want it to name the missing session", m.error)
	}
}

// ── Selecting a freshly created resource ─────────────────────────────────────
//
// The four TestPendingSelection* tests went with pendingSelectPath and
// expandToPath (§3.59). They covered finding a created resource again after the
// refresh that used to follow a creation — including the drill down to a node
// several levels deep, and the row-order lookup that a non-default sort broke.
// There is no refresh now: the row is put at the level the user made it and
// settles in place, so the cursor never loses it.
//
// What replaces them is TestCreationPutsThePlaceholderRowOnScreenAndSettlesIt,
// which asserts the property those four were protecting — after a creation the
// cursor is on the created thing — against the mechanism that now provides it.

// ── Layout ───────────────────────────────────────────────────────────────────

// Rule 116: the selected row must reach the right border, so the columns share
// the width left after the viewport borders and the per-cell padding.
//
// The ratios this replaced kept the sum exact and starved the columns anyway:
// at 80 they gave Type 5 and CI the remainder, neither wide enough for its own
// header. The floors are what the sweep from 60 now pins.
func TestResizeFitsTheColumnsToTheWidth(t *testing.T) {
	for _, width := range []int{60, 80, 120, 200} {
		m := feed(t, loadedModel(t), tea.WindowSizeMsg{Width: width, Height: 30})

		for _, col := range m.table.Table().Columns() {
			if col.Width < 0 {
				t.Errorf("at width %d, column %q is %d wide", width, col.Title, col.Width)
			}
		}
		// RenderedWidth rather than the declared columns plus two cells each: a
		// column dropped for want of room renders nothing and hands its padding
		// back, so that arithmetic asks for less than the line spans (D61).
		if got, want := m.table.RenderedWidth(), width-2; got != want {
			t.Errorf("at width %d the line spans %d, want %d", width, got, want)
		}
	}
}

func TestResizeSurvivesATinyTerminal(t *testing.T) {
	m := feed(t, loadedModel(t), tea.WindowSizeMsg{Width: 20, Height: 1})

	if m.table.Table().Height() < 0 {
		t.Errorf("table height = %d", m.table.Table().Height())
	}
}

// The spinner only animates while something is loading; ticking otherwise would
// keep waking the event loop for nothing.
func TestSpinnerTicksOnlyWhileLoading(t *testing.T) {
	idle := loadedModel(t)
	if _, cmd := step(t, idle, spinner.TickMsg{}); cmd != nil {
		t.Error("the spinner ticked while idle")
	}

	busy := feed(t, loadedModel(t), testutil.Key("right"))
	if _, cmd := step(t, busy, spinner.TickMsg{}); cmd == nil {
		t.Error("the spinner stopped while loading")
	}
}

// ── Delete ───────────────────────────────────────────────────────────────────

func TestDeleteOpensAConfirmationNamingTheTarget(t *testing.T) {
	m := feed(t, drilledModel(t), testutil.Key(keymap.Delete))

	if m.mode != ModeConfirmingDelete {
		t.Fatalf("mode = %v after ctrl+d, want ModeConfirmingDelete", m.mode)
	}
	if m.deleteTargetNode == nil || m.deleteTargetNode.Name != "sub" {
		t.Fatalf("deleteTargetNode = %v, want the highlighted row", m.deleteTargetNode)
	}
	if view := m.deleteConfirmModal.View(); !strings.Contains(view, "sub") || !strings.Contains(view, "Group") {
		t.Errorf("the confirmation does not name the group it will delete:\n%s", view)
	}
}

// A project already scheduled for deletion can only be purged, so the modal
// locks the "permanent" checkbox rather than offering a choice that does
// nothing.
func TestDeletingAScheduledProjectLocksThePermanentOption(t *testing.T) {
	m := feed(t, drilledModel(t), testutil.Key("down"), testutil.Key("down"), testutil.Key(keymap.Delete))

	if m.deleteTargetNode == nil || m.deleteTargetNode.Name != "legacy" {
		t.Fatalf("deleteTargetNode = %v, want the scheduled project", m.deleteTargetNode)
	}
	if view := m.deleteConfirmModal.View(); !strings.Contains(view, "Permanently") {
		t.Errorf("the confirmation does not offer a permanent delete:\n%s", view)
	}
}

func TestDeclineClosesTheConfirmation(t *testing.T) {
	m := feed(t, drilledModel(t), testutil.Key(keymap.Delete), components.OptionConfirmModalNoMsg{})

	if m.mode != ModeNormal || m.deleteConfirmModal != nil || m.deleteTargetNode != nil {
		t.Errorf("declining left mode=%v modal=%v target=%v", m.mode, m.deleteConfirmModal, m.deleteTargetNode)
	}
}

func TestConfirmingDeleteIssuesTheCall(t *testing.T) {
	m := feed(t, drilledModel(t), testutil.Key(keymap.Delete))

	m, cmd := step(t, m, components.OptionConfirmModalYesMsg{})

	if cmd == nil {
		t.Error("confirming a delete issued no command")
	}
	if m.mode != ModeNormal || m.deleteConfirmModal != nil {
		t.Errorf("confirming left mode=%v modal=%v", m.mode, m.deleteConfirmModal)
	}
}

// The tree is pruned locally rather than refetched: a refresh would drop the
// user back at the root and lose their place.
func TestDeleteCompleteRemovesTheRowInPlace(t *testing.T) {
	m := drilledModel(t)
	target := m.currentGroupNode.Children[1] // api

	m = feed(t, m, DeleteCompleteMsg{DeletedNode: target})

	if got := rowNames(m.table.Table().Rows()); !equal(got, []string{"sub", "legacy"}) {
		t.Errorf("rows after the delete = %v, want the deleted project gone", got)
	}
	if m.currentGroupNode == nil {
		t.Error("the delete dropped the user back to the root")
	}
}

func TestDeleteCompleteRemovesARootGroup(t *testing.T) {
	m := loadedModel(t)

	m = feed(t, m, DeleteCompleteMsg{DeletedNode: m.nodes[1]})

	if got := rowNames(m.table.Table().Rows()); !equal(got, []string{"alpha", "gamma"}) {
		t.Errorf("rows after deleting a root group = %v", got)
	}
}

func TestDeleteFailureIsReportedInTheFooter(t *testing.T) {
	m, cmd := step(t, drilledModel(t), DeleteCompleteMsg{Error: errors.New("403 forbidden")})

	if !m.footer.IsSet() {
		t.Error("a failed delete reported nothing")
	}
	if strings.Contains(m.footer.Text(), "403") {
		t.Errorf("footer = %q; Rule 128 keeps the raw error out of the UI", m.footer.Text())
	}
	if cmd == nil {
		t.Fatal("no clear timer was scheduled; Rule 128 caps footer messages at 3s")
	}

	m = feed(t, m, components.ClearFooterMsg{ID: m.footer.ID()})
	if m.footer.IsSet() {
		t.Errorf("footer = %q after the timer fired", m.footer.Text())
	}
}

// ── Clone selection ──────────────────────────────────────────────────────────

func TestCEntersTheSelectionMode(t *testing.T) {
	m := feed(t, drilledModel(t), testutil.Key(keymap.Clone))

	if m.mode != ModeSelecting {
		t.Fatalf("mode = %v after 'c', want ModeSelecting", m.mode)
	}
	if !m.selection.isEmpty() {
		t.Error("the mode opened with something already ticked")
	}
}

// The checkbox rides on the Type cell, so this also pins that it is drawn only
// in the selection mode — and as a bare glyph, since a table cell carries no
// escape sequences (Rule 122).
func TestTheCheckboxAppearsOnlyWhileSelecting(t *testing.T) {
	m := drilledModel(t)
	for _, row := range m.table.Table().Rows() {
		if strings.ContainsAny(row[0], theme.IconCheckbox+theme.IconChecked) {
			t.Fatalf("the plain table draws a checkbox: %q", row[0])
		}
	}

	m = feed(t, m, testutil.Key(keymap.Clone))
	for _, row := range m.table.Table().Rows() {
		if !strings.Contains(row[0], theme.IconCheckbox) {
			t.Errorf("the selection mode row has no empty box: %q", row[0])
		}
		if strings.Contains(row[0], "\x1b") {
			t.Errorf("the checkbox cell carries an escape sequence (Rule 122): %q", row[0])
		}
	}
}

func TestSpaceTicksTheRowAndTheCellFollows(t *testing.T) {
	m := feed(t, drilledModel(t), testutil.Key(keymap.Clone), testutil.Key(" "))

	node, ok := m.selectedNode()
	if !ok {
		t.Fatal("nothing is selected")
	}
	if !m.selection.includes(node.FullPath) {
		t.Errorf("%s was not ticked by space", node.FullPath)
	}
	// The node is kept beside the path: the walk starts from it, and it is no
	// longer on screen once the user has drilled back up.
	if m.selectionNodes[node.FullPath] != node {
		t.Error("the ticked node was not kept for the walk")
	}
	if row := m.table.Table().Rows()[m.table.Cursor()]; !strings.Contains(row[0], theme.IconChecked) {
		t.Errorf("the ticked row still shows an empty box: %q", row[0])
	}
}

// Unticking has to drop the node too, or a root removed from the selection
// would still be walked.
func TestUntickingDropsTheNodeAsWell(t *testing.T) {
	m := feed(t, drilledModel(t), testutil.Key(keymap.Clone), testutil.Key(" "), testutil.Key(" "))

	if len(m.selectionNodes) != 0 {
		t.Errorf("selectionNodes = %v after unticking", m.selectionNodes)
	}
	if len(m.rootNodes()) != 0 {
		t.Errorf("rootNodes() = %v after unticking", m.rootNodes())
	}
}

func TestConfirmingAnEmptySelectionSaysSoRatherThanProceeding(t *testing.T) {
	// This test drains the Cmd, and the footer timer inside it really sleeps.
	testutil.FastTimers(t, &components.FooterMsgDuration)

	m, cmd := step(t, feed(t, drilledModel(t), testutil.Key(keymap.Clone)), testutil.Key("enter"))

	if _, ok := testutil.MsgOf[CloneSelectionRequestMsg](cmd); ok {
		t.Fatal("an empty selection asked for a destination")
	}
	if !m.footer.IsSet() {
		t.Error("an empty selection was refused silently")
	}
	if m.mode != ModeSelecting {
		t.Errorf("mode = %v, want to stay in the selection", m.mode)
	}
}

func TestConfirmingASelectionAsksTheAppForADestination(t *testing.T) {
	m := feed(t, drilledModel(t), testutil.Key(keymap.Clone), testutil.Key(" "))

	_, cmd := step(t, m, testutil.Key("enter"))

	if _, ok := testutil.MsgOf[CloneSelectionRequestMsg](cmd); !ok {
		t.Fatalf("enter emitted %T, want a destination request", testutil.Msg(cmd))
	}
}

// The destination picker borrows the workspaces view and comes back. Refusing
// it must not throw the ticks away — only the destination was refused.
func TestCancellingTheDestinationKeepsTheSelection(t *testing.T) {
	m := feed(t, drilledModel(t), testutil.Key(keymap.Clone), testutil.Key(" "),
		CloneSelectionCancelledMsg{})

	if m.mode != ModeSelecting {
		t.Errorf("mode = %v after cancelling the picker, want ModeSelecting", m.mode)
	}
	if m.selection.isEmpty() {
		t.Error("cancelling the destination picker discarded the selection")
	}
}

func TestEscLeavesTheSelectionMode(t *testing.T) {
	m := feed(t, drilledModel(t), testutil.Key(keymap.Clone), testutil.Key(" "), testutil.Key("esc"))

	if m.mode != ModeNormal {
		t.Errorf("mode = %v after esc, want ModeNormal", m.mode)
	}
	if !m.selection.isEmpty() {
		t.Error("esc left the selection behind")
	}
}

// Drilling still works while selecting, which is the only way to deselect
// inside a ticked group (decision 10).
func TestDrillingStillWorksWhileSelecting(t *testing.T) {
	m := feed(t, drilledModel(t), testutil.Key(keymap.Clone))
	before := m.currentGroupNode

	m = feed(t, m, tea.KeyMsg{Type: tea.KeyLeft})

	if m.currentGroupNode == before {
		t.Error("the left arrow did not drill up in the selection mode")
	}
	if m.mode != ModeSelecting {
		t.Errorf("mode = %v after drilling, want to stay in the selection", m.mode)
	}
}

// ── Clone run ────────────────────────────────────────────────────────────────

// The clone screen renders a run it does not own (D3), so these tests drive a
// real registry beside the view and hand back its snapshot — which is exactly
// what the router does: apply the transition, then broadcast (routeWork,
// jobsChanged). Asserting on the view alone would now assert on nothing.

// cloneHarness is the router, reduced to the two things this screen needs from
// it: a registry to apply events to, and a broadcast to hand back.
type cloneHarness struct {
	t   *testing.T
	reg *jobs.Registry
	m   Model
}

// cloningModel is the view in ModeCloning, for the tests that only need the
// screen (its shortcuts, its rendering) and not the run behind it.
func cloningModel(t *testing.T) Model {
	t.Helper()
	return cloning(t).m
}

// cloning puts the view in ModeCloning with an open clone run registered, so
// the list can be driven event by event.
func cloning(t *testing.T) *cloneHarness {
	t.Helper()
	m := feed(t, drilledModel(t), testutil.Key(keymap.Clone), testutil.Key(" "))
	m.mode = ModeCloning
	m.clone = newCloneList("/ws", make(chan cloneEvent))

	h := &cloneHarness{t: t, reg: jobs.New(), m: m}
	h.reg.Start(jobs.NewOpenRun(jobs.KindClone, command.ViewGitExplorer, "", "/ws"))
	h.broadcast()
	return h
}

// emit routes messages the way the router does and broadcasts after each.
func (h *cloneHarness) emit(msgs ...tea.Msg) {
	h.t.Helper()
	for _, msg := range msgs {
		if reporter, ok := msg.(jobs.Reporter); ok {
			h.reg.Apply(reporter.Transition())
		}
		if sealer, ok := msg.(jobs.Sealer); ok {
			h.reg.Seal(sealer.Seal())
		}
		// The command is dropped, deliberately: handleCloneEvent re-issues the
		// blocking read on the pipeline channel, and running it here would wait
		// forever on a channel no test feeds.
		h.m, _ = step(h.t, h.m, msg)
		h.broadcast()
	}
}

// key presses a key and routes whatever it asked the router for.
func (h *cloneHarness) key(name string) {
	h.t.Helper()
	var cmd tea.Cmd
	h.m, cmd = step(h.t, h.m, testutil.Key(name))
	h.route(cmd)
	h.broadcast()
}

// route handles the one router message this screen sends.
func (h *cloneHarness) route(cmd tea.Cmd) {
	h.t.Helper()
	for _, msg := range testutil.Msgs(cmd) {
		if cancel, ok := msg.(jobs.CancelOpenMsg); ok {
			h.reg.CancelOpen(cancel.Kind)
		}
	}
}

func (h *cloneHarness) broadcast() {
	h.t.Helper()
	h.m = feed(h.t, h.m, jobs.ChangedMsg{Runs: h.reg.Snapshot(), Frame: "*"})
}

// run is this screen's run as the registry holds it.
func (h *cloneHarness) run() jobs.Run {
	h.t.Helper()
	run, ok := cloneRunFrom(h.reg.Snapshot())
	if !ok {
		h.t.Fatal("no clone run was registered")
	}
	return run
}

func (h *cloneHarness) item(i int) jobs.Item {
	h.t.Helper()
	run := h.run()
	if i >= len(run.Items) {
		h.t.Fatalf("the run holds %d targets, wanted index %d", len(run.Items), i)
	}
	return run.Items[i]
}

func found(path string) CloneEventMsg {
	return CloneEventMsg{event: cloneEvent{kind: cloneFound, path: path}}
}

func began(path string) CloneEventMsg {
	return CloneEventMsg{event: cloneEvent{kind: cloneBegan, path: path}}
}

func TestAFoundRepositoryBecomesARowStraightAway(t *testing.T) {
	h := cloning(t)
	h.emit(found("alpha/api"))

	rows := h.m.clone.table.Table().Rows()
	if len(rows) != 1 || rows[0][colCloneRepository] != "alpha/api" {
		t.Fatalf("rows = %v, want the repository as soon as it was found", rows)
	}
	if got, _, _, _ := h.m.clone.counts(); got != 1 {
		t.Errorf("found = %d, want 1", got)
	}
}

// The five states of the old cloneState are jobs.ItemState now, one for one.
func TestARowWalksThroughItsStates(t *testing.T) {
	tests := []struct {
		name  string
		event cloneEvent
		want  jobs.ItemState
	}{
		{"cloned", cloneEvent{kind: cloneEnded, path: "alpha/api"}, jobs.ItemDone},
		{"already there", cloneEvent{kind: cloneEnded, path: "alpha/api", skipped: true}, jobs.ItemSkipped},
		{"failed", cloneEvent{kind: cloneEnded, path: "alpha/api", err: errors.New("boom")}, jobs.ItemFailed},
	}

	for _, tt := range tests {
		t.Run(tt.name, func(t *testing.T) {
			h := cloning(t)
			h.emit(found("alpha/api"), began("alpha/api"))

			if got := h.item(0).State; got != jobs.ItemRunning {
				t.Fatalf("state = %v after began, want running", got)
			}

			h.emit(CloneEventMsg{event: tt.event})
			if got := h.item(0).State; got != tt.want {
				t.Errorf("state = %v, want %v", got, tt.want)
			}
		})
	}
}

// A group that cannot be listed gets a row of its own. Folding it into a
// repository's error would attribute it to one repository out of however many
// were never discovered.
func TestAFailedWalkGetsItsOwnRow(t *testing.T) {
	h := cloning(t)
	h.emit(CloneEventMsg{
		event: cloneEvent{kind: cloneWalkFailed, path: "alpha/sub", err: errors.New("403")},
	})

	run := h.run()
	if len(run.Items) != 1 || run.Items[0].Target != "alpha/sub" || run.Items[0].State != jobs.ItemFailed {
		t.Fatalf("items = %+v, want the group as a failed row", run.Items)
	}
	if !strings.Contains(run.Items[0].Detail, "403") {
		t.Errorf("detail = %q, want the API error", run.Items[0].Detail)
	}
}

// The run is not finished until the walk is sealed, whatever its rows say: a
// walk that has found three repositories and cloned all three is still going.
func TestARunIsNotFinishedUntilTheWalkIsSealed(t *testing.T) {
	h := cloning(t)
	h.emit(found("alpha/api"), began("alpha/api"),
		CloneEventMsg{event: cloneEvent{kind: cloneEnded, path: "alpha/api"}})

	if h.m.clone.finished() {
		t.Error("the run settled while the walk was still going")
	}

	h.emit(CloneRunFinishedMsg{})
	if !h.m.clone.finished() {
		t.Error("the sealed run did not settle")
	}
}

// Decision 13 discards the list, and a failed clone wrote nothing — so the
// failures have to be said while the view is still alive.
func TestTheFailuresAreReportedWhenTheRunEnds(t *testing.T) {
	h := cloning(t)
	h.emit(found("alpha/api"), began("alpha/api"),
		CloneEventMsg{event: cloneEvent{kind: cloneEnded, path: "alpha/api", err: errors.New("boom")}})

	h.reg.Seal(jobs.KindClone)
	h.broadcast()
	m, cmd := step(t, h.m, CloneRunFinishedMsg{})

	if !m.clone.finished() {
		t.Error("the run was not marked finished")
	}
	if !strings.Contains(m.footer.Text(), "alpha/api") {
		t.Errorf("footer = %q, want the failed repository named", m.footer.Text())
	}
	if cmd == nil {
		t.Error("no clear timer was scheduled; Rule 128 caps footer messages at 3s")
	}
}

// Esc cancels, and cancelling is not instant: the running clones are awaited
// rather than killed, because killing one leaves half a repository on disk.
func TestEscCancelsWithoutClosingTheList(t *testing.T) {
	h := cloning(t)
	cancelled := false
	h.reg.AttachRun(h.run().ID, func() { cancelled = true })
	h.emit(found("alpha/api"), began("alpha/api"))

	h.key("esc")

	if !cancelled {
		t.Error("esc did not cancel the run")
	}
	if h.m.mode != ModeCloning || h.m.clone == nil {
		t.Fatalf("esc closed the list while a clone was still running (mode=%v)", h.m.mode)
	}
	if !strings.Contains(h.m.cloneStatusLine(), "Cancelling") {
		t.Errorf("the status line does not say it is cancelling: %q", h.m.cloneStatusLine())
	}
}

// The registry is what records the cancellation, so `:jobs` can tell "you
// stopped it" from "it finished". A view calling the pipeline's cancel behind
// the registry's back would leave the record claiming the run ended on its own.
func TestACancelledRunReadsCancelledRatherThanDone(t *testing.T) {
	h := cloning(t)
	h.emit(found("alpha/api"))

	h.key("esc")

	if got := h.run().State(); got != jobs.RunCancelled {
		t.Errorf("state = %q, want %q", got, jobs.RunCancelled)
	}
}

// A second esc must not force. Forcing means killing a git clone mid-write,
// which is the partial directory decision 12 exists to avoid.
func TestASecondEscDoesNotForce(t *testing.T) {
	h := cloning(t)
	calls := 0
	h.reg.AttachRun(h.run().ID, func() { calls++ })
	// A clone in flight, so the run does not settle the moment it is
	// cancelled: an empty cancelled run is over, and esc would rightly close
	// it — which is not what this test is about.
	h.emit(found("alpha/api"), began("alpha/api"))

	h.key("esc")
	h.key("esc")
	h.key("esc")

	if calls != 1 {
		t.Errorf("cancel was called %d times, want once", calls)
	}
	if h.m.mode != ModeCloning {
		t.Errorf("mode = %v, want the list to stay until the run ends", h.m.mode)
	}
}

func TestEscClosesTheListOnceTheRunHasEnded(t *testing.T) {
	h := cloning(t)
	h.emit(CloneRunFinishedMsg{})

	h.key("esc")

	if h.m.mode != ModeNormal || h.m.clone != nil {
		t.Errorf("esc left mode=%v list=%v after the run ended", h.m.mode, h.m.clone)
	}
	if !h.m.selection.isEmpty() {
		t.Error("closing the list kept the selection")
	}
}

// D5: the rows take the router's frame, not a chain of this view's. A clone
// keeps turning while the user is looking at another screen, and cannot freeze
// on frame zero because this view stopped ticking.
func TestARunningRowCarriesTheRoutersFrame(t *testing.T) {
	h := cloning(t)
	h.emit(found("alpha/api"), began("alpha/api"))

	rows := h.m.clone.table.Table().Rows()
	if len(rows) != 1 {
		t.Fatalf("rows = %v, want one", rows)
	}
	if !strings.HasPrefix(rows[0][colCloneStatus], "*") {
		t.Errorf("status = %q, want it to start with the frame the router broadcast", rows[0][colCloneStatus])
	}
}

// ── Modes ────────────────────────────────────────────────────────────────────

// A long-running mode must not act on stray keystrokes.
func TestBlockingModesIgnoreInput(t *testing.T) {
	for _, mode := range []ViewMode{ModeLoadingTemplates} {
		m := drilledModel(t)
		m.mode = mode

		next := feed(t, m, testutil.Key(keymap.Delete), testutil.Key(keymap.Clone), testutil.Key("."))

		if next.mode != mode {
			t.Errorf("mode %v changed to %v under keystrokes", mode, next.mode)
		}
		if next.deleteConfirmModal != nil || !next.selection.isEmpty() {
			t.Errorf("mode %v acted on a keystroke", mode)
		}
	}
}

// InEditMode tells the app router to stop capturing ":" for command mode
// (ctrl+p still gets through — the router handles it before asking).
func TestInEditModeCoversEveryModalState(t *testing.T) {
	if loadedModel(t).InEditMode() {
		t.Error("InEditMode() is true on the plain table")
	}

	m := feed(t, loadedModel(t), testutil.Key("/"))
	if !m.InEditMode() {
		t.Error("InEditMode() is false while the search box has focus")
	}

	for _, mode := range []ViewMode{ModeSelecting, ModeCloning, ModeLoadingTemplates, ModeCreatingProject, ModeConfirmingDelete} {
		m := loadedModel(t)
		m.mode = mode
		if !m.InEditMode() {
			t.Errorf("InEditMode() is false in mode %v", mode)
		}
	}
}

// ── Refresh ──────────────────────────────────────────────────────────────────

// Refresh returns to the root: the group the user was inside may not exist any
// more, and its stale children would otherwise stay on screen.
func TestRefreshResetsTheNavigation(t *testing.T) {
	m := drilledModel(t)

	m, cmd := step(t, m, testutil.Key("ctrl+r"))

	if cmd == nil {
		t.Error("ctrl+r issued no command")
	}
	if m.currentGroupNode != nil || len(m.navigationStack) != 0 || len(m.cursorStack) != 0 {
		t.Errorf("refresh left group=%v stack=%v cursors=%v", m.currentGroupNode, m.navigationStack, m.cursorStack)
	}
	if !m.loading || len(m.table.Table().Rows()) != 0 {
		t.Errorf("refresh left loading=%v rows=%d", m.loading, len(m.table.Table().Rows()))
	}
}

// ── Browser ──────────────────────────────────────────────────────────────────

func TestOpenInBrowserFailureIsReportedAndCleared(t *testing.T) {
	m := drilledModel(t)

	m, cmd := step(t, m, BrowserOpenedMsg{Error: errors.New("exec: \"xdg-open\": not found")})

	if !m.footer.IsSet() {
		t.Error("a failed browser launch reported nothing")
	}
	if cmd == nil {
		t.Error("no clear timer was scheduled; Rule 128 caps footer messages at 3s")
	}

	m = feed(t, m, components.ClearFooterMsg{ID: m.footer.ID()})
	if m.footer.IsSet() {
		t.Errorf("footer = %q after the timer fired", m.footer.Text())
	}
}

// W on a node with no page is refused, and says so — it used to return in
// silence while the key was simply missing from the header (Rule 130).
func TestOpenInBrowserWithoutAURLIsRefusedAndSaysSo(t *testing.T) {
	m := drilledModel(t)
	for _, child := range m.currentGroupNode.Children {
		child.WebURL = ""
	}

	next, _ := step(t, m, testutil.Key(keymap.Web))

	if got := next.footer.Text(); !strings.Contains(got, reasonNoWebURL) {
		t.Errorf("footer = %q, want it to carry %q", got, reasonNoWebURL)
	}
}

// ── Helpers ──────────────────────────────────────────────────────────────────

func equal(a, b []string) bool {
	if len(a) != len(b) {
		return false
	}
	for i := range a {
		if a[i] != b[i] {
			return false
		}
	}
	return true
}

func TestNodeSlugTakesTheLastSegment(t *testing.T) {
	tests := map[string]string{
		"alpha":                "alpha",
		"alpha/sub":            "sub",
		"alpha/sub/deep/thing": "thing",
		"":                     "",
	}
	for path, want := range tests {
		if got := nodeSlug(path); got != want {
			t.Errorf("nodeSlug(%q) = %q, want %q", path, got, want)
		}
	}
}

// Only groups can be drilled into, so only groups toggle.
func TestOnlyGroupsAreExpandable(t *testing.T) {
	group := &TreeNode{Type: NodeTypeGroup}
	project := &TreeNode{Type: NodeTypeProject}

	if !group.IsExpandable() || project.IsExpandable() {
		t.Error("IsExpandable() does not distinguish groups from projects")
	}

	group.Toggle()
	project.Toggle()
	if !group.Expanded {
		t.Error("Toggle() did not expand a group")
	}
	if project.Expanded {
		t.Error("Toggle() expanded a project")
	}
}

func TestTimeBeforeTreatsMissingDatesAsOldest(t *testing.T) {
	early, late := at(1), at(2)

	tests := []struct {
		name string
		a, b *time.Time
		want bool
	}{
		{"both nil", nil, nil, false},
		{"a missing", nil, late, true},
		{"b missing", early, nil, false},
		{"a earlier", early, late, true},
		{"b earlier", late, early, false},
	}
	for _, tt := range tests {
		t.Run(tt.name, func(t *testing.T) {
			if got := timeBefore(tt.a, tt.b); got != tt.want {
				t.Errorf("timeBefore() = %v, want %v", got, tt.want)
			}
		})
	}
}

var _ = tea.Model(Model{})
