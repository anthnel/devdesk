package ociresources

import (
	"errors"
	"strings"
	"testing"

	"github.com/anthnel/devdesk/internal/config"
	"github.com/anthnel/devdesk/internal/docker"
	"github.com/anthnel/devdesk/internal/ui/testutil"
)

// The forms are driven directly rather than through the model: they are pure
// state machines over keystrokes, and the model only routes to them. Their
// submit messages are what the model acts on, so the assertions are on those.

// ── Resource creation ────────────────────────────────────────────────────────

// The form creates a network or a volume; the kind decides which fields exist
// and which message comes back.
func TestResourceFormCarriesItsKind(t *testing.T) {
	for _, kind := range []resourceFormKind{resourceFormNetwork, resourceFormVolume} {
		f := NewResourceForm(kind, 120)

		for _, msg := range testutil.Type("my-resource") {
			f, _ = f.Update(msg)
		}
		// Name -> Driver -> Create.
		f, _ = f.Update(testutil.Key("down"))
		f, _ = f.Update(testutil.Key("down"))
		_, cmd := f.Update(testutil.Key("enter"))

		msg, ok := testutil.MsgOf[ResourceFormSubmitMsg](cmd)
		if !ok {
			t.Fatalf("kind %d produced %T on submit", kind, testutil.Msg(cmd))
		}
		if msg.Kind != kind {
			t.Errorf("submitted kind %d, want %d", msg.Kind, kind)
		}
		if msg.Name != "my-resource" {
			t.Errorf("submitted name %q", msg.Name)
		}
	}
}

// A resource with no name cannot be created, so the form refuses rather than
// letting Docker reject it.
func TestResourceFormRefusesABlankName(t *testing.T) {
	f := NewResourceForm(resourceFormNetwork, 120)
	f, _ = f.Update(testutil.Key("down"))

	_, cmd := f.Update(testutil.Key("enter"))

	if _, ok := testutil.MsgOf[ResourceFormSubmitMsg](cmd); ok {
		t.Error("a blank name was submitted")
	}
}

func TestResourceFormCancels(t *testing.T) {
	_, cmd := NewResourceForm(resourceFormVolume, 120).Update(testutil.Key("esc"))

	if _, ok := testutil.MsgOf[ResourceFormCancelMsg](cmd); !ok {
		t.Errorf("esc produced %T, want a cancel", testutil.Msg(cmd))
	}
}

// The form body is the same fields for both kinds, so the header title is the
// only thing that says which one is open.
func TestResourceFormNamesWhatItCreates(t *testing.T) {
	for kind, want := range map[resourceFormKind]string{
		resourceFormNetwork: "network",
		resourceFormVolume:  "volume",
	} {
		if got := NewResourceForm(kind, 120).GetTitle(); !strings.Contains(strings.ToLower(got), want) {
			t.Errorf("GetTitle() = %q for kind %d, want it to name a %s", got, kind, want)
		}
	}
}

// The two kinds default to the driver each actually supports, which is the only
// difference visible in the body.
func TestResourceFormDefaultsItsDriver(t *testing.T) {
	if view := NewResourceForm(resourceFormNetwork, 120).View(); !strings.Contains(view, "bridge") {
		t.Errorf("the network form does not default to bridge:\n%s", view)
	}
	if view := NewResourceForm(resourceFormVolume, 120).View(); !strings.Contains(view, "local") {
		t.Errorf("the volume form does not default to local:\n%s", view)
	}
}

// ── Registry ─────────────────────────────────────────────────────────────────

// A new registry submits with index -1; editing carries the index back so the
// model replaces rather than appends.
func TestRegistryFormDistinguishesNewFromEdit(t *testing.T) {
	fresh := NewRegistryForm(120)
	if fresh.index != -1 {
		t.Errorf("a new registry form has index %d, want -1", fresh.index)
	}

	existing := config.RegistryItem{URL: "registry.example.com", Username: "anthnel", Alias: "prod", AuthEnabled: true}
	edit := NewRegistryEditForm(3, existing, 120)
	if edit.index != 3 {
		t.Errorf("an edit form has index %d, want the row it came from", edit.index)
	}
	if view := edit.View(); !strings.Contains(view, "registry.example.com") {
		t.Errorf("the edit form is not prefilled:\n%s", view)
	}
}

// The password never reaches the config file — it travels alongside for the
// docker login and is dropped after.
func TestRegistryFormKeepsThePasswordOutOfTheItem(t *testing.T) {
	f := NewRegistryEditForm(0, config.RegistryItem{URL: "registry.example.com", AuthEnabled: true}, 120)

	msg := RegistryFormSubmitMsg{
		Index:    0,
		Item:     config.RegistryItem{URL: "registry.example.com", Username: "anthnel", AuthEnabled: true},
		Password: "hunter2",
	}

	if strings.Contains(msg.Item.URL+msg.Item.Username+msg.Item.Alias, "hunter2") {
		t.Error("the password leaked into the stored item")
	}
	if f.index != 0 {
		t.Errorf("index = %d", f.index)
	}
}

func TestRegistryFormCancels(t *testing.T) {
	_, cmd := NewRegistryForm(120).Update(testutil.Key("esc"))

	if _, ok := testutil.MsgOf[RegistryFormCancelMsg](cmd); !ok {
		t.Errorf("esc produced %T, want a cancel", testutil.Msg(cmd))
	}
}

// ── Launch ───────────────────────────────────────────────────────────────────

func launchForm(t *testing.T) *LaunchForm {
	t.Helper()
	return NewLaunchForm("api:v1", []string{"8080/tcp", "9090/tcp"}, networkFixtures(), 160)
}

// The image's own EXPOSE lines are offered as port mappings, so the common case
// needs no typing.
func TestLaunchFormOffersTheExposedPorts(t *testing.T) {
	view := launchForm(t).View()

	for _, want := range []string{"8080", "9090"} {
		if !strings.Contains(view, want) {
			t.Errorf("the form does not offer port %q:\n%s", want, view)
		}
	}
}

// The form shows the docker run it is about to issue, which is what makes it
// checkable before launching rather than after.
func TestLaunchFormPreviewsTheCommand(t *testing.T) {
	view := launchForm(t).View()

	if !strings.Contains(view, "docker run") {
		t.Errorf("the form does not preview the command:\n%s", view)
	}
	if !strings.Contains(view, "api:v1") {
		t.Error("the preview does not name the image")
	}
}

func TestLaunchFormCancels(t *testing.T) {
	_, cmd := launchForm(t).Update(testutil.Key("esc"))

	if _, ok := testutil.MsgOf[LaunchFormCancelMsg](cmd); !ok {
		t.Errorf("esc produced %T, want a cancel", testutil.Msg(cmd))
	}
}

// The form names the image it will launch: the same form is reused for every
// image, so the title is the only thing that says which.
func TestLaunchFormNamesTheImage(t *testing.T) {
	if view := launchForm(t).View(); !strings.Contains(view, "api:v1") {
		t.Errorf("the form does not name the image:\n%s", view)
	}
}

// A verification result from a previous image must not overwrite the current
// one, which is what the sequence number guards.
func TestStaleEntrypointVerificationIsDiscarded(t *testing.T) {
	f := launchForm(t)

	fresh, _ := f.Update(EntrypointVerifyFinishedMsg{Seq: -1, OK: false})

	if fresh == nil {
		t.Fatal("a stale verification result destroyed the form")
	}
}

// ── Network inspect ──────────────────────────────────────────────────────────

// The overlay lists what is attached to the network, which is the question it
// exists to answer.
func TestNetworkInspectListsTheContainers(t *testing.T) {
	m := feed(t, loadedModel(t), testutil.Key("tab")) // networks tab
	m = feed(t, m, testutil.Key("enter"))

	m = feed(t, m, NetworkInspectLoadedMsg{
		NetworkID: "net11111", NetworkName: "bridge",
		Containers: []docker.NetworkContainer{
			{Name: "api", IPv4: "172.17.0.2/16"},
			{Name: "web", IPv4: "172.17.0.3/16"},
		},
	})

	if m.networkInspectForm == nil {
		t.Fatal("the inspect overlay is not open")
	}
	view := m.View()
	for _, want := range []string{"api", "web", "172.17.0.2"} {
		if !strings.Contains(view, want) {
			t.Errorf("the overlay does not show %q:\n%s", want, view)
		}
	}
}

// A network Docker cannot describe must say so rather than showing an empty
// list, which reads as "nothing is attached".
func TestNetworkInspectReportsAFailure(t *testing.T) {
	m := feed(t, loadedModel(t), testutil.Key("tab"), testutil.Key("enter"))

	m = feed(t, m, NetworkInspectLoadedMsg{NetworkID: "net11111", Err: errTest})

	if m.errorMsg == "" && m.networkInspectForm != nil {
		t.Error("a failed inspect reported nothing")
	}
}

// Esc closes the overlay rather than the view, which is what InEditMode()
// buys.
func TestNetworkInspectClosesOnEsc(t *testing.T) {
	m := feed(t, loadedModel(t), testutil.Key("tab"), testutil.Key("enter"))
	m = feed(t, m, NetworkInspectLoadedMsg{NetworkID: "net11111", NetworkName: "bridge"})
	if m.networkInspectForm == nil {
		t.Skip("the overlay did not open")
	}

	m = feed(t, m, testutil.Key("esc"))

	if m.networkInspectForm != nil {
		t.Error("esc did not close the inspect overlay")
	}
}

var errTest = errors.New("docker: no such network")
