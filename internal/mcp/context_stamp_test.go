package mcp

import (
	"go/ast"
	"go/parser"
	"go/token"
	"os"
	"strings"
	"testing"
)

// §3.38 fixed the served context at process start so it could not change
// underneath an agent mid-conversation. §3.61 gives that up — the server serves
// what the screen serves — and two things pay for it: the session is dropped on
// a switch, and **every answer whose content depends on the context says which
// one served it**. An agent comparing two answers then sees the change instead
// of suffering it.
//
// This walks the output types rather than the values: a field left empty in a
// fixture would pass a value check, and what is being held here is the schema.
//
// The exceptions are declared, in the spirit of keymap.DeclaredExceptions():
// an answer about the machine is the same answer whatever context is on screen,
// and stamping one would say the opposite.
func TestEveryContextDependentAnswerSaysWhichContextServedIt(t *testing.T) {
	machineWide := map[string]string{
		"containersListOut": "the daemon's containers are the machine's, not a context's",
		"imagesListOut":     "the local images are the machine's",
		"portsListOut":      "the socket table is the machine's",
		"netCheckOut":       "a probe answers about the network, from wherever it is run",
		"contextListOut":    "it lists the contexts, and names the served one in its own field",
		"contextGetOut":     "it *is* the context, and names it in its own field",
	}

	fset := token.NewFileSet()
	entries, err := os.ReadDir(".")
	if err != nil {
		t.Fatalf("read internal/mcp: %v", err)
	}

	// The rule is about a tool's *answer*, not about every type that ends in
	// Out: containerOut and socketOut are rows inside one, and stamping each of
	// them would repeat the context per element. So the types are found where
	// they are actually returned — the second result of a handler whose first
	// is *sdk.CallToolResult — rather than by their name.
	structs := map[string]*ast.StructType{}
	answers := map[string]token.Position{}

	for _, entry := range entries {
		name := entry.Name()
		if entry.IsDir() || !strings.HasSuffix(name, ".go") || strings.HasSuffix(name, "_test.go") {
			continue
		}

		file, err := parser.ParseFile(fset, name, nil, parser.SkipObjectResolution)
		if err != nil {
			t.Fatalf("parse %s: %v", name, err)
		}

		ast.Inspect(file, func(n ast.Node) bool {
			if spec, ok := n.(*ast.TypeSpec); ok {
				if st, ok := spec.Type.(*ast.StructType); ok {
					structs[spec.Name.Name] = st
				}
				return true
			}
			fn, ok := n.(*ast.FuncType)
			if !ok || fn.Results == nil || len(fn.Results.List) != 3 {
				return true
			}
			if !isCallToolResult(fn.Results.List[0].Type) {
				return true
			}
			ident, ok := fn.Results.List[1].Type.(*ast.Ident)
			if !ok {
				return true
			}
			answers[ident.Name] = fset.Position(fn.Pos())
			return true
		})
	}

	if len(answers) == 0 {
		t.Fatal("found no tool answers at all — the shape this test matches on has changed")
	}

	for typeName, pos := range answers {
		st, ok := structs[typeName]
		if !ok {
			t.Errorf("%s: a tool answers with %s, which is not a struct declared here", pos, typeName)
			continue
		}
		why, exempt := machineWide[typeName]
		switch {
		case exempt && carriesContext(st):
			t.Errorf("%s: %s carries a context field, but %s", pos, typeName, why)
		case !exempt && !carriesContext(st):
			t.Errorf("%s: %s has no `json:\"context\"` field. An answer that depends on which context served it has to say which one, or an agent reads it against the wrong one after a switch (§3.61). If it is the same answer in every context, declare it in machineWide with the reason.", pos, typeName)
		}
	}

	// A declared exception for a type nothing answers with any more is a note
	// about nothing, and the next reader takes it for a live constraint.
	for typeName := range machineWide {
		if _, ok := answers[typeName]; !ok {
			t.Errorf("machineWide declares %q, which no tool answers with any more", typeName)
		}
	}
}

// isCallToolResult matches *sdk.CallToolResult, which is what marks a function
// literal as a tool handler.
func isCallToolResult(expr ast.Expr) bool {
	star, ok := expr.(*ast.StarExpr)
	if !ok {
		return false
	}
	sel, ok := star.X.(*ast.SelectorExpr)
	if !ok {
		return false
	}
	pkg, ok := sel.X.(*ast.Ident)
	return ok && pkg.Name == "sdk" && sel.Sel.Name == "CallToolResult"
}

func carriesContext(st *ast.StructType) bool {
	for _, field := range st.Fields.List {
		if field.Tag == nil {
			continue
		}
		if strings.Contains(field.Tag.Value, `json:"context"`) {
			return true
		}
	}
	return false
}
