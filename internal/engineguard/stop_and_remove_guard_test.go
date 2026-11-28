package engineguard_test

import (
	"fmt"
	"go/ast"
	"go/parser"
	"go/token"
	"os"
	"path/filepath"
	"slices"
	"strconv"
	"strings"
	"testing"
)

// OC-25 (DEVDESK-823), guards G3 and F/P for `Stop and delete`: the one composed
// destructive function, docker.StopAndRemoveContainer, adds no argv to the module. It is
// `StopContainer(id)` then `RemoveContainer(id)`, so the identifier is validated by the
// first wrapper before anything is built (G3 at run time, destructive_ids_test.go),
// `stop` and `rm` are spelled once each, where the static guards already read them
// (G3: only internal/docker and internal/kind; F/P: no `-f`, `--force`, `-v`, `--all`,
// `--depend`, `--filter`, no prune), and the engine's refusal is read by the removal
// classification, never answered with a force.
//
// TestNoDestructiveArgvForcesOrPrunes (destroy_previewed_guard_test.go) keeps running
// unchanged on the whole module: this test adds what it cannot see, a function that
// builds no argv at all, and so could grow a `-f` or a timeout word by word (the limit
// the F/P guard states) without any literal for it to read.
//
// Limit, stated: it reads the body of that one function. A helper it calls that appends
// an option word by word is outside it, as it is outside the F/P guard.

// stopAndRemoveName is the function judged.
const stopAndRemoveName = "StopAndRemoveContainer"

// stopAndRemoveForbidden are the words the body may not spell: a force or collateral
// option, a delay, a prune, and the verbs themselves (they belong to StopContainer and
// RemoveContainer, which also validate the identifier).
var stopAndRemoveForbidden = append(slices.Clone(forceOptions),
	"-t", "--time", "-s", "--signal", "prune", "stop", "rm", "kill", "remove", "rmi")

// judgeStopAndRemove reads the sources of one non-test file of package docker and
// reports what is wrong with StopAndRemoveContainer in it, or "not found".
func judgeStopAndRemove(name string, src []byte) (problems []string, found bool) {
	fset := token.NewFileSet()
	f, err := parser.ParseFile(fset, name, src, parser.SkipObjectResolution)
	if err != nil {
		return []string{fmt.Sprintf("%s does not parse: %v", name, err)}, false
	}
	for _, decl := range f.Decls {
		fn, ok := decl.(*ast.FuncDecl)
		if !ok || fn.Recv != nil || fn.Name.Name != stopAndRemoveName || fn.Body == nil {
			continue
		}
		return judgeStopAndRemoveDecl(fset, fn), true
	}
	return nil, false
}

func judgeStopAndRemoveDecl(fset *token.FileSet, fn *ast.FuncDecl) []string {
	var problems []string
	at := func(p token.Pos) string {
		return fmt.Sprintf("%s:%d", fset.Position(p).Filename, fset.Position(p).Line)
	}

	// The signature: the identifier and nothing else, one report back.
	params := fn.Type.Params.List
	if len(params) != 1 || len(params[0].Names) > 1 || !isIdent(params[0].Type, "string") {
		problems = append(problems, at(fn.Pos())+": "+stopAndRemoveName+" must take exactly one string, the identifier: no force flag, no timeout, no options (C2)")
	}
	if fn.Type.Results == nil || len(fn.Type.Results.List) != 1 || !isIdent(fn.Type.Results.List[0].Type, "RemovalReport") {
		problems = append(problems, at(fn.Pos())+": "+stopAndRemoveName+" must return one RemovalReport")
	}

	// The calls, in source order, by name.
	var calls []string
	positions := map[string]token.Pos{}
	ast.Inspect(fn.Body, func(n ast.Node) bool {
		switch n := n.(type) {
		case *ast.CallExpr:
			if id, ok := n.Fun.(*ast.Ident); ok {
				calls = append(calls, id.Name)
				if _, seen := positions[id.Name]; !seen {
					positions[id.Name] = n.Pos()
				}
			}
		case *ast.BasicLit:
			if n.Kind != token.STRING {
				return true
			}
			if s, err := strconv.Unquote(n.Value); err == nil && slices.Contains(stopAndRemoveForbidden, s) {
				problems = append(problems, fmt.Sprintf("%s: %s spells %q itself: the argv is written once, in StopContainer and RemoveContainer, "+
					"which validate the identifier first and add no option", at(n.Pos()), stopAndRemoveName, s))
			}
		}
		return true
	})
	stop, rm := slices.Index(calls, "StopContainer"), slices.Index(calls, "RemoveContainer")
	switch {
	case stop < 0 || rm < 0:
		problems = append(problems, at(fn.Pos())+": "+stopAndRemoveName+" must call StopContainer then RemoveContainer: the identifier is validated by the first, "+
			"and `stop <id>` and `rm <id>` are written there, never forced")
	case stop > rm:
		problems = append(problems, at(positions["RemoveContainer"])+": "+stopAndRemoveName+" removes before it stops: the order is `stop`, then `rm`")
	}
	for _, other := range []string{"RemoveContainers", "PruneNetworks", "mutate", "mutateContext", "dockerOutput", "dockerCombined", "prune"} {
		if p, ok := positions[other]; ok {
			problems = append(problems, fmt.Sprintf("%s: %s calls %s: it goes through StopContainer and RemoveContainer only", at(p), stopAndRemoveName, other))
		}
	}
	// The refusal of the engine is read by the removal classification (OC-22), so the
	// footer says the same words as `P` and `D` on an image: never a text of its own.
	if !slices.Contains(calls, "SingleRemoval") && !slices.Contains(calls, "ClassifyRemoval") {
		problems = append(problems, at(fn.Pos())+": "+stopAndRemoveName+" must read the errors with SingleRemoval or ClassifyRemoval (OC-22): "+
			"a refusal is a RemovalReport, not an error string")
	}
	return problems
}

func isIdent(e ast.Expr, name string) bool {
	id, ok := e.(*ast.Ident)
	return ok && id.Name == name
}

// OC-25 criterion 1 on the real module: internal/docker holds the function, and it is
// the composition above.
func TestStopAndRemoveContainerReusesTheValidatedWrappers(t *testing.T) {
	root, err := launchModuleRoot()
	if err != nil {
		t.Fatal(err)
	}
	dir := filepath.Join(root, "internal", "docker")
	entries, err := os.ReadDir(dir)
	if err != nil {
		t.Fatalf("reading %s: %v", dir, err)
	}
	var homes int
	for _, e := range entries {
		if e.IsDir() || !strings.HasSuffix(e.Name(), ".go") || strings.HasSuffix(e.Name(), "_test.go") {
			continue
		}
		src, err := os.ReadFile(filepath.Join(dir, e.Name()))
		if err != nil {
			t.Fatal(err)
		}
		problems, found := judgeStopAndRemove("internal/docker/"+e.Name(), src)
		if !found {
			continue
		}
		homes++
		for _, p := range problems {
			t.Error(p)
		}
	}
	if homes != 1 {
		t.Errorf("%s is declared in %d files of internal/docker, want exactly one", stopAndRemoveName, homes)
	}
}

// The detector, on sources that break the rules and on one that keeps them: a guard that
// cannot tell them apart passes for the wrong reason.
func TestStopAndRemoveDetectorOnSources(t *testing.T) {
	const head = "package docker\n"
	tests := []struct {
		name string
		src  string
		want []string // substrings of the problems, in any order; nil for none
	}{
		{"the composition", head + `
func StopAndRemoveContainer(id string) RemovalReport {
	if err := StopContainer(id); err != nil {
		return SingleRemoval(id, err)
	}
	return SingleRemoval(id, RemoveContainer(id))
}`, nil},
		{"its own argv, forced", head + `
func StopAndRemoveContainer(id string) RemovalReport {
	err := StopContainer(id)
	_ = mutate("docker rm", "rm", "-f", id)
	return SingleRemoval(id, err)
}`, []string{`spells "rm"`, `spells "-f"`, "must call StopContainer then RemoveContainer", "calls mutate"}},
		{"a timeout word", head + `
func StopAndRemoveContainer(id string) RemovalReport {
	err := StopContainer(id)
	_ = []string{"--time", "0"}
	return SingleRemoval(id, errorsJoin(err, RemoveContainer(id)))
}`, []string{`spells "--time"`}},
		{"removes before it stops", head + `
func StopAndRemoveContainer(id string) RemovalReport {
	err := RemoveContainer(id)
	return SingleRemoval(id, errorsJoin(err, StopContainer(id)))
}`, []string{"removes before it stops"}},
		{"never stops", head + `
func StopAndRemoveContainer(id string) RemovalReport {
	return SingleRemoval(id, RemoveContainer(id))
}`, []string{"must call StopContainer then RemoveContainer"}},
		{"an option parameter", head + `
func StopAndRemoveContainer(id string, force bool) RemovalReport {
	if err := StopContainer(id); err != nil {
		return SingleRemoval(id, err)
	}
	return SingleRemoval(id, RemoveContainer(id))
}`, []string{"exactly one string"}},
		{"an error instead of a report", head + `
func StopAndRemoveContainer(id string) error {
	if err := StopContainer(id); err != nil {
		return err
	}
	return RemoveContainer(id)
}`, []string{"one RemovalReport", "SingleRemoval or ClassifyRemoval"}},
		{"a prune", head + `
func StopAndRemoveContainer(id string) RemovalReport {
	_, _ = PruneNetworks()
	err := StopContainer(id)
	return SingleRemoval(id, errorsJoin(err, RemoveContainer(id)))
}`, []string{"calls PruneNetworks"}},
	}
	for _, tt := range tests {
		t.Run(tt.name, func(t *testing.T) {
			problems, found := judgeStopAndRemove("internal/docker/x.go", []byte(tt.src))

			if !found {
				t.Fatal("the function was not found in the source")
			}
			if tt.want == nil && len(problems) != 0 {
				t.Errorf("problems = %q, want none", problems)
			}
			for _, w := range tt.want {
				if !slices.ContainsFunc(problems, func(p string) bool { return strings.Contains(p, w) }) {
					t.Errorf("problems = %q, want one that says %q", problems, w)
				}
			}
		})
	}
}

// The blindness check: a file with no such function reports "not found" instead of
// passing, and one that does not parse is a problem, not silence.
func TestStopAndRemoveDetectorSaysWhenItSeesNothing(t *testing.T) {
	if _, found := judgeStopAndRemove("internal/docker/x.go", []byte("package docker\nfunc Other() {}\n")); found {
		t.Error("a file without the function was reported as holding it")
	}
	if problems, _ := judgeStopAndRemove("internal/docker/x.go", []byte("package docker\nfunc (")); len(problems) != 1 {
		t.Errorf("problems = %q, want one for the source that does not parse", problems)
	}
}
