package components

import (
	"strings"
	"testing"

	tea "github.com/charmbracelet/bubbletea"

	"github.com/anthnel/devdesk/internal/ui/testutil"
)

// Rule 136: the bar is completely hidden until a filter is actually active.
func TestFilterBarHiddenUntilFiltered(t *testing.T) {
	fb := NewFilterBar()

	if fb.IsVisible() {
		t.Error("a fresh FilterBar reports itself visible")
	}
	if got := fb.ExtraHeight(); got != 0 {
		t.Errorf("ExtraHeight() = %d on a hidden bar, want 0", got)
	}
}

func TestFilterBarVisibilityTriggers(t *testing.T) {
	t.Run("search mode makes it visible", func(t *testing.T) {
		fb := NewFilterBar()
		fb.ActivateSearch()

		if !fb.IsVisible() {
			t.Error("bar is hidden while searching")
		}
		if got := fb.ExtraHeight(); got != 2 {
			t.Errorf("ExtraHeight() = %d while visible, want 2", got)
		}
	})

	t.Run("a confirmed query keeps it visible", func(t *testing.T) {
		fb := NewFilterBar()
		fb.ActivateSearch()
		feedFilterBar(&fb, testutil.Type("web")...)
		fb.Update(testutil.Key("enter"))

		if fb.InEditMode() {
			t.Error("still in edit mode after enter")
		}
		if !fb.IsVisible() {
			t.Error("bar hidden despite a confirmed query")
		}
		if got := fb.SearchQuery(); got != "web" {
			t.Errorf("SearchQuery() = %q, want %q", got, "web")
		}
	})

	t.Run("an active token makes it visible", func(t *testing.T) {
		fb := NewFilterBarWithTokens([]FilterToken{{Label: "tcp"}, {Label: "udp"}})
		if fb.IsVisible() {
			t.Error("bar visible with no active token")
		}

		fb.SetTokenActive("tcp", true)
		if !fb.IsVisible() {
			t.Error("bar hidden despite an active token")
		}
	})
}

func TestFilterBarActivateSearchEntersEditMode(t *testing.T) {
	fb := NewFilterBar()

	if fb.InEditMode() {
		t.Error("a fresh FilterBar is already in edit mode")
	}
	cmd := fb.ActivateSearch()
	if !fb.InEditMode() {
		t.Error("ActivateSearch() did not enter edit mode")
	}
	if cmd == nil {
		t.Error("ActivateSearch() returned no blink command")
	}
}

func TestFilterBarTypingUpdatesQueryLive(t *testing.T) {
	fb := NewFilterBar()
	fb.ActivateSearch()

	feedFilterBar(&fb, testutil.Type("eth")...)

	if got := fb.SearchQuery(); got != "eth" {
		t.Errorf("SearchQuery() = %q while typing, want %q", got, "eth")
	}
}

func TestFilterBarEscapeDiscardsQuery(t *testing.T) {
	fb := NewFilterBar()
	fb.ActivateSearch()
	feedFilterBar(&fb, testutil.Type("discard me")...)

	fb.Update(testutil.Key("esc"))

	if fb.InEditMode() {
		t.Error("still in edit mode after esc")
	}
	if got := fb.SearchQuery(); got != "" {
		t.Errorf("SearchQuery() = %q after esc, want empty", got)
	}
	if fb.IsVisible() {
		t.Error("bar still visible after esc discarded the query")
	}
}

// Update must ignore keys unless the bar owns them, otherwise a view forwarding
// every keypress would swallow its own shortcuts.
func TestFilterBarIgnoresKeysWhenNotSearching(t *testing.T) {
	fb := NewFilterBar()

	fb.Update(testutil.Key("a"))

	if got := fb.SearchQuery(); got != "" {
		t.Errorf("SearchQuery() = %q after a key outside search mode, want empty", got)
	}
	if fb.IsVisible() {
		t.Error("bar became visible from a key outside search mode")
	}
}

func TestFilterBarIgnoresNonKeyMessages(t *testing.T) {
	fb := NewFilterBar()
	fb.ActivateSearch()

	fb.Update(testutil.Resize(100, 30))

	if !fb.InEditMode() {
		t.Error("a window resize dropped the bar out of edit mode")
	}
}

func TestFilterBarClearSearch(t *testing.T) {
	fb := NewFilterBar()
	fb.ActivateSearch()
	feedFilterBar(&fb, testutil.Type("query")...)

	fb.ClearSearch()

	if fb.InEditMode() || fb.SearchQuery() != "" || fb.IsVisible() {
		t.Errorf("ClearSearch() left state behind: editing=%v query=%q visible=%v",
			fb.InEditMode(), fb.SearchQuery(), fb.IsVisible())
	}
}

func TestFilterBarTokenToggling(t *testing.T) {
	fb := NewFilterBarWithTokens([]FilterToken{{Label: "tcp"}, {Label: "udp"}})

	if fb.IsTokenActive("tcp") {
		t.Error("token starts active")
	}
	fb.SetTokenActive("tcp", true)
	if !fb.IsTokenActive("tcp") {
		t.Error("SetTokenActive(true) had no effect")
	}
	if fb.IsTokenActive("udp") {
		t.Error("activating tcp also activated udp")
	}
	fb.SetTokenActive("tcp", false)
	if fb.IsTokenActive("tcp") {
		t.Error("SetTokenActive(false) had no effect")
	}
}

func TestFilterBarUnknownTokenIsInert(t *testing.T) {
	fb := NewFilterBarWithTokens([]FilterToken{{Label: "tcp"}})

	fb.SetTokenActive("nope", true)

	if fb.IsTokenActive("nope") {
		t.Error("an unknown label reports as active")
	}
	if fb.IsVisible() {
		t.Error("setting an unknown label made the bar visible")
	}
}

// SetTokens is called on every refresh of a live table; losing the active state
// there would silently reset the user's filter under them.
func TestSetTokensPreservesActiveStateByLabel(t *testing.T) {
	fb := NewFilterBarWithTokens([]FilterToken{{Label: "tcp"}, {Label: "udp"}})
	fb.SetTokenActive("tcp", true)

	fb.SetTokens([]FilterToken{{Label: "udp"}, {Label: "tcp"}, {Label: "raw"}})

	if !fb.IsTokenActive("tcp") {
		t.Error("SetTokens() dropped the active state of tcp")
	}
	if fb.IsTokenActive("udp") || fb.IsTokenActive("raw") {
		t.Error("SetTokens() activated a token that was not active before")
	}
}

func TestSetTokensDropsRemovedLabels(t *testing.T) {
	fb := NewFilterBarWithTokens([]FilterToken{{Label: "tcp"}})
	fb.SetTokenActive("tcp", true)

	fb.SetTokens([]FilterToken{{Label: "udp"}})

	if fb.IsTokenActive("tcp") {
		t.Error("a token absent from the new list is still reported active")
	}
	if fb.IsVisible() {
		t.Error("bar still visible after its only active token disappeared")
	}
}

// The bar draws a closed rectangle that joins the viewport's bottom border,
// so both lines must span exactly the configured width.
func TestFilterBarViewFormsClosedRectangle(t *testing.T) {
	fb := NewFilterBarWithTokens([]FilterToken{{Label: "tcp"}})
	fb.SetTokenActive("tcp", true)
	fb.Resize(40)

	lines := strings.Split(fb.View(), "\n")
	if len(lines) != 2 {
		t.Fatalf("View() produced %d lines, want 2", len(lines))
	}
	if !strings.Contains(lines[1], "└") || !strings.Contains(lines[1], "┘") {
		t.Errorf("bottom line is not a closed border: %q", lines[1])
	}
	if !strings.Contains(lines[0], "tcp") {
		t.Error("active token is missing from the rendered bar")
	}
}

func TestFilterBarViewFallsBackToDefaultWidth(t *testing.T) {
	fb := NewFilterBar()
	fb.ActivateSearch()

	lines := strings.Split(fb.View(), "\n")
	if len(lines) != 2 {
		t.Fatalf("View() produced %d lines at width 0, want 2", len(lines))
	}
	if strings.TrimSpace(lines[1]) == "" {
		t.Error("bottom border is empty when no width was set")
	}
}

// feedFilterBar applies messages in order, mirroring how a view forwards keys
// while the bar owns input.
func feedFilterBar(fb *FilterBar, msgs ...tea.Msg) {
	for _, m := range msgs {
		fb.Update(m)
	}
}
