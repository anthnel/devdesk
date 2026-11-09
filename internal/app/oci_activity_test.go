package app

import (
	"errors"
	"strings"
	"testing"

	"github.com/charmbracelet/x/ansi"

	"github.com/anthnel/devdesk/internal/command"
	"github.com/anthnel/devdesk/internal/docker"
	"github.com/anthnel/devdesk/internal/jobs"
	"github.com/anthnel/devdesk/internal/ui/indicator"
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

func ociScreen(a *App) string { return ansi.Strip(a.views[command.ViewOCIResources].View()) }

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
