package ociresources

import (
	"reflect"
	"testing"
	"time"

	"github.com/anthnel/devdesk/internal/docker"
	"github.com/anthnel/devdesk/internal/imageupdate"
	"github.com/anthnel/devdesk/internal/ui/theme"
	"github.com/anthnel/devdesk/internal/ui/updatecol"
)

// Only a tagged image that was pulled is asked about: a built one has no
// registry digest, and its name would reach a registry that never heard of it.
func TestOnlyPulledTaggedImagesAreChecked(t *testing.T) {
	images := []docker.Image{
		{Repository: "alpine", Tag: "latest", RepoDigests: []string{"alpine@sha256:a"}},
		{Repository: "myapp", Tag: "dev"},
		{Repository: "node", Tag: "<none>", RepoDigests: []string{"node@sha256:n"}},
	}
	if got, want := pulledImageRefs(images), []string{"alpine:latest"}; !reflect.DeepEqual(got, want) {
		t.Errorf("refs = %v, want %v", got, want)
	}
}

func updateCell(t *testing.T, m Model, name string) string {
	t.Helper()
	for _, r := range m.imageTable.Items() {
		if r.RawName == name {
			return updatecol.Column(false, func(r imageRow) imageupdate.Status { return r.Update }).Cell(r)
		}
	}
	t.Fatalf("no row %s", name)
	return ""
}

// The arrow is decided against the local digest when shown, so a pull that
// brings the new digest clears it without asking the registry again.
func TestTheArrowFollowsTheLocalDigest(t *testing.T) {
	old := docker.Image{ID: "aaa", Repository: "alpine", Tag: "latest", RepoDigests: []string{"alpine@sha256:old"}}
	m := feed(t, newTestModel(t),
		ImagesListMsg{Images: []docker.Image{old}},
		ImageUpdatesCheckedMsg{Facts: map[string]imageupdate.Facts{
			"alpine:latest": {CheckedAt: time.Now(), Digest: "sha256:new"},
		}},
	)
	if got := updateCell(t, m, "alpine:latest"); got != theme.IconArrowDown {
		t.Errorf("cell = %q, want the new-build arrow", got)
	}

	pulled := old
	pulled.RepoDigests = []string{"alpine@sha256:new"}
	m = feed(t, m, ImagesListMsg{Images: []docker.Image{pulled}})
	if got := updateCell(t, m, "alpine:latest"); got != theme.IconOK {
		t.Errorf("cell after the pull = %q, want the up-to-date check mark", got)
	}
}

// Each state that is not an update says which it is: a blank used to stand for
// up to date, not comparable and failed alike.
func TestTheCellSaysWhyThereIsNoUpdate(t *testing.T) {
	now := time.Now()
	m := feed(t, newTestModel(t),
		ImagesListMsg{Images: []docker.Image{
			{ID: "a", Repository: "alpine", Tag: "latest", RepoDigests: []string{"alpine@sha256:a"}},
			{ID: "b", Repository: "myapp", Tag: "dev"},
			{ID: "c", Repository: "private.example/app", Tag: "1", RepoDigests: []string{"private.example/app@sha256:c"}},
			{ID: "d", Repository: "node", Tag: "20", RepoDigests: []string{"node@sha256:d"}},
		}},
		ImageUpdatesCheckedMsg{Facts: map[string]imageupdate.Facts{
			"alpine:latest":         {CheckedAt: now, Digest: "sha256:a"},
			"private.example/app:1": {CheckedAt: now, Failed: true},
		}},
	)
	for name, want := range map[string]string{
		"alpine:latest":         theme.IconOK,
		"myapp:dev":             theme.IconHammer,
		"private.example/app:1": theme.IconHelpCircle,
		"node:20":               theme.IconHourglass,
	} {
		if got := updateCell(t, m, name); got != want {
			t.Errorf("%s: cell = %q, want %q", name, got, want)
		}
	}
}
