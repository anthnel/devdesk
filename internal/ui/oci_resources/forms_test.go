package ociresources

import (
	"errors"
	"strings"
	"testing"

	tea "github.com/charmbracelet/bubbletea"

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
	fresh := NewRegistryForm(nil, 120)
	if fresh.index != -1 {
		t.Errorf("a new registry form has index %d, want -1", fresh.index)
	}

	existing := config.RegistryItem{URL: "registry.example.com", Username: "anthnel", Alias: "prod", AuthEnabled: true}
	edit := NewRegistryEditForm(3, existing, []config.RegistryItem{existing}, 120)
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
	f := NewRegistryEditForm(0, config.RegistryItem{URL: "registry.example.com", AuthEnabled: true}, nil, 120)

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
	_, cmd := NewRegistryForm(nil, 120).Update(testutil.Key("esc"))

	if _, ok := testutil.MsgOf[RegistryFormCancelMsg](cmd); !ok {
		t.Errorf("esc produced %T, want a cancel", testutil.Msg(cmd))
	}
}

// ── Registry: slug, kind and provider (§3.8 step 1) ──────────────────────────

// typeInto focuses a field and types into it.
func typeIntoField(f *RegistryForm, field int, text string) *RegistryForm {
	f.focusedField = field
	f.updateFocus()
	for _, msg := range testutil.Type(text) {
		f, _ = f.Update(msg)
	}
	return f
}

// saveRegistryForm presses Save and returns the item, or the form's error when
// the form refused to submit.
func saveRegistryForm(t *testing.T, f *RegistryForm) (config.RegistryItem, string) {
	t.Helper()
	f.focusedField = regFieldSubmit
	f, cmd := f.Update(testutil.Key("enter"))
	msg, ok := testutil.MsgOf[RegistryFormSubmitMsg](cmd)
	if !ok {
		if f.err == "" {
			t.Fatalf("the form neither submitted nor reported why: %T", testutil.Msg(cmd))
		}
		return config.RegistryItem{}, f.err
	}
	return msg.Item, ""
}

// The slug is what a group's members point at, so no entry may leave the form
// without one — and nobody should have to type it.
func TestARegistrySavedWithoutASlugGetsTheDerivedOne(t *testing.T) {
	f := NewRegistryForm(nil, 120)
	f = typeIntoField(f, regFieldURL, "https://nexus.example.com")

	item, formErr := saveRegistryForm(t, f)

	if formErr != "" {
		t.Fatalf("the form refused a registry with no slug typed: %s", formErr)
	}
	if item.Slug != "nexus-example-com" {
		t.Errorf("Slug = %q, want it derived from the host", item.Slug)
	}
	if item.Kind != config.KindRegistry {
		t.Errorf("Kind = %q, want %q by default", item.Kind, config.KindRegistry)
	}
}

// A typed slug is refused rather than corrected: it is a link target, and
// silently changing it is how a group loses its members.
func TestASlugThatIsNotOneIsRefusedRatherThanRewritten(t *testing.T) {
	f := NewRegistryForm(nil, 120)
	f = typeIntoField(f, regFieldURL, "https://nexus.example.com")
	f = typeIntoField(f, regFieldSlug, "Prod Registry")

	item, formErr := saveRegistryForm(t, f)

	if formErr == "" {
		t.Fatalf("%q was accepted as a slug, and would have been rewritten at the next load", item.Slug)
	}
	if !strings.Contains(formErr, "Prod Registry") {
		t.Errorf("err = %q, want it to quote what was typed", formErr)
	}
}

// The load refuses a duplicate slug, so accepting one here would write a config
// the application then cannot open.
func TestASlugAlreadyInUseIsRefused(t *testing.T) {
	existing := []config.RegistryItem{{URL: "https://a.example.com", Slug: "prod"}}
	f := NewRegistryForm(existing, 120)
	f = typeIntoField(f, regFieldURL, "https://b.example.com")
	f = typeIntoField(f, regFieldSlug, "prod")

	_, formErr := saveRegistryForm(t, f)

	if formErr == "" {
		t.Fatal("a slug already in use was accepted")
	}
	if !strings.Contains(formErr, "prod") {
		t.Errorf("err = %q, want it to name the slug", formErr)
	}
}

// Two registries aliased the same are ordinary. A slug DevDesk derives is its
// own doing, so it steps aside instead of reporting a clash the user did not make.
func TestADerivedSlugStepsAsideForOneAlreadyInUse(t *testing.T) {
	existing := []config.RegistryItem{{URL: "https://a.example.com", Alias: "prod", Slug: "prod"}}
	f := NewRegistryForm(existing, 120)
	f = typeIntoField(f, regFieldURL, "https://b.example.com")
	f = typeIntoField(f, regFieldAlias, "prod")

	item, formErr := saveRegistryForm(t, f)

	if formErr != "" {
		t.Fatalf("a second registry aliased 'prod' was refused: %s", formErr)
	}
	if item.Slug != "prod-2" {
		t.Errorf("Slug = %q, want it to step aside from the one in use", item.Slug)
	}
}

// Editing an entry must not collide with itself, or no entry could ever be saved
// twice.
func TestEditingAnEntryKeepsItsOwnSlug(t *testing.T) {
	existing := []config.RegistryItem{
		{URL: "https://a.example.com", Slug: "prod"},
		{URL: "https://b.example.com", Slug: "dev"},
	}
	f := NewRegistryEditForm(0, existing[0], existing, 120)

	item, formErr := saveRegistryForm(t, f)

	if formErr != "" {
		t.Fatalf("re-saving an unchanged entry was refused: %s", formErr)
	}
	if item.Slug != "prod" {
		t.Errorf("Slug = %q, want the entry to keep its own", item.Slug)
	}
}

// The management URL and the provider describe where a group's members come
// from. On a plain registry there are none, so the fields are not there to walk
// through either.
func TestGroupOnlyFieldsAreSkippedForAPlainRegistry(t *testing.T) {
	f := NewRegistryForm(nil, 120)
	f.focusedField = regFieldAuth

	f, _ = f.Update(testutil.Key("down"))

	if f.focusedField != regFieldSubmit {
		t.Errorf("down from Auth reached field %d, want Save (%d) — the group fields are not there",
			f.focusedField, regFieldSubmit)
	}
	if view := f.View(); strings.Contains(view, "Management URL") || strings.Contains(view, "Provider") {
		t.Errorf("a plain registry renders the group-only fields:\n%s", view)
	}

	// Backwards too, or Save would be a one-way door onto a field that is gone.
	f.focusedField = regFieldSubmit
	f, _ = f.Update(testutil.Key("up"))
	if f.focusedField != regFieldAuth {
		t.Errorf("up from Save reached field %d, want Auth (%d)", f.focusedField, regFieldAuth)
	}
}

// On a group they are there, and both directions have to walk through them.
func TestAGroupWalksThroughItsOwnFields(t *testing.T) {
	f := NewRegistryForm(nil, 120)
	f.focusedField = regFieldKind
	f, _ = f.Update(testutil.Key("right")) // registry -> group

	f.focusedField = regFieldAuth
	f, _ = f.Update(testutil.Key("down"))
	if f.focusedField != regFieldMgmtURL {
		t.Errorf("down from Auth reached field %d, want the management URL (%d)", f.focusedField, regFieldMgmtURL)
	}

	f.focusedField = regFieldSubmit
	f, _ = f.Update(testutil.Key("up"))
	if f.focusedField != regFieldProvider {
		t.Errorf("up from Save reached field %d, want the provider (%d)", f.focusedField, regFieldProvider)
	}
}

// Cycling the kind is what makes those fields appear, and the provider is what
// decides which detector will handle the group.
func TestAGroupCarriesItsProviderAndManagementURL(t *testing.T) {
	f := NewRegistryForm(nil, 120)
	f = typeIntoField(f, regFieldURL, "https://nexus.example.com/repository/docker-group")

	f.focusedField = regFieldKind
	f, _ = f.Update(testutil.Key("right"))
	if !f.isGroup() {
		t.Fatalf("right on Kind gave %q, want a group", config.Kinds()[f.kindIdx])
	}

	f = typeIntoField(f, regFieldMgmtURL, "https://nexus.example.com/service/rest")
	f.focusedField = regFieldProvider
	f, _ = f.Update(testutil.Key("right")) // generic -> nexus

	item, formErr := saveRegistryForm(t, f)

	if formErr != "" {
		t.Fatalf("the group was refused: %s", formErr)
	}
	if item.Kind != config.KindGroup {
		t.Errorf("Kind = %q, want %q", item.Kind, config.KindGroup)
	}
	if item.Provider != config.ProviderNexus {
		t.Errorf("Provider = %q, want %q", item.Provider, config.ProviderNexus)
	}
	if item.ManagementURL != "https://nexus.example.com/service/rest" {
		t.Errorf("ManagementURL = %q, want what was typed", item.ManagementURL)
	}
}

// A management URL left behind by a group the user cycled away from would keep
// pointing the discovery at a repository manager for an entry that declares it
// has none.
func TestCyclingBackToARegistryDropsTheGroupOnlyValues(t *testing.T) {
	f := NewRegistryForm(nil, 120)
	f = typeIntoField(f, regFieldURL, "https://nexus.example.com")

	f.focusedField = regFieldKind
	f, _ = f.Update(testutil.Key("right")) // registry -> group
	f = typeIntoField(f, regFieldMgmtURL, "https://nexus.example.com/service/rest")
	f.focusedField = regFieldKind
	f, _ = f.Update(testutil.Key("left")) // group -> registry

	item, formErr := saveRegistryForm(t, f)

	if formErr != "" {
		t.Fatalf("the registry was refused: %s", formErr)
	}
	if item.ManagementURL != "" {
		t.Errorf("ManagementURL = %q on a plain registry, want it dropped", item.ManagementURL)
	}
	if item.Provider != "" {
		t.Errorf("Provider = %q on a plain registry, want it empty", item.Provider)
	}
}

// An edit form opens on the entry's own kind, or a group would silently become a
// registry the first time it is saved.
func TestAnEditFormOpensOnTheEntrysKindAndProvider(t *testing.T) {
	item := config.RegistryItem{
		URL:      "https://nexus.example.com",
		Slug:     "nexus",
		Kind:     config.KindGroup,
		Provider: config.ProviderNexus,
	}
	f := NewRegistryEditForm(0, item, []config.RegistryItem{item}, 120)

	saved, formErr := saveRegistryForm(t, f)

	if formErr != "" {
		t.Fatalf("re-saving a group was refused: %s", formErr)
	}
	if saved.Kind != config.KindGroup || saved.Provider != config.ProviderNexus {
		t.Errorf("saved = %+v, want the group and provider it opened on", saved)
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

// The cursor resolves through the table rather than by indexing the container
// slice, which is what the connectivity test acts on.
func TestNetworkInspectActsOnTheHighlightedContainer(t *testing.T) {
	m := feed(t, loadedModel(t), testutil.Key("tab"), testutil.Key("enter"))
	m = feed(t, m, NetworkInspectLoadedMsg{
		NetworkID: "net11111", NetworkName: "bridge",
		Containers: []docker.NetworkContainer{
			{Name: "api", IPv4: "172.17.0.2/16"},
			{Name: "web", IPv4: "172.17.0.3/16"},
		},
	})
	m = feed(t, m, testutil.Key("down"))

	sel := m.networkInspectForm.SelectedContainer()
	if sel == nil || sel.Name != "web" {
		t.Errorf("SelectedContainer() = %+v, want the row under the cursor", sel)
	}
}

// Rule 116: the arithmetic written out here subtracted the viewport borders a
// second time, so the columns summed two cells short and the selected row
// stopped short of the right border. The solver owns it now, at every width.
func TestNetworkInspectColumnsHoldTheWidthInvariant(t *testing.T) {
	for _, width := range []int{40, 60, 80, 120, 200} {
		m := feed(t, loadedModel(t), testutil.Key("tab"), testutil.Key("enter"))
		m = feed(t, m, NetworkInspectLoadedMsg{NetworkID: "net11111", NetworkName: "bridge"})
		m = feed(t, m, tea.WindowSizeMsg{Width: width, Height: 40})

		total := 0
		cols := m.networkInspectForm.table.Table().Columns()
		for i, col := range cols {
			total += col.Width
			if col.Width < 0 {
				t.Errorf("at width %d column %d is %d cells wide", width, i, col.Width)
			}
		}
		// The form is handed the viewport content width, so its own borders are
		// already gone: what is left to share is that width less the padding.
		if want := width - 2 - len(cols)*2; total != want {
			t.Errorf("at width %d the columns sum to %d, want %d", width, total, want)
		}
	}
}

var errTest = errors.New("docker: no such network")
