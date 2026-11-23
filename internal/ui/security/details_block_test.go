package security

import (
	"go/ast"
	"go/parser"
	"go/token"
	"os"
	"path/filepath"
	"reflect"
	"strings"
	"testing"

	"github.com/anthnel/devdesk/internal/remediation"
	"github.com/anthnel/devdesk/internal/scan"
)

// DEVDESK-751 (step 2): the pure helpers behind the Fix block. None of them
// reads the disk or a model: a finding, and sometimes the scanned target, go in.

// ── runCommand (AC4, AC5) ────────────────────────────────────────────────────

func TestRunCommand(t *testing.T) {
	tests := []struct {
		name   string
		f      scan.Finding
		want   string
		wantOK bool
	}{
		{"a go command", scan.Finding{PkgName: "golang.org/x/net", FixCommand: "go get golang.org/x/net@v1"},
			"go get golang.org/x/net@v1", true},
		{"an apt command", scan.Finding{PkgName: "curl", FixCommand: "apt-get install --only-upgrade curl"},
			"apt-get install --only-upgrade curl", true},
		{"a sentence for the package is an instruction", scan.Finding{PkgName: "x", FixCommand: "Update x to 1.2"}, "", false},
		{"a sentence for another package is a command", scan.Finding{PkgName: "x", FixCommand: "Update y to 1.2"},
			"Update y to 1.2", true},
		{"empty", scan.Finding{PkgName: "x"}, "", false},
		{"nothing", scan.Finding{}, "", false},
		{"two lines", scan.Finding{PkgName: "x", FixCommand: "a\nb"}, "", false},
		{"two lines with a carriage return", scan.Finding{PkgName: "x", FixCommand: "a\r\nb"}, "", false},
		{"spaces around are trimmed", scan.Finding{PkgName: "x", FixCommand: "  go get x@v1  "}, "go get x@v1", true},
		{"a trailing newline is trimmed", scan.Finding{PkgName: "x", FixCommand: "go get x@v1\n"}, "go get x@v1", true},
		{"only white space", scan.Finding{PkgName: "x", FixCommand: " \t\n"}, "", false},
	}
	for _, tc := range tests {
		t.Run(tc.name, func(t *testing.T) {
			got, ok := runCommand(tc.f)
			if got != tc.want || ok != tc.wantOK {
				t.Errorf("runCommand(%q) = (%q, %v), want (%q, %v)", tc.f.FixCommand, got, ok, tc.want, tc.wantOK)
			}
		})
	}
}

// One computation, two readers: what the detail shows after "Run: $ " is what
// the copy key puts on the clipboard, and it is a command fixAction calls Run.
func TestRunCommandAgreesWithFixAction(t *testing.T) {
	for _, f := range []scan.Finding{
		{PkgName: "golang.org/x/net", FixCommand: "go get golang.org/x/net@v1"},
		{PkgName: "x", FixCommand: "Update x to 1.2"},
		{PkgName: "x", FixCommand: "a\nb"},
		{PkgName: "x"},
	} {
		label, text := fixAction(f)
		cmd, ok := runCommand(f)
		if ok && (label != "Run" || strings.TrimSpace(text) != cmd) {
			t.Errorf("runCommand(%q) = %q but fixAction = (%q, %q)", f.FixCommand, cmd, label, text)
		}
		if label != "Run" && ok {
			t.Errorf("runCommand accepts %q which fixAction does not call a command", f.FixCommand)
		}
	}
}

// ── fixSteps (AC2) ───────────────────────────────────────────────────────────

func TestFixSteps(t *testing.T) {
	missing, ok := remediation.MissingFor(misconfigOf("KSV-0012"))
	if !ok {
		t.Fatal("KSV-0012 must have a MissingFor text")
	}
	tests := []struct {
		name string
		f    scan.Finding
		want []string
	}{
		{"a rule with a MissingFor text and no catalog fix", misconfigOf("KSV-0012"), []string{missing}},
		{"an instruction for an ecosystem with no command", scan.Finding{PkgName: "x", FixCommand: "Update x to 1.2"},
			[]string{"Update x to 1.2"}},
		{"a real command is shown under Run, not as a step", scan.Finding{PkgName: "x", FixCommand: "go get x@v1"}, nil},
		{"a catalog rule is fixed by the built-in fix", misconfigOf("DS002"), nil},
		{"a rule with no text", misconfigOf("KSV-0003"), nil},
		{"the Resolution is a paragraph, not a step", withResolution(misconfigOf("KSV-0003")), nil},
		{"a minimal finding", scan.Finding{ID: "X-1"}, nil},
	}
	for _, tc := range tests {
		t.Run(tc.name, func(t *testing.T) {
			got := fixSteps(tc.f)
			if len(got) == 0 && len(tc.want) == 0 {
				return
			}
			if !reflect.DeepEqual(got, tc.want) {
				t.Errorf("fixSteps = %q, want %q", got, tc.want)
			}
		})
	}
}

// ── whereFacts (AC2) ─────────────────────────────────────────────────────────

func TestWhereFacts(t *testing.T) {
	tests := []struct {
		name string
		f    scan.Finding
		want []fact
	}{
		{"a file and a line", scan.Finding{File: "k8s/deploy.yaml", Line: 12},
			[]fact{{"File", "k8s/deploy.yaml:12"}}},
		{"a file with no line has no colon", scan.Finding{File: "k8s/deploy.yaml"},
			[]fact{{"File", "k8s/deploy.yaml"}}},
		{"a path under the target is shortened", scan.Finding{File: detailsTarget + "/k8s/deploy.yaml", Line: 3},
			[]fact{{"File", "k8s/deploy.yaml:3"}}},
		{"a path under the container mount is shortened", scan.Finding{File: "/scan/k/x.yaml", Line: 7},
			[]fact{{"File", "k/x.yaml:7"}}},
		{"a package of an application dependency",
			scan.Finding{PkgName: "golang.org/x/net", Version: "0.1.0", Class: scan.ClassLangPackages},
			[]fact{{"Package", "golang.org/x/net 0.1.0 (Application dependency)"}}},
		{"a package of the base image", scan.Finding{PkgName: "openssl", Version: "3.0.1", Class: scan.ClassOSPackages},
			[]fact{{"Package", "openssl 3.0.1 (Base image package)"}}},
		{"a package of an unknown class is not guessed", scan.Finding{PkgName: "openssl", Version: "3.0.1"},
			[]fact{{"Package", "openssl 3.0.1"}}},
		{"a CI job and its script line", scan.Finding{File: ".gitlab-ci.yml", Job: "build", ScriptLine: "curl http://x | sh"},
			[]fact{{"File", ".gitlab-ci.yml"}, {"Job", "build"}, {"Script", "curl http://x | sh"}}},
		{"a secret", scan.Finding{File: "config/prod.env", Line: 12, Match: "AKIA0000"},
			[]fact{{"File", "config/prod.env:12"}, {"Match", "AKIA0000"}}},
		{"every fact, in order", scan.Finding{File: "a.yml", Line: 1, PkgName: "p", Version: "1", Job: "j", ScriptLine: "s", Match: "m"},
			[]fact{{"File", "a.yml:1"}, {"Package", "p 1"}, {"Job", "j"}, {"Script", "s"}, {"Match", "m"}}},
		{"a minimal finding has no fact", scan.Finding{ID: "X-1", Title: "Only a title"}, nil},
	}
	for _, tc := range tests {
		t.Run(tc.name, func(t *testing.T) {
			got := whereFacts(tc.f, detailsTarget)
			if len(got) == 0 && len(tc.want) == 0 {
				return
			}
			if !reflect.DeepEqual(got, tc.want) {
				t.Errorf("whereFacts = %+v, want %+v", got, tc.want)
			}
		})
	}
}

// ── findingFilePath (AC4) ────────────────────────────────────────────────────

func TestFindingFilePath(t *testing.T) {
	const target = "/repo"
	under := func(parts ...string) string { return filepath.Join(append([]string{target}, parts...)...) }
	tests := []struct {
		name       string
		file       string
		target     string
		onDisk     bool
		want       string
		wantReason string
	}{
		{"a relative path is joined to the target", "k/x.yaml", target, true, under("k", "x.yaml"), ""},
		{"a path under the container mount is moved to the target", "/scan/k/x.yaml", target, true, under("k", "x.yaml"), ""},
		{"an absolute path under the target is kept", "/repo/k/x.yaml", target, true, under("k", "x.yaml"), ""},
		{"a target with a trailing slash", "k/x.yaml", target + "/", true, under("k", "x.yaml"), ""},
		{"a dot segment inside the target is cleaned", "k/./y/../x.yaml", target, true, under("k", "x.yaml"), ""},
		{"a parent segment out of the target", "../etc/passwd", target, true, "", reasonFileOutsideTarget},
		{"a parent segment hidden in a path", "k/../../etc/passwd", target, true, "", reasonFileOutsideTarget},
		{"an absolute path outside the target", "/etc/passwd", target, true, "", reasonFileOutsideTarget},
		{"a sibling that shares the target as a prefix", "/repo-evil/x", target, true, "", reasonFileOutsideTarget},
		{"an absolute path that climbs out of the target", "/repo/../etc/passwd", target, true, "", reasonFileOutsideTarget},
		{"the container mount that climbs out", "/scan/../etc/passwd", target, true, "", reasonFileOutsideTarget},
		{"no file", "", target, true, "", reasonNoFile},
		{"an image target has no file on disk", "k/x.yaml", "app:1.0", false, "", reasonFileNotOnDisk},
		{"an image target, whatever the file", "/etc/passwd", "app:1.0", false, "", reasonFileNotOnDisk},
	}
	for _, tc := range tests {
		t.Run(tc.name, func(t *testing.T) {
			got, a := findingFilePath(scan.Finding{File: tc.file}, tc.target, tc.onDisk)
			if a.Reason != tc.wantReason {
				t.Errorf("reason = %q, want %q", a.Reason, tc.wantReason)
			}
			if a.Enabled() != (tc.wantReason == "") {
				t.Errorf("Enabled() = %v for reason %q", a.Enabled(), a.Reason)
			}
			if got != tc.want {
				t.Errorf("path = %q, want %q", got, tc.want)
			}
		})
	}
}

// findingFilePath answers for the header every frame and for the key: it never
// touches the disk. A file that does not exist is the viewer's to report (Rule
// 130, "Not knowing is not knowing it is a no"), so a path under a target that
// does not exist is still enabled.
func TestFindingFilePathDoesNotTouchTheDisk(t *testing.T) {
	_, a := findingFilePath(scan.Finding{File: "no/such/file.yaml"}, "/definitely/not/a/directory", true)
	if !a.Enabled() {
		t.Errorf("a file that may not exist is refused (%q): the viewer says so, not the header", a.Reason)
	}

	entries, err := os.ReadDir(".")
	if err != nil {
		t.Fatal(err)
	}
	fset := token.NewFileSet()
	found := false
	for _, entry := range entries {
		name := entry.Name()
		if !strings.HasSuffix(name, ".go") || strings.HasSuffix(name, "_test.go") {
			continue
		}
		file, err := parser.ParseFile(fset, name, nil, 0)
		if err != nil {
			t.Fatal(err)
		}
		for _, decl := range file.Decls {
			fn, ok := decl.(*ast.FuncDecl)
			if !ok || fn.Name.Name != "findingFilePath" || fn.Body == nil {
				continue
			}
			found = true
			ast.Inspect(fn.Body, func(n ast.Node) bool {
				sel, ok := n.(*ast.SelectorExpr)
				if !ok {
					return true
				}
				if id, ok := sel.X.(*ast.Ident); ok && (id.Name == "os" || id.Name == "ioutil" || id.Name == "fs") {
					t.Errorf("findingFilePath calls %s.%s: it must do no I/O", id.Name, sel.Sel.Name)
				}
				return true
			})
		}
	}
	if !found {
		t.Error("findingFilePath is not declared in a non-test file")
	}
}

// ── sameFinding (AC3) ────────────────────────────────────────────────────────

func TestSameFinding(t *testing.T) {
	base := ksv0001("Container 'web' of Deployment 'app' should set allowPrivilegeEscalation")
	cve := cveWith("gomod", "go get golang.org/x/net@v0.7.0")
	with := func(f scan.Finding, edit func(*scan.Finding)) scan.Finding { edit(&f); return f }

	tests := []struct {
		name string
		a, b scan.Finding
		want bool
	}{
		{"the same finding", base, base, true},
		{"the same finding on another line, as the fix moves it", base, with(base, func(f *scan.Finding) { f.Line += 1 }), true},
		{"a new description is the same finding", base, with(base, func(f *scan.Finding) { f.Description = "Reworded" }), true},
		{"another message", base, with(base, func(f *scan.Finding) { f.Message = "Container 'sidecar' of Deployment 'app'" }), false},
		{"another title", base, with(base, func(f *scan.Finding) { f.Title = "Another title" }), false},
		{"another file", base, with(base, func(f *scan.Finding) { f.File = "other.yaml" }), false},
		{"another rule", base, with(base, func(f *scan.Finding) { f.ID = "KSV-0012" }), false},
		{"another source", base, with(base, func(f *scan.Finding) { f.Source = scan.SourceKubeconform }), false},
		{"a rule written with its AVD id is the same rule", with(base, func(f *scan.Finding) { f.ID = "DS002" }),
			with(base, func(f *scan.Finding) { f.ID = "AVD-DS-0002" }), true},
		{"the same CVE", cve, cve, true},
		{"another version of the package", cve, with(cve, func(f *scan.Finding) { f.Version = "0.2.0" }), false},
		{"another package", cve, with(cve, func(f *scan.Finding) { f.PkgName = "golang.org/x/text" }), false},
		{"another CVE", cve, with(cve, func(f *scan.Finding) { f.ID = "CVE-2026-2222" }), false},
	}
	for _, tc := range tests {
		t.Run(tc.name, func(t *testing.T) {
			if got := sameFinding(tc.a, tc.b); got != tc.want {
				t.Errorf("sameFinding = %v, want %v", got, tc.want)
			}
			if got := sameFinding(tc.b, tc.a); got != tc.want {
				t.Errorf("sameFinding is not symmetric: reversed = %v, want %v", got, tc.want)
			}
		})
	}
}
