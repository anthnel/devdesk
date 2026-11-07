package app

import (
	"fmt"
	"go/ast"
	"go/token"
	"sort"
	"strconv"
	"strings"
	"testing"

	tea "github.com/charmbracelet/bubbletea"

	"github.com/anthnel/devdesk/internal/command"
	"github.com/anthnel/devdesk/internal/ui/datatable"
	"github.com/anthnel/devdesk/internal/ui/help"
	"github.com/anthnel/devdesk/internal/ui/indicator"
)

// A-16 (DEVDESK-143), skeleton delivered with the :jobs pilot (DEVDESK-142).
//
// The guard answers one question for every view the router can build: is it on
// the activity column, is it waiting for its migration, or is it exempt for a
// stated reason? A view that is none of the three fails the test by name. It
// is a skeleton: DEVDESK-143 completes it (footer half of criterion 5,
// criterion 7).

// activityView is what a migrated view exposes to the guard: read-only tables,
// keyed "<view>/<table>".
type activityView interface {
	ActivityTables() map[string]datatable.ActivityTable
}

// migratedTables are the tables already on the activity column, by view.
var migratedTables = map[command.ViewType][]string{
	command.ViewJobs: {"jobs/runs", "jobs/targets"},
}

// pendingTable is a table that still has to move to the activity column.
type pendingTable struct {
	Table string
	View  command.ViewType
	Story string
}

// pendingTables may only shrink: each migration story removes its own entries.
var pendingTables = []pendingTable{
	{"ws", command.ViewWorkspaces, "A-07"},
	{"sec/inventory", command.ViewSecurity, "A-08"},
	{"oci/images", command.ViewOCIResources, "A-09a"},
	{"oci/registry-browser", command.ViewOCIResources, "A-09a"},
	{"oci/networks", command.ViewOCIResources, "A-09b"},
	{"oci/volumes", command.ViewOCIResources, "A-09b"},
	{"oci/registries", command.ViewOCIResources, "A-09b"},
	{"explorer/tree", command.ViewGitExplorer, "A-10a"},
	{"explorer/clone-list", command.ViewGitExplorer, "A-10b"},
	{"templates", command.ViewTemplates, "A-11"},
	{"netdiag/ports", command.ViewNetdiag, "A-12"},
	{"agent-usage/agents", command.ViewAgentUsage, "A-13"},
	{"containers", command.ViewContainers, "A-14"},
}

// maxPendingTables is the ceiling: adding an exception means raising both the
// list and this constant in the same diff, which is visible in review. Lower it
// with every migration.
const maxPendingTables = 13

// exemptViews have no long-running operation to show per row (inventory:
// "sans objet" or out of the batch), each with its reason.
var exemptViews = map[command.ViewType]string{
	command.ViewDashboard:     "no table with row operations; the dashboard is a summary",
	command.ViewStatus:        "monitors are polled as a whole, not per-row operations",
	command.ViewGitAuth:       "a form, no table",
	command.ViewConfiguration: "a form of settings, no table of operations",
	command.ViewAbout:         "static facts, no table",
	command.ViewFiles:         "out of the batch, candidate for a later S story (the delete goes through the registry, no story covers it)",
	command.ViewViewer:        "a document pane, no table",
}

func pendingViews() map[command.ViewType][]pendingTable {
	out := map[command.ViewType][]pendingTable{}
	for _, p := range pendingTables {
		out[p.View] = append(out[p.View], p)
	}
	return out
}

func TestActivityGuardPendingListOnlyShrinks(t *testing.T) {
	if got := len(pendingTables); got > maxPendingTables {
		t.Errorf("pendingTables has %d entries, ceiling is %d: the list may only shrink (A-16 criterion 6)", got, maxPendingTables)
	}
	seen := map[string]bool{}
	for _, p := range pendingTables {
		if p.Story == "" {
			t.Errorf("pending table %q names no story", p.Table)
		}
		if seen[p.Table] {
			t.Errorf("pending table %q is listed twice", p.Table)
		}
		seen[p.Table] = true
	}
}

// builtView returns the view the router builds for vt, laid out at 60x30.
func builtView(t *testing.T, vt command.ViewType) tea.Model {
	t.Helper()
	a := routerAt(t, &bareView{}, 60, 30) // isolateHome, session-only secrets
	a.createView(vt)
	m := a.views[vt]
	if m == nil {
		t.Fatalf("view %q: the router did not build it", vt)
	}
	m, _ = m.Update(tea.WindowSizeMsg{Width: 60, Height: 30})
	return m
}

// classificationProblems says what is wrong with the classification of one
// view, empty when it is fine. It works per table: a view may be migrated for
// some tables and pending for others (A-09a/A-09b, A-10a/A-10b) as long as the
// two sets are disjoint; an exempt view is neither migrated nor pending.
func classificationProblems(vt command.ViewType, migrated map[command.ViewType][]string, pending []pendingTable, exempt map[command.ViewType]string) []string {
	var problems []string
	mig := migrated[vt]
	var pend []string
	for _, p := range pending {
		if p.View == vt {
			pend = append(pend, p.Table)
		}
	}
	_, isExempt := exempt[vt]
	switch {
	case len(mig) == 0 && len(pend) == 0 && !isExempt:
		problems = append(problems, "neither migrated, pending (with its story) nor exempt (with its reason)")
	case isExempt && (len(mig) > 0 || len(pend) > 0):
		problems = append(problems, "exempt and also migrated or pending")
	}
	for _, name := range pend {
		if contains(mig, name) {
			problems = append(problems, fmt.Sprintf("table %q is both migrated and pending", name))
		}
	}
	return problems
}

// staleExceptions lists the pending tables of a view that the view already
// reports: their exception must go.
func staleExceptions(vt command.ViewType, reported []string, pending []pendingTable) []string {
	var stale []string
	for _, p := range pending {
		if p.View == vt && contains(reported, p.Table) {
			stale = append(stale, p.Table)
		}
	}
	return stale
}

// Criterion 6: every view the router builds is migrated, pending or exempt.
func TestActivityGuardEveryViewIsClassified(t *testing.T) {
	for _, name := range command.AllViewNames() {
		vt := command.ViewType(name)
		t.Run(name, func(t *testing.T) {
			for _, p := range classificationProblems(vt, migratedTables, pendingTables, exemptViews) {
				t.Errorf("view %q is %s: classify it in activity_guard_test.go", vt, p)
			}
		})
	}
}

// The logic itself, on a fictitious view that is partly migrated.
func TestActivityGuardClassificationLogic(t *testing.T) {
	const v command.ViewType = "fiction"
	pending := []pendingTable{{"fiction/b", v, "A-99"}}
	tests := []struct {
		name     string
		migrated map[command.ViewType][]string
		pending  []pendingTable
		exempt   map[command.ViewType]string
		problems int
		stale    []string
		reported []string
	}{
		{"partly migrated is fine", map[command.ViewType][]string{v: {"fiction/a"}}, pending, nil, 0, nil, []string{"fiction/a"}},
		{"unclassified fails", nil, nil, nil, 1, nil, nil},
		{"same table both ways fails", map[command.ViewType][]string{v: {"fiction/b"}}, pending, nil, 1, []string{"fiction/b"}, []string{"fiction/b"}},
		{"exempt and pending fails", nil, pending, map[command.ViewType]string{v: "why"}, 1, nil, nil},
		{"pending table not reported is not stale", map[command.ViewType][]string{v: {"fiction/a"}}, pending, nil, 0, nil, []string{"fiction/a"}},
	}
	for _, tt := range tests {
		t.Run(tt.name, func(t *testing.T) {
			if got := classificationProblems(v, tt.migrated, tt.pending, tt.exempt); len(got) != tt.problems {
				t.Errorf("problems = %v, want %d", got, tt.problems)
			}
			if got := staleExceptions(v, tt.reported, tt.pending); !equalStrings(got, tt.stale) {
				t.Errorf("stale = %v, want %v", got, tt.stale)
			}
		})
	}
}

func equalStrings(a, b []string) bool {
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

// Criteria 1, 4 and the help half of 5, at runtime, on the views the router builds.
func TestActivityGuardMigratedViews(t *testing.T) {
	legendTitle := indicator.Legend().Title
	for vt, want := range migratedTables {
		t.Run(string(vt), func(t *testing.T) {
			m := builtView(t, vt)
			av, ok := m.(activityView)
			if !ok {
				t.Fatalf("view %q is migrated but does not expose ActivityTables()", vt)
			}
			tables := av.ActivityTables()
			for _, name := range want {
				tb, ok := tables[name]
				if !ok {
					t.Errorf("view %q: table %q is migrated but ActivityTables() does not report it (reported: %v)", vt, name, sortedKeys(tables))
					continue
				}
				checkColumnZero(t, vt, name, tb.Shapes())
				// Criterion 4, Rule 116: the rendered extent, never the sum of columns.
				if got, want := tb.RenderedWidth(), 60-2; got != want {
					t.Errorf("view %q table %q: RenderedWidth() = %d at 60 columns, want %d (Rule 116)", vt, name, got, want)
				}
			}
			for name := range tables {
				if !contains(want, name) {
					t.Errorf("view %q reports table %q, which migratedTables does not list", vt, name)
				}
			}
			// Criterion 5, help half: the legend.
			hp, ok := m.(help.Provider)
			if !ok {
				t.Fatalf("view %q has no help", vt)
			}
			found := false
			for _, s := range hp.GetHelpContent().Sections {
				found = found || s.Title == legendTitle
			}
			if !found {
				t.Errorf("view %q: help has no %q section (indicator.Legend)", vt, legendTitle)
			}
		})
	}
}

// Criterion 6, other direction: a view that reports a table still listed as
// pending has migrated it; the exception must go. An exempt view must report
// none at all.
func TestActivityGuardNoStaleException(t *testing.T) {
	for _, name := range command.AllViewNames() {
		vt := command.ViewType(name)
		_, isExempt := exemptViews[vt]
		if !isExempt && len(pendingViews()[vt]) == 0 {
			continue
		}
		t.Run(name, func(t *testing.T) {
			av, ok := builtView(t, vt).(activityView)
			if !ok {
				return
			}
			if isExempt {
				t.Errorf("view %q exposes ActivityTables() but is exempt: move it out of exemptViews", vt)
			}
			reported := sortedKeys(av.ActivityTables())
			if stale := staleExceptions(vt, reported, pendingTables); len(stale) > 0 {
				t.Errorf("view %q reports %v, still listed as pending: remove the exception", vt, stale)
			}
		})
	}
}

func checkColumnZero(t *testing.T, vt command.ViewType, table string, shapes []datatable.ColumnShape) {
	t.Helper()
	if len(shapes) == 0 {
		t.Errorf("view %q table %q: no column reported", vt, table)
		return
	}
	c := shapes[0]
	var bad []string
	if !c.Indicator {
		bad = append(bad, "not an Indicator column")
	}
	if c.Title != "" {
		bad = append(bad, fmt.Sprintf("title %q (want empty)", c.Title))
	}
	if c.Sizing != datatable.SizingFixed {
		bad = append(bad, "not SizingFixed")
	}
	if c.MinWidth != datatable.IconColumnWidth {
		bad = append(bad, fmt.Sprintf("MinWidth %d (want IconColumnWidth)", c.MinWidth))
	}
	if c.Less || c.Search {
		bad = append(bad, "declares Less or Search")
	}
	if len(bad) > 0 {
		t.Errorf("view %q table %q: column 0 is wrong: %s (A-16 criterion 1)", vt, table, strings.Join(bad, "; "))
	}
}

// migratedSourceDirs are scanned for the retired mechanisms (criterion 2b, and
// the hard-coded glyphs of 3). Only migrated views: pending ones still use them.
var migratedSourceDirs = []string{"ui/jobsview/"}

// activityGlyphs are the runes of internal/ui/indicator; a view takes them from
// indicator.Glyph and never spells one.
const activityGlyphs = "●○◴⊗◐◕◉⊘⊖◌"

// Criterion 2b: no AdvanceSpinner, no StatusColumn:, no frame field, no glyph
// literal in a migrated view. RenderedFrame (a method of the viewport frame) is allowed.
func TestActivityGuardMigratedSourcesUseNoRetiredMechanism(t *testing.T) {
	scanned := 0
	sourceFiles(t, func(rel string, fset *token.FileSet, file *ast.File) {
		if !hasAnyPrefix(rel, migratedSourceDirs) {
			return
		}
		scanned++
		report := func(pos token.Pos, what string) {
			t.Errorf("%s: %s in a migrated view (A-16 criterion 2b/3)", fset.Position(pos), what)
		}
		isFrame := func(name string) bool {
			return name == "Frame" || name == "frame" || name == "SpinnerFrame"
		}
		ast.Inspect(file, func(n ast.Node) bool {
			switch x := n.(type) {
			case *ast.CallExpr:
				if sel, ok := x.Fun.(*ast.SelectorExpr); ok && sel.Sel.Name == "AdvanceSpinner" {
					report(x.Pos(), "call to AdvanceSpinner")
				}
			case *ast.KeyValueExpr:
				if id, ok := x.Key.(*ast.Ident); ok {
					if id.Name == "StatusColumn" {
						report(id.Pos(), "StatusColumn: key")
					}
					if isFrame(id.Name) {
						report(id.Pos(), "frame field "+id.Name+": set")
					}
				}
			case *ast.Field:
				for _, id := range x.Names {
					if isFrame(id.Name) {
						report(id.Pos(), "frame field "+id.Name)
					}
				}
			case *ast.SelectorExpr:
				if isFrame(x.Sel.Name) {
					report(x.Sel.Pos(), "frame selector ."+x.Sel.Name)
				}
			case *ast.BasicLit:
				if x.Kind == token.STRING || x.Kind == token.CHAR {
					s, err := strconv.Unquote(x.Value)
					if x.Kind == token.CHAR {
						var q rune
						q, _, _, err = strconv.UnquoteChar(x.Value[1:len(x.Value)-1], '\'')
						s = string(q)
					}
					if err == nil && strings.ContainsAny(s, activityGlyphs) {
						report(x.Pos(), "activity glyph written as a literal "+x.Value+" (use indicator.Glyph)")
					}
				}
			}
			return true
		})
	})
	if scanned == 0 {
		t.Fatalf("no source file found under %v: the scan reads nothing", migratedSourceDirs)
	}
}

func hasAnyPrefix(s string, prefixes []string) bool {
	for _, p := range prefixes {
		if strings.HasPrefix(s, p) {
			return true
		}
	}
	return false
}

func contains(list []string, s string) bool {
	for _, x := range list {
		if x == s {
			return true
		}
	}
	return false
}

func sortedKeys(m map[string]datatable.ActivityTable) []string {
	out := make([]string, 0, len(m))
	for k := range m {
		out = append(out, k)
	}
	sort.Strings(out)
	return out
}
