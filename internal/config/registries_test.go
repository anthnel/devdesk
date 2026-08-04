package config

import (
	"os"
	"path/filepath"
	"strings"
	"testing"
)

// A slug has to survive being a file name, a cache key and an unquoted YAML
// scalar, so it is reduced to one alphabet rather than trusted as typed.
func TestSlugify(t *testing.T) {
	tests := []struct {
		in, want string
	}{
		{"prod", "prod"},
		{"Prod Registry", "prod-registry"},
		{"nexus.example.com", "nexus-example-com"},
		{"nexus.example.com:8081", "nexus-example-com-8081"},
		{"  spaced  ", "spaced"},
		{"--dashes--", "dashes"},
		{"a///b", "a-b"},
		{"", ""},
		{"???", ""},
		{"Ünïcode", "n-code"}, // not transliterated: anything outside the alphabet separates
	}
	for _, tt := range tests {
		if got := Slugify(tt.in); got != tt.want {
			t.Errorf("Slugify(%q) = %q, want %q", tt.in, got, tt.want)
		}
	}
}

// The form opens a new entry on the first value of each list, so the order is
// what decides the defaults: a plain registry served by nothing in particular.
func TestTheCycleListsStartOnTheDefaults(t *testing.T) {
	if got := Kinds()[0]; got != KindRegistry {
		t.Errorf("Kinds()[0] = %q, want %q", got, KindRegistry)
	}
	if got := Providers()[0]; got != ProviderGeneric {
		t.Errorf("Providers()[0] = %q, want %q", got, ProviderGeneric)
	}
	for _, want := range []string{ProviderNexus, ProviderHarbor, ProviderArtifactory, ProviderGitLab} {
		found := false
		for _, p := range Providers() {
			found = found || p == want
		}
		if !found {
			t.Errorf("Providers() does not offer %q, so no group can declare it", want)
		}
	}
}

// The alias is what the user already reads in the table, so it is the first
// choice; the host is the fallback because it is the only other thing every
// entry has.
func TestASlugIsDerivedFromTheAliasThenTheHost(t *testing.T) {
	tests := []struct {
		name string
		item RegistryItem
		want string
	}{
		{"alias wins", RegistryItem{Alias: "prod", URL: "https://nexus.example.com"}, "prod"},
		{"host when no alias", RegistryItem{URL: "https://nexus.example.com/repository/g"}, "nexus-example-com"},
		{"scheme is optional", RegistryItem{URL: "registry.example.com:5000/path"}, "registry-example-com-5000"},
		{"nothing to derive from", RegistryItem{}, ""},
	}
	for _, tt := range tests {
		if got := DeriveSlug(tt.item); got != tt.want {
			t.Errorf("%s: DeriveSlug(%+v) = %q, want %q", tt.name, tt.item, got, tt.want)
		}
	}
}

// Every entry must come out of a load with a slug, because it is what the group
// cache and the parent link are keyed on.
func TestEveryRegistryLeavesNormalizationWithASlug(t *testing.T) {
	items := []RegistryItem{
		{URL: "https://nexus.example.com", Alias: "prod"},
		{URL: "https://docker.io"},
		{}, // nothing to derive from at all
	}

	if err := normalizeRegistries(items); err != nil {
		t.Fatalf("normalizeRegistries: %v", err)
	}

	want := []string{"prod", "docker-io", fallbackSlug}
	for i, w := range want {
		if items[i].Slug != w {
			t.Errorf("items[%d].Slug = %q, want %q", i, items[i].Slug, w)
		}
	}
}

// Two registries aliased the same are ordinary, and a derived slug is DevDesk's
// own doing — so it is DevDesk that has to make them distinct.
func TestDerivedSlugsNeverCollide(t *testing.T) {
	items := []RegistryItem{
		{URL: "https://a.example.com", Alias: "prod"},
		{URL: "https://b.example.com", Alias: "prod"},
		{URL: "https://c.example.com", Alias: "Prod"},
	}

	if err := normalizeRegistries(items); err != nil {
		t.Fatalf("normalizeRegistries: %v", err)
	}

	seen := map[string]bool{}
	for i, item := range items {
		if item.Slug == "" {
			t.Fatalf("items[%d] came out with no slug", i)
		}
		if seen[item.Slug] {
			t.Fatalf("items[%d] repeats the slug %q", i, item.Slug)
		}
		seen[item.Slug] = true
	}
	if items[0].Slug != "prod" {
		t.Errorf("the first entry lost its slug to a later one: %q", items[0].Slug)
	}
}

// A declared slug is a link target. Rewriting it to resolve a clash would move
// one group's members under another, so the clash is reported instead.
func TestTwoDeclaredSlugsAreRefused(t *testing.T) {
	items := []RegistryItem{
		{URL: "https://a.example.com", Slug: "shared"},
		{URL: "https://b.example.com", Slug: "shared"},
	}

	err := normalizeRegistries(items)
	if err == nil {
		t.Fatal("a config declaring the same slug twice was accepted")
	}
	if !strings.Contains(err.Error(), "shared") {
		t.Errorf("err = %v, want it to name the slug", err)
	}
}

func TestADeclaredSlugIsNotRewritten(t *testing.T) {
	items := []RegistryItem{{URL: "https://a.example.com", Alias: "prod", Slug: "  kept  "}}

	if err := normalizeRegistries(items); err != nil {
		t.Fatalf("normalizeRegistries: %v", err)
	}

	if items[0].Slug != "kept" {
		t.Errorf("Slug = %q, want the declared slug with only its whitespace removed", items[0].Slug)
	}
}

// A member pointing at a group that is not there is unreachable, and silently
// keeping it would hide a typo in a hand-edited file.
func TestAParentThatNamesNoGroupIsRefused(t *testing.T) {
	items := []RegistryItem{
		{URL: "https://a.example.com", Slug: "member", Parent: "absent"},
	}

	err := normalizeRegistries(items)
	if err == nil {
		t.Fatal("a member pointing at a group that is not configured was accepted")
	}
	if !strings.Contains(err.Error(), "absent") {
		t.Errorf("err = %v, want it to name the missing group", err)
	}
}

func TestAParentThatNamesAConfiguredGroupIsAccepted(t *testing.T) {
	items := []RegistryItem{
		{URL: "https://nexus.example.com/repository/g", Slug: "grp", Kind: KindGroup},
		{URL: "https://nexus.example.com/repository/m", Slug: "member", Parent: "grp"},
	}

	if err := normalizeRegistries(items); err != nil {
		t.Fatalf("a member of a configured group was refused: %v", err)
	}
}

// Before `kind` existed, a management URL was the only way to mark a group, and
// NexusDetector.CanHandle keyed on exactly that. Migrating such an entry to
// anything but a Nexus group would change what it does.
func TestAManagementURLFromAnOlderConfigBecomesANexusGroup(t *testing.T) {
	items := []RegistryItem{
		{URL: "https://nexus.example.com/repository/docker-group", ManagementURL: "https://nexus.example.com/repository/docker-group"},
		{URL: "https://docker.io"},
	}

	if err := normalizeRegistries(items); err != nil {
		t.Fatalf("normalizeRegistries: %v", err)
	}

	if items[0].Kind != KindGroup {
		t.Errorf("Kind = %q for an entry with a management URL, want %q", items[0].Kind, KindGroup)
	}
	if items[0].Provider != ProviderNexus {
		t.Errorf("Provider = %q, want %q — that is what used to handle it", items[0].Provider, ProviderNexus)
	}
	if items[1].Kind != KindRegistry {
		t.Errorf("Kind = %q for a plain registry, want %q", items[1].Kind, KindRegistry)
	}
	if items[1].Provider != "" {
		t.Errorf("Provider = %q on a plain registry, want it left empty", items[1].Provider)
	}
}

// A group declared as one, with no provider stated, is served by nothing in
// particular until the user says otherwise.
func TestADeclaredGroupDefaultsToTheGenericProvider(t *testing.T) {
	items := []RegistryItem{{URL: "https://harbor.example.com", Kind: KindGroup}}

	if err := normalizeRegistries(items); err != nil {
		t.Fatalf("normalizeRegistries: %v", err)
	}

	if items[0].Provider != ProviderGeneric {
		t.Errorf("Provider = %q, want %q", items[0].Provider, ProviderGeneric)
	}
}

func TestADeclaredProviderIsKept(t *testing.T) {
	items := []RegistryItem{{
		URL:           "https://nexus.example.com",
		Kind:          KindGroup,
		Provider:      ProviderHarbor,
		ManagementURL: "https://nexus.example.com",
	}}

	if err := normalizeRegistries(items); err != nil {
		t.Fatalf("normalizeRegistries: %v", err)
	}

	if items[0].Provider != ProviderHarbor {
		t.Errorf("Provider = %q, want the declared one", items[0].Provider)
	}
}

// ── Through the loader ───────────────────────────────────────────────────────

// writeContext points HOME at a temp dir and writes a config file into it, so
// no test ever reads or rewrites the developer's own ~/.devdesk.
func writeContext(t *testing.T, body string) {
	t.Helper()
	home := t.TempDir()
	t.Setenv("HOME", home)
	t.Setenv("USERPROFILE", home)

	dir := filepath.Join(home, ".devdesk")
	if err := os.MkdirAll(dir, 0o755); err != nil {
		t.Fatalf("creating the config dir: %v", err)
	}
	if err := os.WriteFile(filepath.Join(dir, "config.yaml"), []byte(body), 0o600); err != nil {
		t.Fatalf("writing the config: %v", err)
	}
}

func TestLoadFillsInSlugsForAConfigThatPredatesThem(t *testing.T) {
	writeContext(t, `
registry:
  registries:
    - url: https://nexus.example.com/repository/docker-group
      alias: nexus
      management_url: https://nexus.example.com/repository/docker-group
    - url: https://docker.io
`)

	cfg, err := LoadContext("default")
	if err != nil {
		t.Fatalf("LoadContext: %v", err)
	}

	regs := cfg.Registry.Registries
	if len(regs) != 2 {
		t.Fatalf("got %d registries, want 2", len(regs))
	}
	if regs[0].Slug != "nexus" || regs[1].Slug != "docker-io" {
		t.Errorf("slugs = %q, %q, want them derived from the alias and the host", regs[0].Slug, regs[1].Slug)
	}
	if regs[0].Kind != KindGroup {
		t.Errorf("the entry with a management URL loaded as %q, want %q", regs[0].Kind, KindGroup)
	}
}

// The registry migrated from the legacy single-registry keys goes through the
// same normalization as a listed one, or it would be the one entry with no slug.
func TestTheLegacySingleRegistryAlsoGetsASlug(t *testing.T) {
	writeContext(t, `
registry:
  url: https://legacy.example.com
  username: anthnel
`)

	cfg, err := LoadContext("default")
	if err != nil {
		t.Fatalf("LoadContext: %v", err)
	}

	regs := cfg.Registry.Registries
	if len(regs) != 1 {
		t.Fatalf("got %d registries, want the migrated one", len(regs))
	}
	if regs[0].Slug != "legacy-example-com" {
		t.Errorf("Slug = %q, want it derived from the host", regs[0].Slug)
	}
}

// A config that cannot round-trip must not load silently: the alternative is an
// application running on a model that does not match the file on disk.
func TestLoadRefusesAConfigWithDuplicateSlugs(t *testing.T) {
	writeContext(t, `
registry:
  registries:
    - url: https://a.example.com
      slug: shared
    - url: https://b.example.com
      slug: shared
`)

	_, err := LoadContext("default")
	if err == nil {
		t.Fatal("a config declaring the same slug twice loaded without complaint")
	}
	if !strings.Contains(err.Error(), "shared") {
		t.Errorf("err = %v, want it to name the slug", err)
	}
}
