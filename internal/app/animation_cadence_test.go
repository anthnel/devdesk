package app

import (
	"go/ast"
	"go/parser"
	"go/token"
	"testing"
	"time"

	"github.com/anthnel/devdesk/internal/jobs"
)

// DEVDESK-341 step 5 (red): the router no longer animates a braille spinner, so
// the 10 Hz chain is gone. While jobs run the chain ticks at the blink interval
// (500 ms), under app.motion: static as well, so the durations the views show
// keep refreshing. The literal is on purpose: the test must not follow a change
// of the animationBlinkInterval variable.
const stepFiveInterval = 500 * time.Millisecond

func TestARunningJobChainsAtTheBlinkInterval(t *testing.T) {
	for _, static := range []bool{false, true} {
		name := "animated"
		if static {
			name = "static"
		}
		t.Run(name, func(t *testing.T) {
			a, _, got := blinkRouter(t, false) // the view does not blink: only the job asks for ticks
			a.setMotionStatic(static)

			a.Update(jobs.StartMsg{Run: scanRun("default", "a")})

			if len(*got) == 0 {
				t.Fatal("a running job started no animation chain")
			}
			for ms := int64(1000); ms <= 3000; ms += 500 {
				if cmd := tickAt(a, ms); cmd == nil {
					t.Fatalf("tick at %d ms did not renew the chain while a job runs", ms)
				}
			}
			for i, d := range *got {
				if d != stepFiveInterval {
					t.Errorf("chain request %d: interval = %v, want %v (no 100 ms chain any more)", i, d, stepFiveInterval)
				}
			}
			if a.anim.interval != stepFiveInterval {
				t.Errorf("live chain interval = %v, want %v", a.anim.interval, stepFiveInterval)
			}
		})
	}
}

// A blinking view and a running job together are still one 500 ms chain.
func TestARunningJobAndABlinkingViewShareOneSlowChain(t *testing.T) {
	a, _, got := blinkRouter(t, true)
	startBlinkChain(t, a)
	seq := a.anim.seq

	a.Update(jobs.StartMsg{Run: scanRun("default", "a")})

	if a.anim.seq != seq {
		t.Errorf("seq = %d, want %d: starting a job must not replace a 500 ms chain", a.anim.seq, seq)
	}
	for i, d := range *got {
		if d != stepFiveInterval {
			t.Errorf("chain request %d: interval = %v, want %v", i, d, stepFiveInterval)
		}
	}
}

// --- Guard: the frame fields are gone from the non-test sources ------------

// frameFieldUses reports a FrameMsg declaration holding a Frame field and every
// FrameMsg literal that sets Frame, plus any RenderedFrame identifier.
func frameFieldUses(fset *token.FileSet, file *ast.File) []finding {
	out := renderedFrameUses(fset, file)
	isFrameMsg := func(e ast.Expr) bool {
		switch x := e.(type) {
		case *ast.Ident:
			return x.Name == "FrameMsg"
		case *ast.SelectorExpr:
			return x.Sel.Name == "FrameMsg"
		}
		return false
	}
	ast.Inspect(file, func(n ast.Node) bool {
		switch x := n.(type) {
		case *ast.TypeSpec:
			st, ok := x.Type.(*ast.StructType)
			if !ok || x.Name.Name != "FrameMsg" {
				return true
			}
			for _, f := range st.Fields.List {
				for _, id := range f.Names {
					if id.Name == "Frame" {
						out = append(out, finding{Pos: fset.Position(id.Pos()), What: "FrameMsg.Frame field"})
					}
				}
			}
		case *ast.CompositeLit:
			if !isFrameMsg(x.Type) {
				return true
			}
			for _, el := range x.Elts {
				kv, ok := el.(*ast.KeyValueExpr)
				if !ok {
					continue
				}
				if id, ok := kv.Key.(*ast.Ident); ok && id.Name == "Frame" {
					out = append(out, finding{Pos: fset.Position(id.Pos()), What: "FrameMsg literal sets Frame"})
				}
			}
		}
		return true
	})
	return out
}

func TestActivityGuardFrameFieldLogic(t *testing.T) {
	tests := []struct {
		name, body string
		want       int
	}{
		{"declared field", "type FrameMsg struct{ Frame string; Blink bool }", 1},
		{"blink only", "type FrameMsg struct{ Blink bool }", 0},
		{"literal key", "var x = jobs.FrameMsg{Frame: \"x\", Blink: true}", 1},
		{"local literal key", "var x = FrameMsg{Frame: \"x\"}", 1},
		{"another type's Frame", "var x = Other{Frame: \"x\"}", 0},
		{"rendered frame", "var x = T{RenderedFrame: \"x\"}", 1},
	}
	for _, tt := range tests {
		t.Run(tt.name, func(t *testing.T) {
			fset := token.NewFileSet()
			file, err := parser.ParseFile(fset, "v.go", "package v\n"+tt.body, 0)
			if err != nil {
				t.Fatal(err)
			}
			if got := frameFieldUses(fset, file); len(got) != tt.want {
				t.Errorf("findings = %v, want %d", got, tt.want)
			}
		})
	}
}

// DEVDESK-341 step 5: extends TestActivityGuardNoRenderedFrameUnderUI to the whole
// of internal/ (internal/jobs and internal/app included). Red until the fields
// and their producer (internal/app/animation.go frameMsg) are removed.
func TestActivityGuardNoFrameFieldAnywhere(t *testing.T) {
	scanned := map[string]bool{}
	sourceFiles(t, func(rel string, fset *token.FileSet, file *ast.File) {
		for _, prefix := range []string{"jobs/", "app/", "ui/"} {
			if len(rel) >= len(prefix) && rel[:len(prefix)] == prefix {
				scanned[prefix] = true
			}
		}
		for _, f := range frameFieldUses(fset, file) {
			t.Errorf("%s: %s (DEVDESK-341: jobs.FrameMsg carries the blink phase only)", f.Pos, f.What)
		}
	})
	for _, p := range []string{"jobs/", "app/", "ui/"} {
		if !scanned[p] {
			t.Errorf("no source file scanned under internal/%s: the scan reads nothing", p)
		}
	}
}
