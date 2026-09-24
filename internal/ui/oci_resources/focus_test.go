package ociresources

import (
	"testing"

	"github.com/anthnel/devdesk/internal/docker"
	"github.com/anthnel/devdesk/internal/ui/testutil"
)

func TestImageMatchesTheEnginesSpellings(t *testing.T) {
	hub := docker.Image{ID: "sha256:0123456789abcdef0123", Repository: "nginx", Tag: "latest",
		RepoDigests: []string{"nginx@sha256:feed"}}
	podman := docker.Image{ID: "0123456789ab", Repository: "docker.io/library/nginx", Tag: "1.27"}
	private := docker.Image{ID: "fedcba987654", Repository: "registry.local:5000/team/app", Tag: "v2"}

	cases := []struct {
		name string
		img  docker.Image
		ref  string
		want bool
	}{
		{"exact name", hub, "nginx:latest", true},
		{"implicit latest", hub, "nginx", true},
		{"another tag", hub, "nginx:1.27", false},
		{"hub-qualified ref, bare repository", hub, "docker.io/library/nginx:latest", true},
		{"bare ref, podman's qualified repository", podman, "nginx:1.27", true},
		{"registry port is not a tag", private, "registry.local:5000/team/app:v2", true},
		{"registry port, implicit latest", private, "registry.local:5000/team/app", false},
		{"digest reference", hub, "nginx@sha256:feed", true},
		{"unknown digest", hub, "nginx@sha256:beef", false},
		{"short ID", hub, "0123456789ab", true},
		{"full ID with prefix", podman, "sha256:0123456789abcdef", true},
		{"other ID", private, "0123456789ab", false},
		{"empty ref", hub, "", false},
	}
	for _, tc := range cases {
		if got := imageMatches(tc.img, tc.ref); got != tc.want {
			t.Errorf("%s: imageMatches(%s, %q) = %v, want %v", tc.name, tc.img.Name(), tc.ref, got, tc.want)
		}
	}
}

func TestFocusImagePutsTheCursorOnTheImagesTab(t *testing.T) {
	m := loadedModel(t)
	m, _ = step(t, m, testutil.Key("tab")) // leave the images tab

	m = feed(t, m, FocusImageRequestMsg{Image: "web:v3"})

	if m.activeTab != tabImages {
		t.Fatalf("activeTab = %d, want the images tab", m.activeTab)
	}
	if img := m.getSelectedImage(); img == nil || img.Name() != "web:v3" {
		t.Errorf("selected = %v, want web:v3", img)
	}
}

func TestFocusImageDropsASearchThatHidesTheTarget(t *testing.T) {
	m := loadedModel(t)
	m.imageTable.FilterBar().ActivateSearch()
	m = feed(t, m, testutil.Key("c"), testutil.Key("a"), testutil.Key("enter"))
	if len(m.imageTable.Visible()) != 1 {
		t.Fatalf("the search left %d rows, want only cache", len(m.imageTable.Visible()))
	}

	m = feed(t, m, FocusImageRequestMsg{Image: "api:v1"})

	if img := m.getSelectedImage(); img == nil || img.Name() != "api:v1" {
		t.Errorf("selected = %v, want api:v1", img)
	}
	if q := m.imageTable.FilterBar().SearchQuery(); q != "" {
		t.Errorf("search = %q, want it dropped", q)
	}
}

func TestFocusImageWarnsWhenTheImageIsGone(t *testing.T) {
	m := loadedModel(t)

	m = feed(t, m, FocusImageRequestMsg{Image: "gone:v9"})

	if got := m.footer.Text(); got == "" {
		t.Error("no footer message for an image that is not listed")
	}
}

func TestFocusImageWaitsForTheFirstListing(t *testing.T) {
	m := newTestModel(t)

	m = feed(t, m, FocusImageRequestMsg{Image: "cache:v2"})
	if len(m.pendingRequests) != 1 {
		t.Fatalf("pending = %d, want the request held until the list arrives", len(m.pendingRequests))
	}
	m, cmd := step(t, m, ImagesListMsg{Images: imageFixtures()})
	m = feed(t, m, testutil.Msgs(cmd)...)

	if img := m.getSelectedImage(); img == nil || img.Name() != "cache:v2" {
		t.Errorf("selected = %v, want cache:v2", img)
	}
}
