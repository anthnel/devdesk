package explorer

import (
	"errors"
	"strings"
	"testing"
	"time"

	"github.com/charmbracelet/bubbles/spinner"
	tea "github.com/charmbracelet/bubbletea"

	"github.com/anthnel/devdesk/internal/oci"
	"github.com/anthnel/devdesk/internal/shared"
	"github.com/anthnel/devdesk/internal/ui/components"
	"github.com/anthnel/devdesk/internal/ui/keymap"
	"github.com/anthnel/devdesk/internal/ui/testutil"
	"github.com/anthnel/devdesk/internal/ui/theme"
)

// ── Construction and loading ─────────────────────────────────────────────────

func TestNewStartsEmptyAndSorted(t *testing.T) {
	m := New(testConfig(), &shared.State{})

	if len(m.nodes) != 0 {
		t.Errorf("a new model holds %d nodes", len(m.nodes))
	}
	if m.mode != ModeNormal {
		t.Errorf("mode = %v on a new model, want ModeNormal", m.mode)
	}
	if column, desc := m.table.SortState(); column != columnType || desc {
		t.Errorf("sort = (column %d, desc=%v), want Type ascending", column, desc)
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
	parent := &TreeNode{ID: 1, Name: "alpha", Type: NodeTypeGroup, Loading: true}

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
	if column, desc := m.table.SortState(); column != columnType || !desc {
		t.Errorf("after one '.', sort = (column %d, desc=%v), want the same column descending", column, desc)
	}

	m = feed(t, m, testutil.Key("."))
	if column, desc := m.table.SortState(); column != columnType+1 || desc {
		t.Errorf("after two '.', sort = (column %d, desc=%v), want Name ascending", column, desc)
	}
}

func TestSortCycleWrapsBackToTheFirstColumn(t *testing.T) {
	m := loadedModel(t)
	// Type, Name, Visibility, Created and Activity sort; Slug, Role and CI do not.
	const sortable = 5
	for range sortable * 2 {
		m = feed(t, m, testutil.Key("."))
	}

	if column, desc := m.table.SortState(); column != columnType || desc {
		t.Errorf("sort = (column %d, desc=%v) after a full cycle, want the starting state", column, desc)
	}
}

func TestSortReordersTheRows(t *testing.T) {
	m := drilledModel(t)

	byName := feed(t, m, testutil.Keys(".", ".")...) // type desc, then name asc
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

	if got := m.table.Table().Columns()[0].Title; !strings.Contains(got, "▲") {
		t.Errorf("Type header = %q, want an ascending arrow", got)
	}

	m = feed(t, m, testutil.Key("."))
	if got := m.table.Table().Columns()[0].Title; !strings.Contains(got, "▼") {
		t.Errorf("Type header = %q after reversing, want a descending arrow", got)
	}

	m = feed(t, m, testutil.Key("."))
	if got := m.table.Table().Columns()[0].Title; strings.ContainsAny(got, "▲▼") {
		t.Errorf("Type header = %q once the sort moved to Name, want no arrow", got)
	}
	if got := m.table.Table().Columns()[1].Title; !strings.Contains(got, "▲") {
		t.Errorf("Name header = %q, want the arrow to have moved here", got)
	}
}

// Nodes with no date sort as oldest rather than crashing the comparison.
func TestSortHandlesMissingDates(t *testing.T) {
	m := loadedModel(t)
	m.nodes[1].CreatedAt = nil

	m = feed(t, m, testutil.Keys(".", ".", ".", ".", ".", ".")...) // to Created ascending

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
		Children:   []*TreeNode{{ID: 90, Name: "only", FullPath: "gamma/only", Type: NodeTypeProject}},
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
	if m.creationParentID != 0 || m.creationParentName != "" {
		t.Errorf("at the root, parent = (%d, %q), want none", m.creationParentID, m.creationParentName)
	}
	if m.mode != ModeLoadingTemplates {
		t.Errorf("mode = %v after ctrl+n, want ModeLoadingTemplates", m.mode)
	}

	m = feed(t, drilledModel(t), testutil.Key("down"), testutil.Key(keymap.New))
	if m.creationParentID != 1 || m.creationParentName != "alpha" {
		t.Errorf("inside alpha, parent = (%d, %q), want alpha regardless of the cursor", m.creationParentID, m.creationParentName)
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

// A created resource is selected after the refresh, so the user sees what they
// just made instead of having to hunt for it.
func TestCreationSchedulesASelection(t *testing.T) {
	m := feed(t, loadedModel(t), GroupCreatedMsg{Group: newGroup(7, "alpha/new")})

	if m.pendingSelectPath != "alpha/new" {
		t.Errorf("pendingSelectPath = %q after creating a group", m.pendingSelectPath)
	}
	if !m.loading {
		t.Error("creating a group did not trigger a refresh")
	}
}

func TestCreationFailureIsReported(t *testing.T) {
	m := feed(t, loadedModel(t), GroupCreatedMsg{Error: errors.New("name has already been taken")})

	if !strings.Contains(m.error, "already been taken") {
		t.Errorf("error = %q, want the API message", m.error)
	}
	if m.loading {
		t.Error("a failed creation triggered a refresh")
	}
}

// A project whose template failed still exists, so the refresh must happen and
// the message has to say both things.
func TestProjectCreatedWithAFailedTemplateStillRefreshes(t *testing.T) {
	m := feed(t, loadedModel(t), ProjectCreatedMsg{
		Project:       newProject(9, "alpha/svc"),
		TemplateError: errors.New("download template: 404"),
	})

	if !m.loading {
		t.Error("a template failure suppressed the refresh of a project that was created")
	}
	if !strings.Contains(m.error, "template failed") {
		t.Errorf("error = %q, want it to say the project exists but the template did not apply", m.error)
	}
	if m.pendingSelectPath != "alpha/svc" {
		t.Errorf("pendingSelectPath = %q, want the created project", m.pendingSelectPath)
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
		t.Errorf("submitting without a client issued %T", testutil.Msg(cmd))
	}
	if !strings.Contains(m.error, "not initialized") {
		t.Errorf("error = %q, want it to name the missing client", m.error)
	}
}

// ── Selecting a freshly created resource ─────────────────────────────────────

// After the refresh, a resource created at the current level is selected
// directly.
func TestPendingSelectionLandsOnAVisibleNode(t *testing.T) {
	m := newTestModel(t)
	m.pendingSelectPath = "gamma"

	m = feed(t, m, RootGroupsLoadedMsg{Nodes: rootFixtures()})

	if m.pendingSelectPath != "" {
		t.Errorf("pendingSelectPath = %q, want it consumed", m.pendingSelectPath)
	}
	if got := m.table.Cursor(); got != 2 {
		t.Errorf("cursor = %d, want the created group at row 2", got)
	}
}

// The cursor indexes the rows, not the level's raw child list. Under a
// non-default sort those are two different orderings — and both in range, so
// nothing clamps the mistake away: the lookup walked the raw list and landed on
// whichever resource happened to share the index.
func TestPendingSelectionUsesTheRowOrderNotTheRawList(t *testing.T) {
	m := feed(t, drilledModel(t), testutil.Keys(".", ".")...) // sort by name ascending

	if got := rowNames(m.table.Table().Rows()); !equal(got, []string{"api", "legacy", "sub"}) {
		t.Fatalf("rows = %v, want them sorted by name", got)
	}

	next, _ := m.expandToPath("alpha/legacy") // raw index 2, row 1
	m = next.(Model)

	if got := rowNames(m.table.Table().Rows())[m.table.Cursor()]; got != "legacy" {
		t.Errorf("cursor is on %q, want legacy", got)
	}
}

// A resource created inside a group needs the tree drilled into first.
func TestPendingSelectionDrillsTowardsTheTarget(t *testing.T) {
	m := newTestModel(t)
	m.pendingSelectPath = "alpha/api"

	m, cmd := step(t, m, RootGroupsLoadedMsg{Nodes: rootFixtures()})

	if m.currentGroupNode == nil || m.currentGroupNode.Name != "alpha" {
		t.Fatalf("currentGroupNode = %v, want it to have drilled into alpha", m.currentGroupNode)
	}
	if cmd == nil {
		t.Fatal("drilling toward the target issued no load")
	}

	alpha := m.nodes[0]
	m = feed(t, m, ChildrenLoadedMsg{ParentNode: alpha, Children: childFixtures(alpha)})
	if m.pendingSelectPath != "" {
		t.Errorf("pendingSelectPath = %q once the children arrived", m.pendingSelectPath)
	}
	if got := rowNames(m.table.Table().Rows())[m.table.Cursor()]; got != "api" {
		t.Errorf("cursor is on %q, want the created project", got)
	}
}

// A path that no longer exists must not leave the view chasing it on every
// subsequent load.
func TestPendingSelectionGivesUpOnAMissingPath(t *testing.T) {
	m := newTestModel(t)
	m.pendingSelectPath = "nowhere/at/all"

	m = feed(t, m, RootGroupsLoadedMsg{Nodes: rootFixtures()})

	if m.pendingSelectPath != "" {
		t.Errorf("pendingSelectPath = %q, want it abandoned", m.pendingSelectPath)
	}
	if m.currentGroupNode != nil {
		t.Errorf("chasing a missing path drilled into %v", m.currentGroupNode)
	}
}

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

		total := 0
		for _, col := range m.table.Table().Columns() {
			total += col.Width
			if col.Width < 0 {
				t.Errorf("at width %d, column %q is %d wide", width, col.Title, col.Width)
			}
		}
		if want := width - 2 - numColumns*2; total != want {
			t.Errorf("at width %d the columns total %d, want %d", width, total, want)
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

// cloningModel puts the view in ModeCloning with a run nothing feeds, so the
// list can be driven event by event.
func cloningModel(t *testing.T) Model {
	t.Helper()
	m := feed(t, drilledModel(t), testutil.Key(keymap.Clone), testutil.Key(" "))
	m.mode = ModeCloning
	m.clone = newCloneList("/ws", &cloneRun{events: make(chan cloneEvent), cancel: func() {}})
	return m
}

func TestAFoundRepositoryBecomesARowStraightAway(t *testing.T) {
	m := feed(t, cloningModel(t), CloneEventMsg{event: cloneEvent{kind: cloneFound, path: "alpha/api"}})

	rows := m.clone.table.Table().Rows()
	if len(rows) != 1 || rows[0][1] != "alpha/api" {
		t.Fatalf("rows = %v, want the repository as soon as it was found", rows)
	}
	if found, _, _, _ := m.clone.counts(); found != 1 {
		t.Errorf("found = %d, want 1", found)
	}
}

func TestARowWalksThroughItsStates(t *testing.T) {
	tests := []struct {
		name  string
		event cloneEvent
		want  cloneState
	}{
		{"cloned", cloneEvent{kind: cloneEnded, path: "alpha/api"}, cloneCloned},
		{"already there", cloneEvent{kind: cloneEnded, path: "alpha/api", skipped: true}, cloneAlreadyThere},
		{"failed", cloneEvent{kind: cloneEnded, path: "alpha/api", err: errors.New("boom")}, cloneFailed},
	}

	for _, tt := range tests {
		t.Run(tt.name, func(t *testing.T) {
			m := feed(t, cloningModel(t),
				CloneEventMsg{event: cloneEvent{kind: cloneFound, path: "alpha/api"}},
				CloneEventMsg{event: cloneEvent{kind: cloneBegan, path: "alpha/api"}})

			if m.clone.rows[0].state != cloneRunning {
				t.Fatalf("state = %v after began, want cloneRunning", m.clone.rows[0].state)
			}

			m = feed(t, m, CloneEventMsg{event: tt.event})
			if got := m.clone.rows[0].state; got != tt.want {
				t.Errorf("state = %v, want %v", got, tt.want)
			}
		})
	}
}

// A group that cannot be listed gets a row of its own. Folding it into a
// repository's error would attribute it to one repository out of however many
// were never discovered.
func TestAFailedWalkGetsItsOwnRow(t *testing.T) {
	m := feed(t, cloningModel(t), CloneEventMsg{
		event: cloneEvent{kind: cloneWalkFailed, path: "alpha/sub", err: errors.New("403")},
	})

	rows := m.clone.rows
	if len(rows) != 1 || rows[0].path != "alpha/sub" || rows[0].state != cloneFailed {
		t.Fatalf("rows = %+v, want the group as a failed row", rows)
	}
	if !strings.Contains(rows[0].detail, "403") {
		t.Errorf("detail = %q, want the API error", rows[0].detail)
	}
}

// Decision 13 discards the list, and a failed clone wrote nothing — so the
// failures have to be said while the view is still alive.
func TestTheFailuresAreReportedWhenTheRunEnds(t *testing.T) {
	m := feed(t, cloningModel(t),
		CloneEventMsg{event: cloneEvent{kind: cloneEnded, path: "alpha/api", err: errors.New("boom")}})

	m, cmd := step(t, m, CloneRunFinishedMsg{})

	if !m.clone.finished {
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
	cancelled := false
	m := cloningModel(t)
	m.clone.run = &cloneRun{events: make(chan cloneEvent), cancel: func() { cancelled = true }}
	m = feed(t, m, CloneEventMsg{event: cloneEvent{kind: cloneFound, path: "alpha/api"}},
		CloneEventMsg{event: cloneEvent{kind: cloneBegan, path: "alpha/api"}})

	m = feed(t, m, testutil.Key("esc"))

	if !cancelled {
		t.Error("esc did not cancel the run")
	}
	if m.mode != ModeCloning || m.clone == nil {
		t.Fatalf("esc closed the list while a clone was still running (mode=%v)", m.mode)
	}
	if !strings.Contains(m.cloneStatusLine(), "Cancelling") {
		t.Errorf("the status line does not say it is cancelling: %q", m.cloneStatusLine())
	}
}

// A second esc must not force. Forcing means killing a git clone mid-write,
// which is the partial directory decision 12 exists to avoid.
func TestASecondEscDoesNotForce(t *testing.T) {
	calls := 0
	m := cloningModel(t)
	m.clone.run = &cloneRun{events: make(chan cloneEvent), cancel: func() { calls++ }}

	m = feed(t, m, testutil.Key("esc"), testutil.Key("esc"), testutil.Key("esc"))

	if calls != 1 {
		t.Errorf("cancel was called %d times, want once", calls)
	}
	if m.mode != ModeCloning {
		t.Errorf("mode = %v, want the list to stay until the run ends", m.mode)
	}
}

func TestEscClosesTheListOnceTheRunHasEnded(t *testing.T) {
	m := feed(t, cloningModel(t), CloneRunFinishedMsg{})

	m = feed(t, m, testutil.Key("esc"))

	if m.mode != ModeNormal || m.clone != nil {
		t.Errorf("esc left mode=%v list=%v after the run ended", m.mode, m.clone)
	}
	if !m.selection.isEmpty() {
		t.Error("closing the list kept the selection")
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

func TestOpenInBrowserWithoutAURLDoesNothing(t *testing.T) {
	m := drilledModel(t)
	for _, child := range m.currentGroupNode.Children {
		child.WebURL = ""
	}

	_, cmd := step(t, m, testutil.Key(keymap.Web))

	if cmd != nil {
		t.Error("ctrl+w issued a command for a node with no web URL")
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

func TestAccessLevelNames(t *testing.T) {
	tests := []struct {
		level int
		want  string
	}{
		{50, "Owner"}, {60, "Owner"},
		{40, "Maintainer"}, {30, "Developer"},
		{20, "Reporter"}, {10, "Guest"},
		{0, ""}, {5, ""},
	}
	for _, tt := range tests {
		node := &TreeNode{AccessLevel: tt.level}
		if got := node.AccessLevelName(); got != tt.want {
			t.Errorf("AccessLevelName(%d) = %q, want %q", tt.level, got, tt.want)
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
