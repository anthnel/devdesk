package scan

import (
	"fmt"
	"go/ast"
	"go/token"
	"regexp"
	"strconv"
	"strings"
	"testing"
)

// DEVDESK-772 guard (Q1): a command template of steps.go never receives a name
// from the repository except through shellQuote.
//
// The folder names in a step come from a repository DevDesk does not trust, and a
// step the user copies is read by a shell. The quoting lives in one helper,
// shellQuote (POSIX single quotes, an apostrophe written as quote, backslash,
// quote, quote), and typedDir and typedKustomizeDir may
// wrap it with the "./" that DEVDESK-763 and DEVDESK-768 need. This test parses
// steps.go and fails, naming file and line, when a command template is filled with
// anything else.
//
// What it holds:
//
//	Q1  shellQuote is defined in a non-test file of the package;
//	Q2  in steps.go, a fmt.Sprintf whose format holds a command ("run helm ...",
//	    "Run kustomize ..." and the other programs below) takes only string
//	    literals and calls of a protector as arguments, in any function, not only
//	    the *Steps ones; a protector is shellQuote, or a function of the package
//	    whose body calls it (typedDir, typedKustomizeDir);
//	Q3  the same for a "+" concatenation that holds a command literal;
//	Q4  a command template literal ("run helm lint %s") that is not the format of
//	    a Sprintf is refused: nothing says what fills it, so a constant or a
//	    variable cannot hide a template from Q2.
//
// Limits, written because no check is free of them:
//
//   - It matches names, not types: an argument is a protector because of the name
//     of the function called, so a `shellQuote` that returns its input passes.
//     TestAPastedStepRunsNothingElseAndGivesTheFolderAsOneArgument runs the result.
//   - A command is recognised by the phrase "run <program>" in the template; a
//     step worded "Type helm lint ..." or "Use ..." is not seen. The programs are
//     in stepsCommandPrograms: add one when a step starts to name another.
//   - Only steps.go is read: provenance_guard_test.go already refuses a *Steps
//     function anywhere else, and a Steps that is not from one.
//   - A protector is a function that calls shellQuote on some path; whether the
//     path is taken for every name is not checked (the behavior tests do).
//   - A prose step that names a folder without a command ("move the file below
//     %s.", "Fix what helm lint reports in %s.") is out of Q2, and so are
//     strings.Join, strings.NewReplacer, a template package and reflection.
//   - A control character in a name is a text, not a quote: only the behavior
//     test sees that.
//   - Windows PowerShell and cmd read the single quotes otherwise: documented,
//     not guarded.
const stepsCommandPrograms = "helm|kustomize|kubeconform|kubectl|trivy|gitleaks|plumber|git|docker|podman|cosign"

var stepsCommandTemplate = regexp.MustCompile(`(?i)\brun\s+(?:` + stepsCommandPrograms + `)\b`)

const stepsQuoteHelper = "shellQuote"

// stepQuotingViolations returns one line per breach, each starting with file:line.
func stepQuotingViolations(sources []guardSource) []string {
	var out []string
	if !definesFunc(sources, stepsQuoteHelper) {
		out = append(out, "steps.go:1: "+stepsQuoteHelper+" is not defined in a non-test file of the package: "+
			"the folder names of a step have nothing to be quoted by")
	}
	protectors := stepProtectors(sources)
	for _, src := range sources {
		if src.name != "steps.go" {
			continue
		}
		out = append(out, commandTemplateViolations(src, protectors)...)
	}
	return out
}

func definesFunc(sources []guardSource, name string) bool {
	for _, src := range sources {
		for _, d := range src.file.Decls {
			if fd, ok := d.(*ast.FuncDecl); ok && fd.Recv == nil && fd.Name.Name == name {
				return true
			}
		}
	}
	return false
}

// stepProtectors is shellQuote and every function of the package whose body
// calls it.
func stepProtectors(sources []guardSource) map[string]bool {
	protectors := map[string]bool{stepsQuoteHelper: true}
	for _, src := range sources {
		for _, d := range src.file.Decls {
			fd, ok := d.(*ast.FuncDecl)
			if !ok || fd.Recv != nil || fd.Body == nil {
				continue
			}
			ast.Inspect(fd.Body, func(n ast.Node) bool {
				if call, ok := n.(*ast.CallExpr); ok && isIdentCall(call, stepsQuoteHelper) {
					protectors[fd.Name.Name] = true
				}
				return true
			})
		}
	}
	return protectors
}

func isIdentCall(call *ast.CallExpr, name string) bool {
	id, ok := call.Fun.(*ast.Ident)
	return ok && id.Name == name
}

func isSprintf(call *ast.CallExpr) bool {
	sel, ok := call.Fun.(*ast.SelectorExpr)
	if !ok || sel.Sel.Name != "Sprintf" {
		return false
	}
	pkg, ok := sel.X.(*ast.Ident)
	return ok && pkg.Name == "fmt"
}

func stringLiteral(e ast.Expr) (string, bool) {
	lit, ok := e.(*ast.BasicLit)
	if !ok || lit.Kind != token.STRING {
		return "", false
	}
	s, err := strconv.Unquote(lit.Value)
	return s, err == nil
}

// isProtected says an argument is safe to put in a command: a literal, or a call
// of a protector.
func isProtected(e ast.Expr, protectors map[string]bool) bool {
	if _, ok := stringLiteral(e); ok {
		return true
	}
	if call, ok := e.(*ast.CallExpr); ok {
		if id, ok := call.Fun.(*ast.Ident); ok && protectors[id.Name] {
			return true
		}
	}
	return false
}

// describeArg is what the message shows of an argument.
func describeArg(src guardSource, e ast.Expr) string {
	p, q := src.fset.Position(e.Pos()), src.fset.Position(e.End())
	if p.Line != q.Line || q.Column <= p.Column {
		return "an expression"
	}
	return fmt.Sprintf("the expression at column %d", p.Column)
}

func commandTemplateViolations(src guardSource, protectors map[string]bool) []string {
	var out []string
	handled := map[*ast.BasicLit]bool{}
	reported := map[token.Pos]bool{}
	report := func(n ast.Node, format string, args ...any) {
		if reported[n.Pos()] {
			return
		}
		reported[n.Pos()] = true
		out = append(out, pos(src, n)+": "+fmt.Sprintf(format, args...))
	}

	ast.Inspect(src.file, func(n ast.Node) bool {
		switch n := n.(type) {
		case *ast.CallExpr:
			if !isSprintf(n) || len(n.Args) == 0 {
				return true
			}
			format, ok := stringLiteral(n.Args[0])
			if !ok {
				return true
			}
			handled[n.Args[0].(*ast.BasicLit)] = true
			if !stepsCommandTemplate.MatchString(format) {
				return true
			}
			for _, arg := range n.Args[1:] {
				if !isProtected(arg, protectors) {
					report(arg, "the command %q is filled with %s that does not go through %s: a folder name from the repository must be quoted",
						format, describeArg(src, arg), stepsQuoteHelper)
				}
			}
		case *ast.BinaryExpr:
			if n.Op != token.ADD {
				return true
			}
			operands := flattenAdd(n)
			command := ""
			for _, op := range operands {
				if s, ok := stringLiteral(op); ok && stepsCommandTemplate.MatchString(s) {
					command = s
					if lit, ok := op.(*ast.BasicLit); ok {
						handled[lit] = true
					}
				}
			}
			if command == "" {
				return true
			}
			for _, op := range operands {
				if !isProtected(op, protectors) {
					report(op, "the command %q is built with + and %s, which does not go through %s",
						command, describeArg(src, op), stepsQuoteHelper)
				}
			}
		}
		return true
	})

	// Q4: a template that is not the format of a Sprintf, or part of a "+".
	ast.Inspect(src.file, func(n ast.Node) bool {
		lit, ok := n.(*ast.BasicLit)
		if !ok || lit.Kind != token.STRING || handled[lit] {
			return true
		}
		if s, ok := stringLiteral(lit); ok && stepsCommandTemplate.MatchString(s) && strings.Contains(s, "%") {
			report(lit, "the command template %q is not the format of a fmt.Sprintf: nothing shows what fills it", s)
		}
		return true
	})
	return out
}

// flattenAdd lists the operands of a chain a + b + c.
func flattenAdd(e ast.Expr) []ast.Expr {
	if b, ok := e.(*ast.BinaryExpr); ok && b.Op == token.ADD {
		return append(flattenAdd(b.X), flattenAdd(b.Y)...)
	}
	return []ast.Expr{e}
}

const quotedStepsSource = `package scan

import (
	"fmt"
	"strings"
)

func shellQuote(s string) string { return "'" + strings.ReplaceAll(s, "'", "'\\''") + "'" }

func typedDir(dir string) string {
	if strings.HasPrefix(dir, "-") {
		return shellQuote("./" + dir)
	}
	return shellQuote(dir)
}

func renderSteps(target string) []string {
	return []string{
		fmt.Sprintf("Run helm template %s and fix the first error it reports.", typedDir(target)),
		fmt.Sprintf("If a dependency is missing, run helm dependency build %s.", shellQuote(target)),
		fmt.Sprintf("Fix what helm lint reports in %s.", target),
		fmt.Sprintf("Render it with %s and validate the output.", target),
	}
}
`

func stepSourcesNamed(t *testing.T, steps string, others ...string) []guardSource {
	t.Helper()
	files := map[string]string{"steps.go": steps}
	for i, o := range others {
		files[fmt.Sprintf("other%d.go", i)] = o
	}
	return parseGuardSources(t, files)
}

// The detector must be able to fail: each breach below is a steps.go that must be
// refused, and the one that follows the rule must pass.
func TestTheQuotingGuardCatchesWhatItIsWrittenFor(t *testing.T) {
	const header = "package scan\n\nimport \"fmt\"\n\nfunc shellQuote(s string) string { return \"'\" + s + \"'\" }\n\n"
	cases := []struct {
		name string
		src  string
		want string
	}{
		{"a bare name", header + `func f(target string) string { return fmt.Sprintf("Run helm template %s now.", target) }`, "does not go through shellQuote"},
		{"a bare name in a lowercase run", header + `func f(dir string) string { return fmt.Sprintf("If it fails, run kustomize build %s.", dir) }`, "does not go through shellQuote"},
		{"a function that does not quote", header + `func typedDir(d string) string { return d }
func f(dir string) string { return fmt.Sprintf("Run helm lint %s.", typedDir(dir)) }`, "does not go through shellQuote"},
		{"a method call", header + `func f(a struct{ Dir string }) string { return fmt.Sprintf("Run helm lint %s.", a.Dir) }`, "does not go through shellQuote"},
		{"a second argument", header + `func f(dir, other string) string { return fmt.Sprintf("Run helm lint %s in %s.", shellQuote(dir), other) }`, "does not go through shellQuote"},
		{"a concatenation", header + `func f(chart string) string { return "Run helm lint " + chart + " again." }`, "built with +"},
		{"a template held in a constant", header + `const tpl = "Run helm template %s."
func f(dir string) string { return fmt.Sprintf(tpl, dir) }`, "not the format of a fmt.Sprintf"},
		{"a function that is not a *Steps function", header + `func describe(dir string) string { return fmt.Sprintf("Run trivy fs %s.", dir) }`, "does not go through shellQuote"},
	}
	for _, c := range cases {
		got := strings.Join(stepQuotingViolations(stepSourcesNamed(t, c.src)), "\n")
		if !strings.Contains(got, c.want) || !strings.Contains(got, "steps.go:") {
			t.Errorf("%s: violations = %q, want one at steps.go:<line> mentioning %q", c.name, got, c.want)
		}
	}

	if got := stepQuotingViolations(stepSourcesNamed(t, "package scan\n")); len(got) != 1 || !strings.Contains(got[0], "shellQuote is not defined") {
		t.Errorf("no helper: violations = %q, want the missing helper", got)
	}
}

func TestTheQuotingGuardAcceptsAQuotedFolderAndProse(t *testing.T) {
	if got := stepQuotingViolations(stepSourcesNamed(t, quotedStepsSource)); len(got) != 0 {
		t.Errorf("a source that quotes was flagged: %q", got)
	}
	// The helper may live in another file of the package.
	steps := `package scan
import "fmt"
func renderSteps(target string) []string {
	return []string{fmt.Sprintf("Run helm template %s.", shellQuote(target))}
}
`
	helper := "package scan\nfunc shellQuote(s string) string { return s }\n"
	if got := stepQuotingViolations(stepSourcesNamed(t, steps, helper)); len(got) != 0 {
		t.Errorf("a helper in another file was not found: %q", got)
	}
	// A violation outside steps.go is none of this guard's business.
	other := "package scan\nimport \"fmt\"\nfunc g(d string) string { return fmt.Sprintf(\"Run helm lint %s.\", d) }\n"
	if got := stepQuotingViolations(stepSourcesNamed(t, quotedStepsSource, other)); len(got) != 0 {
		t.Errorf("a file other than steps.go was read: %q", got)
	}
}

// The real steps.go: red until shellQuote exists and typedDir and typedKustomizeDir
// go through it.
func TestNoStepInterpolatesAFolderNameWithoutShellQuote(t *testing.T) {
	sources := parseGuardSources(t, packageGuardSources(t))
	var seenSteps bool
	for _, s := range sources {
		seenSteps = seenSteps || s.name == "steps.go"
	}
	if !seenSteps {
		t.Fatal("steps.go not found: the guard would pass on nothing")
	}
	for _, v := range stepQuotingViolations(sources) {
		t.Error(v)
	}
}
