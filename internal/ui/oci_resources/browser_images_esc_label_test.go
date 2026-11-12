package ociresources

import (
	"testing"

	"github.com/anthnel/devdesk/internal/ui/testutil"
)

// escDescription returns the description the header gives esc.
func escDescription(t *testing.T, m Model) string {
	t.Helper()
	for _, s := range m.GetShortcuts() {
		if s.Key == "esc" {
			return s.Description
		}
	}
	t.Fatal("no esc shortcut")
	return ""
}

func TestLevelOneEscLabelFollowsTheConfirmedQuery(t *testing.T) {
	m := lvl1ImagesModel(t)
	if got := escDescription(t, m); got != "Go back" {
		t.Fatalf("esc label = %q, want Go back", got)
	}
	keys := testutil.ShortcutKeys(m.GetShortcuts())

	m = feed(t, m, testutil.Key("/"))
	m = feed(t, m, testutil.Type("redis")...)
	m = feed(t, m, testutil.Key("enter"))
	if got := escDescription(t, m); got != "Clear filter" {
		t.Errorf("esc label = %q, want Clear filter", got)
	}
	if got := testutil.ShortcutKeys(m.GetShortcuts()); len(got) != len(keys) {
		t.Errorf("keys = %v, want %v", got, keys)
	}

	m = feed(t, m, testutil.Key("esc"))
	if got := escDescription(t, m); got != "Go back" {
		t.Errorf("esc label = %q, want Go back after clearing", got)
	}
}
