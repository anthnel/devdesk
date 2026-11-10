package app

import (
	"go/ast"
	"go/parser"
	"go/token"
	"io"
	"io/fs"
	"path/filepath"
	"strings"
	"testing"
	"time"

	tea "charm.land/bubbletea/v2"

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

// chainHit is one of the row-animation chains the router's one tick replaced
// (DEVDESK-139): a call to AdvanceSpinner or portsSpinnerCmd, and any write to
// tagScanSpinnerIdx. None may exist outside the router and internal/ui/datatable.
type chainHit struct {
	Pos  token.Position
	What string
}

// rowAnimationChains lists every retired row-animation chain in file.
func rowAnimationChains(fset *token.FileSet, file *ast.File) []chainHit {
	var out []chainHit
	ast.Inspect(file, func(n ast.Node) bool {
		what := ""
		switch x := n.(type) {
		case *ast.CallExpr:
			switch f := x.Fun.(type) {
			case *ast.SelectorExpr:
				if f.Sel.Name == "AdvanceSpinner" {
					what = "AdvanceSpinner"
				}
			case *ast.Ident:
				if f.Name == "portsSpinnerCmd" {
					what = "portsSpinnerCmd"
				}
			}
		case *ast.IncDecStmt:
			if mentionsIdent(x.X, "tagScanSpinnerIdx") {
				what = "tagScanSpinnerIdx"
			}
		case *ast.AssignStmt:
			for _, l := range x.Lhs {
				if mentionsIdent(l, "tagScanSpinnerIdx") {
					what = "tagScanSpinnerIdx"
				}
			}
		}
		if what != "" {
			out = append(out, chainHit{Pos: fset.Position(n.Pos()), What: what})
		}
		return true
	})
	return out
}

func TestNoRowAnimationChainOutsideTheRouter(t *testing.T) {
	sourceFiles(t, func(rel string, fset *token.FileSet, file *ast.File) {
		if strings.HasPrefix(rel, "ui/datatable/") {
			return
		}
		for _, h := range rowAnimationChains(fset, file) {
			t.Errorf("%s: %s is a row animation chain outside the router; the router's tick animates rows (DEVDESK-139)", h.Pos, h.What)
		}
	})
}

// The detection itself, on sources held in strings.
func TestNoRowAnimationChainLogic(t *testing.T) {
	tests := []struct {
		name string
		body string
		want int
	}{
		{"tag scan index incremented", "func f(m *M) { m.tagScanSpinnerIdx++ }", 1},
		{"tag scan index assigned", "func f(m *M) { m.tagScanSpinnerIdx = 0 }", 1},
		{"AdvanceSpinner", "func f(t T) { t.AdvanceSpinner() }", 1},
		{"portsSpinnerCmd", "func f() { _ = portsSpinnerCmd() }", 1},
		{"clean", "func f(m *M) { _ = m.tagScanSpinnerIdx }", 0},
	}
	for _, tt := range tests {
		t.Run(tt.name, func(t *testing.T) {
			fset := token.NewFileSet()
			file, err := parser.ParseFile(fset, "v.go", "package v\n"+tt.body, 0)
			if err != nil {
				t.Fatal(err)
			}
			got := rowAnimationChains(fset, file)
			if len(got) != tt.want {
				t.Fatalf("hits = %v, want %d", got, tt.want)
			}
			if tt.want > 0 && got[0].Pos.Line != 2 {
				t.Errorf("hit at %v, want line 2", got[0].Pos)
			}
		})
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

func (m programModel) Init() tea.Cmd  { return nil }
func (m programModel) View() tea.View { return m.app.View() }
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
		p.Send(testutil.Key("down")) // wants = true
		time.Sleep(60 * time.Millisecond)
		p.Send(jobs.CancelMsg{ID: 1}) // the run ends; the blink keeps the chain
		time.Sleep(60 * time.Millisecond)
		p.Send(testutil.Key("down")) // wants = false
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
