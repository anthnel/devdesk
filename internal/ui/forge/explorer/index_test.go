package explorer

import (
	"errors"
	"strings"
	"testing"
	"time"

	tea "github.com/charmbracelet/bubbletea"

	"github.com/anthnel/devdesk/internal/forgeindex"
	"github.com/anthnel/devdesk/internal/shared"
	"github.com/anthnel/devdesk/internal/ui/fuzzy"
	"github.com/anthnel/devdesk/internal/ui/testutil"
)

// indexFixture mirrors rootFixtures and childFixtures, one level deeper: what
// the router's walk would have left for this session.
func indexFixture() *forgeindex.Index {
	entries := []forgeindex.Entry{
		{ID: "1", Path: "alpha", Name: "alpha", Kind: forgeindex.KindNamespace},
		{ID: "2", Path: "beta", Name: "beta", Kind: forgeindex.KindNamespace},
		{ID: "3", Path: "gamma", Name: "gamma", Kind: forgeindex.KindNamespace},
		{ID: "10", Path: "alpha/sub", Name: "sub", Parent: "alpha", Kind: forgeindex.KindNamespace},
		{ID: "11", Path: "alpha/api", Name: "api", Parent: "alpha", Kind: forgeindex.KindRepository},
		{ID: "12", Path: "alpha/legacy", Name: "legacy", Parent: "alpha", Kind: forgeindex.KindRepository},
		{ID: "20", Path: "alpha/sub/deep", Name: "deep", Parent: "alpha/sub", Kind: forgeindex.KindRepository},
		{ID: "21", Path: "alpha/sub/other", Name: "other", Parent: "alpha/sub", Kind: forgeindex.KindRepository},
	}
	return forgeindex.New("https://gitlab.example.com", "u", time.Now(), entries, nil)
}

// indexedModel is a laid-out, authenticated model whose session already has an
// index — the explorer opened after the router read or walked it.
func indexedModel(t *testing.T) Model {
	t.Helper()
	state := authenticatedState(t)
	state.ForgeIndex = indexFixture()
	return feed(t, New(testConfig(), state), tea.WindowSizeMsg{Width: 160, Height: 30})
}

// editOf runs the index edit a command carries against ix.
func editOf(t *testing.T, cmd tea.Cmd, ix *forgeindex.Index) *forgeindex.Index {
	t.Helper()
	edit, ok := testutil.MsgOf[shared.ForgeIndexEditMsg](cmd)
	if !ok {
		t.Fatal("no ForgeIndexEditMsg was sent")
	}
	return edit.Edit(ix)
}

func TestTheExplorerOpensOnTheIndexedRoots(t *testing.T) {
	m := indexedModel(t)

	if got := rowNames(m.table.Table().Rows()); len(got) != 3 {
		t.Fatalf("rows = %v, want the three roots from the index before the forge answers", got)
	}
	if m.loadingTree() {
		t.Error("the tree reads as loading while the index is on screen")
	}
	if !m.showsTree() {
		t.Error("the breadcrumb is hidden while the roots are re-read — the footer would jump")
	}
	if st := m.status(); !st.Spinner || !strings.Contains(st.Text, "Updating") {
		t.Errorf("status = %+v, want the background refresh in the footer", st)
	}
}

// The decorated roots land on the same nodes, so the role appears in place and
// the cursor does not move.
func TestTheDecoratedRootsLandInPlace(t *testing.T) {
	m := indexedModel(t)
	m = feed(t, m, testutil.Key("down"))
	before := m.nodes[1]

	m, cmd := step(t, m, RootGroupsLoadedMsg{Nodes: rootFixtures()})

	if m.nodes[1] != before {
		t.Error("the root node was replaced rather than updated — a drilled path would be orphaned")
	}
	if m.nodes[1].Role != "Developer" {
		t.Errorf("role = %q, want the forge's", m.nodes[1].Role)
	}
	if got := m.selectedPath(); got != "beta" {
		t.Errorf("cursor on %q after the refresh, want it left on beta", got)
	}
	if m.refreshing != 0 {
		t.Errorf("refreshing = %d after the roots landed", m.refreshing)
	}
	if next := editOf(t, cmd, indexFixture()); next.Len() != indexFixture().Len() {
		t.Errorf("the index lost entries when told the same roots: %d", next.Len())
	}
}

func TestDrillingIntoAnIndexedGroupShowsItAtOnce(t *testing.T) {
	m := feed(t, indexedModel(t), RootGroupsLoadedMsg{Nodes: rootFixtures()})

	m, cmd := step(t, m, testutil.Key("right"))

	if got := rowNames(m.table.Table().Rows()); len(got) != 3 {
		t.Fatalf("rows = %v, want alpha's three children from the index", got)
	}
	if m.loading {
		t.Error("an indexed level blocked on the forge")
	}
	if cmd == nil || !m.nodes[0].Loading {
		t.Error("the level was not read again from the forge behind the index")
	}
}

// What the forge answers replaces the level: a repository deleted since the walk
// goes, and the index is told so.
func TestTheForgesAnswerCorrectsAnIndexedLevel(t *testing.T) {
	m := feed(t, indexedModel(t), RootGroupsLoadedMsg{Nodes: rootFixtures()}, testutil.Key("right"))
	alpha := m.nodes[0]
	fresh := childFixtures(alpha)[:2] // "legacy" is gone

	m, cmd := step(t, m, ChildrenLoadedMsg{ParentNode: alpha, Children: fresh})

	if got := rowNames(m.table.Table().Rows()); len(got) != 2 {
		t.Errorf("rows = %v, want the two the forge still lists", got)
	}
	if !alpha.Fresh || alpha.Loading {
		t.Errorf("alpha Fresh=%v Loading=%v after its answer", alpha.Fresh, alpha.Loading)
	}
	if next := editOf(t, cmd, indexFixture()); func() bool { _, ok := next.Lookup("alpha/legacy"); return ok }() {
		t.Error("the index still offers a repository the forge no longer lists")
	}
}

func TestAFailedBackgroundRefreshKeepsTheIndexedRows(t *testing.T) {
	m := feed(t, indexedModel(t), RootGroupsLoadedMsg{Nodes: rootFixtures()}, testutil.Key("right"))
	alpha := m.nodes[0]

	m = feed(t, m, LoadErrorMsg{Error: errors.New("502"), ParentNode: alpha})

	if m.error != "" {
		t.Errorf("error = %q — a failed refresh replaced the rows with an error state", m.error)
	}
	if got := rowNames(m.table.Table().Rows()); len(got) != 3 {
		t.Errorf("rows = %v, want the indexed rows kept", got)
	}
	if alpha.Loading || m.refreshing != 0 {
		t.Error("the refresh was not released")
	}
}

// ── g ───────────────────────────────────────────────────────────────────────

func TestGOpensTheFinderOverTheIndex(t *testing.T) {
	m := indexedModel(t)

	m, _ = step(t, m, testutil.Key("g"))

	if m.mode != ModeFuzzyFinding || m.finder == nil {
		t.Fatalf("mode = %v, want the prompt open", m.mode)
	}
	if m.finder.Loading() {
		t.Error("the prompt waits although the index is already there")
	}
	if got := m.GetFooterHeight(); got != fuzzy.FooterHeight {
		t.Errorf("footer height = %d, want %d", got, fuzzy.FooterHeight)
	}
	if !m.FilterBarVisible() || !m.InEditMode() {
		t.Error("the prompt's bar does not close the viewport, or keys leak to the router")
	}
	if !strings.Contains(m.RenderFooter(120), "Find") {
		t.Error("the query bar is not in the footer")
	}
}

// Enter on a repository three levels down lands on its row, with the stack a
// drill-down would have built — and no request on the way.
func TestEnterJumpsToADeepRepository(t *testing.T) {
	m := feed(t, indexedModel(t), RootGroupsLoadedMsg{Nodes: rootFixtures()}, testutil.Key("g"))
	m = feed(t, m, testutil.Type("deep")...)

	_, cmd := step(t, m, testutil.Key("enter"))
	confirm, ok := testutil.MsgOf[fuzzy.ConfirmMsg](cmd)
	if !ok || confirm.Key != "alpha/sub/deep" {
		t.Fatalf("confirmed %+v, want alpha/sub/deep", confirm)
	}
	m, _ = step(t, m, confirm)

	if m.mode != ModeNormal || m.finder != nil {
		t.Error("the prompt is still open after the jump")
	}
	if m.currentGroupNode == nil || m.currentGroupNode.FullPath != "alpha/sub" {
		t.Fatalf("landed in %v, want alpha/sub", pathOf(m.currentGroupNode))
	}
	if len(m.navigationStack) != 2 || m.navigationStack[0] != nil || m.navigationStack[1] != m.nodes[0] {
		t.Errorf("stack = %v, want [root, alpha] as drilling would build it", m.navigationStack)
	}
	if got := m.selectedPath(); got != "alpha/sub/deep" {
		t.Errorf("cursor on %q, want the repository's row", got)
	}

	// ← goes back the way a drill-down would have come.
	m = feed(t, m, testutil.Key("left"))
	if pathOf(m.currentGroupNode) != "alpha" {
		t.Errorf("← from the jump landed in %q, want alpha", pathOf(m.currentGroupNode))
	}
}

func TestEnterOnANamespaceLandsInsideIt(t *testing.T) {
	m := feed(t, indexedModel(t), testutil.Key("g"))
	m = feed(t, m, fuzzy.ConfirmMsg{Key: "alpha/sub"})

	if pathOf(m.currentGroupNode) != "alpha/sub" {
		t.Errorf("landed in %q, want inside alpha/sub", pathOf(m.currentGroupNode))
	}
	if got := rowNames(m.table.Table().Rows()); len(got) != 2 {
		t.Errorf("rows = %v, want its two repositories", got)
	}
}

func TestAJumpToAPathNoLongerIndexedWarns(t *testing.T) {
	m := feed(t, indexedModel(t), testutil.Key("g"), fuzzy.ConfirmMsg{Key: "alpha/gone"})

	if m.currentGroupNode != nil {
		t.Error("the view moved for a path the index does not hold")
	}
	if !strings.Contains(m.renderInfoLine(160), "no longer listed") {
		t.Errorf("footer = %q, want the refusal said", m.renderInfoLine(160))
	}
}

// Without an index yet the prompt still opens, and fills when one lands.
func TestTheFinderFillsWhenTheIndexLands(t *testing.T) {
	m := feed(t, newTestModel(t), testutil.Key("g"))
	if !m.finder.Loading() {
		t.Fatal("the prompt did not wait for an index")
	}

	m.shared.ForgeIndex = indexFixture()
	m = feed(t, m, shared.ForgeIndexChangedMsg{})

	if m.finder.Loading() {
		t.Error("the prompt was not filled by the new index")
	}
}

func TestGIsGreyedAndRefusedWithoutASession(t *testing.T) {
	m := feed(t, New(testConfig(), &shared.State{}), tea.WindowSizeMsg{Width: 160, Height: 30})

	if !testutil.ShortcutDisabled(m.GetShortcuts(), "g") {
		t.Error("g is offered without a session")
	}
	m = feed(t, m, testutil.Key("g"))
	if m.mode == ModeFuzzyFinding {
		t.Error("g opened the prompt without a session")
	}
}

func TestEscClosesTheFinder(t *testing.T) {
	m := feed(t, indexedModel(t), testutil.Key("g"))
	_, cmd := step(t, m, testutil.Key("esc"))
	cancel, ok := testutil.MsgOf[fuzzy.CancelMsg](cmd)
	if !ok {
		t.Fatal("esc did not cancel")
	}
	m = feed(t, m, cancel)
	if m.mode != ModeNormal || m.finder != nil {
		t.Error("the prompt survived esc")
	}
}

// ── The index follows what the explorer does ────────────────────────────────

func TestACreatedRepositoryIsAddedToTheIndex(t *testing.T) {
	m := feed(t, indexedModel(t), RootGroupsLoadedMsg{Nodes: rootFixtures()}, testutil.Key("right"))
	alpha := m.nodes[0]
	alpha.Children = append(alpha.Children, &TreeNode{FullPath: "alpha/new", Name: "new", Type: NodeTypeProject, Creating: true, Parent: alpha})

	_, cmd := step(t, m, ProjectCreatedMsg{Repository: newProject(99, "alpha/new"), Target: "alpha/new"})

	next := editOf(t, cmd, indexFixture())
	if e, ok := next.Lookup("alpha/new"); !ok || e.Parent != "alpha" {
		t.Errorf("index entry = %+v, %v; want alpha/new under alpha", e, ok)
	}
}

func TestADeletedGroupLeavesTheIndexWithItsContent(t *testing.T) {
	m := feed(t, indexedModel(t), RootGroupsLoadedMsg{Nodes: rootFixtures()}, testutil.Key("right"))
	sub := m.nodes[0].Children[0]

	_, cmd := step(t, m, DeleteCompleteMsg{Target: sub.FullPath, DeletedNode: sub})

	next := editOf(t, cmd, indexFixture())
	if _, ok := next.Lookup("alpha/sub/deep"); ok {
		t.Error("a deleted group's repositories are still offered by g")
	}
}
