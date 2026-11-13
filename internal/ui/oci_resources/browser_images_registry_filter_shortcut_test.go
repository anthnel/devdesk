package ociresources

import (
	"slices"
	"testing"

	"github.com/anthnel/devdesk/internal/ui/testutil"
)

// DEVDESK-310 step 6: the `r` shortcut of level 1 is always listed (Rule 130)
// and greyed when there is nothing to narrow to; esc and the cursor.

func TestLevelOneRShortcutIsEnabledWithTwoRegistriesHoldingRows(t *testing.T) {
	m := lvl1ImagesModel(t)
	sc := m.GetShortcuts()
	if !testutil.HasShortcut(sc, "r") {
		t.Fatal("no r shortcut")
	}
	if testutil.ShortcutDisabled(sc, "r") {
		t.Error("r is greyed with two registries holding rows")
	}
}

func TestLevelOneRShortcutIsGreyedWithZeroOrOneRegistry(t *testing.T) {
	// Zero: the search is running, nothing answered.
	m := lvl1SearchingModel(t)
	sc := m.GetShortcuts()
	if !testutil.HasShortcut(sc, "r") || !testutil.ShortcutDisabled(sc, "r") {
		t.Errorf("zero registries: r listed=%v greyed=%v, want listed and greyed",
			testutil.HasShortcut(sc, "r"), testutil.ShortcutDisabled(sc, "r"))
	}
	// One: only one registry holds rows.
	m = lvl1Answer(t, m, "prod", "nginx")
	sc = m.GetShortcuts()
	if !testutil.HasShortcut(sc, "r") || !testutil.ShortcutDisabled(sc, "r") {
		t.Errorf("one registry: r listed=%v greyed=%v, want listed and greyed",
			testutil.HasShortcut(sc, "r"), testutil.ShortcutDisabled(sc, "r"))
	}
}

// With an r filter active, r stays lit so the user can get back to off. Stops
// come from the unfiltered rows, so through the keys a filter never outlives
// its stops: the state is forced here (same package) to pin the rule.
func TestLevelOneRShortcutStaysLitWhileAnRFilterIsActive(t *testing.T) {
	m := lvl1SearchingModel(t)
	m = lvl1Answer(t, m, "prod", "nginx")
	b := m.registryBrowser
	if len(b.searchFilterStops()) != 0 {
		t.Fatalf("fixture: stops = %v, want none", b.searchFilterStops())
	}
	b.searchFilter = resultFilter{entryKey: "prod"}
	if testutil.ShortcutDisabled(m.GetShortcuts(), "r") {
		t.Error("r is greyed while an r filter is active")
	}
}

// Through the keys: after r, the shortcut is still lit on every stop.
func TestLevelOneRShortcutStaysLitOnEveryStop(t *testing.T) {
	m, _ := rfAnswered(t)
	for i := 0; i < 5; i++ {
		m = feed(t, m, testutil.Key("r"))
		if testutil.ShortcutDisabled(m.GetShortcuts(), "r") {
			t.Errorf("after %d r presses: r is greyed", i+1)
		}
	}
}

// Rule 130: the key set is the same greyed or not.
func TestLevelOneShortcutKeysSameWithAndWithoutRFilter(t *testing.T) {
	m, _ := rfAnswered(t)
	off := testutil.ShortcutKeys(m.GetShortcuts())
	m = feed(t, m, testutil.Key("r"))
	if on := testutil.ShortcutKeys(m.GetShortcuts()); !slices.Equal(on, off) {
		t.Errorf("keys with r filter = %v, without = %v", on, off)
	}
}

// The cursor row is filtered out by r: the cursor goes to the top, not to its
// old index.
func TestRGoesToTheTopWhenTheCursorRowIsFilteredOut(t *testing.T) {
	m, _ := rfAnswered(t)
	b := m.registryBrowser
	b.searchTable.SetCursor(slices.IndexFunc(b.searchTable.Visible(), func(r searchRow) bool { return r.img.Name == "nginx-hub" }))
	assertSelected(t, b, rfHub, "nginx-hub")
	m = feed(t, m, testutil.Key("r")) // group prod: nginx-hub leaves
	rfWantNames(t, m, "nginx", "redis")
	if got := m.registryBrowser.searchTable.Cursor(); got != 0 {
		t.Errorf("cursor = %d, want 0 (top)", got)
	}
}

// esc with only the r token (bar closed, no query) goes back to the form and
// keeps the r filter; the footer label stays "Go back".
func TestEscWithOnlyTheRTokenGoesBackAndKeepsTheFilter(t *testing.T) {
	m, _ := rfAnswered(t)
	m = feed(t, m, testutil.Key("r"))
	if got := escDescription(t, m); got != "Go back" {
		t.Errorf("esc label = %q, want Go back with only the r token", got)
	}
	m = feed(t, m, testutil.Key("esc"))
	b := m.registryBrowser
	if b.state != browserStateInput {
		t.Fatalf("state = %d, want the form", b.state)
	}
	if b.searchFilter.isEmpty() {
		t.Error("esc cleared the r filter")
	}
}

// "Clear filter" appears only with a query, also next to an r token.
func TestClearFilterLabelNeedsAQueryEvenWithAnRToken(t *testing.T) {
	m, _ := rfAnswered(t)
	m = feed(t, m, testutil.Key("r"))
	if got := escDescription(t, m); got == "Clear filter" {
		t.Fatal("Clear filter shown with the r token alone")
	}
	m = feed(t, m, testutil.Key("/"))
	m = feed(t, m, testutil.Type("nginx")...)
	m = feed(t, m, testutil.Key("enter"))
	if got := escDescription(t, m); got != "Clear filter" {
		t.Errorf("esc label = %q, want Clear filter with a query", got)
	}
}
