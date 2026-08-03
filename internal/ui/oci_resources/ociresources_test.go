package ociresources

import (
	"io"
	"log"
	"os"
	"testing"
	"time"

	"github.com/charmbracelet/bubbles/table"
	tea "github.com/charmbracelet/bubbletea"

	"github.com/anthnel/devdesk/internal/cache"
	"github.com/anthnel/devdesk/internal/config"
	"github.com/anthnel/devdesk/internal/docker"
)

// Every command this view returns shells out to Docker, talks to a registry over
// HTTP or writes the scan cache. No test executes one: image, network, volume
// and registry lists, scan results and login statuses are fed in as messages,
// and the assertions are on model state.
//
// These tests drive Update() and View() only — update.go is 1698 lines and
// registry_browser.go 912, both due to be split, so assertions on their
// internals would pin the current layout.
//
// HOME is redirected for the whole package: the launch form and the registry
// list persist through config.Save, and the scan cache writes to ~/.devdesk.

func TestMain(m *testing.M) {
	log.SetOutput(io.Discard)

	home, err := os.MkdirTemp("", "devdesk-oci-test")
	if err != nil {
		log.SetOutput(os.Stderr)
		panic(err)
	}
	_ = os.Setenv("HOME", home)
	_ = os.Setenv("USERPROFILE", home)

	code := m.Run()

	_ = os.RemoveAll(home)
	log.SetOutput(os.Stderr)
	os.Exit(code)
}

func testConfig() *config.Config {
	cfg := config.Default()
	cfg.Registry.Registries = []config.RegistryItem{
		{URL: "registry.example.com", Username: "anthnel", Alias: "prod", AuthEnabled: true},
		{URL: "docker.io", Alias: "hub"},
	}
	return cfg
}

func at(day int) time.Time {
	return time.Date(2026, 8, day, 12, 0, 0, 0, time.UTC)
}

// imageFixtures give every sortable column a total order — sort.Slice is not
// stable, so ties would make the expected sequences ambiguous. One image is
// untagged, which is the case Name() special-cases.
func imageFixtures() []docker.Image {
	return []docker.Image{
		{ID: "aaa1111", Repository: "api", Tag: "v1", Size: 400, UniqueSize: 40, Containers: 1},
		{ID: "bbb2222", Repository: "cache", Tag: "v2", Size: 300, UniqueSize: 30},
		{ID: "ccc3333", Repository: "web", Tag: "v3", Size: 200, UniqueSize: 20, Containers: 2},
		{ID: "ddd4444", Repository: "orphan", Tag: "<none>", Size: 100, UniqueSize: 10},
	}
}

func networkFixtures() []docker.Network {
	return []docker.Network{
		{ID: "net11111", Name: "bridge", Driver: "bridge", Scope: "local"},
		{ID: "net22222", Name: "devdesk", Driver: "bridge", Scope: "local"},
		{ID: "net33333", Name: "overlay-prod", Driver: "overlay", Scope: "swarm"},
	}
}

func volumeFixtures() []docker.Volume {
	return []docker.Volume{
		{Name: "pgdata", Driver: "local", Mountpoint: "/var/lib/docker/volumes/pgdata/_data"},
		{Name: "redis", Driver: "local", Mountpoint: "/var/lib/docker/volumes/redis/_data"},
	}
}

// scanCacheFixture covers the three states a row can be in: scanned clean,
// scanned with findings, and never scanned (absent from the map).
func scanCacheFixture() map[string]cache.ImageScanEntry {
	return map[string]cache.ImageScanEntry{
		"api:v1":   {Critical: 2, High: 3, Medium: 4, Low: 5, ScannedAt: at(1)},
		"cache:v2": {ScannedAt: at(2)},
	}
}

// newTestModel returns a laid-out model with no data yet.
func newTestModel(t *testing.T) Model {
	t.Helper()
	return feed(t, New(testConfig()), tea.WindowSizeMsg{Width: 180, Height: 30})
}

// loadedModel returns a model holding every tab's fixtures and the scan cache.
func loadedModel(t *testing.T) Model {
	t.Helper()
	return feed(t, newTestModel(t),
		ImagesListMsg{Images: imageFixtures()},
		ScanCacheLoadedMsg{Entries: scanCacheFixture()},
		NetworksListMsg{Networks: networkFixtures()},
		VolumesListMsg{Volumes: volumeFixtures()},
	)
}

func feed(t *testing.T, m Model, msgs ...tea.Msg) Model {
	t.Helper()
	for _, msg := range msgs {
		m, _ = step(t, m, msg)
	}
	return m
}

func step(t *testing.T, m Model, msg tea.Msg) (Model, tea.Cmd) {
	t.Helper()
	next, cmd := m.Update(msg)
	updated, ok := next.(Model)
	if !ok {
		t.Fatalf("Update() returned %T, want ociresources.Model", next)
	}
	return updated, cmd
}

// cells returns column col of every table row.
func cells(rows []table.Row, col int) []string {
	out := make([]string, 0, len(rows))
	for _, row := range rows {
		out = append(out, row[col])
	}
	return out
}

func equal(a, b []string) bool {
	if len(a) != len(b) {
		return false
	}
	for i := range a {
		if a[i] != b[i] {
			return false
		}
	}
	return true
}
