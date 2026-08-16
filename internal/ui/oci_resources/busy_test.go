package ociresources

import (
	"errors"
	"strings"
	"testing"

	sharedcomponents "github.com/anthnel/devdesk/internal/ui/components"
	"github.com/anthnel/devdesk/internal/ui/testutil"
)

// §3.22 extended to the four tabs. Every action here is a `docker` invocation
// that takes seconds, and the row used to be identical to one where nothing was
// happening.

// removeSelected walks the confirm modal for the tab the model is on.
func removeSelected(t *testing.T, m Model) Model {
	t.Helper()
	return feed(t, m, testutil.Key("ctrl+d"), sharedcomponents.ConfirmModalYesMsg{})
}

// busyCell returns the drawn line for a row, where the spinner override lives —
// it is applied at render time so the stored cells do not go stale as the frame
// advances.
func renderedRow(t *testing.T, view, name string) string {
	t.Helper()
	for _, line := range strings.Split(view, "\n") {
		if strings.Contains(line, name) {
			return line
		}
	}
	t.Fatalf("no rendered row for %q in:\n%s", name, view)
	return ""
}

func TestRemovingAnImageSpinsItsRow(t *testing.T) {
	m := removeSelected(t, loadedModel(t))
	img := m.imageTable.Items()[0] // fixtures arrive in this order

	if !m.imageTable.IsBusy(img.Image.ID) {
		t.Fatal("the removal was not marked on the row")
	}
	// The ID cell is what the spinner spends — the row keeps saying which image
	// it is while it goes.
	line := renderedRow(t, m.imageTable.View(), img.RawName)
	if strings.Contains(line, shortID(img.Image.ID)) {
		t.Errorf("the removing row still shows its ID:\n%s", line)
	}
	if !strings.Contains(line, img.RawName) {
		t.Errorf("the removing row lost the name that says which image it is:\n%s", line)
	}
	if got := m.actionLine(); !strings.Contains(got, img.RawName) {
		t.Errorf("actionLine = %q, want it to name the image", got)
	}
}

// The marker has to be lifted on every outcome. Only the success path would
// leave the row spinning for the life of the view.
func TestAFailedImageRemovalStillLiftsTheMarker(t *testing.T) {
	m := removeSelected(t, loadedModel(t))
	id := m.imageTable.Items()[0].Image.ID

	m = feed(t, m, ImageActionMsg{Action: "remove", ID: id, Name: "api:v1", Err: errors.New("image is in use")})

	if m.imageTable.IsBusy(id) {
		t.Error("the marker survived a failed removal: the row spins for good")
	}
	if m.errorMsg == "" {
		t.Error("a failed removal reported nothing")
	}
}

// Confirming twice on the same image used to issue two `docker rmi`; the second
// fails with "no such image" on a removal that worked.
func TestASecondRemovalOfTheSameImageIsRefused(t *testing.T) {
	m := removeSelected(t, loadedModel(t))

	m, _ = step(t, m, testutil.Key("ctrl+d"))

	if m.confirmModal != nil {
		t.Error("a second removal opened its confirmation while the first was running")
	}
	if m.infoMsg != busyMessage {
		t.Errorf("infoMsg = %q, want the busy message", m.infoMsg)
	}
}

func TestRemovingANetworkSpinsItsRow(t *testing.T) {
	m := removeSelected(t, feed(t, loadedModel(t), testutil.Key("tab")))
	net := m.networkTable.Items()[0]

	if !m.networkTable.IsBusy(net.ID) {
		t.Fatal("the removal was not marked on the row")
	}
	if line := renderedRow(t, m.networkTable.View(), net.Name); strings.Contains(line, shortID(net.ID)) {
		t.Errorf("the removing row still shows its ID:\n%s", line)
	}

	m = feed(t, m, NetworkActionMsg{Action: "remove", ID: net.ID})
	if m.networkTable.IsBusy(net.ID) {
		t.Error("the marker outlived the removal")
	}
}

// A volume has no ID, so its name is its identity — which is also why the
// spinner cannot go there and takes the Driver cell instead.
func TestRemovingAVolumeSpinsItsRowAndKeepsItsName(t *testing.T) {
	m := removeSelected(t, feed(t, loadedModel(t), testutil.Key("tab"), testutil.Key("tab")))
	vol := m.volumeTable.Items()[0]

	if !m.volumeTable.IsBusy(vol.Name) {
		t.Fatal("the removal was not marked on the row")
	}
	line := renderedRow(t, m.volumeTable.View(), vol.Name)
	if strings.Contains(line, vol.Driver) {
		t.Errorf("the removing row still shows its driver:\n%s", line)
	}
	if !strings.Contains(line, vol.Name) {
		t.Errorf("the removing row lost its name, which is the only thing that says which volume it is:\n%s", line)
	}

	m = feed(t, m, VolumeActionMsg{Action: "remove", Name: vol.Name})
	if m.volumeTable.IsBusy(vol.Name) {
		t.Error("the marker outlived the removal")
	}
}

// Logged is exactly the answer a login or logout is about to change, so it is
// the cell to spend while one runs.
func TestALogoutSpinsTheLoggedCell(t *testing.T) {
	m := feed(t, registriesTab(t), testutil.Key("L"))
	reg := m.registries[0]

	if !m.registryTable.IsBusy(reg.URL) {
		t.Fatal("the logout was not marked on the row")
	}
	if got := m.actionLine(); !strings.Contains(got, "Logging out") {
		t.Errorf("actionLine = %q, want it to name the logout", got)
	}

	m = feed(t, m, RegistryLogoutCompleteMsg{RegistryURL: reg.URL})
	if m.registryTable.IsBusy(reg.URL) {
		t.Error("the marker outlived the logout")
	}
}

// An action started on one tab goes on running after `tab`. A spinner that
// stopped turning because the user looked elsewhere would read as a hang.
func TestTheFrameTurnsForAnActionOnAnotherTab(t *testing.T) {
	m := removeSelected(t, loadedModel(t))
	m = feed(t, m, testutil.Key("tab")) // networks

	if len(m.busyLabels()) != 1 {
		t.Fatalf("busyLabels = %v, want the image removal still counted", m.busyLabels())
	}
	if !m.advanceBusySpinners() {
		t.Error("the spinner tick stopped being scheduled while an action was running")
	}
}

// A prune acts on no row, so marking every row would say something false. It
// gets a line of its own.
func TestAPruneSaysSoWithoutMarkingAnyRow(t *testing.T) {
	m := feed(t, loadedModel(t), testutil.Key("p"), sharedcomponents.ConfirmModalYesMsg{})
	if m.pruning != "images" {
		t.Fatalf("pruning = %q after confirming, want the images prune marked", m.pruning)
	}

	if len(m.busyLabels()) != 0 {
		t.Errorf("a prune marked %v, want no row marked", m.busyLabels())
	}
	if got := m.actionLine(); !strings.Contains(got, "Pruning") {
		t.Errorf("actionLine = %q, want the prune named", got)
	}

	m = feed(t, m, PruneCompleteMsg{Output: "reclaimed 1.2GB"})
	if m.pruning != "" {
		t.Errorf("pruning = %q after the prune landed, want it cleared", m.pruning)
	}
}
