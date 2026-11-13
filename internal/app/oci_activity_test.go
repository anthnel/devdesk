package app

import (
	"errors"
	"strings"
	"testing"
	"time"

	tea "charm.land/bubbletea/v2"
	"github.com/charmbracelet/x/ansi"

	"github.com/anthnel/devdesk/internal/command"
	"github.com/anthnel/devdesk/internal/config"
	"github.com/anthnel/devdesk/internal/docker"
	"github.com/anthnel/devdesk/internal/jobs"
	sharedcomponents "github.com/anthnel/devdesk/internal/ui/components"
	"github.com/anthnel/devdesk/internal/ui/indicator"
	"github.com/anthnel/devdesk/internal/ui/keymap"
	ociresources "github.com/anthnel/devdesk/internal/ui/oci_resources"
	"github.com/anthnel/devdesk/internal/ui/testutil"
)

// DEVDESK-146 step 3: the OCI Images table takes scan and pull failures from
// jobs.Registry, so they follow the image across views, reloads and rebuilds.

const ociOther = "redis:7"

// ociRouter is the :sec router (which holds nginx:1.27 in its inventory) with
// the OCI view built and listing nginx:1.27 and redis:7.
func ociRouter(t *testing.T) *App {
	t.Helper()
	t.Setenv("PATH", t.TempDir()) // no docker: a reload command must stay inert
	a, _ := secRouter(t, 180)
	openOCI(t, a)
	return a
}

func openOCI(t *testing.T, a *App) {
	t.Helper()
	_ = a.switchView(command.ViewOCIResources)
	a.Update(ociresources.ImagesListMsg{Images: []docker.Image{
		{ID: "aaa1111", Repository: "nginx", Tag: "1.27"},
		{ID: "bbb2222", Repository: "redis", Tag: "7"},
	}})
	a.resize(a.width, a.height)
}

func ociScreen(a *App) string { return ansi.Strip(a.views[command.ViewOCIResources].Render()) }

func ociWantFailed(t *testing.T, a *App, name string) {
	t.Helper()
	if row := rowOf(t, ociScreen(a), name); !strings.Contains(row, failedGlyph()) {
		t.Errorf("OCI row %q has no %s: %q", name, failedGlyph(), row)
	}
}

func ociWantNotFailed(t *testing.T, a *App, name string) {
	t.Helper()
	if row := rowOf(t, ociScreen(a), name); strings.Contains(row, failedGlyph()) {
		t.Errorf("OCI row %q shows %s: %q", name, failedGlyph(), row)
	}
}

// failScanFromSec poses a scan failure the way :sec reports one for an image.
func failScanFromSec(a *App, image string) {
	a.Update(jobs.StartMsg{Run: jobs.NewRun(jobs.KindScan, command.ViewSecurity, "", "inventory", image)})
	a.Update(ociresources.ImageScanFinishedMsg{ImageName: image, Err: errors.New("boom")})
}

func failPull(a *App, image string) {
	a.Update(jobs.StartMsg{Run: jobs.NewRun(jobs.KindPull, command.ViewOCIResources, "", "images", image)})
	a.Update(ociresources.RegistryPullCompleteMsg{ImageName: image, Err: errors.New("boom")})
}

func TestOCIShowsAScanFailedFromSec(t *testing.T) {
	a := ociRouter(t)
	failScanFromSec(a, secImage)
	ociWantFailed(t, a, secImage)
	ociWantNotFailed(t, a, ociOther)
}

func TestOCIRetryClearsTheFailure(t *testing.T) {
	a := ociRouter(t)
	failScanFromSec(a, secImage)
	ociWantFailed(t, a, secImage)

	a.Update(jobs.StartMsg{Run: jobs.NewRun(jobs.KindScan, command.ViewOCIResources, "", "images", secImage)})
	row := rowOf(t, ociScreen(a), secImage)
	if strings.Contains(row, failedGlyph()) {
		t.Errorf("a retry in flight still shows %s: %q", failedGlyph(), row)
	}
	if !strings.Contains(row, indicator.Glyph(indicator.Queued, true)) {
		t.Errorf("a retry waiting to start has no queued dot: %q", row)
	}
}

// A scan launched from the OCI view itself and failing there.
func TestOCIShowsAScanFailedFromOCIAndRetryClearsIt(t *testing.T) {
	a := ociRouter(t)
	a.Update(jobs.StartMsg{Run: jobs.NewRun(jobs.KindScan, command.ViewOCIResources, "", "images", secImage)})
	a.Update(ociresources.ImageScanFinishedMsg{ImageName: secImage, Err: errors.New("boom")})
	ociWantFailed(t, a, secImage)
	ociWantNotFailed(t, a, ociOther)

	a.Update(jobs.StartMsg{Run: jobs.NewRun(jobs.KindScan, command.ViewOCIResources, "", "images", secImage)})
	ociWantNotFailed(t, a, secImage)
	a.Update(ociresources.ImageScanFinishedMsg{ImageName: secImage})
	ociWantNotFailed(t, a, secImage)
	if remembered(a, jobs.KindScan, secImage, "default") {
		t.Error("a successful retry left the failure in the router's memory")
	}
}

func TestOCIFailureSurvivesRefreshAndARebuiltView(t *testing.T) {
	a := ociRouter(t)
	failScanFromSec(a, secImage)

	a.Update(testutil.Key("ctrl+r")) // the reload command is not run: no docker
	ociWantFailed(t, a, secImage)

	delete(a.views, command.ViewOCIResources)
	openOCI(t, a)
	ociWantFailed(t, a, secImage)
}

func TestOCIFailureIsFilteredByContext(t *testing.T) {
	a := ociRouter(t)
	failScanFromSec(a, secImage)

	a.currentContext = "other"
	delete(a.views, command.ViewOCIResources)
	openOCI(t, a)
	ociWantNotFailed(t, a, secImage)

	a.currentContext = "default"
	delete(a.views, command.ViewOCIResources)
	openOCI(t, a)
	ociWantFailed(t, a, secImage)
}

// D5: :sec shows scan failures only, OCI shows scan and pull.
func TestOCIShowsAPullFailureThatSecDoesNot(t *testing.T) {
	a := ociRouter(t)
	failPull(a, secImage)
	ociWantFailed(t, a, secImage)

	openSec(t, a)
	wantNotFailed(t, a, secImage)
}

// A removal that succeeded drops the failure; one that failed keeps it.
func TestOCISuccessfulRemovalClearsTheFailure(t *testing.T) {
	a := ociRouter(t)
	failScanFromSec(a, secImage)

	_, cmd := a.Update(ociresources.ImageActionMsg{Action: "remove", ID: "aaa1111", Name: secImage, Err: errors.New("in use")})
	for _, msg := range testutil.Msgs(cmd) {
		a.Update(msg)
	}
	if !remembered(a, jobs.KindScan, secImage, "default") {
		t.Fatal("a refused removal dropped the failure")
	}

	_, cmd = a.Update(ociresources.ImageActionMsg{Action: "remove", ID: "aaa1111", Name: secImage})
	for _, msg := range testutil.Msgs(cmd) {
		a.Update(msg)
	}
	if remembered(a, jobs.KindScan, secImage, "default") {
		t.Error("the failure survived the removal of the image")
	}
	ociWantNotFailed(t, a, secImage)
}

// DEVDESK-153: a network removal is told by the row's blinking dot, driven by
// the router's frame like the Images table, with no spinner chain of the view.
func TestOCINetworkRemovalBlinksItsDotThroughTheRouter(t *testing.T) {
	stubAnimationEvery(t, time.UnixMilli(1000))
	a := ociRouter(t)
	a.Update(ociresources.NetworksListMsg{Networks: []docker.Network{
		{ID: "net1111", Name: "backnet", Driver: "bridge"},
		{ID: "net2222", Name: "frontnet", Driver: "bridge"},
	}})
	a.resize(a.width, a.height)
	a.Update(testutil.Key("tab")) // networks tab
	a.Update(testutil.Key(keymap.Delete))
	a.Update(sharedcomponents.ConfirmModalYesMsg{})

	held, ok := a.views[command.ViewOCIResources].(BlinkingView)
	if !ok || !held.WantsBlink() {
		t.Fatal("a network removal in flight does not ask the router for a blink")
	}
	a.anim.ticking = false
	if cmd := a.ensureTick(); cmd == nil {
		t.Fatal("a network removal on screen got no animation chain")
	}
	for _, on := range []bool{true, false} {
		m, _ := a.views[command.ViewOCIResources].Update(jobs.FrameMsg{Blink: on})
		a.setView(command.ViewOCIResources, m)
		row := rowOf(t, ociScreen(a), "backnet")
		if want := indicator.Glyph(indicator.Running, on); !strings.Contains(row, want) {
			t.Errorf("blink=%v: the removing network row lacks %q: %q", on, want, row)
		}
		if other := rowOf(t, ociScreen(a), "frontnet"); strings.Contains(other, indicator.Glyph(indicator.Running, true)) ||
			strings.Contains(other, indicator.Glyph(indicator.Running, false)) {
			t.Errorf("blink=%v: the other network shows a dot: %q", on, other)
		}
	}
}

// DEVDESK-374: the :oci loading seeds of footerLoadingSeeds. The view is created
// on screen and its Init is not run, so loading, loadingNets and loadingVols stay
// true; a seed is one of the five reasons the footer shows the dot.

// seedOCILoading puts :oci on screen with nothing received, then presses keys.
func seedOCILoading(t *testing.T, a *App, keys ...string) {
	t.Helper()
	t.Setenv("PATH", t.TempDir()) // no docker: a Cmd must stay inert
	delete(a.views, command.ViewOCIResources)
	_ = a.switchView(command.ViewOCIResources)
	a.resize(a.width, a.height)
	for _, k := range keys {
		a.Update(testutil.Key(k))
	}
}

func seedOCIImagesLoading(t *testing.T, a *App)   { seedOCILoading(t, a) }
func seedOCINetworksLoading(t *testing.T, a *App) { seedOCILoading(t, a, "tab") }
func seedOCIVolumesLoading(t *testing.T, a *App)  { seedOCILoading(t, a, "tab", "tab") }

// seedOCITagSearch submits a search in the registry browser: the Cmds are not run.
func seedOCITagSearch(t *testing.T, a *App) {
	t.Helper()
	a.config.Registry.Registries = []config.RegistryItem{{URL: "registry.example.com", Alias: "prod", Slug: "prod", AuthMode: config.AuthAnonymous}}
	seedOCILoading(t, a)
	a.Update(ociresources.ImagesListMsg{Images: []docker.Image{
		{ID: "aaa1111", Repository: "nginx", Tag: "1.27"},
		{ID: "bbb2222", Repository: "redis", Tag: "7"},
	}})
	a.Update(testutil.Key(keymap.Browser))
	// A name with a "/" goes straight to the tags flow: a short name would
	// start the level 1 image search instead of the tags flow this tests.
	for _, msg := range testutil.Type("library/nginx") {
		a.Update(msg)
	}
	a.Update(testutil.Key("enter"))
}

func ociTagSearchEnd(t *testing.T, a *App) tea.Msg {
	t.Helper()
	// AddRegistryTags decrements pendingSearches whatever the entry key; the
	// message carries the generation the open browser accepts (DEVDESK-393).
	m, ok := a.views[command.ViewOCIResources].(ociresources.Model)
	if !ok {
		t.Fatalf("the :oci view is %T, want ociresources.Model", a.views[command.ViewOCIResources])
	}
	return ociresources.MultiRegistryTagsLoadedMsg{Gen: m.RegistryBrowserGen(), EntryKey: "prod", RegistryURL: "registry.example.com", Alias: "prod", Repo: "nginx", Tags: []string{"1.27"}}
}

// seedOCINetworkInspect opens the inspection of the first network.
func seedOCINetworkInspect(t *testing.T, a *App) {
	t.Helper()
	seedOCILoading(t, a)
	a.Update(ociresources.NetworksListMsg{Networks: []docker.Network{{ID: "net1", Name: "bridge"}}})
	a.Update(testutil.Key("tab"))
	a.Update(testutil.Key("enter"))
}

func ociNetworkInspectEnd() tea.Msg {
	return ociresources.NetworkInspectLoadedMsg{NetworkID: "net1", NetworkName: "bridge"}
}
