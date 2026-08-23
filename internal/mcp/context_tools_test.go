package mcp

import (
	"encoding/json"
	"reflect"
	"strings"
	"testing"

	"github.com/anthnel/devdesk/internal/config"
)

// §3.9's guarantee has to survive the projection: no secret DevDesk holds is
// written to a file DevDesk owns, so a projection of that file has nothing to
// redact — provided nobody adds a field that could carry one.
//
// This walks the output types rather than one answer, because a field that is
// empty in the fixture would pass a value check while still being there.
func TestNothingInAContextAnswerCanCarryASecret(t *testing.T) {
	banned := []string{"token", "password", "secret", "credential", "passphrase", "apikey", "privatekey"}

	var walk func(t *testing.T, typ reflect.Type, path string, seen map[reflect.Type]bool)
	walk = func(t *testing.T, typ reflect.Type, path string, seen map[reflect.Type]bool) {
		for typ.Kind() == reflect.Pointer || typ.Kind() == reflect.Slice || typ.Kind() == reflect.Array {
			typ = typ.Elem()
		}
		if typ.Kind() != reflect.Struct || seen[typ] {
			return
		}
		seen[typ] = true

		for i := 0; i < typ.NumField(); i++ {
			field := typ.Field(i)
			name := strings.ToLower(strings.NewReplacer("_", "", "-", "").Replace(field.Name))
			for _, bad := range banned {
				// A secret is a string. A bool named SecretScanning says whether
				// a stage runs, which is a different claim and cannot carry a
				// credential — so the kind is part of the rule rather than an
				// exception to it.
				if strings.Contains(name, bad) && carriesText(field.Type) {
					t.Errorf("%s.%s is a string-typed field whose name suggests a credential — the guarantee is that no field exists for one, not that it is emptied on the way out",
						path, field.Name)
				}
			}
			walk(t, field.Type, path+"."+field.Name, seen)
		}
	}

	walk(t, reflect.TypeOf(contextGetOut{}), "contextGetOut", map[reflect.Type]bool{})
	// The scan tools go out over the same pipe, and their own guarantee has a
	// test of its own (TestTheMatchedStringOfASecretNeverLeaves). This checks the
	// shape rather than one answer, which is what catches a field added later.
	walk(t, reflect.TypeOf(finding{}), "finding", map[reflect.Type]bool{})
	walk(t, reflect.TypeOf(workspaceRepo{}), "workspaceRepo", map[reflect.Type]bool{})
}

// carriesText says whether a field could hold a credential at all. Strings can;
// counts and flags cannot.
func carriesText(typ reflect.Type) bool {
	for typ.Kind() == reflect.Pointer || typ.Kind() == reflect.Slice || typ.Kind() == reflect.Array {
		typ = typ.Elem()
	}
	return typ.Kind() == reflect.String
}

func TestContextGetReportsTheServedContextsConfiguration(t *testing.T) {
	env := testEnv(nil)
	env.Config.App.WorkspacesDir = "/home/u/work"
	env.Config.Forge.Type = "github"
	env.Config.Forge.URL = "https://github.com"
	env.Config.Scan.EnableSecret = true
	env.Config.Registry.Registries = []config.RegistryItem{{
		Slug: "nx", Alias: "nx", URL: "nexus.example.com",
		RepoPrefix: "docker-hosted", Kind: "registry", AuthMode: "credentials",
	}}
	env.Config.Status.Components = []config.ComponentConfig{{
		Name: "api", Type: "https", Target: "https://api.example.com",
	}}

	var out contextGetOut
	callTool(t, connect(t, env), "context_get", nil, &out)

	if out.Name != "work" {
		t.Errorf("name = %q, want the served context", out.Name)
	}
	if out.WorkspacesDir != "/home/u/work" {
		t.Errorf("workspaces_dir = %q", out.WorkspacesDir)
	}
	if out.Forge.Type != "github" || out.Forge.URL != "https://github.com" {
		t.Errorf("forge = %+v", out.Forge)
	}
	if !out.Scan.SecretScanning {
		t.Error("scan.secret_scanning = false, want the configured true")
	}
	if len(out.Registries) != 1 || out.Registries[0].RepoPrefix != "docker-hosted" {
		t.Errorf("registries = %+v", out.Registries)
	}
	if len(out.Monitors) != 1 || out.Monitors[0].Target != "https://api.example.com" {
		t.Errorf("monitors = %+v", out.Monitors)
	}
}

// The server answers for one context and reads no other file. A tool that took
// a context name would undo the scope decision in one argument.
func TestContextGetTakesNoContextArgument(t *testing.T) {
	cs := connect(t, testEnv(nil))

	res, err := cs.ListTools(t.Context(), nil)
	if err != nil {
		t.Fatalf("ListTools: %v", err)
	}
	for _, tool := range res.Tools {
		if tool.Name != "context_get" {
			continue
		}
		schema, err := json.Marshal(tool.InputSchema)
		if err != nil {
			t.Fatalf("marshal input schema: %v", err)
		}
		if strings.Contains(string(schema), "properties") {
			t.Errorf("context_get accepts arguments (%s) — it must answer for the served context only", schema)
		}
	}
}
