package config

import (
	"errors"
	"os"
	"strings"
	"testing"

	"gopkg.in/yaml.v3"
)

// DEVDESK-663 (S-01): a context created by CreateContext or on a first launch
// carries the Docker Hub entry. The entry comes from one constructor, and is
// injected in newContextConfig only, never in Default() or applyDefaults.

// wantHub is the entry of decision D-E, spelled out so that a change of the
// constructor shows up here and not only in the code that uses it.
func wantHub() RegistryItem {
	return RegistryItem{
		Slug:     "docker-io",
		Kind:     KindRegistry,
		URL:      "docker.io",
		AuthMode: AuthAnonymous,
	}
}

// rawRegistries returns the registries list exactly as the file spells it, so a
// field the struct would hide (an "integrated" marker) is still seen.
func rawRegistries(t *testing.T, path string) []map[string]any {
	t.Helper()
	var doc struct {
		Registry struct {
			Registries []map[string]any `yaml:"registries"`
		} `yaml:"registry"`
	}
	if err := yaml.Unmarshal([]byte(readFile(t, path)), &doc); err != nil {
		t.Fatalf("%s is not valid YAML: %v", path, err)
	}
	return doc.Registry.Registries
}

func TestDefaultHubRegistry_Values(t *testing.T) {
	got := DefaultHubRegistry()
	if got != wantHub() {
		t.Errorf("DefaultHubRegistry() = %+v, want %+v", got, wantHub())
	}
	if again := DefaultHubRegistry(); again != got {
		t.Errorf("two calls differ: %+v and %+v", got, again)
	}
}

func TestNewContextConfig_CarriesExactlyTheHubEntry(t *testing.T) {
	got := newContextConfig().Registry.Registries
	if len(got) != 1 || got[0] != DefaultHubRegistry() {
		t.Fatalf("newContextConfig registries = %+v, want exactly [%+v]", got, DefaultHubRegistry())
	}
	// Two configs must not share their list: editing one cannot change the other.
	a, b := newContextConfig(), newContextConfig()
	a.Registry.Registries[0].Alias = "changed"
	if b.Registry.Registries[0].Alias != "" {
		t.Errorf("two newContextConfig() share their registries slice")
	}
}

// ── criterion 1 ──────────────────────────────────────────────────────────────

func TestCreateContext_WritesExactlyOneHubEntry(t *testing.T) {
	setupTmpHome(t)
	if err := CreateContext("dev"); err != nil {
		t.Fatal(err)
	}
	path, _ := GetContextPath("dev")

	raw := rawRegistries(t, path)
	if len(raw) != 1 {
		t.Fatalf("file has %d registry entries, want exactly 1:\n%s", len(raw), readFile(t, path))
	}
	// The explicit keys are the ones that matter: a missing slug would be
	// derived at load and make the first save change the file (D-E).
	want := map[string]any{
		"slug": "docker-io", "kind": "registry", "url": "docker.io", "auth_mode": "anonymous",
	}
	for k, v := range want {
		if raw[0][k] != v {
			t.Errorf("file entry %q = %v, want %v", k, raw[0][k], v)
		}
	}
	// Everything else is empty or absent: no provider, username, alias, parent,
	// prefix, and no "integrated" marker of any name.
	for k, v := range raw[0] {
		if _, expected := want[k]; expected {
			continue
		}
		if s, isStr := v.(string); !isStr || s != "" {
			t.Errorf("file entry carries an extra field %q = %v", k, v)
		}
	}
}

func TestCreateContext_LoadReturnsTheEntryAsWritten(t *testing.T) {
	setupTmpHome(t)
	if err := CreateContext("dev"); err != nil {
		t.Fatal(err)
	}
	cfg, err := LoadContext("dev")
	if err != nil {
		t.Fatal(err)
	}
	if got := cfg.Registry.Registries; len(got) != 1 || got[0] != DefaultHubRegistry() {
		t.Errorf("LoadContext registries = %+v, want exactly [%+v]", got, DefaultHubRegistry())
	}
}

// registriesBlock returns the text of the registries list of a context file:
// the "registries:" key and the lines indented under it.
func registriesBlock(t *testing.T, content string) string {
	t.Helper()
	lines := strings.Split(content, "\n")
	var out []string
	in := false
	for _, l := range lines {
		switch {
		case !in && strings.TrimSpace(l) == "registries:":
			in = true
			out = append(out, l)
		case in && (strings.HasPrefix(l, "        ") || strings.HasPrefix(l, "    - ")):
			out = append(out, l)
		case in:
			return strings.Join(out, "\n")
		}
	}
	return strings.Join(out, "\n")
}

// The entry survives a load and a first save byte for byte. Only the registries
// block is compared: the rest of a freshly created file is not idempotent today,
// for reasons unrelated to this story (scan.image_verification and
// scan.tools.cosign.source are written empty by Default() and filled at load).
func TestCreateContext_FirstSaveKeepsTheHubEntryUnchanged(t *testing.T) {
	setupTmpHome(t)
	if err := CreateContext("dev"); err != nil {
		t.Fatal(err)
	}
	path, _ := GetContextPath("dev")
	before := registriesBlock(t, readFile(t, path))
	if !strings.Contains(before, "url: docker.io") || !strings.Contains(before, "slug: docker-io") {
		t.Fatalf("the created file has no Docker Hub entry:\n%s", readFile(t, path))
	}

	cfg, err := LoadContext("dev")
	if err != nil {
		t.Fatal(err)
	}
	if err := SaveContext(cfg, "dev"); err != nil {
		t.Fatal(err)
	}
	if after := registriesBlock(t, readFile(t, path)); after != before {
		t.Errorf("the first save changed the registries block.\nbefore:\n%s\nafter:\n%s", before, after)
	}
	// And the entry is still exactly one, with no duplicate from a migration.
	if raw := rawRegistries(t, path); len(raw) != 1 {
		t.Errorf("after the first save the file has %d registry entries, want 1", len(raw))
	}
}

// ── criterion 6 ──────────────────────────────────────────────────────────────

func TestCreateContext_NameTakenKeepsTheFileIntact(t *testing.T) {
	cases := []struct {
		name    string
		prepare func(t *testing.T) // creates the context "dev"
	}{
		{"created by CreateContext", func(t *testing.T) {
			if err := CreateContext("dev"); err != nil {
				t.Fatal(err)
			}
		}},
		{"hand-written without registries", func(t *testing.T) {
			if err := EnsureConfigDir(); err != nil {
				t.Fatal(err)
			}
			path, _ := GetContextPath("dev")
			if err := os.WriteFile(path, []byte("app:\n  theme: dark\n"), 0o600); err != nil {
				t.Fatal(err)
			}
		}},
	}
	for _, tc := range cases {
		t.Run(tc.name, func(t *testing.T) {
			setupTmpHome(t)
			tc.prepare(t)
			path, _ := GetContextPath("dev")
			before := readFile(t, path)

			err := CreateContext("dev")
			if !errors.Is(err, ErrContextExists) {
				t.Fatalf("CreateContext on a taken name = %v, want ErrContextExists", err)
			}
			if after := readFile(t, path); after != before {
				t.Errorf("the existing file changed.\nbefore:\n%s\nafter:\n%s", before, after)
			}
		})
	}
}

// ── criterion 2 ──────────────────────────────────────────────────────────────

func TestCreateDefaultContextIfMissing_BlankMachineWritesTheHubEntry(t *testing.T) {
	setupTmpHome(t)
	created, err := CreateDefaultContextIfMissing()
	if err != nil || !created {
		t.Fatalf("CreateDefaultContextIfMissing() = %v, %v, want true, nil", created, err)
	}
	path, _ := GetContextPath("default")
	raw := rawRegistries(t, path)
	if len(raw) != 1 || raw[0]["url"] != "docker.io" || raw[0]["slug"] != "docker-io" || raw[0]["auth_mode"] != "anonymous" {
		t.Fatalf("config.yaml registries = %v, want the Docker Hub entry once:\n%s", raw, readFile(t, path))
	}
	cfg, err := Load()
	if err != nil {
		t.Fatal(err)
	}
	if got := cfg.Registry.Registries; len(got) != 1 || got[0] != DefaultHubRegistry() {
		t.Errorf("Load registries = %+v, want exactly [%+v]", got, DefaultHubRegistry())
	}
}

func TestCreateDefaultContextIfMissing_SameEntryAsCreateContext(t *testing.T) {
	setupTmpHome(t)
	if err := CreateContext("dev"); err != nil {
		t.Fatal(err)
	}
	if _, err := CreateDefaultContextIfMissing(); err != nil {
		t.Fatal(err)
	}
	dev, _ := GetContextPath("dev")
	def, _ := GetContextPath("default")
	if a, b := rawRegistries(t, dev), rawRegistries(t, def); len(a) != 1 || len(b) != 1 || a[0]["url"] != b[0]["url"] ||
		a[0]["slug"] != b[0]["slug"] || a[0]["auth_mode"] != b[0]["auth_mode"] {
		t.Errorf("the two creation paths disagree: %v vs %v", a, b)
	}
}

func TestCreateDefaultContextIfMissing_ExistingFileIsNotModified(t *testing.T) {
	cases := []struct{ name, content string }{
		{"no registries", "app:\n  theme: dark\n"},
		{"another registry", "registry:\n  registries:\n    - slug: corp\n      url: registry.corp.example\n      auth_mode: credentials\n"},
		{"invalid yaml", "app: [unterminated\n"},
		{"empty file", ""},
	}
	for _, tc := range cases {
		t.Run(tc.name, func(t *testing.T) {
			setupTmpHome(t)
			if err := EnsureConfigDir(); err != nil {
				t.Fatal(err)
			}
			path, _ := GetContextPath("default")
			if err := os.WriteFile(path, []byte(tc.content), 0o600); err != nil {
				t.Fatal(err)
			}
			created, err := CreateDefaultContextIfMissing()
			if err != nil || created {
				t.Fatalf("CreateDefaultContextIfMissing() = %v, %v, want false, nil", created, err)
			}
			if got := readFile(t, path); got != tc.content {
				t.Errorf("config.yaml changed:\n%s", got)
			}
		})
	}
}

// ── criterion 5 ──────────────────────────────────────────────────────────────

// The injection belongs to newContextConfig only. If it moves to Default() or
// applyDefaults, the load fallback of main.go and every file without a
// registries key would gain the entry, and a deleted entry would come back.
func TestDefaultAndApplyDefaults_StayWithoutRegistries(t *testing.T) {
	if got := Default().Registry.Registries; len(got) != 0 {
		t.Errorf("Default() registries = %+v, want none", got)
	}

	cfg := &Config{}
	if err := applyDefaults(cfg); err != nil {
		t.Fatal(err)
	}
	if got := cfg.Registry.Registries; len(got) != 0 {
		t.Errorf("applyDefaults(&Config{}) registries = %+v, want none", got)
	}

	// Applied twice on a default config: still nothing.
	d := Default()
	if err := applyDefaults(d); err != nil {
		t.Fatal(err)
	}
	if got := d.Registry.Registries; len(got) != 0 {
		t.Errorf("applyDefaults(Default()) registries = %+v, want none", got)
	}
}

func TestLoad_WithoutAFileFallsBackToNoRegistries(t *testing.T) {
	setupTmpHome(t)
	cfg, err := Load()
	if err != nil {
		t.Fatal(err)
	}
	if got := cfg.Registry.Registries; len(got) != 0 {
		t.Errorf("Load on a blank machine registries = %+v, want none (the load fallback is Default())", got)
	}
}

func TestLoadContext_FileWithoutRegistriesKeyStaysEmpty(t *testing.T) {
	setupTmpHome(t)
	if err := EnsureConfigDir(); err != nil {
		t.Fatal(err)
	}
	path, _ := GetContextPath("old")
	if err := os.WriteFile(path, []byte("app:\n  theme: dark\n"), 0o600); err != nil {
		t.Fatal(err)
	}
	cfg, err := LoadContext("old")
	if err != nil {
		t.Fatal(err)
	}
	if got := cfg.Registry.Registries; len(got) != 0 {
		t.Errorf("LoadContext on an existing file registries = %+v, want none", got)
	}
}

// ── durable removal ─────────────────────────────────────────────────────────

func TestCreatedContext_RemovedHubEntryDoesNotComeBack(t *testing.T) {
	other := RegistryItem{Slug: "corp", Kind: KindRegistry, URL: "registry.corp.example", AuthMode: AuthCredentials}
	cases := []struct {
		name string
		keep []RegistryItem
	}{
		{"only the hub entry removed", nil},
		{"hub entry removed, another kept", []RegistryItem{other}},
	}
	for _, tc := range cases {
		t.Run(tc.name, func(t *testing.T) {
			setupTmpHome(t)
			if err := CreateContext("dev"); err != nil {
				t.Fatal(err)
			}
			if err := SetCurrentContext("dev"); err != nil {
				t.Fatal(err)
			}
			cfg, err := Load()
			if err != nil {
				t.Fatal(err)
			}
			if len(cfg.Registry.Registries) != 1 {
				t.Fatalf("precondition: the created context has %d registries, want 1", len(cfg.Registry.Registries))
			}
			cfg.Registry.Registries = append([]RegistryItem(nil), tc.keep...)
			if err := Save(cfg); err != nil {
				t.Fatal(err)
			}

			// Reload and save again, twice: nothing puts the entry back.
			for i := 0; i < 2; i++ {
				reloaded, err := Load()
				if err != nil {
					t.Fatal(err)
				}
				got := reloaded.Registry.Registries
				if len(got) != len(tc.keep) {
					t.Fatalf("round %d: registries = %+v, want %+v", i, got, tc.keep)
				}
				for j := range got {
					if got[j].URL == "docker.io" {
						t.Fatalf("round %d: the removed Docker Hub entry came back: %+v", i, got)
					}
				}
				if err := Save(reloaded); err != nil {
					t.Fatal(err)
				}
			}
			path, _ := GetContextPath("dev")
			if strings.Contains(readFile(t, path), "docker.io") {
				t.Errorf("the file still names docker.io:\n%s", readFile(t, path))
			}
		})
	}
}

func TestDefaultContext_RemovedHubEntryDoesNotComeBack(t *testing.T) {
	setupTmpHome(t)
	if created, err := CreateDefaultContextIfMissing(); err != nil || !created {
		t.Fatalf("CreateDefaultContextIfMissing() = %v, %v, want true, nil", created, err)
	}
	cfg, err := Load()
	if err != nil {
		t.Fatal(err)
	}
	if len(cfg.Registry.Registries) != 1 {
		t.Fatalf("precondition: the first-launch file has %d registries, want 1", len(cfg.Registry.Registries))
	}
	cfg.Registry.Registries = nil
	if err := Save(cfg); err != nil {
		t.Fatal(err)
	}
	// A later launch finds the file, so nothing is created again.
	if created, err := CreateDefaultContextIfMissing(); err != nil || created {
		t.Fatalf("second CreateDefaultContextIfMissing() = %v, %v, want false, nil", created, err)
	}
	reloaded, err := Load()
	if err != nil {
		t.Fatal(err)
	}
	if got := reloaded.Registry.Registries; len(got) != 0 {
		t.Errorf("the removed entry came back: %+v", got)
	}
}
