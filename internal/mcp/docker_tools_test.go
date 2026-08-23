package mcp

import (
	"errors"
	"strings"
	"testing"

	"github.com/anthnel/devdesk/internal/cache"
	"github.com/anthnel/devdesk/internal/docker"
)

func TestContainersListReportsWhatTheDaemonHolds(t *testing.T) {
	fakeCacheHome(t)
	stubContainers(t, func(all bool) ([]docker.Container, error) {
		return []docker.Container{
			{ID: "b2", Name: "web", Image: "nginx:1.25", State: "running", Status: "Up 2 hours"},
			{ID: "a1", Name: "api", Image: "app:1.0", State: "exited", Status: "Exited (0) 5 minutes ago"},
		}, nil
	})

	var out containersListOut
	callTool(t, connect(t, testEnv(nil)), "containers_list", map[string]any{"all": true}, &out)

	if len(out.Containers) != 2 {
		t.Fatalf("listed %+v, want both", out.Containers)
	}
	if out.Containers[0].Name != "api" || out.Containers[1].Name != "web" {
		t.Errorf("listed %s then %s, want them ordered by name", out.Containers[0].Name, out.Containers[1].Name)
	}
	if out.Containers[0].State != "exited" {
		t.Errorf("state = %q", out.Containers[0].State)
	}
}

// `all` decides whether stopped containers are included, and it has to reach
// docker rather than being dropped on the way.
func TestTheAllFlagReachesDocker(t *testing.T) {
	fakeCacheHome(t)
	var asked []bool
	stubContainers(t, func(all bool) ([]docker.Container, error) {
		asked = append(asked, all)
		return nil, nil
	})
	cs := connect(t, testEnv(nil))

	var out containersListOut
	callTool(t, cs, "containers_list", nil, &out)
	callTool(t, cs, "containers_list", map[string]any{"all": true}, &out)

	if len(asked) != 2 || asked[0] || !asked[1] {
		t.Errorf("docker was asked with all=%v, want false then true", asked)
	}
}

// A publication's scope is the one bit worth knowing at a glance — reachable
// from the network, or from this machine only. The address is kept only for the
// scope where it is not a constant, since repeating 0.0.0.0 beside "all" says it
// twice.
func TestAPublicationReportsItsScopeAndNotItsConstantAddress(t *testing.T) {
	fakeCacheHome(t)
	stubContainers(t, func(bool) ([]docker.Container, error) {
		return []docker.Container{{
			ID: "a1", Name: "web", State: "running",
			Ports: []docker.PortBinding{
				{Scope: docker.ScopeAll, HostIP: "0.0.0.0", HostPort: "8080", ContainerPort: "80", Protocol: "tcp"},
				{Scope: docker.ScopeLoopback, HostIP: "127.0.0.1", HostPort: "5432", ContainerPort: "5432", Protocol: "tcp"},
				{Scope: docker.ScopeAddress, HostIP: "192.168.1.10", HostPort: "9000", ContainerPort: "9000", Protocol: "tcp"},
				{Scope: docker.ScopeExposed, ContainerPort: "3000", Protocol: "tcp"},
			},
		}}, nil
	})

	var out containersListOut
	callTool(t, connect(t, testEnv(nil)), "containers_list", nil, &out)

	if len(out.Containers) != 1 || len(out.Containers[0].Ports) != 4 {
		t.Fatalf("got %+v", out.Containers)
	}
	ports := out.Containers[0].Ports
	want := []struct{ scope, address string }{
		{"all", ""},
		{"loopback", ""},
		{"address", "192.168.1.10"},
		{"exposed", ""},
	}
	for i, w := range want {
		if ports[i].Scope != w.scope {
			t.Errorf("port %d scope = %q, want %q", i, ports[i].Scope, w.scope)
		}
		if ports[i].HostAddress != w.address {
			t.Errorf("port %d host_address = %q, want %q", i, ports[i].HostAddress, w.address)
		}
	}
}

// A daemon that cannot be reached is an error, not an empty list. There is no
// cache to fall back on here, so reading the silence as "nothing is running"
// would be the lie §3.37 exists to avoid.
func TestADaemonThatCannotBeReachedIsAnError(t *testing.T) {
	fakeCacheHome(t)
	stubContainers(t, func(bool) ([]docker.Container, error) {
		return nil, errors.New("cannot connect to the Docker daemon")
	})

	res := callToolExpectingError(t, connect(t, testEnv(nil)), "containers_list", nil)

	if !strings.Contains(res, "daemon") {
		t.Errorf("the error does not carry docker's reason: %s", res)
	}
}

// ── images_list ─────────────────────────────────────────────────────────────

// The flag answers what neither scan_inventory nor scan_result can: which
// images have never been looked at. It carries no counts, because those live in
// one place.
func TestImagesListSaysWhichImagesHaveBeenScanned(t *testing.T) {
	fakeCacheHome(t)
	storeImageEntry(t, "scanned:1", cache.ImageScanEntry{Critical: 3})
	stubImages(t, "scanned:1", "never:1")

	var out imagesListOut
	callTool(t, connect(t, testEnv(nil)), "images_list", nil, &out)

	if len(out.Images) != 2 {
		t.Fatalf("listed %+v, want both", out.Images)
	}
	byref := map[string]imageOut{}
	for _, img := range out.Images {
		byref[img.Reference] = img
	}
	if !byref["scanned:1"].Scanned {
		t.Error("scanned:1 reports scanned = false, though the cache holds an entry")
	}
	if byref["never:1"].Scanned {
		t.Error("never:1 reports scanned = true")
	}
}

// The counts live in scan_inventory and scan_result. Two tools reporting the
// same numbers is the shape D12, D24 and D25 each turned out to be.
func TestImagesListCarriesNoSeverityCounts(t *testing.T) {
	fakeCacheHome(t)
	storeImageEntry(t, "app:1", cache.ImageScanEntry{Critical: 7})
	stubImages(t, "app:1")

	raw := callToolRaw(t, connect(t, testEnv(nil)), "images_list", nil)

	for _, field := range []string{"critical", "high", "medium", "low"} {
		if strings.Contains(raw, "\""+field+"\"") {
			t.Errorf("images_list reports %q — the counts belong to scan_inventory alone:\n%s", field, raw)
		}
	}
}

func TestAnImageReportsTheReferenceScanResultTakes(t *testing.T) {
	fakeCacheHome(t)
	stubImages(t, "registry.example.com/team/app:1.0")

	var out imagesListOut
	callTool(t, connect(t, testEnv(nil)), "images_list", nil, &out)

	if len(out.Images) != 1 {
		t.Fatalf("listed %+v", out.Images)
	}
	if out.Images[0].Reference != "registry.example.com/team/app:1.0" {
		t.Errorf("reference = %q, want the repository and tag joined", out.Images[0].Reference)
	}
}

// ── fixtures ────────────────────────────────────────────────────────────────

func stubContainers(t *testing.T, fn func(all bool) ([]docker.Container, error)) {
	t.Helper()
	previous := listDockerContainers
	listDockerContainers = fn
	t.Cleanup(func() { listDockerContainers = previous })
}
