package mcp

import (
	"os"
	"path/filepath"
	"strings"
	"testing"
)

func writeCatalog(t *testing.T, yaml string) {
	t.Helper()
	home := fakeCacheHome(t)
	dir := filepath.Join(home, ".devdesk")
	if err := os.MkdirAll(dir, 0o755); err != nil {
		t.Fatal(err)
	}
	if err := os.WriteFile(filepath.Join(dir, "templates.yaml"), []byte(yaml), 0o600); err != nil {
		t.Fatal(err)
	}
}

func TestTemplatesListReportsTheCatalog(t *testing.T) {
	writeCatalog(t, `templates:
  - slug: spring
    name: Spring Boot
    tags: [java]
    source: {kind: git, url: "https://ci-bot:s3cr3t-token@git.example.com/org/spring.git", ref: main}
  - slug: ansible
    name: Ansible role
    source: {kind: git, url: "git@git.example.com:org/role.git"}
  - slug: local-one
    name: Local
    source: {kind: local, path: /srv/tpl}
  - slug: BAD SLUG
    name: Rejected
    source: {kind: git, url: "https://git.example.com/x.git"}
`)

	var out templatesListOut
	callTool(t, connect(t, testEnv(nil)), "templates_list", nil, &out)

	if len(out.Templates) != 3 || out.Unusable != 1 {
		t.Fatalf("got %d templates and %d unusable, want 3 and 1: %+v", len(out.Templates), out.Unusable, out)
	}
	// Sorted by name.
	if out.Templates[0].Slug != "ansible" || out.Templates[1].Slug != "local-one" || out.Templates[2].Slug != "spring" {
		t.Errorf("order = %v", []string{out.Templates[0].Slug, out.Templates[1].Slug, out.Templates[2].Slug})
	}
	if got := out.Templates[0].URL; got != "git@git.example.com:org/role.git" {
		t.Errorf("scp url = %q, want it unchanged", got)
	}
	if got := out.Templates[2].URL; got != "https://git.example.com/org/spring.git" {
		t.Errorf("url = %q, want the userinfo removed", got)
	}
	if out.Templates[2].Ref != "main" || out.Templates[2].Kind != "git" {
		t.Errorf("spring = %+v", out.Templates[2])
	}
}

// The guarantee is on the serialised answer, not on a field name: a credential
// copied into another field would pass a shape check.
func TestNoTemplateCredentialLeavesInAnAnswer(t *testing.T) {
	writeCatalog(t, `templates:
  - slug: spring
    name: Spring Boot
    source: {kind: git, url: "https://ci-bot:s3cr3t-token@git.example.com/org/spring.git"}
  - slug: oci
    name: OCI
    source: {kind: oci, url: "https://user:oci-pass@registry.example.com", path: org/tpl, ref: v1}
`)

	raw := callToolRaw(t, connect(t, testEnv(nil)), "templates_list", nil)
	for _, secret := range []string{"s3cr3t-token", "oci-pass", "ci-bot"} {
		if strings.Contains(raw, secret) {
			t.Errorf("the answer contains %q", secret)
		}
	}
}

func TestTemplatesListWithNoCatalogIsAnEmptyList(t *testing.T) {
	fakeCacheHome(t)

	raw := callToolRaw(t, connect(t, testEnv(nil)), "templates_list", nil)
	if !strings.Contains(raw, `"templates":[]`) {
		t.Errorf("answer = %s, want an empty templates array", raw)
	}
}

func TestWithoutUserinfo(t *testing.T) {
	for in, want := range map[string]string{
		"":                                     "",
		"https://a:b@h/x.git":                  "https://h/x.git",
		"https://h/x.git":                      "https://h/x.git",
		"ssh://git@h/x.git":                    "ssh://h/x.git",
		"git@h:org/x.git":                      "git@h:org/x.git",
		"https://a:b@h:8443/x.git?ref=v1#frag": "https://h:8443/x.git?ref=v1#frag",
		"https://%zz@h/x":                      "",
	} {
		if got := withoutUserinfo(in); got != want {
			t.Errorf("withoutUserinfo(%q) = %q, want %q", in, got, want)
		}
	}
}
