package setup

import (
	"os"
	"path/filepath"
	"strings"
	"testing"

	"github.com/anthnel/devdesk/internal/config"
	"github.com/anthnel/devdesk/internal/ui/components"
	"github.com/anthnel/devdesk/internal/ui/testutil"
)

// DEVDESK-664 (S-02): the setup wizard writes the Docker Hub entry a new
// context starts with (config.DefaultHubRegistry, the constructor :context new
// uses), also when the user confirmed overwriting an existing context (D-A: a
// confirmed overwrite yields a new context, not a retroactive addition).

// runWizardToSave drives the wizard the way a user does, from the context name
// answer to the write: the name check says exists, an existing context asks
// for the overwrite confirmation (answered yes), every other question keeps
// its default, and finishWizard starts the real save (buildConfig, then
// WriteNewContext). It returns the saved message and the model that handled it.
func runWizardToSave(t *testing.T, name string, exists bool) (ContextSavedMsg, Model) {
	t.Helper()
	m := New()
	m = feed(t, m, testutil.Key("enter")) // font check -> context name
	m.contextNameInput.SetValue(name)
	m = feed(t, m, ContextExistsMsg{Exists: exists})
	if exists {
		if m.stage != stageConfirmOverwrite {
			t.Fatalf("stage = %v, want the overwrite confirmation", m.stage)
		}
		m = feed(t, m, components.ConfirmModalYesMsg{})
	}
	if m.step != stepSecretBackend {
		t.Fatalf("step = %d, want stepSecretBackend once the name is settled", m.step)
	}

	_, cmd := m.finishWizard()
	msg, ok := testutil.MsgOf[ContextSavedMsg](cmd)
	if !ok {
		t.Fatal("finishWizard did not start the save: no ContextSavedMsg")
	}
	return msg, feed(t, m, msg)
}

// hubEntryCount counts the docker.io entries of a loaded context.
func hubEntryCount(cfg *config.Config) int {
	n := 0
	for _, r := range cfg.Registry.Registries {
		if r.URL == "docker.io" {
			n++
		}
	}
	return n
}

// Criterion 1: a name with no file. The file holds the entry of
// config.DefaultHubRegistry, once, the same one CreateContext writes.
func TestSetupWritesTheDefaultHubEntryForANewContextName(t *testing.T) {
	for _, name := range []string{"fresh", "staging-2"} {
		t.Run(name, func(t *testing.T) {
			isolateHome(t)

			msg, m := runWizardToSave(t, name, false)
			if msg.Err != nil {
				t.Fatalf("ContextSavedMsg.Err = %v, want nil", msg.Err)
			}
			if m.stage != stageConfirmSetCurrent {
				t.Errorf("stage = %v, want stageConfirmSetCurrent after a successful save", m.stage)
			}

			got, err := config.LoadContext(name)
			if err != nil {
				t.Fatal(err)
			}
			want := []config.RegistryItem{config.DefaultHubRegistry()}
			if len(got.Registry.Registries) != 1 || got.Registry.Registries[0] != want[0] {
				t.Fatalf("registries of the written context = %+v, want exactly %+v", got.Registry.Registries, want)
			}
			if n := strings.Count(readContextFile(t, name), "url: docker.io"); n != 1 {
				t.Errorf("the file holds `url: docker.io` %d times, want once", n)
			}

			// The same entry :context new writes: one constructor, two paths.
			if err := config.CreateContext("via-context-new"); err != nil {
				t.Fatal(err)
			}
			created, err := config.LoadContext("via-context-new")
			if err != nil {
				t.Fatal(err)
			}
			if len(created.Registry.Registries) != 1 || created.Registry.Registries[0] != got.Registry.Registries[0] {
				t.Errorf("setup wrote %+v, :context new wrote %+v: the entries diverge",
					got.Registry.Registries, created.Registry.Registries)
			}
		})
	}
}

// Criterion 2 (D-A): an existing context, with registries of its own, a
// monitor, a forge and even an edited Docker Hub entry, is replaced after the
// confirmation by exactly what a new context gets: the Docker Hub default,
// once, and nothing of the old content.
func TestSetupOverwriteYieldsANewContextDockerHubIncluded(t *testing.T) {
	isolateHome(t)

	old := config.Default()
	old.Forge.URL = "https://old-forge.example.com"
	old.Forge.DefaultParentGroup = "old-group"
	old.Status.Components = []config.ComponentConfig{{Name: "old-monitor", Type: "https", Target: "https://old-monitor.example.com"}}
	old.Registry.Registries = []config.RegistryItem{
		{URL: "registry.old.example.com", Slug: "registry-old-example-com", Kind: config.KindRegistry, AuthMode: config.AuthCredentials, Username: "old-user"},
		// An edited Docker Hub entry: it must not survive, nor be doubled.
		{URL: "docker.io", Slug: "docker-io", Kind: config.KindRegistry, AuthMode: config.AuthCredentials, Username: "hub-user", Alias: "hub"},
	}
	if err := config.SaveContext(old, "old"); err != nil {
		t.Fatal(err)
	}

	if msg, _ := runWizardToSave(t, "old", true); msg.Err != nil {
		t.Fatalf("ContextSavedMsg.Err = %v, want nil", msg.Err)
	}
	if msg, _ := runWizardToSave(t, "fresh", false); msg.Err != nil {
		t.Fatalf("ContextSavedMsg.Err = %v, want nil", msg.Err)
	}

	got, err := config.LoadContext("old")
	if err != nil {
		t.Fatal(err)
	}
	if len(got.Registry.Registries) != 1 || got.Registry.Registries[0] != config.DefaultHubRegistry() {
		t.Fatalf("registries after the overwrite = %+v, want exactly [%+v]", got.Registry.Registries, config.DefaultHubRegistry())
	}
	if n := hubEntryCount(got); n != 1 {
		t.Errorf("docker.io entries after the overwrite = %d, want 1", n)
	}

	overwritten, fresh := readContextFile(t, "old"), readContextFile(t, "fresh")
	for _, leftover := range []string{"registry.old.example.com", "old-user", "hub-user", "alias: hub", "old-monitor", "old-forge.example.com", "old-group"} {
		if strings.Contains(overwritten, leftover) {
			t.Errorf("the overwritten file still contains %q from the old context", leftover)
		}
	}
	if overwritten != fresh {
		t.Errorf("an overwritten context differs from a new one answered the same way:\n--- overwritten\n%s\n--- new\n%s", overwritten, fresh)
	}
}

// Criterion 3: buildConfig carries the entry, once, whatever the answers, and
// every call builds its own slice. config.Default() stays free of it: it also
// serves the load fallback, where a deleted entry would come back.
func TestBuildConfigCarriesTheDefaultHubEntryOnce(t *testing.T) {
	answered := New()
	answered.secretBackendIdx = 1
	answered.containerEngineIdx = 2
	answered.forgeTypeIdx = 1
	answered.forgeURLInput.SetValue("https://git.example.com")
	answered.forgeNamespaceInput.SetValue("my-org")
	answered.visibilityValue = "public"
	answered.cloneMethodIdx = 1

	named := New()
	named.contextNameInput.SetValue("staging")

	tests := []struct {
		name string
		m    Model
	}{
		{"untouched wizard", New()},
		{"every question answered", answered},
		{"context name set", named},
	}
	for _, tt := range tests {
		t.Run(tt.name, func(t *testing.T) {
			got := buildConfig(tt.m).Registry.Registries
			if len(got) != 1 || got[0] != config.DefaultHubRegistry() {
				t.Fatalf("buildConfig registries = %+v, want exactly [%+v]", got, config.DefaultHubRegistry())
			}

			// Own slice per call: mutating one result changes no other.
			got[0].Alias = "mutated"
			if again := buildConfig(tt.m).Registry.Registries; again[0] != config.DefaultHubRegistry() {
				t.Errorf("a second buildConfig returned %+v after the first was mutated: the slice is shared", again[0])
			}
		})
	}

	if got := config.Default().Registry.Registries; len(got) != 0 {
		t.Errorf("config.Default() registries = %+v, want none", got)
	}
}

// Criterion 4: a write failure is reported as before, in ContextSavedMsg.Err,
// shown in the footer, and the wizard does not move on to "set as active".
// ~/.devdesk is a regular file here, so the directory cannot be created.
func TestSetupWriteFailureIsReportedInContextSavedMsg(t *testing.T) {
	isolateHome(t)
	home, err := os.UserHomeDir()
	if err != nil {
		t.Fatal(err)
	}
	if err := os.WriteFile(filepath.Join(home, ".devdesk"), []byte("not a directory"), 0o600); err != nil {
		t.Fatal(err)
	}

	msg, m := runWizardToSave(t, "fresh", false)
	if msg.Err == nil {
		t.Fatal("ContextSavedMsg.Err = nil, want the write error")
	}
	if m.stage == stageConfirmSetCurrent || m.confirmModal != nil {
		t.Errorf("stage = %v: a failed save must not offer to activate the context", m.stage)
	}
	if m.footer.Level() != components.LevelError || !strings.Contains(m.footer.Text(), "Failed to save context") {
		t.Errorf("footer = %q (level %v), want an error saying \"Failed to save context\"", m.footer.Text(), m.footer.Level())
	}
}
