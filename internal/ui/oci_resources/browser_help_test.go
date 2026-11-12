package ociresources

import (
	"strings"
	"testing"
)

// registryBrowserHelp returns the body of the Registry Browser help section.
func registryBrowserHelp(t *testing.T, m Model) string {
	t.Helper()
	for _, s := range m.GetHelpContent().Sections {
		if s.Title == "Registry Browser" {
			return s.Body
		}
	}
	t.Fatal("no Registry Browser help section")
	return ""
}

// Rule 114: the browser help follows the form's mode (DEVDESK-300, IS-11).
func TestRegistryBrowserHelpFollowsTheMode(t *testing.T) {
	t.Run("no Searcher", func(t *testing.T) {
		m := lvl1ImagesModel(t)
		m.registryBrowser.searcher = nil
		body := registryBrowserHelp(t, m)
		if !strings.Contains(body, "Browse Tags") {
			t.Error("help does not name Browse Tags")
		}
		if strings.Contains(body, "Open Tags") {
			t.Error("help names Open Tags without a Searcher")
		}
	})
	t.Run("with Searcher", func(t *testing.T) {
		m := lvl1ImagesModel(t)
		body := registryBrowserHelp(t, m)
		for _, want := range []string{"Search", "Open Tags", "Repository", "down"} {
			if !strings.Contains(body, want) {
				t.Errorf("help lacks %q", want)
			}
		}
		if strings.Contains(body, "Browse Tags") {
			t.Error("help still names Browse Tags next to Search and Open Tags")
		}
	})
}

// With no browser open yet, the help follows the Searcher one would open with.
func TestRegistryBrowserHelpWithoutBrowserFollowsDefaultSearcher(t *testing.T) {
	prev := defaultImageSearcher
	t.Cleanup(func() { defaultImageSearcher = prev })

	m := lvl1ImagesModel(t)
	m.registryBrowser = nil

	defaultImageSearcher = nil
	if body := registryBrowserHelp(t, m); !strings.Contains(body, "Browse Tags") {
		t.Error("no default Searcher: help does not name Browse Tags")
	}

	defaultImageSearcher = &fakeSearcher{}
	body := registryBrowserHelp(t, m)
	if !strings.Contains(body, "Open Tags") || strings.Contains(body, "Browse Tags") {
		t.Error("default Searcher: help does not describe the search mode")
	}
}
