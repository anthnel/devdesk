package app

import (
	"go/ast"
	"go/parser"
	"go/token"
	"io"
	"io/fs"
	"path/filepath"
	"sort"
	"strings"
	"testing"
	"time"

	tea "github.com/charmbracelet/bubbletea"

	"github.com/anthnel/devdesk/internal/command"
	"github.com/anthnel/devdesk/internal/jobs"
	"github.com/anthnel/devdesk/internal/ui/testutil"
)

// sourceFiles parses every non-test Go file under ../ (internal/) and calls visit
// with its path relative to internal/, slash-separated.
func sourceFiles(t *testing.T, visit func(rel string, fset *token.FileSet, file *ast.File)) {
	t.Helper()
	root := ".."
	err := filepath.WalkDir(root, func(path string, d fs.DirEntry, err error) error {
		if err != nil {
			return err
		}
		if d.IsDir() || !strings.HasSuffix(path, ".go") || strings.HasSuffix(path, "_test.go") {
			return nil
		}
		fset := token.NewFileSet()
		file, perr := parser.ParseFile(fset, path, nil, 0)
		if perr != nil {
			t.Fatalf("parse %s: %v", path, perr)
		}
		rel, _ := filepath.Rel(root, path)
		visit(filepath.ToSlash(rel), fset, file)
		return nil
	})
	if err != nil {
		t.Fatalf("walk %s: %v", root, err)
	}
}

// Criterion 4: the frame a view reads comes from the router and from nowhere else.
func TestOnlyTheRouterBuildsAFrameMsg(t *testing.T) {
	sourceFiles(t, func(rel string, fset *token.FileSet, file *ast.File) {
		if strings.HasPrefix(rel, "app/") {
			return
		}
		ast.Inspect(file, func(n ast.Node) bool {
			lit, ok := n.(*ast.CompositeLit)
			if !ok {
				return true
			}
			sel, ok := lit.Type.(*ast.SelectorExpr)
			if ok && sel.Sel.Name == "FrameMsg" {
				t.Errorf("%s: only internal/app builds a jobs.FrameMsg", fset.Position(lit.Pos()))
			}
			return true
		})
	})
}

// Criterion 4: the router starts its animation chain in one place.
func TestTheRouterHasOneAnimationChain(t *testing.T) {
	builders := map[string][]string{} // function -> positions
	callers := map[string][]string{}
	sourceFiles(t, func(rel string, fset *token.FileSet, file *ast.File) {
		if !strings.HasPrefix(rel, "app/") || strings.Count(rel, "/") != 1 {
			return
		}
		for _, decl := range file.Decls {
			fn, ok := decl.(*ast.FuncDecl)
			if !ok || fn.Body == nil {
				continue
			}
			ast.Inspect(fn.Body, func(n ast.Node) bool {
				switch x := n.(type) {
				case *ast.CompositeLit:
					if id, ok := x.Type.(*ast.Ident); ok && id.Name == "animationTickMsg" {
						builders[fn.Name.Name] = append(builders[fn.Name.Name], fset.Position(x.Pos()).String())
					}
				case *ast.CallExpr:
					if id, ok := x.Fun.(*ast.Ident); ok && id.Name == "animationTick" {
						callers[fn.Name.Name] = append(callers[fn.Name.Name], fset.Position(x.Pos()).String())
					}
				}
				return true
			})
		}
	})
	for fn, at := range builders {
		if fn != "animationTick" {
			t.Errorf("animationTickMsg is built in %s (%v); only animationTick may", fn, at)
		}
	}
	if len(builders["animationTick"]) != 1 {
		t.Errorf("animationTick builds %d animationTickMsg, want exactly 1", len(builders["animationTick"]))
	}
	for fn, at := range callers {
		if fn != "ensureTick" && fn != "handleAnimationTick" {
			t.Errorf("animationTick is called from %s (%v); only ensureTick starts a chain and handleAnimationTick renews it", fn, at)
		}
	}
}

// rowChain is a declared row-animation chain: the story that removes it and
// what it is.
type rowChain struct{ Story, What string }

// declaredRowChains are the row-animation chains that still live outside the
// router (DEVDESK-139, Q2 = A). The list may only shrink: each entry names the
// story that removes it, and a stale entry fails the tests below.
var declaredRowChains = map[string]rowChain{
	"ui/oci_resources/busy.go":         {"A-09b", "AdvanceSpinner on the three OCI tables"},
	"ui/containers/update.go":          {"A-14", "AdvanceSpinner on the containers table"},
	"ui/oci_resources/browser_keys.go": {"A-09a2", "tagScanSpinnerIdx"},
}

// maxDeclaredRowChains is the ceiling (DEVDESK-143): raising it takes a visible
// diff. Lower it with every migration.
const maxDeclaredRowChains = 3

// chainProblems says what is wrong with the declared chains, in file order:
// no story, or a story whose tables have all left pendingTables (the chain
// should have gone with the migration).
func chainProblems(chains map[string]rowChain, pending []pendingTable) []string {
	var problems []string
	files := make([]string, 0, len(chains))
	for f := range chains {
		files = append(files, f)
	}
	sort.Strings(files)
	for _, f := range files {
		c := chains[f]
		if c.Story == "" || c.What == "" {
			problems = append(problems, f+": declared row animation chain names no story or no description")
			continue
		}
		still := false
		for _, p := range pending {
			still = still || p.Story == c.Story
		}
		if !still {
			problems = append(problems, f+": declared for story "+c.Story+", which has no table left in pendingTables: remove the chain ("+c.What+")")
		}
	}
	return problems
}

func TestDeclaredRowChainsFollowTheirStories(t *testing.T) {
	if got := len(declaredRowChains); got > maxDeclaredRowChains {
		t.Errorf("declaredRowChains has %d entries, ceiling is %d: the list may only shrink", got, maxDeclaredRowChains)
	}
	for _, p := range chainProblems(declaredRowChains, pendingTables) {
		t.Error(p)
	}
}

func TestDeclaredRowChainsLogic(t *testing.T) {
	pending := []pendingTable{{"netdiag/ports", command.ViewNetdiag, "A-12"}}
	tests := []struct {
		name   string
		chains map[string]rowChain
		want   []string
	}{
		{"story still pending", map[string]rowChain{"ui/netdiag/ports_model.go": {"A-12", "portsSpinnerCmd"}}, nil},
		{"story migrated", map[string]rowChain{"ui/netdiag/ports_model.go": {"A-13", "portsSpinnerCmd"}}, []string{"ui/netdiag/ports_model.go"}},
		{"no story", map[string]rowChain{"ui/x.go": {"", "what"}}, []string{"ui/x.go"}},
		{"only the stale one is named", map[string]rowChain{"ui/a.go": {"A-12", "a"}, "ui/b.go": {"A-99", "b"}}, []string{"ui/b.go"}},
	}
	for _, tt := range tests {
		t.Run(tt.name, func(t *testing.T) {
			got := chainProblems(tt.chains, pending)
			if len(got) != len(tt.want) {
				t.Fatalf("problems = %v, want %d", got, len(tt.want))
			}
			for i, w := range tt.want {
				if !strings.HasPrefix(got[i], w) {
					t.Errorf("problem %d = %q, want it to name %s", i, got[i], w)
				}
			}
		})
	}
}

func TestRowAnimationChainsOutsideTheRouterAreDeclared(t *testing.T) {
	found := map[string]bool{}
	sourceFiles(t, func(rel string, fset *token.FileSet, file *ast.File) {
		if strings.HasPrefix(rel, "ui/datatable/") {
			return
		}
		ast.Inspect(file, func(n ast.Node) bool {
			hit := false
			switch x := n.(type) {
			case *ast.CallExpr:
				switch f := x.Fun.(type) {
				case *ast.SelectorExpr:
					hit = f.Sel.Name == "AdvanceSpinner"
				case *ast.Ident:
					hit = f.Name == "portsSpinnerCmd"
				}
			case *ast.IncDecStmt:
				hit = mentionsIdent(x.X, "tagScanSpinnerIdx")
			case *ast.AssignStmt:
				for _, l := range x.Lhs {
					hit = hit || mentionsIdent(l, "tagScanSpinnerIdx")
				}
			}
			if hit {
				found[rel] = true
				if _, ok := declaredRowChains[rel]; !ok {
					t.Errorf("%s: a row animation chain outside the router; the router's tick animates rows (DEVDESK-139)", fset.Position(n.Pos()))
				}
			}
			return true
		})
	})
	var stale []string
	for rel := range declaredRowChains {
		if !found[rel] {
			stale = append(stale, rel)
		}
	}
	sort.Strings(stale)
	for _, rel := range stale {
		t.Errorf("%s is declared as a row animation chain but has none: remove the exception", rel)
	}
}

func mentionsIdent(e ast.Expr, name string) bool {
	switch x := e.(type) {
	case *ast.Ident:
		return x.Name == name
	case *ast.SelectorExpr:
		return x.Sel.Name == name
	}
	return false
}

// programModel wraps the router so the program does not run App.Init (keyring,
// MCP server, port forwards): the chain is what is under test, not startup.
type programModel struct{ app *App }

func (m programModel) Init() tea.Cmd { return nil }
func (m programModel) View() string  { return m.app.View() }
func (m programModel) Update(msg tea.Msg) (tea.Model, tea.Cmd) {
	_, cmd := m.app.Update(msg)
	return m, cmd
}

// Criterion 6: the chain under a real tea.Program, with real Cmds on their own
// goroutines. Its value is under -race.
func TestTheAnimationChainUnderARealProgram(t *testing.T) {
	testutil.FastTimers(t, &animationFastInterval)
	testutil.FastTimers(t, &animationBlinkInterval)

	v := &blinkView{wants: false}
	v.onKeyWow = func(v *blinkView) { v.wants = !v.wants }
	a := router(t, v)
	p := tea.NewProgram(programModel{app: a},
		tea.WithInput(nil), tea.WithOutput(io.Discard), tea.WithoutSignalHandler())

	go func() {
		p.Send(jobs.StartMsg{Run: scanRun("default", "a")})
		time.Sleep(60 * time.Millisecond)
		p.Send(tea.KeyMsg{Type: tea.KeyDown}) // wants = true
		time.Sleep(60 * time.Millisecond)
		p.Send(jobs.CancelMsg{ID: 1}) // the run ends; the blink keeps the chain
		time.Sleep(60 * time.Millisecond)
		p.Send(tea.KeyMsg{Type: tea.KeyDown}) // wants = false
		time.Sleep(120 * time.Millisecond)
		p.Quit()
	}()

	done := make(chan error, 1)
	go func() { _, err := p.Run(); done <- err }()
	select {
	case err := <-done:
		if err != nil {
			t.Fatalf("program: %v", err)
		}
	case <-time.After(5 * time.Second):
		t.Fatal("the program did not exit")
	}

	if a.jobs.Running() != 0 {
		t.Fatalf("the run never ended: Running() = %d", a.jobs.Running())
	}
	if a.anim.ticking {
		t.Error("the chain was still alive after nothing needed it")
	}
	if !a.anim.blinkOn {
		t.Error("the chain stopped on the off phase")
	}
	if len(framesSeen(&v.fakeView)) < 3 {
		t.Errorf("the view got %d frames, want several", len(framesSeen(&v.fakeView)))
	}
}
