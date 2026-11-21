package config

import (
	"os"
	"path/filepath"
	"slices"
	"strings"
	"testing"

	"github.com/anthnel/devdesk/internal/testutil/contextfiles"
)

// DEVDESK-665 (S-03): non-regression of the "default registry" epic. What S-01
// and S-02 added is written at creation only (newContextConfig, buildConfig);
// these tests pin the other half: an existing context is never given Docker
// Hub, a removed entry never comes back, and no load or save path creates an
// entry the file did not carry. The creation-side tests are in
// default_hub_registry_test.go and are not repeated here.

func registryURLs(cfg *Config) []string {
	var urls []string
	for _, r := range cfg.Registry.Registries {
		urls = append(urls, r.URL)
	}
	return urls
}

// writeShapes writes every fixture into one context directory, as separate
// contexts, and returns their paths by name.
func writeShapes(t *testing.T, shapes []contextfiles.Shape) map[string]string {
	t.Helper()
	if err := EnsureConfigDir(); err != nil {
		t.Fatal(err)
	}
	paths := map[string]string{}
	for _, s := range shapes {
		p, err := GetContextPath(s.Name)
		if err != nil {
			t.Fatal(err)
		}
		if err := os.WriteFile(p, []byte(s.Content), 0o600); err != nil {
			t.Fatal(err)
		}
		paths[s.Name] = p
	}
	return paths
}

// Criterion 1: a directory of existing contexts of every shape. Loading each
// one, directly and as the current context, adds no entry and leaves the file
// byte for byte as it was.
func TestExistingContextDirectory_LoadAddsNothingAndChangesNoByte(t *testing.T) {
	setupTmpHome(t)
	shapes := contextfiles.Shapes()
	paths := writeShapes(t, shapes)
	// The default context is one of them too: config.yaml is where the startup
	// path (CreateDefaultContextIfMissing, Load) looks.
	def := "registry:\n    registries: []\n"
	defPath, _ := GetContextPath("default")
	if err := os.WriteFile(defPath, []byte(def), 0o600); err != nil {
		t.Fatal(err)
	}

	if created, err := CreateDefaultContextIfMissing(); err != nil || created {
		t.Fatalf("CreateDefaultContextIfMissing() = %v, %v, want false, nil", created, err)
	}
	if cfg, err := Load(); err != nil || len(cfg.Registry.Registries) != 0 {
		t.Fatalf("Load() on the default context = %+v, %v, want no registries", cfg, err)
	}

	for _, s := range shapes {
		t.Run(s.Name, func(t *testing.T) {
			viaName, err := LoadContext(s.Name)
			if err != nil {
				t.Fatal(err)
			}
			if err := SetCurrentContext(s.Name); err != nil {
				t.Fatal(err)
			}
			viaCurrent, err := Load()
			if err != nil {
				t.Fatal(err)
			}
			for how, cfg := range map[string]*Config{"LoadContext": viaName, "Load": viaCurrent} {
				if got := registryURLs(cfg); !slices.Equal(got, s.URLs) {
					t.Errorf("%s registries = %v, want %v", how, got, s.URLs)
				}
			}
			if got := readFile(t, paths[s.Name]); got != s.Content {
				t.Errorf("loading rewrote the file.\nbefore:\n%s\nafter:\n%s", s.Content, got)
			}
		})
	}
	if got := readFile(t, defPath); got != def {
		t.Errorf("the default context file changed:\n%s", got)
	}
}

// Criterion 3: an empty list with an empty registry.url stays empty through
// both candidate injection points. The existing test feeds applyDefaults a zero
// Config and Default(); these are the non-nil empty list and the explicit
// empty url a file can spell.
func TestEmptyRegistryListAndEmptyURLStayEmpty(t *testing.T) {
	cases := map[string]*Config{
		"nil list":           {},
		"non-nil empty list": {Registry: RegistryConfig{Registries: []RegistryItem{}}},
		"empty url":          {Registry: RegistryConfig{URL: ""}},
		"both":               {Registry: RegistryConfig{URL: "", Registries: []RegistryItem{}}},
	}
	for name, cfg := range cases {
		t.Run(name, func(t *testing.T) {
			if err := applyDefaults(cfg); err != nil {
				t.Fatal(err)
			}
			if got := cfg.Registry.Registries; len(got) != 0 {
				t.Errorf("applyDefaults added registries: %+v", got)
			}
		})
	}

	for name, items := range map[string][]RegistryItem{"nil": nil, "empty": {}} {
		t.Run("normalizeRegistries "+name, func(t *testing.T) {
			if err := normalizeRegistries(items); err != nil {
				t.Fatal(err)
			}
			if len(items) != 0 {
				t.Errorf("normalizeRegistries grew the list: %+v", items)
			}
		})
	}

	// Through the file, and through a save that is reloaded: still nothing.
	byName := map[string]contextfiles.Shape{}
	for _, s := range contextfiles.Shapes() {
		byName[s.Name] = s
	}
	for _, name := range []string{"empty-list", "empty-list-empty-url", "empty-url-only", "no-section"} {
		t.Run("file "+name, func(t *testing.T) {
			setupTmpHome(t)
			path := writeShapes(t, []contextfiles.Shape{byName[name]})[name]
			cfg, err := LoadContext(name)
			if err != nil {
				t.Fatal(err)
			}
			for round := 0; round < 2; round++ {
				if len(cfg.Registry.Registries) != 0 {
					t.Fatalf("round %d: registries = %+v, want none", round, cfg.Registry.Registries)
				}
				if err := SaveContext(cfg, name); err != nil {
					t.Fatal(err)
				}
				if cfg, err = LoadContext(name); err != nil {
					t.Fatal(err)
				}
			}
			if strings.Contains(readFile(t, path), "docker.io") {
				t.Errorf("a save wrote docker.io:\n%s", readFile(t, path))
			}
		})
	}
}

// Criterion 4: the legacy registry.url migration and an entry the file already
// carries. Whatever the shape, a load then two save/reload rounds give at most
// one entry per URL, no Docker Hub entry the file did not have, and a file that
// stops moving after the first save.
func TestLoadAndSave_NeverDuplicateNorAddAnEntry(t *testing.T) {
	for _, s := range contextfiles.Shapes() {
		t.Run(s.Name, func(t *testing.T) {
			setupTmpHome(t)
			paths := writeShapes(t, []contextfiles.Shape{s})
			path := paths[s.Name]

			cfg, err := LoadContext(s.Name)
			if err != nil {
				t.Fatal(err)
			}
			var afterFirst string
			for round := 0; round < 3; round++ {
				if got := registryURLs(cfg); !slices.Equal(got, s.URLs) {
					t.Fatalf("round %d: registries = %v, want %v", round, got, s.URLs)
				}
				seen := map[string]bool{}
				slugs := map[string]bool{}
				for _, r := range cfg.Registry.Registries {
					if seen[r.URL] || slugs[r.Slug] {
						t.Fatalf("round %d: duplicate entry %+v in %+v", round, r, cfg.Registry.Registries)
					}
					seen[r.URL], slugs[r.Slug] = true, true
				}
				if err := SaveContext(cfg, s.Name); err != nil {
					t.Fatal(err)
				}
				switch round {
				case 0:
					afterFirst = readFile(t, path)
				default:
					if got := readFile(t, path); got != afterFirst {
						t.Errorf("round %d: the file still moves after the first save.\nfirst:\n%s\nnow:\n%s", round, afterFirst, got)
					}
				}
				if cfg, err = LoadContext(s.Name); err != nil {
					t.Fatal(err)
				}
			}
			// The list the file now spells holds docker.io exactly as often as the
			// fixture's own entries (or its migrated registry.url) did.
			want := 0
			for _, u := range s.URLs {
				if u == "docker.io" {
					want++
				}
			}
			got := 0
			for _, e := range rawRegistries(t, path) {
				if e["url"] == "docker.io" {
					got++
				}
			}
			if got != want {
				t.Errorf("the saved file lists docker.io %d times, want %d:\n%s", got, want, afterFirst)
			}
		})
	}
}

// Criterion 2, the paths the creation-side tests do not walk: the user removes
// the entry by hand (the key left empty or deleted), and a context other than
// the current one is loaded and saved by name. Neither load nor save puts it
// back, in two rounds.
func TestHubEntryRemovedByHandOrInANamedContextDoesNotComeBack(t *testing.T) {
	cases := []struct {
		name string
		edit func(t *testing.T, path string)
	}{
		{"hand edit to an empty list", func(t *testing.T, path string) {
			rewrite(t, path, "registry:\n    registries: []\n")
		}},
		{"hand edit deleting the registry section", func(t *testing.T, path string) {
			rewrite(t, path, "app:\n    theme: dark\n")
		}},
		{"removed through a named load and save", func(t *testing.T, path string) {
			cfg, err := LoadContext("dev")
			if err != nil {
				t.Fatal(err)
			}
			cfg.Registry.Registries = nil
			if err := SaveContext(cfg, "dev"); err != nil {
				t.Fatal(err)
			}
		}},
	}
	for _, tc := range cases {
		t.Run(tc.name, func(t *testing.T) {
			setupTmpHome(t)
			if err := CreateContext("dev"); err != nil {
				t.Fatal(err)
			}
			path, _ := GetContextPath("dev")
			if !strings.Contains(readFile(t, path), "docker.io") {
				t.Fatalf("precondition: the new context has no Docker Hub entry:\n%s", readFile(t, path))
			}
			tc.edit(t, path)

			for round := 0; round < 2; round++ {
				cfg, err := LoadContext("dev")
				if err != nil {
					t.Fatal(err)
				}
				if len(cfg.Registry.Registries) != 0 {
					t.Fatalf("round %d: the removed entry came back on load: %+v", round, cfg.Registry.Registries)
				}
				if err := SaveContext(cfg, "dev"); err != nil {
					t.Fatal(err)
				}
				if strings.Contains(readFile(t, path), "docker.io") {
					t.Fatalf("round %d: a save wrote docker.io again:\n%s", round, readFile(t, path))
				}
			}
			// A restart: the current-context pointer and the default path.
			if err := SetCurrentContext("dev"); err != nil {
				t.Fatal(err)
			}
			if cfg, err := Load(); err != nil || len(cfg.Registry.Registries) != 0 {
				t.Errorf("Load() after the removal = %+v, %v, want no registries", cfg, err)
			}
		})
	}
}

func rewrite(t *testing.T, path, body string) {
	t.Helper()
	if err := os.WriteFile(filepath.Clean(path), []byte(body), 0o600); err != nil {
		t.Fatal(err)
	}
}
