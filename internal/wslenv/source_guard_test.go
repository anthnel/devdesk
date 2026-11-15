package wslenv

import (
	"fmt"
	"go/ast"
	"go/parser"
	"go/token"
	"os"
	"sort"
	"strconv"
	"strings"
	"testing"
)

// The detection reads files and runs one fixed program. It must never reach
// the network, and the only child process is wslinfo, started from wslinfo.go
// by absolute path. The guard reads every non-test .go file whatever its build
// constraints (go/parser does not evaluate them).

const wslinfoFile = "wslinfo.go"

// networkImports are packages that must not be imported at all.
var networkImports = map[string]bool{
	"net/http": true, "net/rpc": true, "net/smtp": true, "net/textproto": true,
	"net/http/httputil": true, "crypto/tls": true,
	"github.com/libp2p/go-netroute": true,
}

// bannedNetSymbols are the symbols of "net" that open a connection or resolve
// a name. Interface enumeration (net.Interfaces) stays allowed.
var bannedNetSymbols = map[string]bool{
	"Dial": true, "DialTimeout": true, "Dialer": true, "Listen": true, "ListenPacket": true,
	"ListenConfig": true, "ListenTCP": true, "ListenUDP": true, "DialTCP": true, "DialUDP": true,
	"DialIP": true, "Resolver": true, "DefaultResolver": true, "LookupHost": true, "LookupIP": true,
	"LookupAddr": true, "LookupCNAME": true, "LookupPort": true, "LookupTXT": true, "LookupMX": true,
	"LookupNS": true, "LookupSRV": true, "ResolveTCPAddr": true, "ResolveUDPAddr": true, "ResolveIPAddr": true,
}

// bannedImports are packages that would let the package start a process or
// reach the system around the one sanctioned exec call ("C" is import "C").
var bannedImports = map[string]bool{
	"syscall": true, "golang.org/x/sys/unix": true, "C": true,
}

type fileFacts struct {
	imports  map[string]string // local name -> path
	literals []string          // string literals, in source order
	selects  []string          // "path.Sym" selector expressions on imported packages
	execArgs []ast.Expr        // path argument of each exec.Command* call
	envSet   bool              // some X.Env = ... assignment exists
	offences []string
}

func scan(fset *token.FileSet, name string, src any) (*fileFacts, error) {
	file, err := parser.ParseFile(fset, name, src, parser.SkipObjectResolution)
	if err != nil {
		return nil, err
	}
	ff := &fileFacts{imports: map[string]string{}}
	for _, imp := range file.Imports {
		path, _ := strconv.Unquote(imp.Path.Value)
		local := path[strings.LastIndex(path, "/")+1:]
		if imp.Name != nil {
			local = imp.Name.Name
			if local == "." {
				ff.offences = append(ff.offences, fmt.Sprintf("%s: dot-import of %s", fset.Position(imp.Pos()), path))
			}
		}
		ff.imports[local] = path
		if bannedImports[path] {
			ff.offences = append(ff.offences, fmt.Sprintf("%s: import %s", fset.Position(imp.Pos()), path))
		}
		if networkImports[path] || strings.HasPrefix(path, "golang.org/x/net") {
			ff.offences = append(ff.offences, fmt.Sprintf("%s: network import %s", fset.Position(imp.Pos()), path))
		}
	}
	ast.Inspect(file, func(n ast.Node) bool {
		switch x := n.(type) {
		case *ast.BasicLit:
			if x.Kind == token.STRING {
				s, _ := strconv.Unquote(x.Value)
				ff.literals = append(ff.literals, s)
			}
		case *ast.AssignStmt:
			for _, l := range x.Lhs {
				if sel, ok := l.(*ast.SelectorExpr); ok && sel.Sel.Name == "Env" {
					ff.envSet = true
				}
			}
		case *ast.CallExpr:
			if arg, ok := execPathArg(ff, x); ok {
				ff.execArgs = append(ff.execArgs, arg)
			}
		case *ast.SelectorExpr:
			id, ok := x.X.(*ast.Ident)
			if !ok {
				return true
			}
			path, ok := ff.imports[id.Name]
			if !ok {
				return true
			}
			ff.selects = append(ff.selects, path+"."+x.Sel.Name)
			if path == "os" && x.Sel.Name == "StartProcess" {
				ff.offences = append(ff.offences, fmt.Sprintf("%s: use of os.StartProcess", fset.Position(x.Pos())))
			}
			if path == "net" && bannedNetSymbols[x.Sel.Name] {
				ff.offences = append(ff.offences, fmt.Sprintf("%s: use of net.%s", fset.Position(x.Pos()), x.Sel.Name))
			}
		}
		return true
	})
	return ff, nil
}

// execPathArg returns the program argument of an exec.Command* call.
func execPathArg(ff *fileFacts, c *ast.CallExpr) (ast.Expr, bool) {
	sel, ok := c.Fun.(*ast.SelectorExpr)
	if !ok || !strings.HasPrefix(sel.Sel.Name, "Command") {
		return nil, false
	}
	id, ok := sel.X.(*ast.Ident)
	if !ok || ff.imports[id.Name] != "os/exec" {
		return nil, false
	}
	idx := 0
	if sel.Sel.Name == "CommandContext" {
		idx = 1
	}
	if len(c.Args) <= idx {
		return nil, true
	}
	return c.Args[idx], true
}

func (ff *fileFacts) importsPath(path string) bool {
	for _, p := range ff.imports {
		if p == path {
			return true
		}
	}
	return false
}

// wslinfoOffences checks the one file allowed to start a process.
func wslinfoOffences(ff *fileFacts) []string {
	var out []string
	if !ff.importsPath("os/exec") {
		out = append(out, wslinfoFile+": does not import os/exec")
	}
	var paths []string
	for _, l := range ff.literals {
		if strings.HasPrefix(l, "/") {
			paths = append(paths, l)
		}
	}
	if got, want := strings.Join(paths, " "), "/usr/bin/wslinfo /usr/sbin/wslinfo"; got != want {
		out = append(out, fmt.Sprintf("%s: absolute path literals are %q, want exactly %q in that order", wslinfoFile, got, want))
	}
	if len(ff.execArgs) != 1 {
		out = append(out, fmt.Sprintf("%s: %d exec.Command* calls, want exactly 1", wslinfoFile, len(ff.execArgs)))
	}
	for _, a := range ff.execArgs {
		if _, ok := a.(*ast.Ident); !ok {
			out = append(out, wslinfoFile+": the exec program must be a variable (from findWslinfo), not a literal or expression")
		}
	}
	if !ff.envSet {
		out = append(out, wslinfoFile+": cmd.Env is never assigned")
	}
	hasArg := false
	for _, l := range ff.literals {
		if l == "--networking-mode" {
			hasArg = true
		}
		if strings.Contains(l, "PATH") {
			out = append(out, fmt.Sprintf("%s: literal %q reads PATH", wslinfoFile, l))
		}
	}
	if !hasArg {
		out = append(out, wslinfoFile+": constant argument --networking-mode not found")
	}
	for _, s := range ff.selects {
		switch s {
		case "os/exec.LookPath", "os.Getenv", "os.LookupEnv", "os.Environ":
			out = append(out, fmt.Sprintf("%s: use of %s (no PATH lookup)", wslinfoFile, s))
		}
	}
	return out
}

func TestGuardScanFlagsOffences(t *testing.T) {
	for _, c := range []struct {
		name, src string
		want      int
	}{
		{"http.go", "package x\nimport \"net/http\"\nvar _ = http.Get\n", 1},
		{"xnet.go", "package x\nimport \"golang.org/x/net/icmp\"\nvar _ = icmp.ListenPacket\n", 1},
		{"dial.go", "package x\nimport \"net\"\nfunc f() { _, _ = net.Dial(\"tcp\", \"a:1\") }\n", 1},
		{"lookup.go", "package x\nimport \"net\"\nfunc f() { _, _ = net.LookupHost(\"a\") }\n", 1},
		{"dot.go", "package x\nimport . \"net\"\nvar _ = Interfaces\n", 1},
		{"ifaces.go", "package x\nimport \"net\"\nvar _ = net.Interfaces\n", 0},
		{"startproc.go", "package x\nimport \"os\"\nvar _ = os.StartProcess\n", 1},
		{"syscall.go", "package x\nimport \"syscall\"\nvar _ = syscall.Getpid\n", 1},
		{"unix.go", "package x\nimport \"golang.org/x/sys/unix\"\nvar _ = unix.Getpid\n", 1},
		{"cgo.go", "package x\nimport \"C\"\n", 1},
		{"clean.go", "package x\nimport \"os\"\n// net.Dial in a comment\nvar _ = os.ReadFile\n", 0},
	} {
		ff, err := scan(token.NewFileSet(), c.name, c.src)
		if err != nil {
			t.Fatalf("%s: %v", c.name, err)
		}
		if len(ff.offences) != c.want {
			t.Errorf("%s: got %v, want %d offences", c.name, ff.offences, c.want)
		}
	}

	const consts = "var a, b, c = \"/usr/bin/wslinfo\", \"/usr/sbin/wslinfo\", \"--networking-mode\"\n"
	const call = "func f(p string) { c := exec.CommandContext(nil, p, \"--networking-mode\"); c.Env = nil }\n"
	good := "package x\nimport \"os/exec\"\n" + call + consts
	ff, err := scan(token.NewFileSet(), wslinfoFile, good)
	if err != nil {
		t.Fatal(err)
	}
	if o := wslinfoOffences(ff); len(o) != 0 {
		t.Errorf("good wslinfo.go flagged: %v", o)
	}
	for name, src := range map[string]string{
		"lookpath":  "package x\nimport \"os/exec\"\n" + call + "var _ = exec.LookPath\n" + consts,
		"no call":   "package x\nimport \"os/exec\"\nvar _ = exec.LookPath\n" + consts,
		"two calls": "package x\nimport \"os/exec\"\n" + call + call + consts,
		"literal":   "package x\nimport \"os/exec\"\nfunc f() { c := exec.Command(\"/usr/bin/wslinfo\"); c.Env = nil }\n" + consts,
		"no env":    "package x\nimport \"os/exec\"\nfunc f(p string) { _ = exec.Command(p) }\n" + consts,
		"bare name": "package x\nimport \"os/exec\"\nvar a, c = \"wslinfo\", \"--networking-mode\"\n",
		"extra":     "package x\nimport \"os/exec\"\nvar a, b, d, c = \"/usr/bin/wslinfo\", \"/usr/sbin/wslinfo\", \"/bin/sh\", \"--networking-mode\"\n",
		"order":     "package x\nimport \"os/exec\"\nvar a, b, c = \"/usr/sbin/wslinfo\", \"/usr/bin/wslinfo\", \"--networking-mode\"\n",
		"no arg":    "package x\nimport \"os/exec\"\nvar a, b = \"/usr/bin/wslinfo\", \"/usr/sbin/wslinfo\"\n",
		"path":      "package x\nimport \"os/exec\"\n" + consts + "var p = \"PATH\"\n",
		"no exec":   "package x\n" + consts,
	} {
		ff, err := scan(token.NewFileSet(), wslinfoFile, src)
		if err != nil {
			t.Fatal(err)
		}
		if len(wslinfoOffences(ff)) == 0 {
			t.Errorf("wslinfo.go variant %q not flagged", name)
		}
	}
}

func TestPackageSourceGuard(t *testing.T) {
	entries, err := os.ReadDir(".")
	if err != nil {
		t.Fatal(err)
	}
	var names []string
	for _, e := range entries {
		n := e.Name()
		if !e.IsDir() && strings.HasSuffix(n, ".go") && !strings.HasSuffix(n, "_test.go") {
			names = append(names, n)
		}
	}
	sort.Strings(names)
	if len(names) == 0 {
		t.Fatal("the scan saw no Go file: the guard would pass vacuously")
	}
	fset := token.NewFileSet()
	for _, n := range names {
		src, err := os.ReadFile(n)
		if err != nil {
			t.Fatal(err)
		}
		ff, err := scan(fset, n, src)
		if err != nil {
			t.Errorf("%s: %v", n, err)
			continue
		}
		for _, o := range ff.offences {
			t.Error(o)
		}
		if n == wslinfoFile {
			for _, o := range wslinfoOffences(ff) {
				t.Error(o)
			}
			continue
		}
		if ff.importsPath("os/exec") || ff.importsPath("golang.org/x/sys/execabs") {
			t.Errorf("%s: imports os/exec; only %s may start a process", n, wslinfoFile)
		}
	}
}
