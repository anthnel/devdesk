package app

import (
	"fmt"
	"go/ast"
	"go/parser"
	"go/token"
	"sort"
	"strconv"
	"strings"
	"testing"

	tea "github.com/charmbracelet/bubbletea"

	"github.com/charmbracelet/x/ansi"

	"github.com/anthnel/devdesk/internal/command"
	"github.com/anthnel/devdesk/internal/jobs"
	"github.com/anthnel/devdesk/internal/ui/datatable"
	"github.com/anthnel/devdesk/internal/ui/help"
	"github.com/anthnel/devdesk/internal/ui/indicator"
	"github.com/anthnel/devdesk/internal/ui/testutil"
)

// A-16 (DEVDESK-143), the cross-cutting guard, started with the :jobs pilot
// (DEVDESK-142).
//
// The guard answers one question for every view the router can build: is it on
// the activity column, is it waiting for its migration, or is it exempt for a
// stated reason? A view that is none of the three fails the test by name. For a
// migrated view it also checks the column, the help legend, the retired
// mechanisms in its declared sources (including its own ticks), and the footer
// of its selected row. Declared exceptions may only shrink, and follow their
// stories: pendingTables here, declaredRowChains in animation_guard_test.go.

// activityView is what a migrated view exposes to the guard: read-only tables,
// keyed "<view>/<table>".
type activityView interface {
	ActivityTables() map[string]datatable.ActivityTable
}

// migratedTables are the tables already on the activity column, by view.
var migratedTables = map[command.ViewType][]string{
	command.ViewJobs:       {"jobs/runs", "jobs/targets"},
	command.ViewWorkspaces: {"ws"},
	command.ViewSecurity:   {"sec/inventory"},
}

// pendingTable is a table that still has to move to the activity column.
type pendingTable struct {
	Table string
	View  command.ViewType
	Story string
}

// pendingTables may only shrink: each migration story removes its own entries.
var pendingTables = []pendingTable{
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
const maxPendingTables = 12

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

// migratedSources are the source prefixes (a directory ending in "/" or a file
// path, relative to internal/) of each migrated view. They are scanned for the
// retired mechanisms (criterion 2a/2b, and the hard-coded glyphs of 3). Only
// migrated views: pending ones still use them. A prefix may be a file so a view
// that migrates in two steps (OCI A-09a/b, explorer A-10a/b) can be scanned
// file by file.
var migratedSources = map[command.ViewType][]string{
	command.ViewJobs:       {"ui/jobsview/"},
	command.ViewWorkspaces: {"ui/workspaces/"},
	command.ViewSecurity:   {"ui/security/inventory.go", "ui/security/inventory_table.go", "ui/security/jobs.go", "ui/security/activity.go"},
}

// sourceDeclarationProblems says what is wrong with the link between migrated
// views and the sources scanned for them, empty when it is fine. files are the
// paths relative to internal/ of every non-test source file.
func sourceDeclarationProblems(migrated map[command.ViewType][]string, sources map[command.ViewType][]string, files []string) []string {
	var problems []string
	for vt := range migrated {
		if len(sources[vt]) == 0 {
			problems = append(problems, fmt.Sprintf("view %q is migrated but declares no source to scan in migratedSources", vt))
		}
	}
	for vt, prefixes := range sources {
		if len(migrated[vt]) == 0 {
			problems = append(problems, fmt.Sprintf("view %q has sources in migratedSources but is not migrated (migratedTables)", vt))
		}
		for _, p := range prefixes {
			if !anyHasPrefix(files, p) {
				problems = append(problems, fmt.Sprintf("view %q: source prefix %q matches no file", vt, p))
			}
		}
	}
	sort.Strings(problems)
	return problems
}

func anyHasPrefix(files []string, prefix string) bool {
	for _, f := range files {
		if prefix != "" && strings.HasPrefix(f, prefix) {
			return true
		}
	}
	return false
}

// Criteria 2b and 6: the sources scanned are exactly those of the migrated views.
func TestActivityGuardMigratedSourcesAreDeclared(t *testing.T) {
	var files []string
	sourceFiles(t, func(rel string, _ *token.FileSet, _ *ast.File) { files = append(files, rel) })
	for _, p := range sourceDeclarationProblems(migratedTables, migratedSources, files) {
		t.Error(p)
	}
}

// The logic itself, on fictitious views and files.
func TestActivityGuardMigratedSourcesLogic(t *testing.T) {
	const v command.ViewType = "fiction"
	files := []string{"ui/jobsview/model.go", "ui/oci/a.go", "ui/oci/b.go"}
	tests := []struct {
		name     string
		migrated map[command.ViewType][]string
		sources  map[command.ViewType][]string
		want     []string // substrings, one per expected problem
	}{
		{"jobs passes", map[command.ViewType][]string{command.ViewJobs: {"jobs/runs"}}, map[command.ViewType][]string{command.ViewJobs: {"ui/jobsview/"}}, nil},
		{"file prefix passes", map[command.ViewType][]string{v: {"x"}}, map[command.ViewType][]string{v: {"ui/oci/a.go"}}, nil},
		{"migrated view without sources", map[command.ViewType][]string{v: {"x"}}, nil, []string{`view "fiction" is migrated but declares no source`}},
		{"sources without migrated view", nil, map[command.ViewType][]string{v: {"ui/oci/"}}, []string{`view "fiction" has sources`}},
		{"prefix matching nothing", map[command.ViewType][]string{v: {"x"}}, map[command.ViewType][]string{v: {"ui/nowhere/"}}, []string{`"ui/nowhere/" matches no file`}},
		{"empty prefix matches nothing", map[command.ViewType][]string{v: {"x"}}, map[command.ViewType][]string{v: {""}}, []string{`"" matches no file`}},
	}
	for _, tt := range tests {
		t.Run(tt.name, func(t *testing.T) {
			got := sourceDeclarationProblems(tt.migrated, tt.sources, files)
			if len(got) != len(tt.want) {
				t.Fatalf("problems = %v, want %d", got, len(tt.want))
			}
			for i, w := range tt.want {
				if !strings.Contains(got[i], w) {
					t.Errorf("problem %d = %q, want it to contain %q", i, got[i], w)
				}
			}
		})
	}
}

// activityGlyphs are the runes of internal/ui/indicator; a view takes them from
// indicator.Glyph and never spells one.
const activityGlyphs = "●○◴⊗◐◕◉⊘⊖◌"

// declaredViewTicks are the tea.Tick / tea.Every calls a migrated view may
// still make, keyed "<file relative to internal/>:<function>" with the reason.
// Empty today: the router's one tick animates every row (DEVDESK-139). The list
// may only shrink.
var declaredViewTicks = map[string]string{}

// declaredGlyphConstants are the constants a migrated view may still define with
// an activity glyph in them, keyed "<file relative to internal/>:<constant>" with
// the reason (DEVDESK-144, decision Q4-A). The exception is the constant, not the
// file: any other literal in the same file is still rejected, so the help text
// has to build its sentence from the constant. The list may only shrink.
var declaredGlyphConstants = map[string]string{
	"ui/workspaces/view.go:gitMarkNoUpstream": "git marker for a branch with no upstream; it is a git fact, not a row state, and ws never shows the cancelled state, so nothing on screen is ambiguous (changing the marker is a UX ticket of its own)",
}

// finding is one retired mechanism found in a source file.
type finding struct {
	Pos  token.Position // file:line:column
	What string
}

// bubbleTeaName returns the local name file gives the bubbletea import: its
// alias, "." for a dot import, "tea" when unnamed, or "" when the file does not
// import it (or blank-imports it). A different package that happens to be
// called tea is therefore not mistaken for it.
func bubbleTeaName(file *ast.File) string {
	for _, imp := range file.Imports {
		path, err := strconv.Unquote(imp.Path.Value)
		if err != nil || path != "github.com/charmbracelet/bubbletea" {
			continue
		}
		switch {
		case imp.Name == nil:
			return "tea"
		case imp.Name.Name == "_":
			return ""
		default:
			return imp.Name.Name
		}
	}
	return ""
}

// retiredMechanisms reports, in a migrated view's file, every retired
// mechanism: AdvanceSpinner, a StatusColumn: key, a frame field, an activity
// glyph literal, and any reference to bubbletea's Tick / Every (under whatever name the file
// imports it) outside declaredViewTicks.
// rel is the file's path relative to internal/; it is the key of a declared tick.
// RenderedFrame (a method of the viewport frame) is allowed.
func retiredMechanisms(fset *token.FileSet, file *ast.File, rel string) []finding {
	var out []finding
	report := func(pos token.Pos, what string) {
		out = append(out, finding{Pos: fset.Position(pos), What: what})
	}
	tea := bubbleTeaName(file)
	selectors := map[*ast.Ident]bool{} // identifiers seen as the Sel of a selector
	// reportTick flags any reference to bubbletea's Tick / Every, called or not
	// (`var tick = tea.Tick` is as much a tick as `tea.Tick(...)`), unless the
	// enclosing "<file>:<function>" is declared.
	reportTick := func(pos token.Pos, fn, name string) {
		if name != "Tick" && name != "Every" {
			return
		}
		if _, declared := declaredViewTicks[rel+":"+fn]; !declared {
			report(pos, "reference to bubbletea's "+name+" (a view's rows are animated by the router's tick)")
		}
	}
	isFrame := func(name string) bool {
		return name == "Frame" || name == "frame" || name == "SpinnerFrame"
	}
	visit := func(fn string) func(ast.Node) bool {
		return func(n ast.Node) bool {
			switch x := n.(type) {
			case *ast.CallExpr:
				sel, ok := x.Fun.(*ast.SelectorExpr)
				if !ok {
					break
				}
				if sel.Sel.Name == "AdvanceSpinner" {
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
			case *ast.Ident:
				// Dot import: a bare Tick / Every that no declaration in the file binds.
				if tea == "." && x.Obj == nil && !selectors[x] {
					reportTick(x.Pos(), fn, x.Name)
				}
			case *ast.SelectorExpr:
				selectors[x.Sel] = true
				if pkg, ok := x.X.(*ast.Ident); ok && tea != "" && tea != "." && pkg.Name == tea {
					reportTick(x.Pos(), fn, x.Sel.Name)
				}
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
		}
	}
	for _, decl := range file.Decls {
		fn := ""
		if fd, ok := decl.(*ast.FuncDecl); ok {
			fn = fd.Name.Name
		}
		if gd, ok := decl.(*ast.GenDecl); ok && gd.Tok == token.CONST {
			for _, spec := range gd.Specs {
				if isDeclaredGlyphConstant(rel, spec) {
					continue
				}
				ast.Inspect(spec, visit(fn))
			}
			continue
		}
		ast.Inspect(decl, visit(fn))
	}
	return out
}

// isDeclaredGlyphConstant reports whether spec defines only constants declared in
// declaredGlyphConstants for the file rel.
func isDeclaredGlyphConstant(rel string, spec ast.Spec) bool {
	vs, ok := spec.(*ast.ValueSpec)
	if !ok || len(vs.Names) == 0 {
		return false
	}
	for _, id := range vs.Names {
		if _, declared := declaredGlyphConstants[rel+":"+id.Name]; !declared {
			return false
		}
	}
	return true
}

// Criterion 2a/2b: no retired mechanism and no tick in a migrated view.
func TestActivityGuardMigratedSourcesUseNoRetiredMechanism(t *testing.T) {
	scanned := 0
	sourceFiles(t, func(rel string, fset *token.FileSet, file *ast.File) {
		if !hasAnyPrefix(rel, allMigratedSources()) {
			return
		}
		scanned++
		for _, f := range retiredMechanisms(fset, file, rel) {
			t.Errorf("%s: %s in a migrated view (A-16 criterion 2/3)", f.Pos, f.What)
		}
	})
	if scanned == 0 {
		t.Fatalf("no source file found under %v: the scan reads nothing", migratedSources)
	}
	for key, reason := range declaredViewTicks {
		if reason == "" {
			t.Errorf("declared view tick %q names no reason", key)
		}
	}
	for key, reason := range declaredGlyphConstants {
		if reason == "" {
			t.Errorf("declared glyph constant %q names no reason", key)
		}
	}
}

// A declared glyph constant must still exist and still hold a glyph, otherwise
// the exception is stale and must go; and it excuses that constant only.
func TestActivityGuardDeclaredGlyphConstantsAreLive(t *testing.T) {
	live := map[string]bool{}
	sourceFiles(t, func(rel string, fset *token.FileSet, file *ast.File) {
		for _, decl := range file.Decls {
			gd, ok := decl.(*ast.GenDecl)
			if !ok || gd.Tok != token.CONST {
				continue
			}
			for _, spec := range gd.Specs {
				vs, ok := spec.(*ast.ValueSpec)
				if !ok {
					continue
				}
				for i, id := range vs.Names {
					key := rel + ":" + id.Name
					if _, declared := declaredGlyphConstants[key]; !declared || i >= len(vs.Values) {
						continue
					}
					if lit, ok := vs.Values[i].(*ast.BasicLit); ok {
						s, _ := strconv.Unquote(lit.Value)
						live[key] = strings.ContainsAny(s, activityGlyphs)
					}
				}
			}
		}
	})
	for key := range declaredGlyphConstants {
		if !live[key] {
			t.Errorf("declared glyph constant %q does not exist or holds no activity glyph: remove the exception", key)
		}
	}
	if got := len(declaredGlyphConstants); got > 1 {
		t.Errorf("declaredGlyphConstants has %d entries, ceiling is 1: the list may only shrink", got)
	}
}

// The exception covers the constant and nothing else in the file.
func TestActivityGuardGlyphConstantExceptionIsNarrow(t *testing.T) {
	src := "package v\nconst gitMarkNoUpstream = \"\u2298\"\nconst other = \"\u2298\"\nvar help = \"a \u2298 b\"\n"
	fset := token.NewFileSet()
	file, err := parser.ParseFile(fset, "view.go", src, 0)
	if err != nil {
		t.Fatal(err)
	}
	got := retiredMechanisms(fset, file, "ui/workspaces/view.go")
	if len(got) != 2 {
		t.Fatalf("findings = %v, want the undeclared const and the var literal only", got)
	}
	for _, f := range got {
		if f.Pos.Line == 2 {
			t.Errorf("the declared constant was flagged: %v", f)
		}
	}
}

// The detection itself, on sources held in strings.
func TestActivityGuardRetiredMechanismsLogic(t *testing.T) {
	const head = "package v\nimport tea \"github.com/charmbracelet/bubbletea\"\nimport \"github.com/charmbracelet/bubbles/spinner\"\n"
	const (
		bt    = "package v\nimport bt \"github.com/charmbracelet/bubbletea\"\nimport \"github.com/charmbracelet/bubbles/spinner\"\n"
		dot   = "package v\nimport . \"github.com/charmbracelet/bubbletea\"\nimport \"github.com/charmbracelet/bubbles/spinner\"\n"
		other = "package v\nimport tea \"example.com/notbubbletea\"\nimport \"github.com/charmbracelet/bubbles/spinner\"\n"
	)
	tests := []struct {
		name     string
		head     string // import block overriding head; it must keep the same 3 lines
		body     string
		declared map[string]string
		want     int
		at       string // "line:column" of the first finding, when want > 0
	}{
		{"tea.Tick", "", "func f() { _ = tea.Tick(0, nil) }", nil, 1, "4:16"},
		{"tea.Every", "", "func f() { _ = tea.Every(0, nil) }", nil, 1, "4:16"},
		{"spinner.Tick is not a view tick", "", "func f() { _ = spinner.Tick }\nfunc g() { _ = spinner.Tick() }", nil, 0, ""},
		{"declared tick", "", "func f() { _ = tea.Tick(0, nil) }", map[string]string{"ui/v/v.go:f": "why"}, 0, ""},
		{"tick declared for another function", "", "func g() { _ = tea.Tick(0, nil) }", map[string]string{"ui/v/v.go:f": "why"}, 1, "4:16"},
		{"AdvanceSpinner", "", "func f(t T) { t.AdvanceSpinner() }", nil, 1, "4:15"},
		{"glyph literal", "", "var g = \"\u25cf\"", nil, 1, "4:9"},
		{"clean", "", "func f() {}", nil, 0, ""},
		{"aliased import", bt, "func f() { _ = bt.Tick(0, nil) }", nil, 1, "4:16"},
		{"dot import", dot, "func f() { _ = Tick(0, nil) }", nil, 1, "4:16"},
		{"dot import, bare reference", dot, "var tick = Every", nil, 1, "4:12"},
		{"package variable holding tea.Tick", "", "var tick = tea.Tick", nil, 1, "4:12"},
		{"tick in a method", "", "func (m M) f() { _ = tea.Tick(0, nil) }", nil, 1, "4:22"},
		{"declared tick, aliased import", bt, "func f() { _ = bt.Tick(0, nil) }", map[string]string{"ui/v/v.go:f": "why"}, 0, ""},
		{"non-bubbletea package named tea", other, "func f() { _ = tea.Tick(0, nil) }", nil, 0, ""},
		{"dot import does not flag a method named Tick", dot, "func f(t T) { t.Tick() }", nil, 0, ""},
	}
	for _, tt := range tests {
		t.Run(tt.name, func(t *testing.T) {
			old := declaredViewTicks
			declaredViewTicks = tt.declared
			defer func() { declaredViewTicks = old }()
			fset := token.NewFileSet()
			h := head
			if tt.head != "" {
				h = tt.head
			}
			src := h + tt.body
			file, err := parser.ParseFile(fset, "v.go", src, 0)
			if err != nil {
				t.Fatal(err)
			}
			got := retiredMechanisms(fset, file, "ui/v/v.go")
			if len(got) != tt.want {
				t.Fatalf("findings = %v, want %d", got, tt.want)
			}
			if tt.want > 0 {
				if at := fmt.Sprintf("%d:%d", got[0].Pos.Line, got[0].Pos.Column); at != tt.at {
					t.Errorf("first finding at %s, want %s", at, tt.at)
				}
			}
		})
	}
}

func allMigratedSources() []string {
	var out []string
	for _, prefixes := range migratedSources {
		out = append(out, prefixes...)
	}
	return out
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

// footerRenderer is what a view exposes for its footer (the router's contract,
// restated so the guard reads it without the router).
type footerRenderer interface {
	RenderFooter(width int) string
}

// migratedFooterSeeds put a migrated view on screen with one active row selected
// and return the state that row is in. A migrated view without a seed fails the
// guard by name.
var migratedFooterSeeds = map[command.ViewType]func(*testing.T, *App) indicator.State{
	command.ViewJobs:       seedJobsFooter,
	command.ViewWorkspaces: seedWorkspacesFooter,
}

// seedJobsFooter starts a scan run, moves its item to running (a started run
// is queued until a transition says otherwise), and opens :jobs, whose first run
// is selected.
func seedJobsFooter(t *testing.T, a *App) indicator.State {
	t.Helper()
	a.Update(jobs.StartMsg{Run: scanRun("default", "a")})
	snap := a.jobs.Snapshot()
	if len(snap) != 1 || !a.jobs.Advance(snap[0].ID, "a", jobs.ItemRunning, "") {
		t.Fatalf("the seed could not put the run's item in the running state: %+v", snap)
	}
	testutil.Msgs(a.switchView(command.ViewJobs))
	a.resize(a.width, a.height)
	return indicator.Running
}

// footerSeedProblems says which migrated views have no seed, and which seeds
// belong to no migrated view.
func footerSeedProblems(migrated map[command.ViewType][]string, seeds map[command.ViewType]func(*testing.T, *App) indicator.State) []string {
	var problems []string
	for vt := range migrated {
		if seeds[vt] == nil {
			problems = append(problems, fmt.Sprintf("view %q is migrated but has no entry in migratedFooterSeeds", vt))
		}
	}
	for vt := range seeds {
		if len(migrated[vt]) == 0 {
			problems = append(problems, fmt.Sprintf("view %q has a footer seed but is not migrated", vt))
		}
	}
	sort.Strings(problems)
	return problems
}

// footerGlyphProblem is empty when footer carries the glyph of state. The footer
// glyph never blinks, so the steady one (on = true) is the one to look for.
func footerGlyphProblem(footer string, state indicator.State) string {
	glyph := indicator.Glyph(state, true)
	if glyph == "" {
		return fmt.Sprintf("state %v has no glyph: the seed must put the row in a visible state", state)
	}
	if !strings.Contains(ansi.Strip(footer), glyph) {
		return fmt.Sprintf("the footer does not carry the glyph %q of state %v", glyph, state)
	}
	return ""
}

// Criterion 5, footer half: the selected row's footer says its state with the
// same glyph as the column.
func TestActivityGuardMigratedFooterCarriesTheRowGlyph(t *testing.T) {
	for _, p := range footerSeedProblems(migratedTables, migratedFooterSeeds) {
		t.Error(p)
	}
	for vt, seed := range migratedFooterSeeds {
		if seed == nil || len(migratedTables[vt]) == 0 {
			continue
		}
		t.Run(string(vt), func(t *testing.T) {
			a := routerAt(t, &bareView{}, 180, 40)
			state := seed(t, a)
			fr, ok := a.views[vt].(footerRenderer)
			if !ok {
				t.Fatalf("view %q is migrated but is a %T, which renders no footer", vt, a.views[vt])
			}
			if msg := footerGlyphProblem(fr.RenderFooter(120), state); msg != "" {
				t.Errorf("view %q: %s (A-16 criterion 5)\n%s", vt, msg, ansi.Strip(fr.RenderFooter(120)))
			}
		})
	}
}

// The footer logic, on fictitious data.
func TestActivityGuardFooterLogic(t *testing.T) {
	seed := func(*testing.T, *App) indicator.State { return indicator.Running }
	const v command.ViewType = "fiction"
	mig := map[command.ViewType][]string{v: {"x"}}

	if got := footerSeedProblems(mig, nil); len(got) != 1 || !strings.Contains(got[0], `"fiction"`) {
		t.Errorf("a migrated view without a seed: %v", got)
	}
	if got := footerSeedProblems(nil, map[command.ViewType]func(*testing.T, *App) indicator.State{v: seed}); len(got) != 1 {
		t.Errorf("a seed without a migrated view: %v", got)
	}
	if got := footerSeedProblems(mig, map[command.ViewType]func(*testing.T, *App) indicator.State{v: seed}); len(got) != 0 {
		t.Errorf("a seeded migrated view: %v", got)
	}

	run := indicator.Glyph(indicator.Running, true)
	if msg := footerGlyphProblem("  Scanning "+run+" a", indicator.Running); msg != "" {
		t.Errorf("a footer with the glyph: %s", msg)
	}
	if msg := footerGlyphProblem("  1 job running", indicator.Running); msg == "" {
		t.Error("a footer without the glyph passed")
	}
	if msg := footerGlyphProblem("anything", indicator.Idle); msg == "" {
		t.Error("an idle seed passed: it has no glyph to look for")
	}
}
