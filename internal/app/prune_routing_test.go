package app

import (
	"strings"
	"testing"

	tea "charm.land/bubbletea/v2"
	"github.com/charmbracelet/x/ansi"

	"github.com/anthnel/devdesk/internal/command"
	"github.com/anthnel/devdesk/internal/docker"
	"github.com/anthnel/devdesk/internal/ui/components"
	"github.com/anthnel/devdesk/internal/ui/containers"
	"github.com/anthnel/devdesk/internal/ui/keymap"
	ociresources "github.com/anthnel/devdesk/internal/ui/oci_resources"
	"github.com/anthnel/devdesk/internal/ui/testutil"
)

// OC-22 (DEVDESK-820), the answers of a `P` reach the view that asked, wherever the
// user is: without it the view stayed on "Pruning images…" for good, and an answer
// heard on another screen could not end the wait. An answer heard off screen opens no
// confirmation.

const danglingID = "eee5555aaaa1"

// seedOCIPruning opens the OCI view on the Images tab with a dangling image and
// presses P. The returned Cmd (the engine read) is never run: PATH is emptied as a
// second guard against reaching an engine.
func seedOCIPruning(t *testing.T, a *App) {
	t.Helper()
	t.Setenv("PATH", t.TempDir())
	a.createView(command.ViewOCIResources)
	var m tea.Model = a.views[command.ViewOCIResources]
	for _, msg := range []tea.Msg{
		tea.WindowSizeMsg{Width: a.width, Height: a.height},
		ociresources.ImagesListMsg{Images: []docker.Image{{ID: danglingID, Repository: "<none>", Tag: "<none>"}}},
		testutil.Key(keymap.Prune),
	} {
		m, _ = m.Update(msg)
	}
	a.setView(command.ViewOCIResources, m)
	if !strings.Contains(ociFooter(a), "Pruning images") {
		t.Fatalf("setup: P did not mark the prune pending; footer = %q", ociFooter(a))
	}
}

func ociFooter(a *App) string {
	v := a.views[command.ViewOCIResources].(interface{ RenderFooter(int) string })
	return ansi.Strip(v.RenderFooter(180))
}

func ociHoldsAConfirmation(a *App) bool {
	return a.views[command.ViewOCIResources].(interface{ InEditMode() bool }).InEditMode()
}

// The user left the OCI view while the engine was read: the view still hears the
// answer, its wait ends, and no confirmation opens behind the user's back.
func TestImagePreviewAnswerReachesTheOCIViewOffScreen(t *testing.T) {
	a := routerAt(t, &bareView{}, 180, 40)
	seedOCIPruning(t, a)

	a.Update(ociresources.ImagePruneUsersMsg{Candidates: []string{danglingID}})

	if strings.Contains(ociFooter(a), "Pruning") {
		t.Error("the OCI view is still waiting: the answer sent while it was off screen was dropped")
	}
	if ociHoldsAConfirmation(a) {
		t.Error("a confirmation opened for a user who is in another view")
	}
	if a.currentView != command.ViewDashboard {
		t.Errorf("current view = %s: delivering the answer changed which view is on screen", a.currentView)
	}
}

// With the OCI view on screen the same answer opens the confirmation.
func TestImagePreviewAnswerOnScreenOpensTheConfirmation(t *testing.T) {
	a := routerAt(t, &bareView{}, 180, 40)
	seedOCIPruning(t, a)
	a.currentView = command.ViewOCIResources

	a.Update(ociresources.ImagePruneUsersMsg{Candidates: []string{danglingID}})

	if !ociHoldsAConfirmation(a) {
		t.Error("the answer opened no confirmation on screen")
	}
}

// The volumes preview follows the same route.
func TestVolumePreviewAnswerReachesTheOCIViewOffScreen(t *testing.T) {
	a := routerAt(t, &bareView{}, 180, 40)
	t.Setenv("PATH", t.TempDir())
	a.createView(command.ViewOCIResources)
	var m tea.Model = a.views[command.ViewOCIResources]
	vols := []docker.Volume{{Name: "scratch", Driver: "local"}}
	for _, msg := range []tea.Msg{
		tea.WindowSizeMsg{Width: a.width, Height: a.height},
		ociresources.VolumesListMsg{Volumes: vols},
		testutil.Key("tab"), testutil.Key("tab"),
		testutil.Key(keymap.Prune),
	} {
		m, _ = m.Update(msg)
	}
	a.setView(command.ViewOCIResources, m)
	if !strings.Contains(ociFooter(a), "Pruning volumes") {
		t.Fatalf("setup: P did not mark the prune pending; footer = %q", ociFooter(a))
	}

	a.Update(ociresources.VolumePruneUsersMsg{Volumes: vols})

	if strings.Contains(ociFooter(a), "Pruning") || ociHoldsAConfirmation(a) {
		t.Errorf("the answer was dropped or opened a confirmation off screen; footer = %q", ociFooter(a))
	}
}

// The outcome of the removal reaches the view off screen too: the wait ends and the
// bilan is there when the user comes back.
func TestRemovalOutcomeReachesTheOCIViewOffScreen(t *testing.T) {
	a := routerAt(t, &bareView{}, 180, 40)
	seedOCIPruning(t, a)
	// The preview answer opens the confirmation, Yes starts the removal.
	var m tea.Model = a.views[command.ViewOCIResources]
	for _, msg := range []tea.Msg{
		ociresources.ImagePruneUsersMsg{Candidates: []string{danglingID}},
		components.ConfirmModalYesMsg{},
	} {
		m, _ = m.Update(msg)
	}
	a.setView(command.ViewOCIResources, m)
	if !strings.Contains(ociFooter(a), "Pruning images") || ociHoldsAConfirmation(a) {
		t.Fatalf("setup: the removal is not pending; footer = %q", ociFooter(a))
	}

	a.Update(ociresources.PruneCompleteMsg{Report: docker.RemovalReport{{Target: danglingID, Status: docker.RemovalRemoved}}})

	if got := ociFooter(a); strings.Contains(got, "Pruning") || !strings.Contains(got, "1 removed") {
		t.Errorf("footer = %q, want the wait ended and the bilan there", got)
	}
}

// The containers view: the outcome of P reaches it off screen, and ends its wait.
func TestContainerPruneOutcomeReachesTheContainersViewOffScreen(t *testing.T) {
	a := routerAt(t, &bareView{}, 180, 40)
	t.Setenv("PATH", t.TempDir())
	a.createView(command.ViewContainers)
	id := strings.Repeat("cd", 32)
	var m tea.Model = a.views[command.ViewContainers]
	for _, msg := range []tea.Msg{
		tea.WindowSizeMsg{Width: a.width, Height: a.height},
		containers.ContainersListMsg{Containers: []docker.Container{{ID: id, Name: "old", State: "exited"}}},
		testutil.Key(keymap.Prune),
		// The confirmation is open; answering Yes starts the removal.
		components.ConfirmModalYesMsg{},
	} {
		m, _ = m.Update(msg)
	}
	a.setView(command.ViewContainers, m)
	footer := func() string {
		return ansi.Strip(a.views[command.ViewContainers].(interface{ RenderFooter(int) string }).RenderFooter(180))
	}
	if !strings.Contains(footer(), "Pruning containers") {
		t.Fatalf("setup: the removal is not pending; footer = %q", footer())
	}

	a.Update(containers.ContainerPruneMsg{Report: docker.RemovalReport{{Target: id, Status: docker.RemovalRemoved}}})

	if got := footer(); strings.Contains(got, "Pruning") || !strings.Contains(got, "1 removed") {
		t.Errorf("footer = %q, want the wait ended and the bilan there", got)
	}
}
