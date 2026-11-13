package app

import (
	"fmt"
	"go/ast"
	"go/parser"
	"go/token"
	"path/filepath"
	"sort"
	"strconv"
	"strings"
	"testing"

	tea "charm.land/bubbletea/v2"

	"github.com/charmbracelet/x/ansi"

	"github.com/anthnel/devdesk/internal/command"
	"github.com/anthnel/devdesk/internal/jobs"
	"github.com/anthnel/devdesk/internal/ports"
	"github.com/anthnel/devdesk/internal/template"
	"github.com/anthnel/devdesk/internal/ui/components"
	"github.com/anthnel/devdesk/internal/ui/datatable"
	"github.com/anthnel/devdesk/internal/ui/help"
	"github.com/anthnel/devdesk/internal/ui/indicator"
	"github.com/anthnel/devdesk/internal/ui/keymap"
	"github.com/anthnel/devdesk/internal/ui/netdiag"
	"github.com/anthnel/devdesk/internal/ui/testutil"
)

// A-16 (DEVDESK-143), the cross-cutting guard, started with the :jobs pilot
// (DEVDESK-142) and closed by its last story: no view is waiting any more.
//
// The guard answers one question for every view the router can build: is it on
// the activity column, or is it exempt for a stated reason? A view that is
// neither fails the test by name. For a migrated view it also checks the column,
// the help legend, the retired mechanisms in its declared sources (any tick
// included), and the footer of its selected row. There is no list of views
// waiting for their migration: exemptViews holds permanent, motivated exceptions.

// activityView is what a migrated view exposes to the guard: read-only tables,
// keyed "<view>/<table>".
type activityView interface {
	ActivityTables() map[string]datatable.ActivityTable
}

// migratedTables are the tables already on the activity column, by view.
var migratedTables = map[command.ViewType][]string{
	command.ViewJobs:         {"jobs/runs", "jobs/targets"},
	command.ViewWorkspaces:   {"ws"},
	command.ViewSecurity:     {"sec/inventory"},
	command.ViewOCIResources: {"oci/images", "oci/networks", "oci/volumes", "oci/registries", "oci/registry-browser", "oci/registry-search"},
	command.ViewGitExplorer:  {"explorer/tree", "explorer/clone-list"},
	command.ViewTemplates:    {"templates"},
	command.ViewNetdiag:      {"netdiag/ports"},
	command.ViewAgentUsage:   {"agent-usage/agents"},
	command.ViewContainers:   {"containers"},
}

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

// builtView returns the view the router builds for vt, laid out at 60x30.
func builtView(t *testing.T, vt command.ViewType) tea.Model {
	t.Helper()
	a := routerAt(t, &bareView{}, 60, 30) // isolateHome, session-only secrets
	a.createView(vt)
	var m tea.Model = a.views[vt] // Update returns a tea.Model, not a Screen
	if m == nil {
		t.Fatalf("view %q: the router did not build it", vt)
	}
	m, _ = m.Update(tea.WindowSizeMsg{Width: 60, Height: 30})
	return m
}

// classificationProblems says what is wrong with the classification of one
// view, empty when it is fine. A view is migrated (it has tables on the activity
// column) or exempt (with its reason), never both and never neither.
func classificationProblems(vt command.ViewType, migrated map[command.ViewType][]string, exempt map[command.ViewType]string) []string {
	var problems []string
	_, isExempt := exempt[vt]
	isMigrated := len(migrated[vt]) > 0
	switch {
	case !isMigrated && !isExempt:
		problems = append(problems, "neither migrated nor exempt (with its reason)")
	case isMigrated && isExempt:
		problems = append(problems, "both migrated and exempt")
	}
	return problems
}

// Criterion 6: every view the router builds is migrated or exempt.
func TestActivityGuardEveryViewIsClassified(t *testing.T) {
	for _, name := range command.AllViewNames() {
		vt := command.ViewType(name)
		t.Run(name, func(t *testing.T) {
			for _, p := range classificationProblems(vt, migratedTables, exemptViews) {
				t.Errorf("view %q is %s: classify it in activity_guard_test.go", vt, p)
			}
		})
	}
}

// The logic itself, on a fictitious view.
func TestActivityGuardClassificationLogic(t *testing.T) {
	const v command.ViewType = "fiction"
	tests := []struct {
		name     string
		migrated map[command.ViewType][]string
		exempt   map[command.ViewType]string
		problems int
	}{
		{"migrated is fine", map[command.ViewType][]string{v: {"fiction/a"}}, nil, 0},
		{"exempt is fine", nil, map[command.ViewType]string{v: "why"}, 0},
		{"unclassified fails", nil, nil, 1},
		{"migrated and exempt fails", map[command.ViewType][]string{v: {"fiction/a"}}, map[command.ViewType]string{v: "why"}, 1},
	}
	for _, tt := range tests {
		t.Run(tt.name, func(t *testing.T) {
			if got := classificationProblems(v, tt.migrated, tt.exempt); len(got) != tt.problems {
				t.Errorf("problems = %v, want %d", got, tt.problems)
			}
		})
	}
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

// Criterion 6, other direction: an exempt view must report no activity table.
func TestActivityGuardExemptViewsReportNoTable(t *testing.T) {
	for vt := range exemptViews {
		t.Run(string(vt), func(t *testing.T) {
			if _, ok := builtView(t, vt).(activityView); ok {
				t.Errorf("view %q exposes ActivityTables() but is exempt: move it out of exemptViews", vt)
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
// retired mechanisms (criterion 2a/2b, and the hard-coded glyphs of 3). A prefix may be a file so a view
// that migrates in two steps (OCI A-09a/A-09a2/A-09b, explorer A-10a/b) can be scanned
// file by file.
var migratedSources = map[command.ViewType][]string{
	command.ViewJobs:       {"ui/jobsview/"},
	command.ViewWorkspaces: {"ui/workspaces/"},
	command.ViewSecurity:   {"ui/security/"},
	// OCI migrates in steps (A-09a images, A-09a2 registry browser, A-09b the rest):
	// only the files that belong to the migrated tables are scanned. Excluded:
	// commands.go (the refresh tea.Tick). The registry browser (A-09a2) is in.
	command.ViewOCIResources: {
		"ui/oci_resources/activity.go", "ui/oci_resources/table.go", "ui/oci_resources/jobs.go",
		"ui/oci_resources/images.go", "ui/oci_resources/results.go", "ui/oci_resources/view.go",
		"ui/oci_resources/model.go", "ui/oci_resources/busy.go", "ui/oci_resources/registries.go",
		"ui/oci_resources/update.go", "ui/oci_resources/keys.go",
		"ui/oci_resources/registry_browser.go", "ui/oci_resources/browser_keys.go",
		"ui/oci_resources/browser_view.go", "ui/oci_resources/browser_state.go",
		"ui/oci_resources/browser_bridge.go", "ui/oci_resources/browser_tags.go",
		"ui/oci_resources/browser_flow.go", "ui/oci_resources/browser_images.go",
		"ui/oci_resources/browser_form.go", "ui/oci_resources/browser_images_order.go",
	},
	// The explorer is fully migrated (A-10a the tree, A-10b the clone list):
	// the whole package is scanned.
	command.ViewGitExplorer: {"ui/forge/explorer/"},
	command.ViewTemplates:   {"ui/templates/"},
	// Netdiag is migrated except forward_model.go, whose ticker is a data
	// refresh, and ports_commands.go (portsTickCmd is the data refresh tick, not
	// a row animation). Diagnostics and Interfaces loads are the shared dot
	// (DEVDESK-373).
	command.ViewNetdiag: {
		"ui/netdiag/model.go", "ui/netdiag/update.go", "ui/netdiag/run.go", "ui/netdiag/header.go",
		"ui/netdiag/interfaces_model.go", "ui/netdiag/ports_model.go", "ui/netdiag/ports_activity.go",
	},
	// The agent-usage view is fully migrated (A-13, DEVDESK-151): the whole package.
	command.ViewAgentUsage: {"ui/agentusageview/"},
	// Containers is fully migrated (A-14, DEVDESK-152) except commands.go, whose
	// tea.Tick is the 2 s data refresh, and image_updates.go, ports.go and
	// sources.go, which carry no row state.
	command.ViewContainers: {"ui/containers/activity.go", "ui/containers/model.go", "ui/containers/update.go", "ui/containers/view.go"},
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
		if err != nil || path != "charm.land/bubbletea/v2" {
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
// imports it): the router's one tick animates every row.
// rel is the file's path relative to internal/; it keys declaredGlyphConstants.
// RenderedFrame is retired too (DEVDESK-341): no footer takes the router's
// styled braille frame any more.
func retiredMechanisms(fset *token.FileSet, file *ast.File, rel string) []finding {
	var out []finding
	report := func(pos token.Pos, what string) {
		out = append(out, finding{Pos: fset.Position(pos), What: what})
	}
	tea := bubbleTeaName(file)
	selectors := map[*ast.Ident]bool{} // identifiers seen as the Sel of a selector
	// reportTick flags any reference to bubbletea's Tick / Every, called or not
	// (`var tick = tea.Tick` is as much a tick as `tea.Tick(...)`).
	reportTick := func(pos token.Pos, name string) {
		if name == "Tick" || name == "Every" {
			report(pos, "reference to bubbletea's "+name+" (a view's rows are animated by the router's tick)")
		}
	}
	isFrame := func(name string) bool {
		return name == "Frame" || name == "frame" || name == "SpinnerFrame" || name == renderedFrameName
	}
	visit := func() func(ast.Node) bool {
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
					reportTick(x.Pos(), x.Name)
				}
			case *ast.SelectorExpr:
				selectors[x.Sel] = true
				if pkg, ok := x.X.(*ast.Ident); ok && tea != "" && tea != "." && pkg.Name == tea {
					reportTick(x.Pos(), x.Sel.Name)
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
		if gd, ok := decl.(*ast.GenDecl); ok && gd.Tok == token.CONST {
			for _, spec := range gd.Specs {
				if isDeclaredGlyphConstant(rel, spec) {
					continue
				}
				ast.Inspect(spec, visit())
			}
			continue
		}
		ast.Inspect(decl, visit())
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
	const head = "package v\nimport tea \"charm.land/bubbletea/v2\"\nimport \"charm.land/bubbles/v2/spinner\"\n"
	const (
		bt    = "package v\nimport bt \"charm.land/bubbletea/v2\"\nimport \"charm.land/bubbles/v2/spinner\"\n"
		dot   = "package v\nimport . \"charm.land/bubbletea/v2\"\nimport \"charm.land/bubbles/v2/spinner\"\n"
		other = "package v\nimport tea \"example.com/notbubbletea\"\nimport \"charm.land/bubbles/v2/spinner\"\n"
	)
	tests := []struct {
		name string
		head string // import block overriding head; it must keep the same 3 lines
		body string
		want int
		at   string // "line:column" of the first finding, when want > 0
	}{
		{"tea.Tick", "", "func f() { _ = tea.Tick(0, nil) }", 1, "4:16"},
		{"tea.Every", "", "func f() { _ = tea.Every(0, nil) }", 1, "4:16"},
		{"spinner.Tick is not a view tick", "", "func f() { _ = spinner.Tick }\nfunc g() { _ = spinner.Tick() }", 0, ""},
		{"tick in any function", "", "func g() { _ = tea.Tick(0, nil) }", 1, "4:16"},
		{"AdvanceSpinner", "", "func f(t T) { t.AdvanceSpinner() }", 1, "4:15"},
		{"glyph literal", "", "var g = \"\u25cf\"", 1, "4:9"},
		{"StatusColumn key", "", "var x = T{StatusColumn: 1}", 1, "4:11"},
		{"clean", "", "func f() {}", 0, ""},
		{"RenderedFrame selector", "", "func f(m M) { _ = m.RenderedFrame }", 1, "4:21"},
		{"RenderedFrame key", "", "var x = T{RenderedFrame: \"\"}", 1, "4:11"},
		{"a name that merely starts with RenderedFrame", "", "func f(m M) { _ = m.RenderedFrameCount }", 0, ""},
		{"aliased import", bt, "func f() { _ = bt.Tick(0, nil) }", 1, "4:16"},
		{"dot import", dot, "func f() { _ = Tick(0, nil) }", 1, "4:16"},
		{"dot import, bare reference", dot, "var tick = Every", 1, "4:12"},
		{"package variable holding tea.Tick", "", "var tick = tea.Tick", 1, "4:12"},
		{"tick in a method", "", "func (m M) f() { _ = tea.Tick(0, nil) }", 1, "4:22"},
		{"non-bubbletea package named tea", other, "func f() { _ = tea.Tick(0, nil) }", 0, ""},
		{"dot import does not flag a method named Tick", dot, "func f(t T) { t.Tick() }", 0, ""},
	}
	for _, tt := range tests {
		t.Run(tt.name, func(t *testing.T) {
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
	command.ViewJobs:         seedJobsFooter,
	command.ViewWorkspaces:   seedWorkspacesFooter,
	command.ViewSecurity:     seedSecurityFooter,
	command.ViewOCIResources: seedOCIFooter,
	command.ViewGitExplorer:  seedExplorerFooter,
	command.ViewTemplates:    seedTemplatesFooter,
	command.ViewNetdiag:      seedNetdiagFooter,
	command.ViewAgentUsage:   seedAgentUsageFooter,
	command.ViewContainers:   seedContainersFooter,
}

// seedOCIFooter puts a scan of the first Images row (nginx:1.27, the selected
// one) in the running state and opens the OCI view on it.
func seedOCIFooter(t *testing.T, a *App) indicator.State {
	t.Helper()
	t.Setenv("PATH", t.TempDir()) // no docker: a reload command must stay inert
	const image = "nginx:1.27"
	a.Update(jobs.StartMsg{Run: jobs.NewRun(jobs.KindScan, command.ViewOCIResources, "", "images", image)})
	snap := a.jobs.Snapshot()
	if len(snap) != 1 || !a.jobs.Advance(snap[0].ID, image, jobs.ItemRunning, "") {
		t.Fatalf("the seed could not put the scan in the running state: %+v", snap)
	}
	openOCI(t, a)
	return indicator.Running
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

// seedNetdiagFooter puts the Ports tab on screen with a kill in flight on its
// selected row. It calls the view's Update directly and never runs the Cmds it
// returns: the PID does not exist (above the kernel's pid_max), so even a Cmd
// that slipped through could not signal a real process.
func seedNetdiagFooter(t *testing.T, a *App) indicator.State {
	t.Helper()
	a.createView(command.ViewNetdiag)
	var m tea.Model = a.views[command.ViewNetdiag]
	for _, msg := range []tea.Msg{
		tea.WindowSizeMsg{Width: a.width, Height: a.height},
		testutil.Key("tab"), // the Ports tab
		netdiag.NewPortsDataMsg([]ports.Socket{{
			Protocol: "tcp", State: "LISTEN", LocalAddr: "127.0.0.1:9", PID: "4194305", Process: "ghost",
		}}),
		testutil.Key(keymap.Kill),
		components.ConfirmModalYesMsg{},
	} {
		m, _ = m.Update(msg)
	}
	a.setView(command.ViewNetdiag, m)
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
	// Exactly one: the row's own dot is in the footer detail, and the footer's
	// activity dot is dropped when a detail has a glyph (DEVDESK-341, Q4).
	switch n := strings.Count(ansi.Strip(footer), glyph); {
	case n == 0:
		return fmt.Sprintf("the footer does not carry the glyph %q of state %v", glyph, state)
	case n > 1:
		return fmt.Sprintf("the footer carries the glyph %q of state %v %d times, want exactly one (a detail glyph and the activity dot together)", glyph, state, n)
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
			// The phase on, so a footer dot would show as the same glyph as the
			// row's own (a hollow one would hide a double dot).
			forceRouterFrame(a, vt)
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
	if msg := footerGlyphProblem("  "+run+" Scanning a "+run+" 1/3", indicator.Running); msg == "" {
		t.Error("a footer with two running glyphs passed")
	}
	if msg := footerGlyphProblem("anything", indicator.Idle); msg == "" {
		t.Error("an idle seed passed: it has no glyph to look for")
	}
}

// seedTemplatesFooter puts the templates view on screen with a sync running on
// its selected row (api), for the activity guard.
func seedTemplatesFooter(t *testing.T, a *App) indicator.State {
	t.Helper()
	path, err := template.DefaultPath() // under the isolated HOME
	if err != nil {
		t.Fatal(err)
	}
	store, err := template.Open(path)
	if err != nil {
		t.Fatal(err)
	}
	err = store.Put(template.Entry{
		Slug: "api", Name: "API",
		Source: template.Source{Kind: template.KindGit, URL: "https://git.example.com/acme/api.git", Ref: "main"},
	})
	if err != nil {
		t.Fatal(err)
	}
	a.Update(jobs.StartMsg{Run: jobs.NewRun(jobs.KindSync, command.ViewTemplates, "", "API", "api").WithDisplay("api", "API")})
	snap := a.jobs.Snapshot()
	if len(snap) != 1 || !a.jobs.Advance(snap[0].ID, "api", jobs.ItemRunning, "") {
		t.Fatalf("the seed could not put the sync in the running state: %+v", snap)
	}
	_ = a.switchView(command.ViewTemplates)
	for _, msg := range testutil.Msgs(a.views[command.ViewTemplates].Init()) {
		a.Update(msg)
	}
	a.resize(a.width, a.height)
	return indicator.Running
}

// seedAgentUsageFooter opens a live session at the agents level: its
// orchestrator row is running and selected. It drops the Cmd that waits for the
// 2 s poll, so nothing is ever read twice.
func seedAgentUsageFooter(t *testing.T, a *App) indicator.State {
	t.Helper()
	live, _ := openLiveAgentSession(t)
	a.views[command.ViewAgentUsage] = live.views[command.ViewAgentUsage]
	return indicator.Running
}

// DEVDESK-371..376: no view owns a braille spinner any more. Every loading state
// is the shared activity dot, driven by the router frame. No non-test file under
// internal/ imports bubbles/v2/spinner, dk setup included (DEVDESK-376,
// DEVDESK-377). There is no exception list: a reintroduction fails.
const bubblesSpinnerPath = "charm.land/bubbles/v2/spinner"

// importsBubblesSpinner reports whether file imports bubbles/v2/spinner, under
// any alias (a blank or dot import counts too).
func importsBubblesSpinner(file *ast.File) bool {
	for _, imp := range file.Imports {
		if path, err := strconv.Unquote(imp.Path.Value); err == nil && path == bubblesSpinnerPath {
			return true
		}
	}
	return false
}

// spinnerImportProblems reports every package found importing the spinner.
func spinnerImportProblems(found map[string]bool) []string {
	var out []string
	for dir := range found {
		out = append(out, fmt.Sprintf("%s imports %s: use the shared activity dot (Status.Activity, WantsBlink, jobs.FrameMsg); there is no exception", dir, bubblesSpinnerPath))
	}
	sort.Strings(out)
	return out
}

func TestNoViewImportsTheBubblesSpinnerLogic(t *testing.T) {
	parse := func(src string) *ast.File {
		f, err := parser.ParseFile(token.NewFileSet(), "v.go", src, parser.ImportsOnly)
		if err != nil {
			t.Fatal(err)
		}
		return f
	}
	if importsBubblesSpinner(parse("package v\nimport \"fmt\"\n")) {
		t.Error("a file with no spinner import was flagged")
	}
	for name, src := range map[string]string{
		"plain":   "package v\nimport \"" + bubblesSpinnerPath + "\"\n",
		"aliased": "package v\nimport sp \"" + bubblesSpinnerPath + "\"\n",
		"blank":   "package v\nimport _ \"" + bubblesSpinnerPath + "\"\n",
	} {
		if !importsBubblesSpinner(parse(src)) {
			t.Errorf("%s spinner import not detected", name)
		}
	}
	if p := spinnerImportProblems(map[string]bool{"ui/a": true, "x/b": true}); len(p) != 2 {
		t.Errorf("importers must each be reported: %v", p)
	}
	if p := spinnerImportProblems(map[string]bool{}); len(p) != 0 {
		t.Errorf("no importer reported %v", p)
	}
}

func TestNoViewImportsTheBubblesSpinner(t *testing.T) {
	found := map[string]bool{}
	sourceFiles(t, func(rel string, _ *token.FileSet, file *ast.File) {
		if importsBubblesSpinner(file) {
			found[filepath.ToSlash(filepath.Dir(rel))] = true
		}
	})
	for _, p := range spinnerImportProblems(found) {
		t.Error(p)
	}
}

// DEVDESK-377: the footer's own spinner API is gone. Status has no Spinner
// field, FooterMessage keeps no frame, and theme has no spinner style or
// message: the activity dot is the only loading indicator.
var removedSpinnerSymbols = map[string]bool{
	"Spinner":         true, // components.Status.Spinner (field, selector, literal key)
	"spinnerFrame":    true,
	"SetSpinnerFrame": true,
	"SpinnerMessage":  true,
	"SpinnerStyle":    true,
}

// removedSpinnerSymbolUses reports every identifier in file that names a
// removed symbol.
func removedSpinnerSymbolUses(fset *token.FileSet, file *ast.File) []string {
	var out []string
	ast.Inspect(file, func(n ast.Node) bool {
		if id, ok := n.(*ast.Ident); ok && removedSpinnerSymbols[id.Name] {
			out = append(out, fmt.Sprintf("%s: %s", fset.Position(id.Pos()), id.Name))
		}
		return true
	})
	return out
}

func TestRemovedSpinnerSymbolsStayGone(t *testing.T) {
	parse := func(src string) (*token.FileSet, *ast.File) {
		fset := token.NewFileSet()
		f, err := parser.ParseFile(fset, "v.go", src, 0)
		if err != nil {
			t.Fatal(err)
		}
		return fset, f
	}
	for name, src := range map[string]string{
		"field":    "package v\ntype S struct{ Spinner bool }\n",
		"selector": "package v\nfunc f(s S) bool { return s.Spinner }\n",
		"literal":  "package v\nvar s = S{Spinner: true}\n",
		"frame":    "package v\ntype F struct{ spinnerFrame string }\nfunc (f *F) SetSpinnerFrame(s string) {}\n",
		"theme":    "package v\nvar _ = SpinnerMessage\nvar _ = SpinnerStyle()\n",
	} {
		if len(removedSpinnerSymbolUses(parse(src))) == 0 {
			t.Errorf("%s: a removed symbol was not detected", name)
		}
	}
	if got := removedSpinnerSymbolUses(parse("package v\nvar Activity bool\n")); len(got) != 0 {
		t.Errorf("a clean file was flagged: %v", got)
	}

	scanned := 0
	sourceFiles(t, func(rel string, fset *token.FileSet, file *ast.File) {
		if !strings.HasPrefix(rel, "ui/") {
			return
		}
		scanned++
		for _, use := range removedSpinnerSymbolUses(fset, file) {
			t.Errorf("%s (internal/%s): the footer spinner API was removed in DEVDESK-377; use Status.Activity and the shared dot", use, rel)
		}
	})
	if scanned == 0 {
		t.Fatal("no file under internal/ui scanned")
	}
}
