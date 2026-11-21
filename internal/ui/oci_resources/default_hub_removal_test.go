package ociresources

import (
	"os"
	"strings"
	"testing"

	"github.com/anthnel/devdesk/internal/config"
	sharedcomponents "github.com/anthnel/devdesk/internal/ui/components"
	"github.com/anthnel/devdesk/internal/ui/keymap"
	"github.com/anthnel/devdesk/internal/ui/testutil"
)

// DEVDESK-665 (S-03), criterion 2 through the real removal path: the user
// removes the Docker Hub entry of a new context from the Registries tab (the
// delete key, then the confirmation), which saves the config. A reload, a save
// and a restart of the view do not bring it back.
func TestRemovingTheHubEntryFromTheRegistriesTabIsDurable(t *testing.T) {
	home := t.TempDir()
	t.Setenv("HOME", home)
	t.Setenv("USERPROFILE", home)

	if err := config.CreateContext("dev"); err != nil {
		t.Fatal(err)
	}
	if err := config.SetCurrentContext("dev"); err != nil {
		t.Fatal(err)
	}
	cfg, err := config.Load()
	if err != nil {
		t.Fatal(err)
	}
	if len(cfg.Registry.Registries) != 1 || cfg.Registry.Registries[0] != config.DefaultHubRegistry() {
		t.Fatalf("precondition: the new context holds %+v, want the Docker Hub entry", cfg.Registry.Registries)
	}

	open := func(cfg *config.Config) Model {
		m := feed(t, New(cfg), testutil.Resize(180, 30),
			ImagesListMsg{Images: imageFixtures()}, NetworksListMsg{Networks: networkFixtures()}, VolumesListMsg{Volumes: volumeFixtures()})
		for m.activeTab != tabRegistries {
			m = feed(t, m, testutil.Key("tab"))
		}
		return m
	}

	m := open(cfg)
	if got := cells(m.registryTable.Table().Rows(), 2); !equal(got, []string{"docker.io"}) {
		t.Fatalf("the registry tab holds %v, want the Docker Hub row", got)
	}
	m = feed(t, m, testutil.Key(keymap.Delete), sharedcomponents.ConfirmModalYesMsg{})
	if rows := m.registryTable.Table().Rows(); len(rows) != 0 {
		t.Fatalf("the row is still listed after the removal: %v", rows)
	}

	path, _ := config.GetContextPath("dev")
	for round := 0; round < 2; round++ {
		if data, _ := os.ReadFile(path); strings.Contains(string(data), "docker.io") {
			t.Fatalf("round %d: the file still names docker.io:\n%s", round, data)
		}
		reloaded, err := config.Load()
		if err != nil {
			t.Fatal(err)
		}
		if len(reloaded.Registry.Registries) != 0 {
			t.Fatalf("round %d: the removed entry came back on reload: %+v", round, reloaded.Registry.Registries)
		}
		// A restart of the view on the reloaded config, then a save from it.
		reopened := open(reloaded)
		if rows := reopened.registryTable.Table().Rows(); len(rows) != 0 {
			t.Fatalf("round %d: the reopened tab lists %v", round, rows)
		}
		if err := config.Save(reloaded); err != nil {
			t.Fatal(err)
		}
	}
}
