package engineguard_test

import (
	"fmt"
	"go/ast"
	"go/parser"
	"go/token"
	"regexp"
	"slices"
	"sort"
	"strings"
	"sync"
	"testing"
)

// DEVDESK-945, requirement S9: no secret goes in an argv. A command line is
// readable by every local user (`ps`, /proc/<pid>/cmdline) and ends up in audit
// logs. A secret travels in the child's environment (`-e NAME` in argv, the
// `NAME=value` entry in cmd.Env), on stdin (`--password-stdin`) or in a file.
//
// A static test in the vein of G2, G3 and G8 (it reuses their helpers: importName,
// constString, fileConsts, funcLabel, execCallee, argvCallees, calleeName,
// isStringSlice, scanDestructiveTree, launchModuleRoot, launchGuardMinimum). It
// reads every non-test .go file of the module with go/parser, no type
// information, and reports, with file and line, each word list that puts a
// dynamic value where a secret goes:
//
//	env flag    `-e`, `--env` or `-env` followed by a word that has an `=` and a
//	            part that is not a constant: `"-e", name + "=" + token`, the shape
//	            of the plumber bug, or a call that spells one
//	            (`fmt.Sprintf("%s=%s", n, v)`, `strings.Join(..., "=")`: the literal
//	            texts of its arguments are read). `"-e", name` is the good form
//	            and is not seen.
//	env option  a word `--env=...`, `--password=...`, `--token=...`, `--secret=...`,
//	            `--api-key=...`, `--creds=...`, `--src-creds=...`,
//	            `--dest-creds=...` or `--build-arg=K=...` with a part that is not a
//	            constant.
//	secret flag `--password`, `--passwd`, `--token`, `--secret`, `--api-key`,
//	            `--registry-password`, `--registry-token`, `--creds`,
//	            `--src-creds` or `--dest-creds` (skopeo) followed by a word that is
//	            not a constant (`--password-stdin` is another word).
//	build arg   `--build-arg` followed by `K=` and a part that is not a constant.
//	user:secret `-u` (curl) followed by a word with a `:` then a part that is not
//	            a constant (`-u user:<dynamic>`); `-u user` alone is not seen, it
//	            is how `docker login` names its user.
//	URL         any word of the list holding `scheme://` then a part that is not a
//	            constant before the `@` (credentials in the URL).
//	header      `-H` or `--header` followed by a word with an Authorization,
//	            token, bearer, cookie or api-key part and a part that is not a
//	            constant, or by a word with nothing constant at all (a header held
//	            whole in a variable: nothing says it is not a secret, and the real
//	            module has none).
//	login -p    a list that holds the word `login`, then `-p` followed by a word
//	            that is not a constant (`-p` is a port everywhere else).
//
// A "word list" is a []string literal, the arguments of an append after its first
// one, the arguments of exec.Command(Context) after the binary, and the arguments
// of the argv helpers G3 knows (argvCallees). A constant is a literal, a constant of
// the same file, or a concatenation of those.
//
// Limits, stated here and not hidden:
//
//   - A bare variable after `-e` (`"-e", kv`) is not seen: nothing says whether
//     it holds `NAME` or `NAME=value`; nor is a call whose literal arguments hold
//     no `=` (`"-e", join(name, value)`): the guard reads the text it can see. The
//     same goes for an argv assembled word by word across two statements, a flag
//     read from a variable, a constant defined in another file (it is dynamic
//     here, so it is reported, and a named exception says why it is harmless), a
//     shell line (`sh -c "... --password x"`), a script, a secret passed as a bare
//     positional argument (`--src-creds` and the like only when written as a
//     flag), a `-u` whose value has no `:` in the text the guard can read, and a URL built by a call whose literal arguments hold no `scheme://`.
//   - The environment is not checked: `cmd.Env = append(env, "TOKEN="+t)` is the
//     good form, and a word list is not told apart from an environment list by
//     syntax alone (`[]string{"A=" + v}`); only the flags above make it an argv.
//   - Test files are not scanned.

// secretFlags are the flags whose next word is a secret.
var secretFlags = []string{
	"--password", "--passwd", "--token", "--secret", "--api-key", "--registry-password", "--registry-token",
	"--creds", "--src-creds", "--dest-creds",
}

var (
	envFlags       = []string{"-e", "--env", "-env"}
	headerFlags    = []string{"-H", "--header"}
	secretOption   = regexp.MustCompile(`(?i)^--?(env|password|passwd|token|secret|api[-_]?key|registry-password|registry-token|creds|src-creds|dest-creds|build-arg=[^=]+)=`)
	urlCredentials = regexp.MustCompile(`[A-Za-z][A-Za-z0-9+.-]*://[^/?#@]*\x00[^/?#@]*@`)
	userSecret     = regexp.MustCompile(`:.*\x00`)
	secretHeader   = regexp.MustCompile(`(?i)(authorization|token|bearer|cookie|api[-_]?key)`)
	dynamicMarker  = "\x00"
	secretLoginCmd = "login"
)

const (
	secretRuleEnvFlag   = "env flag with NAME=value"
	secretRuleOption    = "secret option with a value"
	secretRuleFlag      = "secret flag with a value"
	secretRuleHeader    = "secret header with a value"
	secretRuleLoginPass = "login -p with a value"
	secretRuleBuildArg  = "build arg with a value"
	secretRuleUserPass  = "user:secret with a value"
	secretRuleURL       = "credentials in a URL"
)

// secretException is a function that builds such a word and why it is harmless.
// Same rules as the other guards: keyed by file and function, Sites is the exact
// count, a debt register that does not grow for convenience, an entry whose code
// is gone, whose count differs or without a reason fails the test.
type secretException struct {
	File   string // slash path from the module root
	Func   string // see funcLabel
	Sites  int
	Reason string
}

// The register is empty: the plumber site it used to hold (`GIT_CONFIG_VALUE_0=` +
// containerScanPath) is now a literal, which the guard reads as a constant.
var secretExceptions = []secretException{}

// secretSite is one word list that puts a dynamic value where a secret goes.
type secretSite struct {
	File string
	Line int
	Func string
	Rule string
	What string
}

func (s secretSite) String() string {
	return fmt.Sprintf("%s:%d: %s: %s (in %s)", s.File, s.Line, s.Rule, s.What, s.Func)
}

// ── Detector ─────────────────────────────────────────────────────────────────

// wordShape folds e into its text with each part that is not a constant replaced
// by dynamicMarker, and reports whether there was one.
func wordShape(e ast.Expr, consts map[string]string) (string, bool) {
	if s, ok := constString(e, consts, nil); ok {
		return s, false
	}
	switch e := e.(type) {
	case *ast.ParenExpr:
		return wordShape(e.X, consts)
	case *ast.BinaryExpr:
		if e.Op == token.ADD {
			l, ld := wordShape(e.X, consts)
			r, rd := wordShape(e.Y, consts)
			return l + r, ld || rd
		}
	case *ast.CallExpr:
		// A call is dynamic, and the literal texts of its arguments are all that
		// can be read of it: fmt.Sprintf("%s=%s", ...), strings.Join(x, "=").
		return dynamicMarker + callLiterals(e, consts), true
	}
	return dynamicMarker, true
}

// callLiterals joins the constant strings found in the arguments of c, nested
// calls and composite literals included, each after a marker.
func callLiterals(c *ast.CallExpr, consts map[string]string) string {
	var b strings.Builder
	var walk func(e ast.Expr)
	walk = func(e ast.Expr) {
		if s, ok := constString(e, consts, nil); ok {
			b.WriteString(s)
			return
		}
		switch e := e.(type) {
		case *ast.CallExpr:
			for _, a := range e.Args {
				walk(a)
			}
		case *ast.CompositeLit:
			for _, a := range e.Elts {
				walk(a)
			}
		case *ast.BinaryExpr:
			walk(e.X)
			walk(e.Y)
		case *ast.ParenExpr:
			walk(e.X)
		default:
			b.WriteString(dynamicMarker)
		}
	}
	for _, a := range c.Args {
		walk(a)
	}
	return b.String()
}

// judgeWords returns the problems of one word list, as (rule, what) pairs.
func judgeWords(words []ast.Expr, consts map[string]string) [][2]string {
	var out [][2]string
	login := false
	for _, w := range words {
		if s, ok := constString(w, consts, nil); ok && s == secretLoginCmd {
			login = true
		}
	}
	prev := ""
	for _, w := range words {
		shape, dynamic := wordShape(w, consts)
		switch {
		case !dynamic:
			prev = shape
			continue
		case slices.Contains(envFlags, prev) && strings.Contains(shape, "="):
			out = append(out, [2]string{secretRuleEnvFlag, fmt.Sprintf("%s %q", prev, shape)})
		case prev == "--build-arg" && strings.Contains(shape, "="):
			out = append(out, [2]string{secretRuleBuildArg, fmt.Sprintf("--build-arg %q", shape)})
		case prev == "-u" && userSecret.MatchString(shape):
			out = append(out, [2]string{secretRuleUserPass, fmt.Sprintf("-u %q", shape)})
		case urlCredentials.MatchString(shape):
			out = append(out, [2]string{secretRuleURL, fmt.Sprintf("%q", shape)})
		case secretOption.MatchString(shape):
			out = append(out, [2]string{secretRuleOption, fmt.Sprintf("%q", shape)})
		case slices.Contains(secretFlags, prev):
			out = append(out, [2]string{secretRuleFlag, fmt.Sprintf("%s %q", prev, shape)})
		case slices.Contains(headerFlags, prev) && (secretHeader.MatchString(shape) || shape == dynamicMarker):
			out = append(out, [2]string{secretRuleHeader, fmt.Sprintf("%s %q", prev, shape)})
		case login && prev == "-p":
			out = append(out, [2]string{secretRuleLoginPass, fmt.Sprintf("-p %q", shape)})
		}
		prev = ""
	}
	return out
}

// scanSecretArgv parses src as the file name (a slash path from the module root)
// and returns its sites ordered by line. A _test.go file has none.
func scanSecretArgv(name string, src []byte) ([]secretSite, error) {
	fset := token.NewFileSet()
	f, err := parser.ParseFile(fset, name, src, parser.SkipObjectResolution)
	if err != nil {
		return nil, err
	}
	if strings.HasSuffix(name, "_test.go") {
		return nil, nil
	}

	execName := importName(f, "os/exec", "exec")
	consts := fileConsts(f, nil)

	var out []secretSite
	for _, decl := range f.Decls {
		label := funcLabel(decl)
		report := func(pos token.Pos, words []ast.Expr) {
			for _, p := range judgeWords(words, consts) {
				out = append(out, secretSite{File: name, Line: fset.Position(pos).Line, Func: label, Rule: p[0], What: p[1]})
			}
		}
		ast.Inspect(decl, func(n ast.Node) bool {
			switch n := n.(type) {
			case *ast.CompositeLit:
				if isStringSlice(n.Type) {
					report(n.Pos(), n.Elts)
				}
			case *ast.CallExpr:
				callee := calleeName(n.Fun)
				fn, binIdx := execCallee(n.Fun, execName)
				start := -1
				switch {
				case fn != "":
					start = binIdx + 1
				case callee == "append":
					start = 1
				case contains(argvCallees, callee):
					start = 0
				}
				if start >= 0 && start < len(n.Args) {
					report(n.Pos(), n.Args[start:])
				}
			}
			return true
		})
	}
	sort.SliceStable(out, func(i, j int) bool { return out[i].Line < out[j].Line })
	return out, nil
}

// ── Judgement ────────────────────────────────────────────────────────────────

// judgeSecretArgv turns the sites found into the problems to report: a site
// without an exception, an exception whose count is not the number of sites, an
// exception whose code is gone or without a reason.
func judgeSecretArgv(found []secretSite, exceptions []secretException) []string {
	var problems []string
	byFunc := map[string][]secretSite{}
	for _, s := range found {
		byFunc[s.File+"\x00"+s.Func] = append(byFunc[s.File+"\x00"+s.Func], s)
	}

	covered := map[string]bool{}
	for _, e := range exceptions {
		key := e.File + "\x00" + e.Func
		covered[key] = true
		sites := byFunc[key]
		switch {
		case strings.TrimSpace(e.Reason) == "":
			problems = append(problems, fmt.Sprintf("exception %s %s has no reason: say why that value is not a secret", e.File, e.Func))
		case len(sites) == 0:
			problems = append(problems, fmt.Sprintf(
				"the exception still exists but its code is gone: %s %s has no site left; delete it from secretExceptions",
				e.File, e.Func))
		case len(sites) != e.Sites:
			problems = append(problems, fmt.Sprintf(
				"exception %s %s covers %d site(s) but the function has %d: %s",
				e.File, e.Func, e.Sites, len(sites), joinSecretSites(sites)))
		}
	}

	keys := make([]string, 0, len(byFunc))
	for k := range byFunc {
		if !covered[k] {
			keys = append(keys, k)
		}
	}
	sort.Strings(keys)
	for _, k := range keys {
		for _, s := range byFunc[k] {
			problems = append(problems, fmt.Sprintf(
				"%s: a secret in argv is readable by every local user (`ps`, /proc/<pid>/cmdline) and ends up in audit "+
					"logs; pass `-e NAME` and put NAME=value in the child's environment, or use stdin or a file "+
					"(S9, DEVDESK-945)", s))
		}
	}
	return problems
}

func joinSecretSites(sites []secretSite) string {
	parts := make([]string, len(sites))
	for i, s := range sites {
		parts[i] = fmt.Sprintf("%s:%d", s.File, s.Line)
	}
	return strings.Join(parts, ", ")
}

// ── The module ───────────────────────────────────────────────────────────────

var (
	secretScanOnce  sync.Once
	secretScanSites []secretSite
	secretScanFiles int
	secretScanErr   error
)

func scanSecretRepo(t *testing.T) []secretSite {
	t.Helper()
	secretScanOnce.Do(func() {
		root, err := launchModuleRoot()
		if err != nil {
			secretScanErr = err
			return
		}
		secretScanSites, secretScanFiles, secretScanErr = scanDestructiveTree(root, scanSecretArgv)
	})
	if secretScanErr != nil {
		t.Fatalf("scanning the module for secrets in argv: %v", secretScanErr)
	}
	if secretScanFiles < launchGuardMinimum {
		t.Fatalf("the secret argv guard read %d files, want the whole module (at least %d)", secretScanFiles, launchGuardMinimum)
	}
	return secretScanSites
}

// On the real module: no secret in an argv outside the named exceptions, and an
// exception whose code is gone fails.
func TestNoSecretIsPutInAnArgv(t *testing.T) {
	for _, p := range judgeSecretArgv(scanSecretRepo(t), secretExceptions) {
		t.Error(p)
	}
}

// The detector sees each shape, and only the bad one: the plumber bug of
// DEVDESK-945 is the first case, its fix the second.
func TestTheSecretArgvDetectorSeesEachShape(t *testing.T) {
	for _, tc := range []struct {
		name string
		body string
		rule string // "" for a clean source
	}{
		{"the plumber bug", `[]string{"-e", name + "=" + opts.Token}`, secretRuleEnvFlag},
		{"env flag appended", `append(args, "-e", "TOKEN="+tok)`, secretRuleEnvFlag},
		{"long env flag", `[]string{"run", "--env", k + "=" + v}`, secretRuleEnvFlag},
		{"env option", `[]string{"run", "--env=TOKEN=" + tok}`, secretRuleOption},
		{"password option", `[]string{"login", "--password=" + pass}`, secretRuleOption},
		{"password flag", `exec.Command("tool", "--password", pass)`, secretRuleFlag},
		{"token flag", `exec.CommandContext(ctx, "tool", "--token", tok)`, secretRuleFlag},
		{"authorization header", `[]string{"-H", "Authorization: Bearer " + tok}`, secretRuleHeader},
		{"login -p", `[]string{"login", "-u", user, "-p", pass}`, secretRuleLoginPass},
		{"skopeo creds flag", `[]string{"copy", "--src-creds", user + ":" + pass}`, secretRuleFlag},
		{"skopeo dest creds flag", `[]string{"copy", "--dest-creds", creds}`, secretRuleFlag},
		{"skopeo creds option", `[]string{"copy", "--creds=" + user + ":" + pass}`, secretRuleOption},
		{"build arg", `[]string{"build", "--build-arg", "TOKEN=" + tok}`, secretRuleBuildArg},
		{"build arg option", `[]string{"build", "--build-arg=TOKEN=" + tok}`, secretRuleOption},
		{"curl user", `[]string{"-u", "me:" + pass, url}`, secretRuleUserPass},
		{"sprintf after -e", `[]string{"-e", fmt.Sprintf("%s=%s", name, tok)}`, secretRuleEnvFlag},
		{"join after -e", `[]string{"-e", strings.Join([]string{name, tok}, "=")}`, secretRuleEnvFlag},
		{"sprintf after --env", `append(args, "--env", fmt.Sprintf("TOKEN=%s", tok))`, secretRuleEnvFlag},
		{"credentials in a URL", `[]string{"clone", "https://" + user + ":" + tok + "@" + host + "/x.git"}`, secretRuleURL},
		{"sprintf header", `[]string{"-H", fmt.Sprintf("Authorization: Bearer %s", tok)}`, secretRuleHeader},
		{"header held whole", `[]string{"-H", header}`, secretRuleHeader},

		{"the fix, name only", `[]string{"-e", name}`, ""},
		{"a constant assignment", `[]string{"-e", "GIT_CONFIG_COUNT=1"}`, ""},
		{"a constant of the file", `[]string{"-e", "A=" + fixed}`, ""},
		{"stdin password", `[]string{"login", "-u", user, "--password-stdin"}`, ""},
		{"a port", `[]string{"run", "-p", port + ":80"}`, ""},
		{"a header without a secret", `[]string{"-H", "Accept: " + mediaType}`, ""},
		{"a call that is a name", `[]string{"-e", strings.ToUpper(name)}`, ""},
		{"a name from a call", `[]string{"-e", nameOf(kind)}`, ""},
		{"a constant creds flag", `[]string{"copy", "--src-no-creds"}`, ""},
		{"a build arg that is a constant", `[]string{"build", "--build-arg", "VERSION=1"}`, ""},
		{"a build arg that is a name", `[]string{"build", "--build-arg", name}`, ""},
		{"docker login user", `[]string{"login", reg, "-u", user, "--password-stdin"}`, ""},
		{"curl user without a secret", `[]string{"-u", user, url}`, ""},
		{"a URL without credentials", `[]string{"clone", "https://" + host + "/" + path}`, ""},
		{"a URL with a constant user", `[]string{"clone", "ssh://git@" + host + "/x.git"}`, ""},
		{"a constant header", `[]string{"-H", "Accept: application/json"}`, ""},
	} {
		t.Run(tc.name, func(t *testing.T) {
			src := "package p\nimport \"os/exec\"\nconst fixed = \"1\"\nvar _ = exec.Command\nfunc f() { _ = " + tc.body + " }\n"
			sites, err := scanSecretArgv("internal/p/p.go", []byte(src))
			if err != nil {
				t.Fatal(err)
			}
			switch {
			case tc.rule == "" && len(sites) != 0:
				t.Errorf("a clean word list was reported: %v", sites)
			case tc.rule != "" && (len(sites) != 1 || sites[0].Rule != tc.rule):
				t.Errorf("sites = %v, want one %q", sites, tc.rule)
			}
		})
	}
}

// A secret put in an argv anywhere in the module, here in memory next to the
// module's own sites, fails the guard and names where.
func TestASecretAddedToAnArgvFailsTheGuardOnTheRealModule(t *testing.T) {
	module := scanSecretRepo(t)
	if got := judgeSecretArgv(module, secretExceptions); len(got) != 0 {
		t.Skipf("the module itself is not clean, TestNoSecretIsPutInAnArgv says why: %v", got)
	}
	file := "internal/scan/added.go"
	src := "package scan\nfunc f(tok string) []string {\n\treturn []string{\"-e\", \"GH_TOKEN=\" + tok}\n}\n"
	sites, err := scanSecretArgv(file, []byte(src))
	if err != nil {
		t.Fatal(err)
	}
	got := judgeSecretArgv(append(slices.Clone(module), sites...), secretExceptions)
	if len(got) == 0 {
		t.Fatal("a secret added to an argv did not fail the guard")
	}
	if !strings.Contains(strings.Join(got, "\n"), file+":3") {
		t.Errorf("problems = %q, want one that names %s:3", got, file)
	}
}

// The register is held to its own rules: a reason, a count, and code that is
// still there.
func TestTheSecretExceptionsAreHeldToTheirRules(t *testing.T) {
	site := secretSite{File: "internal/x/x.go", Func: "f", Rule: secretRuleEnvFlag, Line: 4}
	for _, tc := range []struct {
		name  string
		found []secretSite
		ex    []secretException
		want  string
	}{
		{"no reason", []secretSite{site}, []secretException{{File: site.File, Func: "f", Sites: 1}}, "has no reason"},
		{"code gone", nil, []secretException{{File: site.File, Func: "f", Sites: 1, Reason: "r"}}, "its code is gone"},
		{"count differs", []secretSite{site, site}, []secretException{{File: site.File, Func: "f", Sites: 1, Reason: "r"}}, "covers 1 site(s) but the function has 2"},
		{"no exception", []secretSite{site}, nil, "readable by every local user"},
	} {
		t.Run(tc.name, func(t *testing.T) {
			got := strings.Join(judgeSecretArgv(tc.found, tc.ex), "\n")
			if !strings.Contains(got, tc.want) {
				t.Errorf("problems = %q, want one with %q", got, tc.want)
			}
		})
	}
}
