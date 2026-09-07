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
	sharedcomponents "github.com/anthnel/devdesk/internal/ui/components"
	"github.com/anthnel/devdesk/internal/ui/keymap"
	"github.com/anthnel/devdesk/internal/ui/testutil"
)

// Every command this view returns shells out to Docker, talks to a registry over
// HTTP or writes the scan cache. The tests that drive Update() and View() feed
// image, network, volume and registry lists, scan results and login statuses in
// as messages and assert on model state.
//
// commands_test.go executes the commands themselves instead, against a fake
// docker on a PATH of the test's own making (see installFakeTools) and against
// httptest servers standing in for registries.
//
// HOME is redirected for the whole package: the launch form and the registry
// list persist through config.Save, and the scan cache writes to ~/.devdesk.

func TestMain(m *testing.M) {
	// The fake docker is a copy of this binary, so helper mode has to be
	// answered before anything else — including the temporary HOME, which a
	// subprocess must neither create nor delete.
	if os.Getenv(helperMode) == "1" {
		os.Exit(runAsHelper())
	}

	log.SetOutput(io.Discard)

	home, err := os.MkdirTemp("", "devdesk-oci-test")
	if err != nil {
		log.SetOutput(os.Stderr)
		panic(err)
	}
	_ = os.Setenv("HOME", home)
	_ = os.Setenv("USERPROFILE", home)

	code := m.Run()

	removeFakeTools()
	_ = os.RemoveAll(home)
	log.SetOutput(os.Stderr)
	os.Exit(code)
}

func testConfig() *config.Config {
	cfg := config.Default()
	cfg.Registry.Registries = []config.RegistryItem{
		{
			URL: "registry.example.com", Username: "anthnel", Alias: "prod", Slug: "prod",
			AuthMode: config.AuthCredentials,
			// Declared a group, so it is the one entry the browser waits on.
			Kind: config.KindGroup, Provider: config.ProviderNexus,
		},
		{URL: "docker.io", Alias: "hub", Slug: "hub", AuthMode: config.AuthAnonymous},
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

// secretsFound and secretsClean write the two known verdicts; the third
// writes as nil, and that is what a scan that never looked carries.
func secretsFound() *bool { v := true; return &v }
func secretsClean() *bool { v := false; return &v }

// scanCacheFixture covers the three states a row can be in: scanned clean,
// scanned with findings, and never scanned (absent from the map).
//
// The "secrets" verdict has three values that do not overlap with those:
// api:v1 carries one, cache:v2 was looked at and nothing was found, and
// orphan was scanned by a version with no secrets stage — so scanned, with
// no verdict.
func scanCacheFixture() map[string]cache.ImageScanEntry {
	return map[string]cache.ImageScanEntry{
		"api:v1":   {Critical: 2, High: 3, Medium: 4, Low: 5, Sensitive: secretsFound(), ScannedAt: at(1)},
		"cache:v2": {Sensitive: secretsClean(), ScannedAt: at(2)},
		"orphan":   {ScannedAt: at(2)},
	}
}

// groupCacheFixture is one discovery for the configured group, which is what
// the browser and the Members column read instead of the network.
func groupCacheFixture() map[string]cache.RegistryGroupEntry {
	return map[string]cache.RegistryGroupEntry{
		"prod": {
			// A member is an address, not a URL (§3.18): the group's host, and
			// the member's name in front of the repository. Both share the
			// host, which is what makes the entry key the only thing telling
			// them apart.
			Members: []cache.RegistryGroupMember{
				{Alias: "docker-hosted", URL: "registry.example.com", RepoPrefix: "docker-hosted"},
				{Alias: "dhi", URL: "registry.example.com", RepoPrefix: "dhi-proxy"},
			},
			DiscoveredAt: at(3),
		},
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

// scanAll runs A and answers its modal, which is what one of two keys used to
// do. purge is the checkbox: ctrl+a's half of the old pair (§3.26).
func scanAll(t *testing.T, m Model, purge bool) (Model, tea.Cmd) {
	t.Helper()
	m, _ = step(t, m, testutil.Key(keymap.ScanAll))
	if m.scanAllModal == nil {
		t.Fatal("A did not open the scan-all confirmation")
	}
	return step(t, m, sharedcomponents.OptionConfirmModalYesMsg{Option: purge})
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
