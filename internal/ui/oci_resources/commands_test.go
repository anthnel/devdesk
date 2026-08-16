package ociresources

import (
	"encoding/base64"
	"encoding/json"
	"os"
	"path/filepath"
	"strings"
	"testing"

	tea "github.com/charmbracelet/bubbletea"

	"github.com/anthnel/devdesk/internal/cache"
	"github.com/anthnel/devdesk/internal/config"
	"github.com/anthnel/devdesk/internal/docker"
)

// The commands are thin: they call into internal/docker or internal/cache and
// wrap the answer in a message. What is worth pinning is not the parsing —
// internal/docker has its own tests for that — but the wrapping: that the
// message names what the user asked for rather than what the tool echoed back,
// and that a failure is carried into the message instead of being dropped.

// ── Images ───────────────────────────────────────────────────────────────────

func TestFetchImagesCarriesTheList(t *testing.T) {
	installFakeDocker(t, fakeScript{
		"docker image ls": {Stdout: "aaa1111\tapi\tv1\t400MB\t2 days ago\n"},
	})

	msg, ok := run(t, fetchImages()).(ImagesListMsg)
	if !ok {
		t.Fatalf("fetchImages returned %T, want ImagesListMsg", msg)
	}
	if msg.Err != nil {
		t.Fatalf("Err = %v, want the list to have been read", msg.Err)
	}
	if len(msg.Images) != 1 || msg.Images[0].Name() != "api:v1" {
		t.Errorf("Images = %+v, want the single image docker listed", msg.Images)
	}
}

// With no docker at all the list has to arrive as a failure. An empty list and
// a nil error would be indistinguishable from a machine with no images, which
// is the defect §1.1 records for the networks and volumes tabs.
func TestFetchImagesReportsAnAbsentDocker(t *testing.T) {
	noDocker(t)

	msg := run(t, fetchImages()).(ImagesListMsg)

	if msg.Err == nil {
		t.Fatal("a missing docker was reported as an empty image list")
	}
	if msg.Images != nil {
		t.Errorf("Images = %+v, want nothing alongside the error", msg.Images)
	}
}

// The command is handed both the ID it removes and the name the user sees, and
// the two differ. It used to report only one field, called ID and holding the
// name — deliberately, since reporting the hash would put it in the footer. The
// message carries both now: the name is still what is shown, and the ID is what
// lifts the busy marker off the row (§3.22).
func TestRemoveImageReportsBothTheIDAndTheName(t *testing.T) {
	installFakeDocker(t, fakeScript{})

	msg := run(t, removeImageCmd("sha256:aaa1111", "api:v1")).(ImageActionMsg)

	if msg.Action != "remove" {
		t.Errorf("Action = %q, want remove", msg.Action)
	}
	if msg.Name != "api:v1" {
		t.Errorf("Name = %q, want the display name", msg.Name)
	}
	if msg.ID != "sha256:aaa1111" {
		t.Errorf("ID = %q, want the ID the marker is keyed on", msg.ID)
	}
	if msg.Err != nil {
		t.Errorf("Err = %v, want a clean removal", msg.Err)
	}
}

// Docker writes the reason a removal failed to stderr, and that text is the
// only thing that tells a user their image is still in use.
func TestRemoveImageKeepsDockersOwnDiagnostic(t *testing.T) {
	installFakeDocker(t, fakeScript{
		"docker rmi": {Stderr: "image is being used by running container 9f2c", Exit: 1},
	})

	msg := run(t, removeImageCmd("aaa1111", "api:v1")).(ImageActionMsg)

	if msg.Err == nil {
		t.Fatal("a refused removal was reported as a success")
	}
	if !strings.Contains(msg.Err.Error(), "being used by running container") {
		t.Errorf("Err = %v, want docker's own explanation", msg.Err)
	}
}

func TestPruneImagesCarriesTheReclaimReport(t *testing.T) {
	installFakeDocker(t, fakeScript{
		"docker image prune": {Stdout: "Total reclaimed space: 1.2GB\n"},
	})

	msg := run(t, pruneImagesCmd()).(PruneCompleteMsg)

	if msg.Err != nil {
		t.Fatalf("Err = %v", msg.Err)
	}
	if msg.Output != "Total reclaimed space: 1.2GB" {
		t.Errorf("Output = %q, want the trimmed report", msg.Output)
	}
}

func TestPullReportsTheImageItWasAskedFor(t *testing.T) {
	installFakeDocker(t, fakeScript{})

	msg := run(t, pullRegistryImageCmd("registry.example.com/api:v1")).(RegistryPullCompleteMsg)

	if msg.ImageName != "registry.example.com/api:v1" {
		t.Errorf("ImageName = %q, want the reference that was pulled", msg.ImageName)
	}
	if msg.Err != nil {
		t.Errorf("Err = %v", msg.Err)
	}
}

func TestPullCarriesTheFailure(t *testing.T) {
	installFakeDocker(t, fakeScript{
		"docker pull": {Stderr: "manifest unknown", Exit: 1},
	})

	msg := run(t, pullRegistryImageCmd("registry.example.com/api:nope")).(RegistryPullCompleteMsg)

	if msg.Err == nil {
		t.Fatal("a failed pull was reported as a success")
	}
	if msg.ImageName != "registry.example.com/api:nope" {
		t.Errorf("ImageName = %q, want it named even on failure", msg.ImageName)
	}
}

// ── Networks and volumes ─────────────────────────────────────────────────────

func TestFetchNetworksCarriesTheList(t *testing.T) {
	installFakeDocker(t, fakeScript{
		"docker network ls": {Stdout: "net11111\tbridge\tbridge\tlocal\n"},
	})

	msg := run(t, fetchNetworks()).(NetworksListMsg)

	if msg.Err != nil {
		t.Fatalf("Err = %v", msg.Err)
	}
	if len(msg.Networks) != 1 || msg.Networks[0].Name != "bridge" {
		t.Errorf("Networks = %+v, want the one docker listed", msg.Networks)
	}
}

func TestFetchNetworksReportsAnAbsentDocker(t *testing.T) {
	noDocker(t)

	if msg := run(t, fetchNetworks()).(NetworksListMsg); msg.Err == nil {
		t.Fatal("a missing docker was reported as an empty network list")
	}
}

func TestFetchVolumesCarriesTheList(t *testing.T) {
	installFakeDocker(t, fakeScript{
		"docker volume ls": {Stdout: "pgdata\tlocal\t/var/lib/docker/volumes/pgdata/_data\n"},
	})

	msg := run(t, fetchVolumes()).(VolumesListMsg)

	if msg.Err != nil {
		t.Fatalf("Err = %v", msg.Err)
	}
	if len(msg.Volumes) != 1 || msg.Volumes[0].Name != "pgdata" {
		t.Errorf("Volumes = %+v, want the one docker listed", msg.Volumes)
	}
}

func TestFetchVolumesReportsAnAbsentDocker(t *testing.T) {
	noDocker(t)

	if msg := run(t, fetchVolumes()).(VolumesListMsg); msg.Err == nil {
		t.Fatal("a missing docker was reported as an empty volume list")
	}
}

// Create and remove differ only in the verb, and the view distinguishes them by
// the Action field alone — a wrong one would report "created" after a deletion.
func TestNetworkAndVolumeActionsNameWhatTheyDid(t *testing.T) {
	installFakeDocker(t, fakeScript{})

	cases := []struct {
		name   string
		cmd    tea.Cmd
		action string
	}{
		{"network create", createNetworkCmd("devdesk", "bridge"), "create"},
		{"network remove", removeNetworkCmd("net11111"), "remove"},
		{"volume create", createVolumeCmd("pgdata", "local"), "create"},
		{"volume remove", removeVolumeCmd("pgdata"), "remove"},
	}
	for _, tc := range cases {
		t.Run(tc.name, func(t *testing.T) {
			var action string
			var err error
			switch msg := run(t, tc.cmd).(type) {
			case NetworkActionMsg:
				action, err = msg.Action, msg.Err
			case VolumeActionMsg:
				action, err = msg.Action, msg.Err
			default:
				t.Fatalf("returned %T, want a network or volume action", msg)
			}
			if action != tc.action {
				t.Errorf("Action = %q, want %q", action, tc.action)
			}
			if err != nil {
				t.Errorf("Err = %v", err)
			}
		})
	}
}

func TestACreateThatDockerRefusesIsCarriedIntoTheMessage(t *testing.T) {
	installFakeDocker(t, fakeScript{
		"docker network create": {Stderr: "network with name devdesk already exists", Exit: 1},
	})

	msg := run(t, createNetworkCmd("devdesk", "bridge")).(NetworkActionMsg)

	if msg.Err == nil {
		t.Fatal("a refused creation was reported as a success")
	}
	if !strings.Contains(msg.Err.Error(), "already exists") {
		t.Errorf("Err = %v, want docker's own explanation", msg.Err)
	}
}

func TestPruningNetworksAndVolumesCarriesTheirReports(t *testing.T) {
	installFakeDocker(t, fakeScript{
		"docker network prune": {Stdout: "Deleted Networks:\nold-net\n"},
		"docker volume prune":  {Stdout: "Total reclaimed space: 40MB\n"},
	})

	networks := run(t, pruneNetworksCmd()).(NetworkPruneCompleteMsg)
	if networks.Err != nil || !strings.Contains(networks.Output, "old-net") {
		t.Errorf("network prune = %+v, want the deleted list", networks)
	}

	volumes := run(t, pruneVolumesCmd()).(VolumePruneCompleteMsg)
	if volumes.Err != nil || volumes.Output != "Total reclaimed space: 40MB" {
		t.Errorf("volume prune = %+v, want the trimmed report", volumes)
	}
}

// The message carries the network's name as well as its ID because the results
// panel is titled with it, and `docker network inspect` output has no room for
// the name the row was showing.
func TestInspectNetworkKeepsTheNameAlongsideTheID(t *testing.T) {
	installFakeDocker(t, fakeScript{
		"docker network inspect": {Stdout: `[{"Containers":{
			"c1":{"Name":"api","IPv4Address":"172.18.0.2/16","MacAddress":"02:42:ac:12:00:02"}
		}}]`},
	})

	msg := run(t, inspectNetworkCmd("net22222", "devdesk")).(NetworkInspectLoadedMsg)

	if msg.NetworkID != "net22222" || msg.NetworkName != "devdesk" {
		t.Errorf("network = %q/%q, want both the ID and the name", msg.NetworkID, msg.NetworkName)
	}
	if len(msg.Containers) != 1 || msg.Containers[0].IPv4 != "172.18.0.2" {
		t.Errorf("Containers = %+v, want the connected container with its mask dropped", msg.Containers)
	}
}

func TestInspectNetworkStillNamesTheNetworkWhenItFails(t *testing.T) {
	noDocker(t)

	msg := run(t, inspectNetworkCmd("net22222", "devdesk")).(NetworkInspectLoadedMsg)

	if msg.Err == nil {
		t.Fatal("a missing docker was reported as a network with no containers")
	}
	if msg.NetworkName != "devdesk" {
		t.Errorf("NetworkName = %q, want it named even on failure", msg.NetworkName)
	}
}

// A connectivity test that fails is not a test that produced nothing: the
// output is the answer — "100% packet loss" is the diagnostic.
func TestADiagnosticKeepsItsOutputWhenTheCommandFails(t *testing.T) {
	installFakeDocker(t, fakeScript{
		"docker run": {Stdout: "PING api: 3 packets transmitted, 0 received, 100% packet loss\n", Exit: 1},
	})

	msg := run(t, runDiagnosticContainerCmd("net22222", "netshoot", []string{"ping", "-c", "3", "api"})).(DiagnosticTestCompleteMsg)

	if msg.Err == nil {
		t.Fatal("a non-zero exit was reported as a successful test")
	}
	if !strings.Contains(msg.Output, "100% packet loss") {
		t.Errorf("Output = %q, want the tool's own report kept", msg.Output)
	}
}

func TestADiagnosticThatSucceedsCarriesItsOutput(t *testing.T) {
	installFakeDocker(t, fakeScript{
		"docker run": {Stdout: "3 packets transmitted, 3 received\n"},
	})

	msg := run(t, runDiagnosticContainerCmd("net22222", "netshoot", []string{"ping", "api"})).(DiagnosticTestCompleteMsg)

	if msg.Err != nil {
		t.Fatalf("Err = %v", msg.Err)
	}
	if !strings.Contains(msg.Output, "3 received") {
		t.Errorf("Output = %q", msg.Output)
	}
}

// ── Container launch ─────────────────────────────────────────────────────────

func TestExposedPortsAreReturnedAgainstTheImageTheyCameFrom(t *testing.T) {
	installFakeDocker(t, fakeScript{
		"docker image inspect": {Stdout: `{"80/tcp":{},"443/tcp":{}}`},
	})

	msg := run(t, fetchImageExposedPortsCmd("api:v1")).(ImageExposedPortsMsg)

	if msg.ImageName != "api:v1" {
		t.Errorf("ImageName = %q, want the image the ports belong to", msg.ImageName)
	}
	if len(msg.Ports) != 2 {
		t.Fatalf("Ports = %v, want both EXPOSE entries", msg.Ports)
	}
}

// An image declaring no ports is the ordinary case, and it must not look like a
// failure — the launch form would show an error where there is nothing to show.
func TestAnImageWithNoExposedPortsIsNotAFailure(t *testing.T) {
	installFakeDocker(t, fakeScript{
		"docker image inspect": {Stdout: "null"},
	})

	msg := run(t, fetchImageExposedPortsCmd("api:v1")).(ImageExposedPortsMsg)

	if msg.Err != nil {
		t.Fatalf("Err = %v, want an image with no EXPOSE to be ordinary", msg.Err)
	}
	if len(msg.Ports) != 0 {
		t.Errorf("Ports = %v, want none", msg.Ports)
	}
}

func TestLaunchingAContainerReturnsItsID(t *testing.T) {
	installFakeDocker(t, fakeScript{
		"docker run": {Stdout: "9f2c1d4e5b6a\n"},
	})

	msg := run(t, launchContainerCmd(docker.ContainerLaunchOptions{
		Image: "api:v1", Name: "api", Detach: true,
	})).(ContainerLaunchCompleteMsg)

	if msg.Err != nil {
		t.Fatalf("Err = %v", msg.Err)
	}
	if msg.ContainerID != "9f2c1d4e5b6a" {
		t.Errorf("ContainerID = %q, want the trimmed ID docker printed", msg.ContainerID)
	}
}

func TestALaunchThatDockerRefusesCarriesTheReason(t *testing.T) {
	installFakeDocker(t, fakeScript{
		"docker run": {Stdout: "port is already allocated", Exit: 125},
	})

	msg := run(t, launchContainerCmd(docker.ContainerLaunchOptions{Image: "api:v1"})).(ContainerLaunchCompleteMsg)

	if msg.Err == nil {
		t.Fatal("a refused launch was reported as a started container")
	}
	if !strings.Contains(msg.Err.Error(), "already allocated") {
		t.Errorf("Err = %v, want docker's own explanation", msg.Err)
	}
	if msg.ContainerID != "" {
		t.Errorf("ContainerID = %q, want nothing for a container that never started", msg.ContainerID)
	}
}

// The sequence number is what lets a stale answer be discarded, so it has to
// survive the round trip unchanged — the command is debounced and the user
// keeps typing while it runs.
func TestEntrypointVerificationCarriesItsSequenceNumber(t *testing.T) {
	installFakeDocker(t, fakeScript{})

	msg := run(t, verifyEntrypointCmd(7, "api:v1", "/bin/sh")).(EntrypointVerifyFinishedMsg)

	if msg.Seq != 7 {
		t.Errorf("Seq = %d, want the sequence it was given", msg.Seq)
	}
	if !msg.OK {
		t.Error("an entrypoint docker found was reported as missing")
	}
}

func TestAnEntrypointThatIsNotInTheImageIsReportedAsMissing(t *testing.T) {
	installFakeDocker(t, fakeScript{
		"docker run": {Exit: 1},
	})

	msg := run(t, verifyEntrypointCmd(1, "api:v1", "/usr/bin/zsh")).(EntrypointVerifyFinishedMsg)

	if msg.OK {
		t.Error("a binary the image does not have was reported as present")
	}
}

// ── Registry login ───────────────────────────────────────────────────────────

func TestRegistryLoginNamesTheRegistryItAuthenticatedAgainst(t *testing.T) {
	installFakeDocker(t, fakeScript{})

	msg := run(t, registryLoginCmd("registry.example.com", "anthnel", "s3cret")).(RegistryLoginCompleteMsg)

	if msg.RegistryURL != "registry.example.com" {
		t.Errorf("RegistryURL = %q", msg.RegistryURL)
	}
	if msg.Err != nil {
		t.Errorf("Err = %v", msg.Err)
	}
}

func TestRejectedCredentialsAreCarriedIntoTheLoginMessage(t *testing.T) {
	installFakeDocker(t, fakeScript{
		"docker login": {Stderr: "unauthorized: incorrect username or password", Exit: 1},
	})

	msg := run(t, registryLoginCmd("registry.example.com", "anthnel", "wrong")).(RegistryLoginCompleteMsg)

	if msg.Err == nil {
		t.Fatal("a rejected login was reported as a success")
	}
	if !strings.Contains(msg.Err.Error(), "incorrect username or password") {
		t.Errorf("Err = %v, want the registry's own answer", msg.Err)
	}
}

func TestRegistryLogoutNamesTheRegistry(t *testing.T) {
	installFakeDocker(t, fakeScript{})

	msg := run(t, registryLogoutCmd("registry.example.com")).(RegistryLogoutCompleteMsg)

	if msg.RegistryURL != "registry.example.com" || msg.Err != nil {
		t.Errorf("logout = %+v, want a clean logout naming the registry", msg)
	}
}

// The status is read from ~/.docker/config.json rather than from docker, which
// is why it answers for a registry that was never logged into without failing.
func TestLoginStatusIsAnsweredForEveryRegistryAsked(t *testing.T) {
	writeDockerConfig(t, map[string]any{
		"auths": map[string]any{
			"registry.example.com": map[string]string{"auth": encodeAuth("anthnel", "s3cret")},
		},
	})

	msg := run(t, checkRegistryLoginStatusCmd([]string{"registry.example.com", "other.example.com"})).(RegistryLoginStatusMsg)

	if len(msg.Status) != 2 {
		t.Fatalf("Status = %v, want an answer for both registries", msg.Status)
	}
	if !msg.Status["registry.example.com"] {
		t.Error("a registry with stored credentials was reported as logged out")
	}
	if msg.Status["other.example.com"] {
		t.Error("a registry with no stored credentials was reported as logged in")
	}
}

// ── Caches ───────────────────────────────────────────────────────────────────

// The cache commands are executed against the temporary HOME the package
// redirects to, so what they write is what a later launch would read back.

func TestTheScanCacheIsLoadedFromDisk(t *testing.T) {
	c, err := cache.NewImageScanCache(config.CurrentContextName())
	if err != nil {
		t.Fatalf("opening the scan cache: %v", err)
	}
	if err := c.Set("api:v1", cache.ImageScanEntry{Critical: 2, ScannedAt: at(1)}); err != nil {
		t.Fatalf("seeding the scan cache: %v", err)
	}
	t.Cleanup(func() { _ = c.Delete("api:v1") })

	msg := run(t, loadScanCache()).(ScanCacheLoadedMsg)

	entry, ok := msg.Entries["api:v1"]
	if !ok {
		t.Fatalf("Entries = %v, want the seeded image", msg.Entries)
	}
	if entry.Critical != 2 {
		t.Errorf("Critical = %d, want the counts that were saved", entry.Critical)
	}
}

// Rule 126: ctrl+a purges before rescanning, so the delete has to reach the
// disk — a cache cleared only in memory comes back on the next launch.
func TestDeletingScanCacheEntriesReachesTheDisk(t *testing.T) {
	c, err := cache.NewImageScanCache(config.CurrentContextName())
	if err != nil {
		t.Fatalf("opening the scan cache: %v", err)
	}
	for _, key := range []string{"api:v1", "cache:v2"} {
		if err := c.Set(key, cache.ImageScanEntry{ScannedAt: at(1)}); err != nil {
			t.Fatalf("seeding %s: %v", key, err)
		}
	}

	run(t, deleteScanCacheCmd([]string{"api:v1", "cache:v2"}))

	reopened, err := cache.NewImageScanCache(config.CurrentContextName())
	if err != nil {
		t.Fatalf("reopening the scan cache: %v", err)
	}
	if len(reopened.GetAll()) != 0 {
		t.Errorf("the cache still holds %v after a purge", reopened.GetAll())
	}
}

func TestLaunchOptionsSurviveASaveAndReload(t *testing.T) {
	entry := cache.LaunchOptionsEntry{
		Entrypoint:   "/bin/sh",
		Network:      "devdesk",
		Env:          "LOG_LEVEL=debug",
		Detach:       true,
		PortMappings: map[string]cache.PortMappingEntry{"80/tcp": {HostPort: "8080", Enabled: true}},
	}

	run(t, saveLaunchOptionsCmd("api:v1", entry))
	msg := run(t, loadLaunchOptionsCmd("api:v1")).(LaunchOptionsCacheLoadedMsg)

	if msg.ImageName != "api:v1" {
		t.Errorf("ImageName = %q, want the image the options belong to", msg.ImageName)
	}
	if msg.Entry == nil {
		t.Fatal("Entry = nil, want the options that were just saved")
	}
	if msg.Entry.Entrypoint != "/bin/sh" || msg.Entry.Network != "devdesk" || !msg.Entry.Detach {
		t.Errorf("Entry = %+v, want what was saved", msg.Entry)
	}
	// The port mapping is the one field that is not a scalar, so it is the one
	// a round trip through JSON can quietly drop.
	if mapping := msg.Entry.PortMappings["80/tcp"]; mapping.HostPort != "8080" || !mapping.Enabled {
		t.Errorf("PortMappings = %+v, want the mapping that was saved", msg.Entry.PortMappings)
	}
}

// An image nobody has launched has no options, and that is not a failure: the
// form opens with its defaults.
func TestAnImageWithNoSavedLaunchOptionsLoadsNothing(t *testing.T) {
	msg := run(t, loadLaunchOptionsCmd("never-launched:v1")).(LaunchOptionsCacheLoadedMsg)

	if msg.Entry != nil {
		t.Errorf("Entry = %+v, want nothing for an image never launched", msg.Entry)
	}
}

// ── helpers ──────────────────────────────────────────────────────────────────

func writeDockerConfig(t *testing.T, cfg map[string]any) {
	t.Helper()
	home, err := os.UserHomeDir()
	if err != nil {
		t.Fatalf("resolving HOME: %v", err)
	}
	dir := filepath.Join(home, ".docker")
	if err := os.MkdirAll(dir, 0o755); err != nil {
		t.Fatalf("creating %s: %v", dir, err)
	}
	body, err := json.Marshal(cfg)
	if err != nil {
		t.Fatalf("encoding the docker config: %v", err)
	}
	path := filepath.Join(dir, "config.json")
	if err := os.WriteFile(path, body, 0o600); err != nil {
		t.Fatalf("writing %s: %v", path, err)
	}
	t.Cleanup(func() { _ = os.Remove(path) })
}

func encodeAuth(username, password string) string {
	return base64.StdEncoding.EncodeToString([]byte(username + ":" + password))
}
