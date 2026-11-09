package ociresources

import (
	"errors"
	"strings"
	"testing"

	"github.com/anthnel/devdesk/internal/docker"
	"github.com/anthnel/devdesk/internal/jobs"
	sharedcomponents "github.com/anthnel/devdesk/internal/ui/components"
	"github.com/anthnel/devdesk/internal/ui/indicator"
	"github.com/anthnel/devdesk/internal/ui/keymap"
	"github.com/anthnel/devdesk/internal/ui/testutil"
)

// §3.22 extended to the four tabs. Every action here is a `docker` invocation
// that takes seconds, and the row used to be identical to one where nothing was
// happening.

// removeSelected walks the confirm modal for the tab the model is on.
func removeSelected(t *testing.T, m Model) Model {
	t.Helper()
	return feed(t, m, testutil.Key(keymap.Delete), sharedcomponents.ConfirmModalYesMsg{})
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

func TestRemovingAnImageBlinksItsDotAndKeepsItsID(t *testing.T) {
	m := removeSelected(t, loadedModel(t))
	img := m.imageTable.Items()[0] // fixtures arrive in this order

	if !m.imageTable.IsBusy(img.Image.ID) {
		t.Fatal("the removal was not marked on the row")
	}
	// DEVDESK-146: the activity column carries the removal, so the ID cell is
	// no longer spent on a spinner and the row keeps saying which image it is.
	line := renderedRow(t, m.imageTable.View(), img.RawName)
	if !strings.Contains(line, shortID(img.Image.ID)) {
		t.Errorf("the removing row lost its ID:\n%s", line)
	}
	if !strings.Contains(line, img.RawName) {
		t.Errorf("the removing row lost the name that says which image it is:\n%s", line)
	}
	if hasSpinnerFrame(line) {
		t.Errorf("the removing row still carries a spinner frame:\n%s", line)
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
	if !m.footer.IsSet() {
		t.Error("a failed removal reported nothing")
	}
}

// Confirming twice on the same image used to issue two `docker rmi`; the second
// fails with "no such image" on a removal that worked.
func TestASecondRemovalOfTheSameImageIsRefused(t *testing.T) {
	m := removeSelected(t, loadedModel(t))

	m, _ = step(t, m, testutil.Key(keymap.Delete))

	if m.confirmModal != nil {
		t.Error("a second removal opened its confirmation while the first was running")
	}
	if m.footer.Text() != busyMessage {
		t.Errorf("footer = %q, want the busy message", m.footer.Text())
	}
}

// DEVDESK-153: the Networks, Volumes and Registries tables carry a removal,
// a login or a logout in the activity column, like Images (DEVDESK-146). No
// cell is spent on a spinner any more: the row keeps saying which object it is.

func TestRemovingANetworkShowsTheDotAndKeepsItsCells(t *testing.T) {
	m := removeSelected(t, feed(t, loadedModel(t), testutil.Key("tab")))
	net := m.networkTable.Items()[0]

	if !m.networkTable.IsBusy(net.ID) {
		t.Fatal("the removal was not marked on the row")
	}
	for _, on := range []bool{true, false} {
		m = feed(t, m, jobs.FrameMsg{Blink: on})
		line := renderedRow(t, m.networkTable.View(), net.Name)
		if want := indicator.Glyph(indicator.Running, on); !strings.Contains(line, want) {
			t.Errorf("blink=%v: the removing row lacks %q:\n%s", on, want, line)
		}
		if !strings.Contains(line, shortID(net.ID)) {
			t.Errorf("the removing row lost its ID:\n%s", line)
		}
		if !strings.Contains(line, net.Driver) {
			t.Errorf("the removing row lost its driver:\n%s", line)
		}
		if hasSpinnerFrame(line) {
			t.Errorf("the removing row still carries a spinner frame:\n%s", line)
		}
	}

	m = feed(t, m, NetworkActionMsg{Action: "remove", ID: net.ID})
	if m.networkTable.IsBusy(net.ID) {
		t.Error("the marker outlived the removal")
	}
	if line := renderedRow(t, m.networkTable.View(), net.Name); hasAnyActivityGlyph(line) {
		t.Errorf("the dot survived the removal:\n%s", line)
	}
}

func TestAFailedNetworkRemovalLiftsTheDotAndReportsInTheFooter(t *testing.T) {
	m := removeSelected(t, feed(t, loadedModel(t), testutil.Key("tab")))
	net := m.networkTable.Items()[0]

	m = feed(t, m, NetworkActionMsg{Action: "remove", ID: net.ID, Err: errors.New("network has active endpoints")})
	if m.networkTable.IsBusy(net.ID) {
		t.Error("the marker survived a failed removal")
	}
	if line := renderedRow(t, m.networkTable.View(), net.Name); hasAnyActivityGlyph(line) {
		t.Errorf("a failed removal leaves a dot on the row:\n%s", line)
	}
	if !m.footer.IsSet() {
		t.Error("a failed removal reported nothing")
	}
}

// A volume has no ID, so its name is its identity: both cells stay.
func TestRemovingAVolumeShowsTheDotAndKeepsNameAndDriver(t *testing.T) {
	m := removeSelected(t, feed(t, loadedModel(t), testutil.Key("tab"), testutil.Key("tab")))
	vol := m.volumeTable.Items()[0]

	if !m.volumeTable.IsBusy(vol.Name) {
		t.Fatal("the removal was not marked on the row")
	}
	for _, on := range []bool{true, false} {
		m = feed(t, m, jobs.FrameMsg{Blink: on})
		line := renderedRow(t, m.volumeTable.View(), vol.Name)
		if want := indicator.Glyph(indicator.Running, on); !strings.Contains(line, want) {
			t.Errorf("blink=%v: the removing row lacks %q:\n%s", on, want, line)
		}
		if !strings.Contains(line, vol.Driver) {
			t.Errorf("the removing row lost its driver:\n%s", line)
		}
		if hasSpinnerFrame(line) {
			t.Errorf("the removing row still carries a spinner frame:\n%s", line)
		}
	}

	m = feed(t, m, VolumeActionMsg{Action: "remove", Name: vol.Name, Err: errors.New("volume in use")})
	if m.volumeTable.IsBusy(vol.Name) {
		t.Error("the marker outlived the failed removal")
	}
	if line := renderedRow(t, m.volumeTable.View(), vol.Name); hasAnyActivityGlyph(line) {
		t.Errorf("a failed removal leaves a dot on the row:\n%s", line)
	}
	if !m.footer.IsSet() {
		t.Error("a failed removal reported nothing")
	}
}

// Logged is no longer the cell spent while a login or logout runs: the dot is
// in column 0 and Logged keeps its answer. U still picks the direction from
// that cell (§3.26), which is why the status is seeded here.
func TestALogoutShowsTheDotAndKeepsTheLoggedCell(t *testing.T) {
	m := registriesTab(t)
	reg := m.registries[0]
	m = feed(t, m,
		RegistryLoginStatusMsg{Status: map[string]docker.LoginState{reg.URL: docker.LoginHelper}},
		testutil.Key(keymap.Auth))

	if !m.registryTable.IsBusy(reg.URL) {
		t.Fatal("the logout was not marked on the row")
	}
	if got := m.actionLine(); !strings.Contains(got, "Logging out") {
		t.Errorf("actionLine = %q, want it to name the logout", got)
	}
	logged := m.registryTable.Items()[0].logged
	for _, on := range []bool{true, false} {
		m = feed(t, m, jobs.FrameMsg{Blink: on})
		line := renderedRow(t, m.registryTable.View(), reg.URL)
		if want := indicator.Glyph(indicator.Running, on); !strings.Contains(line, want) {
			t.Errorf("blink=%v: the logging-out row lacks %q:\n%s", on, want, line)
		}
		if !strings.Contains(line, logged) {
			t.Errorf("the logging-out row lost its Logged cell %q:\n%s", logged, line)
		}
		if hasSpinnerFrame(line) {
			t.Errorf("the logging-out row still carries a spinner frame:\n%s", line)
		}
	}

	m = feed(t, m, RegistryLogoutCompleteMsg{RegistryURL: reg.URL})
	if m.registryTable.IsBusy(reg.URL) {
		t.Error("the marker outlived the logout")
	}
}

func TestAFailedLoginLiftsTheDotAndReportsInTheFooter(t *testing.T) {
	m := registriesTab(t)
	reg := m.registries[0]
	m.registryTable.MarkBusy(reg.URL, "Logging in to "+reg.URL)
	m = feed(t, m, jobs.FrameMsg{Blink: true})
	if line := renderedRow(t, m.registryTable.View(), reg.URL); !strings.Contains(line, indicator.Glyph(indicator.Running, true)) {
		t.Fatalf("the logging-in row has no running dot:\n%s", line)
	}

	m = feed(t, m, RegistryLoginCompleteMsg{RegistryURL: reg.URL, Err: errors.New("unauthorized")})
	if m.registryTable.IsBusy(reg.URL) {
		t.Error("the marker survived a failed login")
	}
	if line := renderedRow(t, m.registryTable.View(), reg.URL); hasAnyActivityGlyph(line) {
		t.Errorf("a failed login leaves a dot on the row:\n%s", line)
	}
	if !m.footer.IsSet() {
		t.Error("a failed login reported nothing")
	}
}

// An action started on one tab goes on running after `tab`. A spinner that
// stopped turning because the user looked elsewhere would read as a hang.
func TestTheBlinkRunsForAnActionOnAnotherTab(t *testing.T) {
	m := removeSelected(t, loadedModel(t))
	m = feed(t, m, testutil.Key("tab")) // networks

	if len(m.busyLabels()) != 1 {
		t.Fatalf("busyLabels = %v, want the image removal still counted", m.busyLabels())
	}
	if !m.WantsBlink() {
		t.Error("WantsBlink is false while an image removal runs and the user is on the Networks tab")
	}
}

// A prune acts on no row, so marking every row would say something false. It
// gets a line of its own.
func TestAPruneSaysSoWithoutMarkingAnyRow(t *testing.T) {
	m := feed(t, loadedModel(t), testutil.Key(keymap.Prune), sharedcomponents.ConfirmModalYesMsg{})
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
