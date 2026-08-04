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

// ── Auth mode ────────────────────────────────────────────────────────────────

// auth_enabled said whether to log in, not whether to send what was stored.
// The mode says both, so the boolean has to carry over rather than be dropped.
func TestAuthEnabledMigratesToAMode(t *testing.T) {
	items := []RegistryItem{
		{URL: "https://a.example.com", AuthEnabled: true},                          //nolint:staticcheck // that is what is being migrated
		{URL: "https://b.example.com", AuthEnabled: false},                         //nolint:staticcheck // same
		{URL: "https://c.example.com", AuthMode: AuthAnonymous, AuthEnabled: true}, //nolint:staticcheck // same
	}

	if err := normalizeRegistries(items); err != nil {
		t.Fatalf("normalizeRegistries: %v", err)
	}

	want := []string{AuthCredentials, AuthAnonymous, AuthAnonymous}
	for i, w := range want {
		if items[i].AuthMode != w {
			t.Errorf("items[%d].AuthMode = %q, want %q", i, items[i].AuthMode, w)
		}
		if items[i].AuthEnabled { //nolint:staticcheck // the point is that it is cleared
			t.Errorf("items[%d] kept auth_enabled, so the next save writes it back", i)
		}
	}
}

// docker login is keyed on the host a member shares with its group, so a member
// cannot hold a password of its own — there is nowhere to put it.
func TestAMemberCannotDeclareItsOwnCredentials(t *testing.T) {
	items := []RegistryItem{
		{URL: "https://nexus.example.com/repository/g", Slug: "grp", Kind: KindGroup, AuthMode: AuthCredentials},
		{URL: "https://nexus.example.com/repository/m", Slug: "member", Parent: "grp", AuthMode: AuthCredentials},
	}

	err := normalizeRegistries(items)
	if err == nil {
		t.Fatal("a member declaring its own credentials was accepted")
	}
	if !strings.Contains(err.Error(), "member") {
		t.Errorf("err = %v, want it to name the entry", err)
	}
}

// The other direction: inherit means "take the group's", and with no group there
// is nothing to take.
func TestInheritWithoutAGroupIsRefused(t *testing.T) {
	items := []RegistryItem{{URL: "https://a.example.com", Slug: "lone", AuthMode: AuthInherit}}

	err := normalizeRegistries(items)
	if err == nil {
		t.Fatal("an entry inheriting from nothing was accepted")
	}
	if !strings.Contains(err.Error(), AuthInherit) {
		t.Errorf("err = %v, want it to name the mode", err)
	}
}

func TestAnUnknownAuthModeIsRefused(t *testing.T) {
	items := []RegistryItem{{URL: "https://a.example.com", Slug: "typo", AuthMode: "credentails"}}

	err := normalizeRegistries(items)
	if err == nil {
		t.Fatal("an unknown auth mode was accepted")
	}
	if !strings.Contains(err.Error(), "credentails") {
		t.Errorf("err = %v, want it to quote the value", err)
	}
}

// Resolution is where inherit stops being a word and becomes a decision.
func TestResolveAuthMode(t *testing.T) {
	group := RegistryItem{Slug: "grp", AuthMode: AuthCredentials}
	anonGroup := RegistryItem{Slug: "grp", AuthMode: AuthAnonymous}

	tests := []struct {
		name   string
		item   RegistryItem
		parent *RegistryItem
		want   string
	}{
		{"a member takes its group's", RegistryItem{AuthMode: AuthInherit}, &group, AuthCredentials},
		{"including a refusal", RegistryItem{AuthMode: AuthInherit}, &anonGroup, AuthAnonymous},
		{"a member may still refuse alone", RegistryItem{AuthMode: AuthAnonymous}, &group, AuthAnonymous},
		{"inheriting from nothing sends nothing", RegistryItem{AuthMode: AuthInherit}, nil, AuthAnonymous},
		{"saying nothing keeps the old behaviour", RegistryItem{}, nil, AuthCredentials},
	}
	for _, tt := range tests {
		if got := ResolveAuthMode(tt.item, tt.parent); got != tt.want {
			t.Errorf("%s: ResolveAuthMode = %q, want %q", tt.name, got, tt.want)
		}
	}
}

// UsesCredentials is what every caller branches on, and an unresolved inherit
// reaching it must fail closed.
func TestUsesCredentials(t *testing.T) {
	for mode, want := range map[string]bool{
		AuthCredentials: true,
		"":              true, // an entry that says nothing, as before the mode existed
		AuthAnonymous:   false,
		AuthInherit:     false, // unresolved: the safe reading
	} {
		if got := UsesCredentials(mode); got != want {
			t.Errorf("UsesCredentials(%q) = %v, want %v", mode, got, want)
		}
	}
}

func TestTheAuthCycleListsStartOnTheOldBehaviour(t *testing.T) {
	if got := AuthModes(false)[0]; got != AuthCredentials {
		t.Errorf("AuthModes(false)[0] = %q, want %q", got, AuthCredentials)
	}
	if got := AuthModes(true)[0]; got != AuthInherit {
		t.Errorf("AuthModes(true)[0] = %q, want %q", got, AuthInherit)
	}
	for _, mode := range AuthModes(true) {
		if mode == AuthCredentials {
			t.Error("a member is offered credentials of its own, which it cannot store")
		}
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

// The boolean has to survive a round trip through the file, and then leave it:
// a config carrying both would have two answers to the same question.
func TestLoadMigratesAuthEnabledAndSaveDropsIt(t *testing.T) {
	writeContext(t, `
registry:
  registries:
    - url: https://a.example.com
      slug: a
      auth_enabled: true
    - url: https://b.example.com
      slug: b
      auth_enabled: false
`)

	cfg, err := LoadContext("default")
	if err != nil {
		t.Fatalf("LoadContext: %v", err)
	}
	if got := cfg.Registry.Registries[0].AuthMode; got != AuthCredentials {
		t.Errorf("auth_enabled: true loaded as %q, want %q", got, AuthCredentials)
	}
	if got := cfg.Registry.Registries[1].AuthMode; got != AuthAnonymous {
		t.Errorf("auth_enabled: false loaded as %q, want %q", got, AuthAnonymous)
	}

	if err := SaveContext(cfg, "default"); err != nil {
		t.Fatalf("SaveContext: %v", err)
	}
	path, err := GetContextPath("default")
	if err != nil {
		t.Fatalf("GetContextPath: %v", err)
	}
	body, err := os.ReadFile(path) //nolint:gosec // a path this test just wrote
	if err != nil {
		t.Fatalf("reading the saved config: %v", err)
	}
	if strings.Contains(string(body), "auth_enabled") {
		t.Errorf("the saved config still carries auth_enabled:\n%s", body)
	}
	if !strings.Contains(string(body), "auth_mode: "+AuthCredentials) {
		t.Errorf("the saved config does not carry the migrated mode:\n%s", body)
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
