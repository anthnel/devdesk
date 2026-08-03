package app

import (
	"strings"
	"testing"

	tea "github.com/charmbracelet/bubbletea"

	"github.com/anthnel/devdesk/internal/ui/help"
	"github.com/anthnel/devdesk/internal/ui/testutil"
)

// ── Opening ──────────────────────────────────────────────────────────────────

// The list opens with the cursor on the context already in use, so enter is a
// no-op rather than a switch to whatever happens to sort first.
func TestTheContextListOpensOnTheCurrentContext(t *testing.T) {
	a := router(t, &fakeView{})

	a.Update(ContextListMsg{Contexts: []string{"client-a", "default", "work"}, Current: "work"})

	if !a.showContextList {
		t.Fatal("the context list did not open")
	}
	if got := a.contextList[a.contextSelectedIdx]; got != "work" {
		t.Errorf("the cursor is on %q, want the current context work", got)
	}
}

// A current context that is not in the list must not leave the cursor out of
// range of the slice the overlay indexes into.
func TestAnUnknownCurrentContextLeavesTheCursorInRange(t *testing.T) {
	a := router(t, &fakeView{})

	a.Update(ContextListMsg{Contexts: []string{"default", "work"}, Current: "deleted"})

	if a.contextSelectedIdx < 0 || a.contextSelectedIdx >= len(a.contextList) {
		t.Errorf("cursor = %d, out of range for %d contexts", a.contextSelectedIdx, len(a.contextList))
	}
}

func TestTheThemeListOpensOnTheCurrentTheme(t *testing.T) {
	a := router(t, &fakeView{})

	a.Update(ThemeListMsg{Themes: []string{"default", "latte", "mocha"}, Current: "mocha"})

	if !a.showThemeList {
		t.Fatal("the theme list did not open")
	}
	if got := a.themeList[a.themeSelectedIdx]; got != "mocha" {
		t.Errorf("the cursor is on %q, want the current theme mocha", got)
	}
}

// ── Navigating ───────────────────────────────────────────────────────────────

// The cursor clamps at both ends rather than wrapping or running off the slice
// the overlay renders from.
func TestOverlayCursorsClampAtBothEnds(t *testing.T) {
	tests := []struct {
		name  string
		open  func(*App)
		index func(*App) int
		size  int
	}{
		{
			name:  "context list",
			open:  func(a *App) { a.Update(ContextListMsg{Contexts: []string{"a", "b", "c"}, Current: "a"}) },
			index: func(a *App) int { return a.contextSelectedIdx },
			size:  3,
		},
		{
			name:  "theme list",
			open:  func(a *App) { a.Update(ThemeListMsg{Themes: []string{"a", "b", "c"}, Current: "a"}) },
			index: func(a *App) int { return a.themeSelectedIdx },
			size:  3,
		},
	}

	for _, tt := range tests {
		t.Run(tt.name, func(t *testing.T) {
			a := router(t, &fakeView{})
			tt.open(a)

			for range tt.size + 2 {
				a.handleKeyMsg(testutil.Key("down"))
			}
			if got := tt.index(a); got != tt.size-1 {
				t.Errorf("cursor = %d after running past the end, want %d", got, tt.size-1)
			}

			for range tt.size + 2 {
				a.handleKeyMsg(testutil.Key("up"))
			}
			if got := tt.index(a); got != 0 {
				t.Errorf("cursor = %d after running past the start, want 0", got)
			}
		})
	}
}

func TestOverlaysAcceptVimNavigation(t *testing.T) {
	a := router(t, &fakeView{})
	a.Update(ContextListMsg{Contexts: []string{"a", "b", "c"}, Current: "a"})

	a.handleKeyMsg(testutil.Key("j"))
	if a.contextSelectedIdx != 1 {
		t.Errorf("j moved the cursor to %d, want 1", a.contextSelectedIdx)
	}

	a.handleKeyMsg(testutil.Key("k"))
	if a.contextSelectedIdx != 0 {
		t.Errorf("k moved the cursor to %d, want 0", a.contextSelectedIdx)
	}
}

// ── Choosing ─────────────────────────────────────────────────────────────────

func TestChoosingAContextClosesTheListAndSwitches(t *testing.T) {
	a := router(t, &fakeView{})
	a.Update(ContextListMsg{Contexts: []string{"default", "overlay-pick"}, Current: "default"})
	a.handleKeyMsg(testutil.Key("down"))

	cmd := feedKey(t, a, testutil.Key("enter"))

	if a.showContextList {
		t.Error("the list stayed open after a choice")
	}
	done, ok := testutil.MsgOf[ContextSwitchCompleteMsg](cmd)
	if !ok {
		t.Fatalf("choosing produced %T, want a context switch", testutil.Msg(cmd))
	}
	if done.ContextName != "overlay-pick" {
		t.Errorf("switched to %q, want the highlighted overlay-pick", done.ContextName)
	}
}

func TestChoosingAThemeClosesTheListAndApplies(t *testing.T) {
	a := router(t, &fakeView{})
	a.Update(ThemeListMsg{Themes: []string{"default"}, Current: "default"})

	cmd := feedKey(t, a, testutil.Key("enter"))

	if a.showThemeList {
		t.Error("the list stayed open after a choice")
	}
	applied, ok := testutil.MsgOf[ThemeAppliedMsg](cmd)
	if !ok {
		t.Fatalf("choosing produced %T, want a theme applied", testutil.Msg(cmd))
	}
	if applied.ThemeName != "default" {
		t.Errorf("applied %q, want default", applied.ThemeName)
	}
}

// Enter on an empty list must not index into it.
func TestChoosingFromAnEmptyListDoesNothing(t *testing.T) {
	a := router(t, &fakeView{})
	a.showContextList = true
	a.contextList = nil

	feedKey(t, a, testutil.Key("enter"))

	if !a.showContextList {
		t.Error("an empty list closed itself on enter")
	}
}

// A theme that has been applied asks for a re-layout: the styles it changed are
// baked into every rendered line.
func TestApplyingAThemeRefreshesTheLayout(t *testing.T) {
	a := router(t, &fakeView{})

	_, cmd := a.Update(ThemeAppliedMsg{ThemeName: "default"})

	if _, ok := testutil.MsgOf[tea.WindowSizeMsg](cmd); !ok {
		t.Error("applying a theme did not ask for a re-layout")
	}
}

// ── Closing ──────────────────────────────────────────────────────────────────

func TestEveryOverlayClosesOnEscapeAndQ(t *testing.T) {
	tests := []struct {
		name   string
		open   func(*App)
		isOpen func(*App) bool
	}{
		{"help", func(a *App) { a.showHelp = true }, func(a *App) bool { return a.showHelp }},
		{"context list", func(a *App) { a.showContextList = true }, func(a *App) bool { return a.showContextList }},
		{"theme list", func(a *App) { a.showThemeList = true }, func(a *App) bool { return a.showThemeList }},
	}

	for _, tt := range tests {
		for _, key := range []string{"esc", "q"} {
			t.Run(tt.name+"/"+key, func(t *testing.T) {
				a := router(t, &fakeView{})
				tt.open(a)

				a.handleKeyMsg(testutil.Key(key))

				if tt.isOpen(a) {
					t.Errorf("%q did not close the %s overlay", key, tt.name)
				}
			})
		}
	}
}

// "?" closes the help as well as opening it, so the same key toggles.
func TestQuestionMarkTogglesTheHelp(t *testing.T) {
	a := router(t, &fakeView{})

	a.handleKeyMsg(testutil.Key("?"))
	a.handleKeyMsg(testutil.Key("?"))

	if a.showHelp {
		t.Error("? did not close the help it had opened")
	}
}

// Anything the help overlay does not claim scrolls it; the content is longer
// than the screen for most views.
func TestUnclaimedKeysScrollTheHelp(t *testing.T) {
	long := help.Content{Title: "Test View"}
	for range 200 {
		long.KeyBindings = append(long.KeyBindings, help.KeyBinding{Key: "x", Description: "does something"})
	}
	a := router(t, &fakeView{helpContent: long})
	a.handleKeyMsg(testutil.Key("?"))
	before := a.helpViewport.YOffset

	a.handleKeyMsg(testutil.Key("pgdown"))

	if a.helpViewport.YOffset == before {
		t.Errorf("the help did not scroll: offset stayed at %d", before)
	}
}

// ── Rendering ────────────────────────────────────────────────────────────────

// The overlay marks the entry in use, which is the only way to tell where a
// switch would land from what is already loaded.
func TestOverlaysMarkTheEntryInUse(t *testing.T) {
	t.Run("context", func(t *testing.T) {
		a := router(t, &fakeView{})
		a.currentContext = "work"
		a.contextList = []string{"default", "work"}

		rendered := a.renderContextListOverlay()

		if !strings.Contains(rendered, "work (current)") {
			t.Errorf("the overlay does not mark the context in use:\n%s", rendered)
		}
		if !strings.Contains(rendered, "Select Context") {
			t.Error("the overlay has no title")
		}
	})

	t.Run("theme", func(t *testing.T) {
		a := router(t, &fakeView{})
		a.currentTheme = "mocha"
		a.themeList = []string{"default", "mocha"}

		rendered := a.renderThemeListOverlay()

		if !strings.Contains(rendered, "mocha (current)") {
			t.Errorf("the overlay does not mark the theme in use:\n%s", rendered)
		}
		if !strings.Contains(rendered, "Select Theme") {
			t.Error("the overlay has no title")
		}
	})
}

func TestTheHelpOverlayShowsTheViewsContent(t *testing.T) {
	view := &fakeView{helpContent: help.Content{
		Title:       "Network Diagnostics",
		Description: "Run connectivity checks.",
		KeyBindings: []help.KeyBinding{{Key: "ctrl+r", Description: "Refresh"}},
	}}
	a := router(t, view)

	a.handleKeyMsg(testutil.Key("?"))
	rendered := a.renderHelpOverlay()

	for _, want := range []string{"Network Diagnostics", "ctrl+r"} {
		if !strings.Contains(rendered, want) {
			t.Errorf("the help overlay does not show %q:\n%s", want, rendered)
		}
	}
}
