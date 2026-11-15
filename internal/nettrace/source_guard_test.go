package nettrace

import (
	"fmt"
	"go/ast"
	"go/parser"
	"go/token"
	"os"
	"strconv"
	"strings"
	"testing"
)

// The trace sends its probes itself, over ICMP and UDP sockets. It must never
// start a child process (traceroute, ping) nor replace the process image, on
// any platform. The guard reads every .go file of the package whatever its
// build constraints (go/parser does not evaluate them), so a file that only
// builds on another OS is covered too.

// bannedImports are packages that must not be imported at all.
var bannedImports = map[string]bool{
	"os/exec":                  true,
	"golang.org/x/sys/execabs": true,
	// Windows services can launch processes.
	"golang.org/x/sys/windows/svc":     true,
	"golang.org/x/sys/windows/svc/mgr": true,
}

// dotWatched are packages that must not be dot-imported: a dot-import hides
// the package name the selector check relies on.
var dotWatched = map[string]bool{
	"os":                    true,
	"syscall":               true,
	"os/exec":               true,
	"golang.org/x/sys/unix": true,
	winPath:                 true,
}

// winPath is the Windows system-call package. The Windows engine may use it
// to load iphlpapi.dll only (see the rules in guardSource).
const winPath = "golang.org/x/sys/windows"

// winDLL is the one DLL the Windows engine may load.
const winDLL = "iphlpapi.dll"

// winProcs is the exact, case-sensitive list of entry points NewProc may
// resolve: the IP Helper ICMP echo API, nothing else.
var winProcs = map[string]bool{
	"IcmpCreateFile": true, "Icmp6CreateFile": true,
	"IcmpSendEcho2": true, "Icmp6SendEcho2": true,
	"IcmpCloseHandle": true,
}

// bannedSymbols maps an import path to the process-launching symbols it must
// not expose here, called or not (`f := os.StartProcess` is caught too). The
// socket symbols the engine needs (Recvmsg, Setsockopt...) stay allowed.
var bannedSymbols = map[string]map[string]bool{
	"os": {"StartProcess": true},
	"syscall": {
		"StartProcess": true, "ForkExec": true, "Exec": true,
		"Syscall": true, "Syscall6": true, "RawSyscall": true, "RawSyscall6": true,
		"SyscallN": true, "Syscall9": true, "Syscall12": true, "Syscall15": true, "Syscall18": true,
		"CreateProcess": true, "CreateProcessAsUser": true,
		"LoadLibrary": true, "GetProcAddress": true,
		"LoadDLL": true, "MustLoadDLL": true, "NewLazyDLL": true,
		"LazyDLL": true, "DLL": true, "LazyProc": true, "Proc": true,
	},
	"golang.org/x/sys/unix": {
		"Exec":    true,
		"Syscall": true, "Syscall6": true, "RawSyscall": true, "RawSyscall6": true,
		"Syscall9": true, "RawSyscall9": true, "SyscallNoError": true, "RawSyscallNoError": true,
	},
	winPath: {
		"CreateProcess": true, "CreateProcessAsUser": true, "ShellExecute": true,
		"Syscall": true, "Syscall6": true, "Syscall9": true, "Syscall12": true,
		"Syscall15": true, "Syscall18": true, "SyscallN": true,
		"LoadLibrary": true, "LoadLibraryEx": true,
		"GetProcAddress": true, "GetProcAddressByOrdinal": true,
		"LoadDLL": true, "MustLoadDLL": true, "NewLazyDLL": true,
		// Naming these types would build a DLL handle without NewLazySystemDLL;
		// NewProc and Call on the value NewLazySystemDLL returns need no name.
		"LazyDLL": true, "DLL": true, "LazyProc": true, "Proc": true,
		// Services start executables.
		"CreateService": true, "StartService": true, "OpenSCManager": true,
	},
}

// isStringLit reports whether e is a string literal and returns its value.
func isStringLit(e ast.Expr) (string, bool) {
	lit, ok := e.(*ast.BasicLit)
	if !ok || lit.Kind != token.STRING {
		return "", false
	}
	v, err := strconv.Unquote(lit.Value)
	return v, err == nil
}

// winCallOffence checks the two Windows loading rules on a call in a file
// that imports x/sys/windows: NewLazySystemDLL takes the literal "iphlpapi.dll"
// only, and NewProc takes a literal from winProcs only. It returns a message
// or "". One call is one offence at most.
func winCallOffence(fset *token.FileSet, call *ast.CallExpr, winNames map[string]bool) string {
	sel, ok := call.Fun.(*ast.SelectorExpr)
	if !ok || len(call.Args) != 1 {
		return ""
	}
	lit, isLit := isStringLit(call.Args[0])
	switch sel.Sel.Name {
	case "NewLazySystemDLL":
		id, ok := sel.X.(*ast.Ident)
		if !ok || !winNames[id.Name] {
			return ""
		}
		if !isLit || lit != winDLL {
			return fmt.Sprintf("%s: NewLazySystemDLL may load only the literal %q", fset.Position(call.Pos()), winDLL)
		}
	case "NewProc":
		if !isLit || !winProcs[lit] {
			return fmt.Sprintf("%s: NewProc may resolve only the ICMP echo entry points by literal name", fset.Position(call.Pos()))
		}
	}
	return ""
}

// dllFields are the exported fields of a DLL handle that choose what gets
// loaded (Name, System) or what was loaded (Dll). Writing them after
// NewLazySystemDLL would turn the iphlpapi.dll rule into a no-op.
var dllFields = map[string]bool{"Name": true, "System": true, "Dll": true}

func unparen(e ast.Expr) ast.Expr {
	for {
		p, ok := e.(*ast.ParenExpr)
		if !ok {
			return e
		}
		e = p.X
	}
}

// writtenTargets returns the expressions n writes to or takes the address of:
// assignment targets (also op-assign), ++/--, range key/value, and &x.
func writtenTargets(n ast.Node) []ast.Expr {
	switch s := n.(type) {
	case *ast.AssignStmt:
		return s.Lhs
	case *ast.IncDecStmt:
		return []ast.Expr{s.X}
	case *ast.RangeStmt:
		return []ast.Expr{s.Key, s.Value}
	case *ast.UnaryExpr:
		if s.Op == token.AND {
			return []ast.Expr{s.X}
		}
	}
	return nil
}

// guardSource returns one message per offence in src ("name:line: ...").
func guardSource(fset *token.FileSet, name string, src any) ([]string, error) {
	file, err := parser.ParseFile(fset, name, src, parser.SkipObjectResolution)
	if err != nil {
		return nil, err
	}
	var out []string
	local := map[string]string{}  // local package name -> import path
	winNames := map[string]bool{} // local names of x/sys/windows
	importsSys := false           // syscall or x/sys/windows: the NewProc rules apply
	for _, imp := range file.Imports {
		path, _ := strconv.Unquote(imp.Path.Value)
		if bannedImports[path] {
			out = append(out, fmt.Sprintf("%s: import of %s", fset.Position(imp.Pos()), path))
		}
		if imp.Name != nil && imp.Name.Name == "." && dotWatched[path] {
			out = append(out, fmt.Sprintf("%s: dot-import of %s", fset.Position(imp.Pos()), path))
		}
		if path == winPath || path == "syscall" {
			importsSys = true
		}
		if _, watched := bannedSymbols[path]; !watched {
			continue
		}
		n := path[strings.LastIndex(path, "/")+1:]
		if imp.Name != nil {
			n = imp.Name.Name
		}
		local[n] = path
		if path == winPath {
			winNames[n] = true
		}
	}
	// calledLoad holds the bare windows.NewLazySystemDLL selectors that are the
	// function of a call; any other reference (value, parentheses) is refused.
	// calledProc does the same for the NewProc selectors.
	calledLoad := map[*ast.SelectorExpr]bool{}
	calledProc := map[*ast.SelectorExpr]bool{}
	ast.Inspect(file, func(n ast.Node) bool {
		if importsSys {
			for _, target := range writtenTargets(n) {
				if sel, ok := unparen(target).(*ast.SelectorExpr); ok && dllFields[sel.Sel.Name] {
					out = append(out, fmt.Sprintf("%s: writing the %s field could redirect a DLL load", fset.Position(sel.Pos()), sel.Sel.Name))
				}
			}
		}
		if call, ok := n.(*ast.CallExpr); ok && importsSys {
			if sel, ok := call.Fun.(*ast.SelectorExpr); ok {
				switch sel.Sel.Name {
				case "NewLazySystemDLL":
					calledLoad[sel] = true
				case "NewProc":
					calledProc[sel] = true
				}
			}
			if msg := winCallOffence(fset, call, winNames); msg != "" {
				out = append(out, msg)
			}
			return true
		}
		sel, ok := n.(*ast.SelectorExpr)
		if !ok {
			return true
		}
		if importsSys && sel.Sel.Name == "NewProc" && !calledProc[sel] {
			out = append(out, fmt.Sprintf("%s: NewProc must be called directly, not used as a value", fset.Position(sel.Pos())))
		}
		id, ok := sel.X.(*ast.Ident)
		if !ok {
			return true
		}
		if winNames[id.Name] && sel.Sel.Name == "NewLazySystemDLL" && !calledLoad[sel] {
			out = append(out, fmt.Sprintf("%s: %s.NewLazySystemDLL must be called directly, not used as a value", fset.Position(sel.Pos()), id.Name))
		}
		if path, ok := local[id.Name]; ok && bannedSymbols[path][sel.Sel.Name] {
			out = append(out, fmt.Sprintf("%s: use of %s.%s", fset.Position(sel.Pos()), id.Name, sel.Sel.Name))
		}
		return true
	})
	return out, nil
}

func TestGuardSourceFlagsOffences(t *testing.T) {
	for _, c := range []struct {
		name, src string
		want      int
	}{
		{"exec.go", "package x\nimport \"os/exec\"\nfunc f() { _ = exec.Command(\"traceroute\") }\n", 1},
		{"aliased_exec.go", "package x\nimport e \"os/exec\"\nvar _ = e.Command\n", 1},
		{"startprocess.go", "package x\nimport \"os\"\nfunc f() { _, _ = os.StartProcess(\"a\", nil, nil) }\n", 1},
		{"forkexec.go", "package x\nimport \"syscall\"\nfunc f() { _, _ = syscall.ForkExec(\"a\", nil, nil); _ = syscall.Exec(\"a\", nil, nil) }\n", 2},
		{"unix.go", "package x\nimport \"golang.org/x/sys/unix\"\nfunc f() { _ = unix.Exec(\"a\", nil, nil) }\n", 1},
		{"aliased_unix.go", "package x\nimport u \"golang.org/x/sys/unix\"\nfunc f() { _ = u.Exec(\"a\", nil, nil) }\n", 1},
		{"tagged.go", "//go:build windows\n\npackage x\nimport \"os/exec\"\nvar _ = exec.Command\n", 1},
		{"execabs.go", "package x\nimport \"golang.org/x/sys/execabs\"\nvar _ = execabs.Command\n", 1},
		{"syscall_startprocess.go", "package x\nimport \"syscall\"\nfunc f() { _, _ = syscall.StartProcess(\"a\", nil, nil) }\n", 1},
		{"raw_syscall.go", "package x\nimport \"syscall\"\nfunc f() { syscall.RawSyscall(59, 0, 0); syscall.Syscall(59, 0, 0, 0); syscall.Syscall6(59, 0, 0, 0, 0, 0, 0); syscall.RawSyscall6(59, 0, 0, 0, 0, 0, 0) }\n", 4},
		{"unix_syscall.go", "package x\nimport \"golang.org/x/sys/unix\"\nfunc f() { unix.Syscall(59, 0, 0, 0); unix.RawSyscall6(59, 0, 0, 0, 0, 0, 0) }\n", 2},
		{"func_value.go", "package x\nimport \"os\"\nvar f = os.StartProcess\n", 1},
		{"dot_os.go", "package x\nimport . \"os\"\nvar _ = Getpid\n", 1},
		{"dot_syscall.go", "package x\nimport . \"syscall\"\nvar _ = Getpid\n", 1},
		{"dot_exec.go", "package x\nimport . \"os/exec\"\nvar _ = Command\n", 2},
		{"dot_unix.go", "package x\nimport . \"golang.org/x/sys/unix\"\nvar _ = Getpid\n", 1},
		{"allowed_unix.go", "package x\nimport \"golang.org/x/sys/unix\"\nfunc f() { _ = unix.Setsockopt; _ = unix.Recvmsg }\n", 0},
		// DEVDESK-488: the Windows engine may load iphlpapi.dll only, through
		// x/sys/windows, and call five named ICMP entry points.
		{"win_createprocess.go", "package x\nimport \"golang.org/x/sys/windows\"\nfunc f() { _ = windows.CreateProcess }\n", 1},
		{"win_createprocess_aliased.go", "package x\nimport w \"golang.org/x/sys/windows\"\nfunc f() { _ = w.CreateProcessAsUser }\n", 1},
		{"win_banned_symbols.go", "package x\nimport \"golang.org/x/sys/windows\"\nfunc f() { _ = windows.ShellExecute; _ = windows.Syscall; _ = windows.Syscall6; _ = windows.Syscall9; _ = windows.Syscall12; _ = windows.Syscall15; _ = windows.Syscall18; _ = windows.SyscallN; _ = windows.LoadLibrary; _ = windows.LoadLibraryEx; _ = windows.GetProcAddress; _ = windows.GetProcAddressByOrdinal; _ = windows.LoadDLL; _ = windows.MustLoadDLL; _ = windows.NewLazyDLL }\n", 15},
		{"win_dot.go", "package x\nimport . \"golang.org/x/sys/windows\"\nvar _ = GetCurrentProcessId\n", 1},
		{"win_lazy_system_ok.go", "package x\nimport \"golang.org/x/sys/windows\"\nvar dll = windows.NewLazySystemDLL(\"iphlpapi.dll\")\n", 0},
		{"win_lazy_system_other.go", "package x\nimport \"golang.org/x/sys/windows\"\nvar dll = windows.NewLazySystemDLL(\"kernel32.dll\")\n", 1},
		{"win_lazy_system_var.go", "package x\nimport \"golang.org/x/sys/windows\"\nfunc f(n string) { _ = windows.NewLazySystemDLL(n) }\n", 1},
		{"win_newproc_bad.go", "package x\nimport \"golang.org/x/sys/windows\"\nvar p = windows.NewLazySystemDLL(\"iphlpapi.dll\").NewProc(\"CreateProcessW\")\n", 1},
		{"win_newproc_var.go", "package x\nimport \"golang.org/x/sys/windows\"\nfunc f(name string) { _ = windows.NewLazySystemDLL(\"iphlpapi.dll\").NewProc(name) }\n", 1},
		{"win_newproc_ok.go", "package x\nimport \"golang.org/x/sys/windows\"\nvar p = windows.NewLazySystemDLL(\"iphlpapi.dll\").NewProc(\"IcmpSendEcho2\")\n", 0},
		{"win_newproc_allowlist.go", "package x\nimport \"golang.org/x/sys/windows\"\nvar d = windows.NewLazySystemDLL(\"iphlpapi.dll\")\nvar a, b, c, e, g = d.NewProc(\"IcmpCreateFile\"), d.NewProc(\"Icmp6CreateFile\"), d.NewProc(\"IcmpSendEcho2\"), d.NewProc(\"Icmp6SendEcho2\"), d.NewProc(\"IcmpCloseHandle\")\n", 0},
		{"win_newproc_case.go", "package x\nimport \"golang.org/x/sys/windows\"\nvar p = windows.NewLazySystemDLL(\"iphlpapi.dll\").NewProc(\"icmpsendecho2\")\n", 1},
		// Review round 1: NewLazySystemDLL may only be the called function itself.
		{"win_load_value.go", "package x\nimport \"golang.org/x/sys/windows\"\nvar load = windows.NewLazySystemDLL\nvar d = load(\"kernel32.dll\")\n", 1},
		{"win_load_paren.go", "package x\nimport \"golang.org/x/sys/windows\"\nvar d = (windows.NewLazySystemDLL)(\"kernel32.dll\")\n", 1},
		{"win_load_alias.go", "package x\nimport w \"golang.org/x/sys/windows\"\nvar load = w.NewLazySystemDLL\nvar d = w.NewLazySystemDLL(\"kernel32.dll\")\n", 2},
		{"win_load_alias_ok.go", "package x\nimport w \"golang.org/x/sys/windows\"\nvar d = w.NewLazySystemDLL(\"iphlpapi.dll\")\n", 0},
		{"win_lazydll_lit.go", "package x\nimport \"golang.org/x/sys/windows\"\nvar d = &windows.LazyDLL{Name: \"kernel32.dll\"}\nvar p = d.NewProc(\"IcmpSendEcho2\")\n", 1},
		{"win_dll_types.go", "package x\nimport \"golang.org/x/sys/windows\"\nvar a *windows.DLL\nvar b *windows.LazyProc\nvar c *windows.Proc\n", 3},
		{"win_svc_import.go", "package x\nimport _ \"golang.org/x/sys/windows/svc/mgr\"\n", 1},
		// Review round 2: syscall's DLL types, NewProc used as a value, services.
		{"syscall_dll_types.go", "package x\nimport \"syscall\"\nvar a *syscall.DLL\nvar b *syscall.LazyDLL\nvar c *syscall.LazyProc\nvar d *syscall.Proc\n", 4},
		{"syscall_lazydll_call.go", "package x\nimport \"syscall\"\nfunc f() { (&syscall.LazyDLL{Name: \"kernel32.dll\"}).NewProc(\"CreateProcessW\").Call() }\n", 2},
		{"win_newproc_value.go", "package x\nimport \"golang.org/x/sys/windows\"\nvar d = windows.NewLazySystemDLL(\"iphlpapi.dll\")\nvar f = d.NewProc\nvar p = f(\"CreateProcessW\")\n", 1},
		{"win_newproc_paren.go", "package x\nimport \"golang.org/x/sys/windows\"\nvar d = windows.NewLazySystemDLL(\"iphlpapi.dll\")\nvar p = (d.NewProc)(\"CreateProcessW\")\n", 1},
		{"win_newproc_paren_ok_name.go", "package x\nimport \"golang.org/x/sys/windows\"\nvar d = windows.NewLazySystemDLL(\"iphlpapi.dll\")\nvar p = (d.NewProc)(\"IcmpSendEcho2\")\n", 1},
		{"syscall_newproc_value.go", "package x\nimport \"syscall\"\nfunc f(d interface{ NewProc(string) }) { g := d.NewProc; g(\"x\") }\n", 1},
		{"win_services.go", "package x\nimport \"golang.org/x/sys/windows\"\nfunc f() { _ = windows.CreateService; _ = windows.StartService; _ = windows.OpenSCManager }\n", 3},
		{"win_unrelated_newproc.go", "package x\ntype t struct{}\nfunc (t) NewProc(string) {}\nfunc f(n string) { t{}.NewProc(n) }\n", 0},
		{"syscall_extra.go", "package x\nimport \"syscall\"\nfunc f() { _ = syscall.SyscallN; _ = syscall.Syscall9; _ = syscall.Syscall12; _ = syscall.Syscall15; _ = syscall.Syscall18; _ = syscall.CreateProcess; _ = syscall.CreateProcessAsUser; _ = syscall.LoadLibrary; _ = syscall.GetProcAddress; _ = syscall.LoadDLL; _ = syscall.MustLoadDLL; _ = syscall.NewLazyDLL }\n", 12},
		// Review round 3: a DLL handle's exported fields must not be written.
		{"win_field_assign.go", "package x\nimport \"golang.org/x/sys/windows\"\nfunc f() { d := windows.NewLazySystemDLL(\"iphlpapi.dll\"); d.Name = \"x.dll\"; d.System = false; d.NewProc(\"IcmpSendEcho2\") }\n", 2},
		{"win_field_dll.go", "package x\nimport \"golang.org/x/sys/windows\"\nfunc f(d interface{}) { d.Dll = nil }\n", 1},
		{"win_field_paren.go", "package x\nimport \"golang.org/x/sys/windows\"\nfunc f(d struct{ Name string }) { (d.Name) = \"x\" }\n", 1},
		{"win_field_op_assign.go", "package x\nimport \"golang.org/x/sys/windows\"\nfunc f(d struct{ Name string }) { d.Name += \"x\" }\n", 1},
		{"win_field_multi_assign.go", "package x\nimport \"golang.org/x/sys/windows\"\nfunc f(d struct{ Name string; System bool }) { d.Name, d.System = \"x\", true }\n", 2},
		{"win_field_incdec.go", "package x\nimport \"golang.org/x/sys/windows\"\nfunc f(d struct{ Name int }) { d.Name++; d.Name-- }\n", 2},
		{"win_field_addr.go", "package x\nimport \"golang.org/x/sys/windows\"\nfunc f(d struct{ Name string }) { p := &d.Name; *p = \"x\" }\n", 1},
		{"win_field_range.go", "package x\nimport \"golang.org/x/sys/windows\"\nfunc f(d struct{ Name int }, s []int) { for d.Name = range s {} }\n", 1},
		{"syscall_field_assign.go", "package x\nimport \"syscall\"\nfunc f(d struct{ Name string }) { _ = syscall.Getpid; d.Name = \"x\" }\n", 1},
		{"win_field_read_ok.go", "package x\nimport \"golang.org/x/sys/windows\"\nfunc f(d struct{ Name string }) { _ = d.Name; x := d.Name; _ = x }\n", 0},
		{"nosys_field_assign_ok.go", "package x\nfunc f(d struct{ Name string }) { d.Name = \"x\" }\n", 0},
		{"unix_syscall9.go", "package x\nimport \"golang.org/x/sys/unix\"\nfunc f() { _ = unix.Syscall9; _ = unix.SyscallNoError; _ = unix.RawSyscallNoError; _ = unix.RawSyscall9 }\n", 4},
		{"clean.go", "package x\nimport (\"os\"; \"syscall\")\n// os.StartProcess in a comment is not a call\nfunc f() { _ = os.Getpid(); _ = syscall.Getpid(); var s struct{ Exec func() }; s.Exec() }\n", 0},
	} {
		got, err := guardSource(token.NewFileSet(), c.name, c.src)
		if err != nil {
			t.Fatalf("%s: %v", c.name, err)
		}
		if len(got) != c.want {
			t.Errorf("%s: got %d offences %v, want %d", c.name, len(got), got, c.want)
		}
	}
}

func TestPackageNeverStartsAProcess(t *testing.T) {
	entries, err := os.ReadDir(".")
	if err != nil {
		t.Fatal(err)
	}
	fset := token.NewFileSet()
	files := 0
	for _, e := range entries {
		if e.IsDir() || !strings.HasSuffix(e.Name(), ".go") {
			continue
		}
		src, err := os.ReadFile(e.Name())
		if err != nil {
			t.Fatal(err)
		}
		files++
		offences, err := guardSource(fset, e.Name(), src)
		if err != nil {
			t.Errorf("%s: %v", e.Name(), err)
			continue
		}
		for _, o := range offences {
			t.Error(o + ": the trace must not start or exec a process")
		}
	}
	if files == 0 {
		t.Error("the scan saw no Go file: the guard would pass vacuously")
	}
}
