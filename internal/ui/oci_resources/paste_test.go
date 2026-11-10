package ociresources

import (
	"testing"

	"github.com/anthnel/devdesk/internal/config"
	"github.com/anthnel/devdesk/internal/ui/testutil"
)

// DEVDESK-201 D3: a paste reaches the focused field of every form this view
// opens (launch, resource, registry, registry browser) and the images filter
// bar, newlines become spaces, and no key action fires (Rule 111). The forms
// are put in place on the model, then the paste goes through Model.Update: that
// is the gate that used to switch on the key type only.
func TestPasteReachesTheFocusedField(t *testing.T) {
	cfg := []config.RegistryItem{{URL: "registry.example.com", Alias: "prod", Slug: "prod", AuthMode: config.AuthAnonymous}}
	tests := []struct {
		name  string
		open  func(m Model) Model
		read  func(m Model) string
		paste string
		want  string
	}{
		{"launch form, container name", func(m Model) Model {
			m.launchForm = NewLaunchForm("alpine:3", []string{"80/tcp"}, nil, 120)
			return m
		}, func(m Model) string { return m.launchForm.nameInput.Value() }, "web-1", "web-1"},
		{"launch form, multi-line", func(m Model) Model {
			m.launchForm = NewLaunchForm("alpine:3", []string{"80/tcp"}, nil, 120)
			return m
		}, func(m Model) string { return m.launchForm.nameInput.Value() }, "a\nb", "a b"},
		{"network form, name", func(m Model) Model {
			m.resourceForm = NewResourceForm(resourceFormNetwork, 120)
			return m
		}, func(m Model) string { return m.resourceForm.nameInput.Value() }, "my-net", "my-net"},
		{"volume form, action letters", func(m Model) Model {
			m.resourceForm = NewResourceForm(resourceFormVolume, 120)
			return m
		}, func(m Model) string { return m.resourceForm.nameInput.Value() }, "DPq?:", "DPq?:"},
		{"registry form, URL", func(m Model) Model {
			m.registryForm = NewRegistryForm(nil, 120)
			return m
		}, func(m Model) string { return m.registryForm.urlInput.Value() }, "https://nexus.example.com", "https://nexus.example.com"},
		{"registry form, multi-line", func(m Model) Model {
			m.registryForm = NewRegistryForm(nil, 120)
			return m
		}, func(m Model) string { return m.registryForm.urlInput.Value() }, "a\nb", "a b"},
		{"registry browser, repository", func(m Model) Model {
			m.registryBrowser = newRegistryBrowser(cfg, nil, nil, 120, 30)
			return m
		}, func(m Model) string { return m.registryBrowser.repoInput.Value() }, "library/alpine", "library/alpine"},
		{"registry browser, multi-line", func(m Model) Model {
			m.registryBrowser = newRegistryBrowser(cfg, nil, nil, 120, 30)
			return m
		}, func(m Model) string { return m.registryBrowser.repoInput.Value() }, "a\nb", "a b"},
	}
	for _, tc := range tests {
		t.Run(tc.name, func(t *testing.T) {
			m := tc.open(loadedModel(t))
			m, _ = step(t, m, testutil.Paste(tc.paste))

			if got := tc.read(m); got != tc.want {
				t.Errorf("field = %q after pasting %q, want %q", got, tc.paste, tc.want)
			}
			if m.launchForm == nil && m.resourceForm == nil && m.registryForm == nil && m.registryBrowser == nil {
				t.Error("the paste closed the form")
			}
			if m.confirmModal != nil {
				t.Error("the paste opened a confirmation")
			}
		})
	}
}

func TestPasteReachesTheImagesFilterBar(t *testing.T) {
	tests := []struct{ name, paste, want string }{
		{"word", "alpine", "alpine"},
		{"multi-line", "a\nb", "a b"},
		{"action letters", "SDGq", "SDGq"},
	}
	for _, tc := range tests {
		t.Run(tc.name, func(t *testing.T) {
			m := loadedModel(t)
			m.activeTab = tabImages
			m, _ = step(t, m, testutil.Key("/"))
			m, _ = step(t, m, testutil.Paste(tc.paste))
			if got := m.imageTable.FilterBar().SearchQuery(); got != tc.want {
				t.Errorf("search query = %q after pasting %q, want %q", got, tc.paste, tc.want)
			}
			if m.confirmModal != nil || m.launchForm != nil {
				t.Error("the paste triggered an action")
			}
		})
	}
}
