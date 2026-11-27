package engineguard_test

import (
	"fmt"
	"go/ast"
	"go/parser"
	"go/token"
	"io/fs"
	"os"
	"path"
	"path/filepath"
	"slices"
	"sort"
	"strings"
	"sync"
	"testing"
)

// OC-09a1 (DEVDESK-806), guard G5: a contract test, the kind that reaches a real
// engine on purpose, opens with requireEngine(t).
//
// A static test, modelled on G2 (launch_guard_test.go, whose module walk and
// helpers it reuses): it reads every .go file of the module with go/parser, no
// type information, and reports file and line. It is the static half of OC-03's
// contract gate: AllowRealEngine and the gate itself are run-time and live in
// engineguard.go; this one keeps a contract from skipping them.
//
// A contract is a function of a *_contract_test.go file. The rules, each with its
// letter in the report:
//
//	R1  every Test function of a *_contract_test.go file (TestContract*) has
//	    requireEngine(t) as its first statement, alone or as the right-hand side of an
//	    assignment (c := requireEngine(t)), t being the identifier of the *testing.T
//	    parameter of that function. Not counted: a nested call (in an if or a
//	    closure), a method (x.requireEngine(t)), another argument, a second argument.
//	R2  a *_contract_test.go file declares only TestContract* functions (helpers go
//	    in contract_support_test.go, which `-run '^TestContract'` does not select),
//	    and none of them calls Parallel (the seams of internal/docker are global).
//	R3  a TestContract* function outside a *_contract_test.go file, a call
//	    requireEngine(x) with one argument, or a call engineguard.AllowRealEngine(...)
//	    outside the files of the contracts, contract_support_test.go and
//	    contract_support_internal_test.go (the one-argument form is the contract
//	    helper's; internal/ui/netdiag has a three-argument requireEngine of its own,
//	    which is not it). AllowRealEngine is also called from internal/engineguard
//	    and from internal/testutil/enginecontract, whose Require is what the
//	    requireEngine of a package calls.
//	R4  a package with a *_contract_test.go file defines requireEngine in a _test.go
//	    file, and its first statement is a call to enginecontract.Require(...).
//
// Floor: at least one contract file in the module, or the guard would pass on
// nothing. Exceptions: contractExceptions, empty, a debt register like G2's: key =
// file + function, a reason a reviewer can disagree with; an entry whose code is
// gone, or without a reason, fails.
//
// Limits, stated here and not hidden: a function value (re := requireEngine), a
// var declaration (var c = requireEngine(t)), the *testing.T parameter under an
// aliased "testing" import, a dot import, a contract in a file not named
// *_contract_test.go that calls neither requireEngine nor AllowRealEngine (G1
// still panics at run time on its first reach), a type, var or const in a contract
// file (R2 reads functions), and a package that defines requireEngine in a file
// the guard cannot parse.

const (
	engineguardPackage    = "github.com/anthnel/devdesk/internal/engineguard"
	enginecontractPackage = "github.com/anthnel/devdesk/internal/testutil/enginecontract"

	// contractFloor is how many contract files the module must hold.
	contractFloor = 1
	// contractWalkMinimum is a floor on the files read, so a wrong root cannot pass.
	contractWalkMinimum = 200

	ruleFirstStatement = "R1"
	ruleOnlyContracts  = "R2"
	ruleWhereTheyLive  = "R3"
	ruleDefinition     = "R4"
)

var contractAdvice = map[string]string{
	ruleFirstStatement: "a contract test must start with requireEngine(t) (or c := requireEngine(t)), t being its own *testing.T",
	ruleOnlyContracts:  "a *_contract_test.go file holds TestContract* functions only, and no t.Parallel(); put helpers in contract_support_test.go",
	ruleWhereTheyLive:  "a TestContract* function, requireEngine(t) or engineguard.AllowRealEngine belongs to a *_contract_test.go file or contract_support_test.go",
	ruleDefinition:     "a package with a contract defines requireEngine in a _test.go file, and it starts with enginecontract.Require(...)",
}

// contractException is a function that may break a rule, and why. Keyed by file
// and function, not by line. The list does not grow for convenience.
type contractException struct {
	File   string // slash path from the module root
	Func   string // as funcLabel names it
	Reason string
}

var contractExceptions = []contractException{}

// contractSite is one place where a rule is broken.
type contractSite struct {
	File string // slash path from the module root
	Line int
	Func string
	Rule string
	What string
}

func (s contractSite) String() string {
	return fmt.Sprintf("%s:%d: %s %s (in %s)", s.File, s.Line, s.Rule, s.What, s.Func)
}

func isContractFile(rel string) bool { return strings.HasSuffix(rel, "_contract_test.go") }

// isContractSupportFile is a file that may call requireEngine(t) and
// engineguard.AllowRealEngine without being a contract file.
func isContractSupportFile(rel string) bool {
	base := path.Base(rel)
	return base == "contract_support_test.go" || base == "contract_support_internal_test.go"
}

func mayCallAllowRealEngine(rel string) bool {
	return isContractFile(rel) || isContractSupportFile(rel) ||
		strings.HasPrefix(rel, "internal/engineguard/") ||
		strings.HasPrefix(rel, "internal/testutil/enginecontract/")
}

// testingParam returns the identifier of the first *testing.T parameter of fn, ""
// when it has none or it is blank.
func testingParam(fn *ast.FuncDecl) string {
	for _, field := range fn.Type.Params.List {
		star, ok := field.Type.(*ast.StarExpr)
		if !ok {
			continue
		}
		sel, ok := star.X.(*ast.SelectorExpr)
		if !ok || sel.Sel.Name != "T" {
			continue
		}
		if x, ok := sel.X.(*ast.Ident); !ok || x.Name != "testing" {
			continue
		}
		if len(field.Names) == 0 || field.Names[0].Name == "_" {
			return ""
		}
		return field.Names[0].Name
	}
	return ""
}

// isRequireEngineCall reports whether e is requireEngine(<tName>).
func isRequireEngineCall(e ast.Expr, tName string) bool {
	call, ok := e.(*ast.CallExpr)
	if !ok || tName == "" || len(call.Args) != 1 {
		return false
	}
	fun, ok := call.Fun.(*ast.Ident)
	if !ok || fun.Name != "requireEngine" {
		return false
	}
	arg, ok := call.Args[0].(*ast.Ident)
	return ok && arg.Name == tName
}

// firstStatementCall returns the call a block starts with, alone or as the only
// right-hand side of an assignment, nil otherwise.
func firstStatementCall(body *ast.BlockStmt) ast.Expr {
	if body == nil || len(body.List) == 0 {
		return nil
	}
	switch s := body.List[0].(type) {
	case *ast.ExprStmt:
		return s.X
	case *ast.AssignStmt:
		if (s.Tok == token.DEFINE || s.Tok == token.ASSIGN) && len(s.Rhs) == 1 {
			return s.Rhs[0]
		}
	}
	return nil
}

// contractSites parses src as the file rel (a slash path from the module root) and
// returns the sites of R1, R2 and R3, ordered by line. R4 needs the whole package:
// contractPackageSites.
func contractSites(rel string, src []byte) ([]contractSite, error) {
	fset := token.NewFileSet()
	f, err := parser.ParseFile(fset, rel, src, parser.SkipObjectResolution)
	if err != nil {
		return nil, err
	}
	contract := isContractFile(rel)
	guardName := importName(f, engineguardPackage, "engineguard")

	var out []contractSite
	add := func(pos token.Pos, fn, rule, what string) {
		out = append(out, contractSite{File: rel, Line: fset.Position(pos).Line, Func: fn, Rule: rule, What: what})
	}

	for _, decl := range f.Decls {
		label := funcLabel(decl)
		fn, isFunc := decl.(*ast.FuncDecl)

		if isFunc {
			isContractTest := fn.Recv == nil && strings.HasPrefix(fn.Name.Name, "TestContract")
			switch {
			case contract && !isContractTest:
				add(fn.Pos(), label, ruleOnlyContracts, "a contract file declares "+fn.Name.Name+", which is not a TestContract* function")
			case contract:
				if call := firstStatementCall(fn.Body); call == nil || !isRequireEngineCall(call, testingParam(fn)) {
					add(fn.Pos(), label, ruleFirstStatement, fn.Name.Name+" does not start with requireEngine(t)")
				}
			case isContractTest:
				add(fn.Pos(), label, ruleWhereTheyLive, fn.Name.Name+" is a TestContract* function outside a *_contract_test.go file")
			}
		}

		ast.Inspect(decl, func(n ast.Node) bool {
			call, ok := n.(*ast.CallExpr)
			if !ok {
				return true
			}
			switch fun := call.Fun.(type) {
			case *ast.Ident:
				if fun.Name == "requireEngine" && len(call.Args) == 1 && !contract && !isContractSupportFile(rel) {
					add(call.Pos(), label, ruleWhereTheyLive, "requireEngine(...) outside a contract file")
				}
			case *ast.SelectorExpr:
				if fun.Sel.Name == "Parallel" && len(call.Args) == 0 && contract {
					add(call.Pos(), label, ruleOnlyContracts, "a contract calls Parallel")
				}
				if x, ok := fun.X.(*ast.Ident); ok && guardName != "" && guardName != "." && guardName != "_" &&
					x.Name == guardName && fun.Sel.Name == "AllowRealEngine" && !mayCallAllowRealEngine(rel) {
					add(call.Pos(), label, ruleWhereTheyLive, "engineguard.AllowRealEngine outside a contract file")
				}
			}
			return true
		})
	}
	sort.SliceStable(out, func(i, j int) bool { return out[i].Line < out[j].Line })
	return out, nil
}

// contractPackageSites applies R4 to the files of one directory (slash path from
// the module root to source). It returns the sites and how many contract files the
// directory holds; a directory without one has no site.
func contractPackageSites(files map[string][]byte) ([]contractSite, int, error) {
	var names []string
	for rel := range files {
		names = append(names, rel)
	}
	sort.Strings(names)

	contracts := 0
	firstContract := ""
	type definition struct {
		rel  string
		line int
		ok   bool
	}
	var defs []definition
	for _, rel := range names {
		if isContractFile(rel) {
			contracts++
			if firstContract == "" {
				firstContract = rel
			}
		}
		fset := token.NewFileSet()
		f, err := parser.ParseFile(fset, rel, files[rel], parser.SkipObjectResolution)
		if err != nil {
			return nil, 0, err
		}
		contractName := importName(f, enginecontractPackage, "enginecontract")
		for _, decl := range f.Decls {
			fn, ok := decl.(*ast.FuncDecl)
			if !ok || fn.Recv != nil || fn.Name.Name != "requireEngine" {
				continue
			}
			d := definition{rel: rel, line: fset.Position(fn.Pos()).Line}
			if strings.HasSuffix(rel, "_test.go") && contractName != "" && contractName != "." && contractName != "_" {
				if call, ok := firstStatementCall(fn.Body).(*ast.CallExpr); ok {
					if sel, ok := call.Fun.(*ast.SelectorExpr); ok && sel.Sel.Name == "Require" {
						if x, ok := sel.X.(*ast.Ident); ok && x.Name == contractName {
							d.ok = true
						}
					}
				}
			}
			defs = append(defs, d)
		}
	}
	if contracts == 0 {
		return nil, 0, nil
	}
	var out []contractSite
	if len(defs) == 0 {
		out = append(out, contractSite{File: firstContract, Line: 1, Func: "requireEngine", Rule: ruleDefinition,
			What: "the package has contract files and defines no requireEngine"})
	}
	for _, d := range defs {
		if !d.ok {
			out = append(out, contractSite{File: d.rel, Line: d.line, Func: "requireEngine", Rule: ruleDefinition,
				What: "requireEngine is not a _test.go function that starts with enginecontract.Require(...)"})
		}
	}
	return out, contracts, nil
}

// judgeContracts turns the sites found into the problems to report: a site without
// an exception, an exception whose code is gone or without a reason, and a module
// with fewer contract files than the floor.
func judgeContracts(sites []contractSite, contractFiles int, exceptions []contractException) []string {
	var problems []string
	if contractFiles < contractFloor {
		problems = append(problems, fmt.Sprintf(
			"the module holds %d *_contract_test.go file(s), want at least %d: the guard would pass on nothing", contractFiles, contractFloor))
	}
	covered := map[string]bool{}
	for _, e := range exceptions {
		key := e.File + "\x00" + e.Func
		covered[key] = true
		switch {
		case strings.TrimSpace(e.Reason) == "":
			problems = append(problems, fmt.Sprintf("exception %s %s has no reason: say why it may break the contract rules", e.File, e.Func))
		case !slices.ContainsFunc(sites, func(s contractSite) bool { return s.File == e.File && s.Func == e.Func }):
			problems = append(problems, fmt.Sprintf(
				"the exception still exists but its code is gone: %s %s breaks no rule any more; delete it from contractExceptions",
				e.File, e.Func))
		}
	}
	for _, s := range sites {
		if covered[s.File+"\x00"+s.Func] {
			continue
		}
		problems = append(problems, fmt.Sprintf("%s: %s (OC-09a, DEVDESK-806)", s, contractAdvice[s.Rule]))
	}
	return problems
}

// scanContractTree reads every .go file under root and returns the sites, the
// number of files read and the number of contract files.
func scanContractTree(root string) ([]contractSite, int, int, error) {
	var all []contractSite
	files, contracts := 0, 0
	byDir := map[string]map[string][]byte{}
	err := filepath.WalkDir(root, func(p string, d fs.DirEntry, err error) error {
		if err != nil {
			return err
		}
		rel, err := filepath.Rel(root, p)
		if err != nil {
			return err
		}
		rel = filepath.ToSlash(rel)
		if d.IsDir() {
			if rel != "." && (strings.HasPrefix(d.Name(), ".") || slices.Contains(launchGuardExcludedDirs, d.Name())) {
				return filepath.SkipDir
			}
			return nil
		}
		if !strings.HasSuffix(rel, ".go") {
			return nil
		}
		src, err := os.ReadFile(p)
		if err != nil {
			return err
		}
		sites, err := contractSites(rel, src)
		if err != nil {
			return fmt.Errorf("%s: %w", rel, err)
		}
		files++
		all = append(all, sites...)
		dir := path.Dir(rel)
		if byDir[dir] == nil {
			byDir[dir] = map[string][]byte{}
		}
		byDir[dir][rel] = src
		return nil
	})
	if err != nil {
		return nil, 0, 0, err
	}
	dirs := make([]string, 0, len(byDir))
	for dir := range byDir {
		dirs = append(dirs, dir)
	}
	sort.Strings(dirs)
	for _, dir := range dirs {
		sites, n, err := contractPackageSites(byDir[dir])
		if err != nil {
			return nil, 0, 0, fmt.Errorf("%s: %w", dir, err)
		}
		contracts += n
		all = append(all, sites...)
	}
	return all, files, contracts, nil
}

var (
	contractScanOnce      sync.Once
	contractScanSites     []contractSite
	contractScanFiles     int
	contractScanContracts int
	contractScanErr       error
)

// Criterion 4 on the real module: every contract starts with requireEngine(t).
func TestGuardContractTestsCallRequireEngine(t *testing.T) {
	contractScanOnce.Do(func() {
		root, err := launchModuleRoot()
		if err != nil {
			contractScanErr = err
			return
		}
		contractScanSites, contractScanFiles, contractScanContracts, contractScanErr = scanContractTree(root)
	})
	if contractScanErr != nil {
		t.Fatalf("scanning the module for contract tests: %v", contractScanErr)
	}
	if contractScanFiles < contractWalkMinimum {
		t.Fatalf("the contract guard read %d files, want the whole module (at least %d)", contractScanFiles, contractWalkMinimum)
	}
	for _, p := range judgeContracts(contractScanSites, contractScanContracts, contractExceptions) {
		t.Error(p)
	}
}

// ── The detector on sources ──────────────────────────────────────────────────
//
// The guard is only worth something if it sees a violation: each rule is run on
// sources that break it, and on sources that look like a breach and are not.

func TestG5OnSources(t *testing.T) {
	const (
		contractFile = "internal/x/engine_contract_test.go"
		otherTest    = "internal/x/foo_test.go"
		head         = "package x\nimport \"testing\"\n"
	)
	tests := []struct {
		name string
		file string
		src  string
		want []string // "line rule", in line order
	}{
		// R1
		{"assigned, first statement", contractFile, head + "func TestContractA(t *testing.T) {\n\tc := requireEngine(t)\n\t_ = c\n}\n", nil},
		{"alone, first statement", contractFile, head + "func TestContractA(t *testing.T) {\n\trequireEngine(t)\n}\n", nil},
		{"an assignment with =", contractFile, head + "func TestContractA(t *testing.T) {\n\tc = requireEngine(t)\n}\n", nil},
		{"a log before it", contractFile, head + "func TestContractA(t *testing.T) {\n\tt.Log(\"x\")\n\trequireEngine(t)\n}\n", []string{"3 R1"}},
		{"nested in an if", contractFile, head + "func TestContractA(t *testing.T) {\n\tif true {\n\t\trequireEngine(t)\n\t}\n}\n", []string{"3 R1"}},
		{"nested in a closure", contractFile, head + "func TestContractA(t *testing.T) {\n\tfunc() { requireEngine(t) }()\n}\n", []string{"3 R1"}},
		{"a method of that name", contractFile, head + "func TestContractA(t *testing.T) {\n\tx.requireEngine(t)\n}\n", []string{"3 R1"}},
		{"nil instead of t", contractFile, head + "func TestContractA(t *testing.T) {\n\trequireEngine(nil)\n}\n", []string{"3 R1"}},
		{"a second argument", contractFile, head + "func TestContractA(t *testing.T) {\n\trequireEngine(t, 1)\n}\n", []string{"3 R1"}},
		{"another name for the parameter", contractFile, head + "func TestContractA(tt *testing.T) {\n\trequireEngine(tt)\n}\n", nil},
		{"the wrong identifier for that parameter", contractFile, head + "func TestContractA(tt *testing.T) {\n\trequireEngine(t)\n}\n", []string{"3 R1"}},
		{"a blank parameter", contractFile, head + "func TestContractA(_ *testing.T) {\n\trequireEngine(nil)\n}\n", []string{"3 R1"}},
		{"an empty body", contractFile, head + "func TestContractA(t *testing.T) {\n}\n", []string{"3 R1"}},
		{"another call first", contractFile, head + "func TestContractA(t *testing.T) {\n\tengineguard.AllowRealEngine(t)\n\trequireEngine(t)\n}\n", []string{"3 R1"}},
		{"two contracts, one wrong", contractFile, head + "func TestContractA(t *testing.T) {\n\trequireEngine(t)\n}\nfunc TestContractB(t *testing.T) {\n\tt.Skip()\n}\n", []string{"6 R1"}},

		// R2
		{"a helper in a contract file", contractFile, head + "func helper() {}\n", []string{"3 R2"}},
		{"a Test function without Contract", contractFile, head + "func TestFoo(t *testing.T) {\n\trequireEngine(t)\n}\n", []string{"3 R2"}},
		{"a method in a contract file", contractFile, head + "func (c *contract) x() {}\n", []string{"3 R2"}},
		{"TestMain in a contract file", contractFile, head + "func TestMain(m *testing.M) {}\n", []string{"3 R2"}},
		{"Parallel in a contract", contractFile, head + "func TestContractA(t *testing.T) {\n\trequireEngine(t)\n\tt.Parallel()\n}\n", []string{"5 R2"}},
		{"Parallel on a subtest", contractFile, head + "func TestContractA(t *testing.T) {\n\trequireEngine(t)\n\tt.Run(\"x\", func(t *testing.T) { t.Parallel() })\n}\n", []string{"5 R2"}},
		{"a type in a contract file is not read", contractFile, head + "type x struct{}\n", nil},

		// R3
		{"a contract outside a contract file", otherTest, head + "func TestContractX(t *testing.T) {\n\trequireEngine(t)\n}\n", []string{"3 R3", "4 R3"}},
		{"a contract in a non-test file", "internal/x/foo.go", "package x\nfunc TestContractX() {}\n", []string{"2 R3"}},
		{"requireEngine(t) in another test file", otherTest, head + "func TestA(t *testing.T) {\n\trequireEngine(t)\n}\n", []string{"4 R3"}},
		{"requireEngine(t) in the support file", "internal/x/contract_support_test.go", head + "func f(t *testing.T) {\n\trequireEngine(t)\n}\n", nil},
		{"requireEngine(t) in the internal support file", "internal/x/contract_support_internal_test.go", head + "func TestA(t *testing.T) {\n\trequireEngine(t)\n}\n", nil},
		{"the three-argument requireEngine of internal/ui/netdiag", otherTest, head + "func TestA(t *testing.T) {\n\trequireEngine(t, pm, \"x\")\n}\n", nil},
		{"the definition of a requireEngine is not a call", otherTest, head + "func requireEngine(t *testing.T, pm int, want string) {}\n", nil},
		{"AllowRealEngine in another test file", otherTest, head + "import \"github.com/anthnel/devdesk/internal/engineguard\"\nfunc TestA(t *testing.T) {\n\tengineguard.AllowRealEngine(t)\n}\n", []string{"5 R3"}},
		{"AllowRealEngine under an alias", otherTest, "package x\nimport eg \"github.com/anthnel/devdesk/internal/engineguard\"\nfunc f(t any) {\n\teg.AllowRealEngine(t)\n}\n", []string{"4 R3"}},
		{"AllowRealEngine in a non-test file", "internal/x/foo.go", "package x\nimport \"github.com/anthnel/devdesk/internal/engineguard\"\nfunc f(t any) {\n\tengineguard.AllowRealEngine(t)\n}\n", []string{"4 R3"}},
		{"AllowRealEngine in the support file", "internal/x/contract_support_test.go", "package x\nimport \"github.com/anthnel/devdesk/internal/engineguard\"\nfunc f(t any) {\n\tengineguard.AllowRealEngine(t)\n}\n", nil},
		{"AllowRealEngine in internal/engineguard", "internal/engineguard/x_test.go", "package engineguard_test\nimport \"github.com/anthnel/devdesk/internal/engineguard\"\nfunc f(t any) {\n\tengineguard.AllowRealEngine(t)\n}\n", nil},
		{"AllowRealEngine in the helpers of the contracts", "internal/testutil/enginecontract/require.go", "package enginecontract\nimport \"github.com/anthnel/devdesk/internal/engineguard\"\nfunc Require(t any) {\n\tengineguard.AllowRealEngine(t)\n}\n", nil},
		{"AllowRealEngine in a sibling of the helpers", "internal/testutil/enginecontract2/require.go", "package x\nimport \"github.com/anthnel/devdesk/internal/engineguard\"\nfunc Require(t any) {\n\tengineguard.AllowRealEngine(t)\n}\n", []string{"4 R3"}},
		{"another engineguard function", otherTest, "package x\nimport \"github.com/anthnel/devdesk/internal/engineguard\"\nfunc f(t any) {\n\tengineguard.Isolate(t)\n}\n", nil},
		{"a package called engineguard that is not ours", otherTest, "package x\nimport \"example.com/other/engineguard\"\nfunc f(t any) {\n\tengineguard.AllowRealEngine(t)\n}\n", nil},
	}
	for _, tt := range tests {
		t.Run(tt.name, func(t *testing.T) {
			sites, err := contractSites(tt.file, []byte(tt.src))
			if err != nil {
				t.Fatalf("contractSites: %v", err)
			}
			var got []string
			for _, s := range sites {
				got = append(got, fmt.Sprintf("%d %s", s.Line, s.Rule))
				if s.File != tt.file {
					t.Errorf("site names %q, want %q", s.File, tt.file)
				}
			}
			if !slices.Equal(got, tt.want) {
				t.Errorf("sites = %v, want %v\n%v", got, tt.want, sites)
			}
		})
	}
}

// R4 needs the whole directory.
func TestG5OnPackages(t *testing.T) {
	const (
		contract    = "internal/x/engine_contract_test.go"
		support     = "internal/x/contract_support_test.go"
		head        = "package x\nimport (\n\t\"testing\"\n\t\"github.com/anthnel/devdesk/internal/testutil/enginecontract\"\n)\n"
		goodDef     = head + "func requireEngine(t *testing.T) *contract {\n\ts := enginecontract.Require(t, probeEngine)\n\t_ = s\n\treturn nil\n}\n"
		contractSrc = "package x\nimport \"testing\"\nfunc TestContractA(t *testing.T) {\n\trequireEngine(t)\n}\n"
	)
	tests := []struct {
		name      string
		files     map[string]string
		want      []string // "file:line"
		contracts int
	}{
		{"a contract and a good definition", map[string]string{contract: contractSrc, support: goodDef}, nil, 1},
		{"the call as an expression statement", map[string]string{contract: contractSrc,
			support: head + "func requireEngine(t *testing.T) {\n\tenginecontract.Require(t, probeEngine)\n}\n"}, nil, 1},
		{"the helper package under an alias", map[string]string{contract: contractSrc,
			support: "package x\nimport (\n\t\"testing\"\n\tec \"github.com/anthnel/devdesk/internal/testutil/enginecontract\"\n)\nfunc requireEngine(t *testing.T) {\n\tec.Require(t, probeEngine)\n}\n"}, nil, 1},
		{"an empty requireEngine", map[string]string{contract: contractSrc,
			support: "package x\nimport \"testing\"\nfunc requireEngine(t *testing.T) {}\n"},
			[]string{support + ":3"}, 1},
		{"Require is not the first statement", map[string]string{contract: contractSrc,
			support: head + "func requireEngine(t *testing.T) {\n\tt.Helper()\n\tenginecontract.Require(t, probeEngine)\n}\n"},
			[]string{support + ":6"}, 1},
		{"another function than Require", map[string]string{contract: contractSrc,
			support: head + "func requireEngine(t *testing.T) {\n\tenginecontract.Decide(nil)\n}\n"},
			[]string{support + ":6"}, 1},
		{"a contract and no requireEngine at all", map[string]string{contract: contractSrc}, []string{contract + ":1"}, 1},
		{"a method named requireEngine is not the helper", map[string]string{contract: contractSrc,
			support: head + "func (c *contract) requireEngine(t *testing.T) {\n\tenginecontract.Require(t, probeEngine)\n}\n"},
			[]string{contract + ":1"}, 1},
		{"a definition in a non-test file", map[string]string{contract: contractSrc,
			"internal/x/engine.go": head + "func requireEngine(t *testing.T) {\n\tenginecontract.Require(t, probeEngine)\n}\n"},
			[]string{"internal/x/engine.go:6"}, 1},
		{"no contract file: nothing to say, whatever requireEngine does",
			map[string]string{support: "package x\nimport \"testing\"\nfunc requireEngine(t *testing.T) {}\n"}, nil, 0},
	}
	for _, tt := range tests {
		t.Run(tt.name, func(t *testing.T) {
			files := map[string][]byte{}
			for name, src := range tt.files {
				files[name] = []byte(src)
			}
			sites, n, err := contractPackageSites(files)
			if err != nil {
				t.Fatalf("contractPackageSites: %v", err)
			}
			var got []string
			for _, s := range sites {
				got = append(got, fmt.Sprintf("%s:%d", s.File, s.Line))
				if s.Rule != ruleDefinition {
					t.Errorf("site %v has rule %s, want %s", s, s.Rule, ruleDefinition)
				}
			}
			if !slices.Equal(got, tt.want) {
				t.Errorf("sites = %v, want %v", got, tt.want)
			}
			if n != tt.contracts {
				t.Errorf("contract files = %d, want %d", n, tt.contracts)
			}
		})
	}
}

// The judgement, on sites and exceptions built by hand.
func TestG5Judgement(t *testing.T) {
	site := contractSite{File: "internal/x/a_contract_test.go", Line: 7, Func: "TestContractA", Rule: ruleFirstStatement, What: "w"}

	tests := []struct {
		name          string
		sites         []contractSite
		contractFiles int
		exceptions    []contractException
		want          []string // substrings, one per problem, in order; nil: no problem
	}{
		{"a clean module", nil, 1, nil, nil},
		{"no contract file: the guard would pass on nothing", nil, 0, nil, []string{"at least 1"}},
		{"a site without an exception names file, line and rule", []contractSite{site}, 1, nil,
			[]string{"internal/x/a_contract_test.go:7: R1"}},
		{"a site with its exception", []contractSite{site}, 1,
			[]contractException{{File: site.File, Func: site.Func, Reason: "a reason"}}, nil},
		{"an exception for another function does not cover it", []contractSite{site}, 1,
			[]contractException{{File: site.File, Func: "TestContractB", Reason: "a reason"}},
			[]string{"code is gone", "R1"}},
		{"an exception whose code is gone", nil, 1,
			[]contractException{{File: "internal/y/a_contract_test.go", Func: "TestContractZ", Reason: "a reason"}},
			[]string{"code is gone"}},
		{"an exception without a reason", []contractSite{site}, 1,
			[]contractException{{File: site.File, Func: site.Func, Reason: "  "}},
			[]string{"no reason"}},
	}
	for _, tt := range tests {
		t.Run(tt.name, func(t *testing.T) {
			got := judgeContracts(tt.sites, tt.contractFiles, tt.exceptions)

			if len(got) != len(tt.want) {
				t.Fatalf("problems = %q, want %d containing %q", got, len(tt.want), tt.want)
			}
			for i, w := range tt.want {
				if !strings.Contains(got[i], w) {
					t.Errorf("problem %d = %q, want it to contain %q", i, got[i], w)
				}
			}
		})
	}
}
