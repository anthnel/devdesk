package scan

import (
	"go/ast"
	"go/parser"
	"go/token"
	"os"
	"slices"
	"sort"
	"strconv"
	"strings"
	"testing"
)

// Where the text of a finding comes from. The convention (docs/architecture/
// scanning.md, "Writing a Resolution, Steps and Repro for a new finding") has two
// provenance rules, and this guard reads the non-test sources of this package to
// hold them:
//
//	P1  a Repro of a Finding is only ever a value that localRepro (or a wrapper of
//	    it, found by the guard: a function whose every return is a call to a
//	    producer) returned, so the PlainWords refusal, the "no line from a
//	    container" rule and the "never copy the user's arguments" rule always
//	    apply. The empty string is allowed.
//	P2  a Steps of a Finding is only ever a value a *Steps function returned, and
//	    every *Steps function of the package is defined in steps.go
//	    (stepsFunctionsOutsideStepsGo), the only file the content guard of
//	    steps_guard_test.go samples, so it sees every step a finding can carry.
//	    nil is allowed.
//
// A site is `Repro: x` in a composite literal, `y.Repro = x`, or the tuple form
// `y.Repro, y.RanFrom = a, b`. x is accepted when it is a call to a producer, a
// name that a producer's call defined in the same function and that nothing else
// assigns, or a parameter of the function that every caller in the package feeds
// with an accepted value (followed recursively).
//
// Limits, stated: it works on names, not types, so another struct with a field
// called Repro or Steps is held to the same rule; a producer reached through an
// alias, a function value or a method value is not seen; an assignment through a
// pointer to the string (`p := &f.Repro; *p = ...`), reflection, a Finding built
// by another package or a positional literal other than the ones it flags are out
// of its sight; flow is ignored inside a function (a name assigned once by a
// producer and once by anything else is refused, whatever the order); functions
// are keyed by name, so two methods of the same name in this package share one
// entry.

// fieldRule is one of the two rules: the field it guards, what counts as a
// producer and what literal is a harmless zero value.
type fieldRule struct {
	field    string
	producer func(name string, wrappers map[string]bool) bool
	zero     func(ast.Expr) bool
}

var reproRule = fieldRule{
	field:    "Repro",
	producer: func(name string, wrappers map[string]bool) bool { return name == "localRepro" || wrappers[name] },
	zero: func(e ast.Expr) bool {
		lit, ok := e.(*ast.BasicLit)
		return ok && lit.Kind == token.STRING && lit.Value == `""`
	},
}

var stepsRule = fieldRule{
	field:    "Steps",
	producer: func(name string, _ map[string]bool) bool { return strings.HasSuffix(name, "Steps") },
	zero: func(e ast.Expr) bool {
		id, ok := e.(*ast.Ident)
		return ok && id.Name == "nil"
	},
}

type guardSource struct {
	name string
	file *ast.File
	fset *token.FileSet
}

type guardFunc struct {
	decl   *ast.FuncDecl
	source guardSource
	params map[string]int
}

func parseGuardSources(t *testing.T, sources map[string]string) []guardSource {
	t.Helper()
	fset := token.NewFileSet()
	names := make([]string, 0, len(sources))
	for name := range sources {
		names = append(names, name)
	}
	slices.Sort(names)
	out := make([]guardSource, 0, len(names))
	for _, name := range names {
		f, err := parser.ParseFile(fset, name, sources[name], 0)
		if err != nil {
			t.Fatalf("parse %s: %v", name, err)
		}
		out = append(out, guardSource{name, f, fset})
	}
	return out
}

func packageGuardSources(t *testing.T) map[string]string {
	t.Helper()
	entries, err := os.ReadDir(".")
	if err != nil {
		t.Fatal(err)
	}
	sources := map[string]string{}
	for _, e := range entries {
		name := e.Name()
		if !strings.HasSuffix(name, ".go") || strings.HasSuffix(name, "_test.go") {
			continue
		}
		b, err := os.ReadFile(name)
		if err != nil {
			t.Fatal(err)
		}
		sources[name] = string(b)
	}
	if len(sources) == 0 {
		t.Fatal("no source file read: the guard would pass on nothing")
	}
	return sources
}

func exprString(e ast.Expr) string {
	switch x := e.(type) {
	case *ast.Ident:
		return x.Name
	case *ast.SelectorExpr:
		return exprString(x.X) + "." + x.Sel.Name
	case *ast.ParenExpr:
		return exprString(x.X)
	case *ast.StarExpr:
		return "*" + exprString(x.X)
	}
	return ""
}

// calleeName is the name of the function a call names directly, or "".
func calleeName(call *ast.CallExpr) string {
	switch f := call.Fun.(type) {
	case *ast.Ident:
		return f.Name
	case *ast.SelectorExpr:
		return f.Sel.Name
	}
	return ""
}

func flattenParams(decl *ast.FuncDecl) map[string]int {
	params := map[string]int{}
	i := 0
	for _, field := range decl.Type.Params.List {
		if len(field.Names) == 0 {
			i++
			continue
		}
		for _, n := range field.Names {
			params[n.Name] = i
			i++
		}
	}
	return params
}

// findWrappers returns the functions whose every return is a single call to a
// producer (or to a wrapper already found): renderRepro around localRepro.
func findWrappers(funcs map[string]guardFunc, rule fieldRule) map[string]bool {
	wrappers := map[string]bool{}
	for changed := true; changed; {
		changed = false
		for name, fn := range funcs {
			if wrappers[name] || fn.decl.Body == nil || rule.producer(name, nil) {
				continue
			}
			returns, all := 0, true
			ast.Inspect(fn.decl.Body, func(n ast.Node) bool {
				if _, lit := n.(*ast.FuncLit); lit {
					return false
				}
				ret, ok := n.(*ast.ReturnStmt)
				if !ok {
					return true
				}
				returns++
				call, isCall := (*ast.CallExpr)(nil), false
				if len(ret.Results) == 1 {
					call, isCall = ret.Results[0].(*ast.CallExpr)
				}
				if !isCall || !rule.producer(calleeName(call), wrappers) {
					all = false
				}
				return true
			})
			if returns > 0 && all {
				wrappers[name] = true
				changed = true
			}
		}
	}
	return wrappers
}

type scopeFacts struct {
	produced map[string]bool // names a producer call defined
	dirty    map[string]bool // names something else assigned
}

// factsOf reads what a function assigns to which name.
func factsOf(body ast.Node, rule fieldRule, wrappers map[string]bool) scopeFacts {
	facts := scopeFacts{map[string]bool{}, map[string]bool{}}
	note := func(lhs []ast.Expr, rhs []ast.Expr) {
		if len(rhs) == 1 {
			if call, ok := rhs[0].(*ast.CallExpr); ok {
				for i, l := range lhs {
					name := exprString(l)
					if i == 0 && rule.producer(calleeName(call), wrappers) {
						facts.produced[name] = true
					} else {
						facts.dirty[name] = true
					}
				}
				return
			}
		}
		for i, l := range lhs {
			name := exprString(l)
			if name == "_" || name == "" {
				continue
			}
			if len(lhs) == len(rhs) {
				if call, ok := rhs[i].(*ast.CallExpr); ok && rule.producer(calleeName(call), wrappers) {
					facts.produced[name] = true
					continue
				}
			}
			facts.dirty[name] = true
		}
	}
	ast.Inspect(body, func(n ast.Node) bool {
		switch s := n.(type) {
		case *ast.AssignStmt:
			note(s.Lhs, s.Rhs)
		case *ast.ValueSpec:
			names := make([]ast.Expr, len(s.Names))
			for i, id := range s.Names {
				names[i] = id
			}
			note(names, s.Values)
		case *ast.RangeStmt:
			for _, e := range []ast.Expr{s.Key, s.Value} {
				if e != nil {
					facts.dirty[exprString(e)] = true
				}
			}
		}
		return true
	})
	return facts
}

// fieldViolations reports every site that sets rule.field from something the rule
// does not accept, as "file:line: function: reason", sorted. found is how many
// sites it examined, so a caller can tell a pass from a blind pass.
func fieldViolations(sources []guardSource, rule fieldRule) (violations []string, found int) {
	funcs := map[string]guardFunc{}
	for _, src := range sources {
		for _, d := range src.file.Decls {
			if fd, ok := d.(*ast.FuncDecl); ok {
				funcs[fd.Name.Name] = guardFunc{fd, src, flattenParams(fd)}
			}
		}
	}
	wrappers := findWrappers(funcs, rule)
	factsCache := map[string]scopeFacts{}
	facts := func(fn guardFunc) scopeFacts {
		key := fn.decl.Name.Name
		if f, ok := factsCache[key]; ok {
			return f
		}
		f := factsOf(fn.decl.Body, rule, wrappers)
		factsCache[key] = f
		return f
	}

	var check func(fn guardFunc, e ast.Expr, seen map[string]bool) string
	check = func(fn guardFunc, e ast.Expr, seen map[string]bool) string {
		if rule.zero(e) {
			return ""
		}
		if call, ok := e.(*ast.CallExpr); ok {
			if rule.producer(calleeName(call), wrappers) {
				return ""
			}
			return "value comes from a call to " + calleeName(call) + ", not from a producer"
		}
		name := exprString(e)
		if name == "" {
			return "value is an expression the guard does not follow; use a variable a producer returned"
		}
		f := facts(fn)
		if f.dirty[name] {
			return name + " is assigned from something other than a producer"
		}
		if f.produced[name] {
			return ""
		}
		idx, isParam := fn.params[name]
		if !isParam {
			return name + " is not defined by a producer call in the function"
		}
		key := fn.decl.Name.Name + "#" + name
		if seen[key] {
			return ""
		}
		seen[key] = true
		callers := 0
		for _, caller := range funcs {
			if caller.decl.Body == nil {
				continue
			}
			var bad string
			ast.Inspect(caller.decl.Body, func(n ast.Node) bool {
				call, ok := n.(*ast.CallExpr)
				if !ok || calleeName(call) != fn.decl.Name.Name {
					return true
				}
				callers++
				if idx >= len(call.Args) {
					bad = "a call to " + fn.decl.Name.Name + " does not pass " + name
				} else if why := check(caller, call.Args[idx], seen); why != "" && bad == "" {
					bad = "via " + caller.decl.Name.Name + " at " + pos(caller.source, call) + ": " + why
				}
				return true
			})
			if bad != "" {
				return name + " is a parameter and " + bad
			}
		}
		if callers == 0 {
			return name + " is a parameter nobody in the package passes a value to"
		}
		return ""
	}

	report := func(fn *guardFunc, src guardSource, at ast.Node, why string) {
		fname := "(package level)"
		if fn != nil {
			fname = fn.decl.Name.Name
		}
		violations = append(violations, pos(src, at)+": "+fname+": "+rule.field+": "+why)
	}
	visit := func(fn *guardFunc, src guardSource, root ast.Node) {
		ast.Inspect(root, func(n ast.Node) bool {
			var why string
			switch s := n.(type) {
			case *ast.CompositeLit:
				if id, ok := s.Type.(*ast.Ident); ok && id.Name == "Finding" && len(s.Elts) > 0 {
					if _, keyed := s.Elts[0].(*ast.KeyValueExpr); !keyed {
						report(fn, src, s, "positional Finding literal: use keyed fields so the guard can read them")
					}
				}
			case *ast.KeyValueExpr:
				if id, ok := s.Key.(*ast.Ident); ok && id.Name == rule.field {
					found++
					if fn == nil {
						if !rule.zero(s.Value) {
							why = "set at package level"
						}
					} else {
						why = check(*fn, s.Value, map[string]bool{})
					}
					if why != "" {
						report(fn, src, s, why)
					}
				}
			case *ast.AssignStmt:
				for i, l := range s.Lhs {
					sel, ok := l.(*ast.SelectorExpr)
					if !ok || sel.Sel.Name != rule.field {
						continue
					}
					found++
					switch {
					case fn == nil:
						why = "set at package level"
					case s.Tok != token.ASSIGN && s.Tok != token.DEFINE:
						why = "modified in place with " + s.Tok.String()
					case len(s.Rhs) == len(s.Lhs):
						why = check(*fn, s.Rhs[i], map[string]bool{})
					case len(s.Rhs) == 1:
						call, isCall := s.Rhs[0].(*ast.CallExpr)
						if !isCall || i != 0 || !rule.producer(calleeName(call), wrappers) {
							why = "tuple assignment that is not a producer's first result"
						}
					}
					if why != "" {
						report(fn, src, s, why)
					}
				}
			}
			return true
		})
	}
	for _, src := range sources {
		for _, d := range src.file.Decls {
			if fd, ok := d.(*ast.FuncDecl); ok {
				if fd.Body != nil {
					fn := funcs[fd.Name.Name]
					visit(&fn, src, fd.Body)
				}
				continue
			}
			visit(nil, src, d)
		}
	}
	sort.Strings(violations)
	return violations, found
}

func pos(src guardSource, n ast.Node) string {
	p := src.fset.Position(n.Pos())
	return src.name + ":" + strconv.Itoa(p.Line)
}

// A synthetic package that breaks every part of P1 must be caught, and one that
// follows it must pass: a detector that cannot fail proves nothing.
const reproViolatingSource = `package scan

type Finding struct{ Repro, RanFrom string }

func localRepro(tool int, name string) (string, string) { return "", "" }
func wrapRepro(tool int) (string, string)               { return localRepro(tool, "x") }

func literal() Finding         { return Finding{Repro: "go run ./cmd"} }
func concatenated(p string) Finding {
	return Finding{Repro: "kubeconform " + p}
}
func assigned(f *Finding, s string) { f.Repro = s }
func appended(f *Finding)           { f.Repro += " --debug" }
func viaCall(f *Finding)            { f.Repro = describe() }
func reassigned(f *Finding) {
	line, from := localRepro(1, "a")
	line = "rm -rf /"
	f.Repro, f.RanFrom = line, from
}
func tupleFromElsewhere(f *Finding) { f.Repro, f.RanFrom = other() }
func build(repro string) Finding    { return Finding{Repro: repro} }
func callsBuildWithALiteral() Finding {
	return build("echo hi")
}
func positional() Finding { return Finding{"echo hi", ""} }
`

const reproCleanSource = `package scan

type Finding struct{ Repro, RanFrom string }

func localRepro(tool int, name string) (string, string) { return "", "" }
func wrapRepro(tool int) (string, string)               { return localRepro(tool, "x") }

func direct() Finding {
	repro, from := localRepro(1, "a")
	return Finding{Repro: repro, RanFrom: from}
}
func wrapped() Finding {
	repro, from := wrapRepro(1)
	return Finding{Repro: repro, RanFrom: from}
}
func tuple(f *Finding) { f.Repro, f.RanFrom = localRepro(1, "a") }
func stamp(fs []Finding) {
	var r struct{ line, from string }
	r.line, r.from = wrapRepro(1)
	for i := range fs {
		fs[i].Repro, fs[i].RanFrom = r.line, r.from
	}
}
func build(repro string) Finding { return Finding{Repro: repro} }
func callsBuild() Finding {
	repro, _ := wrapRepro(1)
	return build(repro)
}
func none() Finding { return Finding{Repro: ""} }
`

func TestTheReproGuardCatchesAHandWrittenRepro(t *testing.T) {
	got, found := fieldViolations(parseGuardSources(t, map[string]string{"bad.go": reproViolatingSource}), reproRule)
	for _, fn := range []string{"literal", "concatenated", "assigned", "appended", "viaCall", "reassigned", "tupleFromElsewhere", "build", "positional"} {
		if !violationIn(got, ": "+fn+": ") {
			t.Errorf("the guard did not flag %s; violations:\n%s", fn, strings.Join(got, "\n"))
		}
	}
	if found < 8 {
		t.Errorf("examined %d sites, want at least 8", found)
	}
}

func TestTheReproGuardAcceptsWhatLocalReproBuilt(t *testing.T) {
	got, found := fieldViolations(parseGuardSources(t, map[string]string{"ok.go": reproCleanSource}), reproRule)
	if len(got) > 0 {
		t.Errorf("the guard flagged clean code:\n%s", strings.Join(got, "\n"))
	}
	if found < 6 {
		t.Errorf("examined %d sites, want at least 6: the guard is blind", found)
	}
}

func violationIn(violations []string, marker string) bool {
	return slices.ContainsFunc(violations, func(v string) bool { return strings.Contains(v, marker) })
}

// Real code. The floor on the number of sites and the two functions that must be
// found keep a rename from turning the guard into a pass on nothing.
func TestAFindingsReproComesOnlyFromLocalRepro(t *testing.T) {
	sources := parseGuardSources(t, packageGuardSources(t))
	violations, found := fieldViolations(sources, reproRule)
	for _, v := range violations {
		t.Error(v)
	}
	if found < 3 {
		t.Errorf("examined %d Repro sites, expected at least 3 (renderFailure, helmLint, stampSchemaRepro): did the field move?", found)
	}
	funcs := map[string]guardFunc{}
	for _, src := range sources {
		for _, d := range src.file.Decls {
			if fd, ok := d.(*ast.FuncDecl); ok {
				funcs[fd.Name.Name] = guardFunc{fd, src, flattenParams(fd)}
			}
		}
	}
	if _, ok := funcs["localRepro"]; !ok {
		t.Fatal("localRepro is gone: the guard has no producer to hold on to")
	}
	if !findWrappers(funcs, reproRule)["renderRepro"] {
		t.Error("renderRepro is no longer recognised as a wrapper of localRepro")
	}
}

const stepsViolatingSource = `package scan

type Finding struct{ Steps []string }

func secretSteps() []string { return []string{"a"} }

func literal() Finding { return Finding{Steps: []string{"Press ctrl+r"}} }
func assigned(f *Finding) { f.Steps = []string{"x"} }
func appended(f *Finding) { f.Steps = append(f.Steps, "x") }
`

const stepsCleanSource = `package scan

type Finding struct{ Steps []string }

func secretSteps() []string { return []string{"a"} }

func fromFunc() Finding { return Finding{Steps: secretSteps()} }
func assigned(f *Finding) { f.Steps = secretSteps() }
func none() Finding { return Finding{Steps: nil} }
func build(steps []string) Finding { return Finding{Steps: steps} }
func callsBuild() Finding {
	steps := secretSteps()
	return build(steps)
}
`

func TestTheStepsGuardCatchesAStepWrittenOutsideAStepsFunction(t *testing.T) {
	got, _ := fieldViolations(parseGuardSources(t, map[string]string{"bad.go": stepsViolatingSource}), stepsRule)
	for _, fn := range []string{"literal", "assigned", "appended"} {
		if !violationIn(got, ": "+fn+": ") {
			t.Errorf("the guard did not flag %s; violations:\n%s", fn, strings.Join(got, "\n"))
		}
	}
}

func TestTheStepsGuardAcceptsWhatAStepsFunctionReturned(t *testing.T) {
	got, found := fieldViolations(parseGuardSources(t, map[string]string{"ok.go": stepsCleanSource}), stepsRule)
	if len(got) > 0 {
		t.Errorf("the guard flagged clean code:\n%s", strings.Join(got, "\n"))
	}
	if found < 4 {
		t.Errorf("examined %d sites, want at least 4: the guard is blind", found)
	}
}

func TestAFindingsStepsComeOnlyFromAStepsFunction(t *testing.T) {
	violations, found := fieldViolations(parseGuardSources(t, packageGuardSources(t)), stepsRule)
	for _, v := range violations {
		t.Error(v)
	}
	if found < 5 {
		t.Errorf("examined %d Steps sites, expected at least 5 (gitleaks, trivy, schemaFindings, helm lint, renderFailure)", found)
	}
}
