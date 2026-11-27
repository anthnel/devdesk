package ociresources

import (
	"go/ast"
	"go/parser"
	"go/token"
	"testing"
)

// OC-14: the form uses docker.ValidEnvName, so the ASCII-only rule (no wildcard,
// no leading dash, no invisible character) refuses here too.
func TestLaunchFormRefusesAWildcardOrFlagLikeName(t *testing.T) {
	for _, input := range []string{"*=x", "PREFIX*=x", "?=x", "-e=x", "--privileged=x", "-V=x", "A\u202eB=x", "A\u200bB=x", "caf\u00e9=x"} {
		t.Run(input, func(t *testing.T) {
			f := newEnvForm()
			setEnv(f, input)

			if opts, ok := submitOpts(f); ok {
				t.Fatalf("Environment %q was accepted: %+v", input, opts)
			}
			if in, _ := envUnderlined(f); !in {
				t.Error("the field is not underlined")
			}
		})
	}
	for _, input := range []string{"API_TOKEN=x", "my-app.setting=x", "_X=x", "1ABC=x"} {
		t.Run(input, func(t *testing.T) {
			f := newEnvForm()
			setEnv(f, input)

			if _, ok := submitOpts(f); !ok {
				t.Errorf("Environment %q was refused: %q", input, footerStatus(f).Text)
			}
		})
	}
}

// The engine's output can echo an environment value, and ContainerLaunchCompleteMsg.Err
// carries it (the view needs it). handleContainerLaunchComplete therefore never
// logs msg.Err itself: it goes through docker.LaunchLogText. This guard reads the
// source and fails on a log call that takes msg.Err as an argument.
func TestTheLaunchCompletionNeverLogsTheRawError(t *testing.T) {
	file, err := parser.ParseFile(token.NewFileSet(), "launch.go", nil, 0)
	if err != nil {
		t.Fatal(err)
	}
	found := false
	for _, decl := range file.Decls {
		fn, ok := decl.(*ast.FuncDecl)
		if !ok || fn.Name.Name != "handleContainerLaunchComplete" {
			continue
		}
		found = true
		ast.Inspect(fn.Body, func(n ast.Node) bool {
			call, ok := n.(*ast.CallExpr)
			if !ok {
				return true
			}
			if sel, ok := call.Fun.(*ast.SelectorExpr); !ok || sel.Sel.Name != "Printf" {
				return true
			}
			for _, arg := range call.Args {
				if sel, ok := arg.(*ast.SelectorExpr); ok && sel.Sel.Name == "Err" {
					t.Errorf("handleContainerLaunchComplete logs %s: use docker.LaunchLogText", sel.Sel.Name)
				}
			}
			return true
		})
	}
	if !found {
		t.Fatal("handleContainerLaunchComplete not found in launch.go: the guard no longer sees it")
	}
}
