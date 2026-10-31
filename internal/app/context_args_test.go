package app

import (
	"errors"
	"slices"
	"testing"

	"github.com/anthnel/devdesk/internal/command"
	"github.com/anthnel/devdesk/internal/config"
	"github.com/anthnel/devdesk/internal/ui/components"
	"github.com/anthnel/devdesk/internal/ui/testutil"
)

// contextRouter builds a router through newWithSize (which registers the
// providers) over an isolated HOME holding the contexts default, work, wip.
func contextRouter(t *testing.T) *App {
	t.Helper()
	isolateHome(t)
	for _, name := range []string{"default", "work", "wip"} {
		if err := config.SaveContext(testConfig(), name); err != nil {
			t.Fatalf("SaveContext(%q): %v", name, err)
		}
	}
	a := newWithSize(testConfig(), 120, 40)
	a.currentContext = "work"
	return a
}

// loadContexts opens the command line and applies the load it started.
func loadContexts(t *testing.T, a *App) {
	t.Helper()
	cmd := a.enterCommandMode()
	msg, ok := testutil.MsgOf[contextNamesLoadedMsg](cmd)
	if !ok {
		t.Fatal("enterCommandMode produced no contextNamesLoadedMsg")
	}
	a.Update(msg)
}

func typeLine(a *App, text string) command.Suggestions {
	a.commandInput.SetValue(text)
	a.refreshSuggestions()
	return a.suggestions
}

func values(args []command.ArgSuggestion) []string {
	out := make([]string, len(args))
	for i, x := range args {
		out[i] = x.Value
	}
	return out
}

func TestContextOpeningCommandLineLoadsSortedNames(t *testing.T) {
	a := contextRouter(t)
	msg, ok := testutil.MsgOf[contextNamesLoadedMsg](a.enterCommandMode())
	if !ok {
		t.Fatal("no contextNamesLoadedMsg from enterCommandMode")
	}
	if msg.Err != nil {
		t.Fatalf("unexpected error: %v", msg.Err)
	}
	if want := []string{"default", "wip", "work"}; !slices.Equal(msg.Names, want) {
		t.Errorf("names = %v, want sorted %v", msg.Names, want)
	}
}

func TestContextArgsNotReadyBeforeLoad(t *testing.T) {
	a := contextRouter(t)
	s := typeLine(a, "ctx ")
	if s.ArgsReady || len(s.Args) != 0 {
		t.Errorf("before load: ready=%v args=%v, want not ready and none", s.ArgsReady, s.Args)
	}
	if a.snapshot().Ready {
		t.Error("snapshot is ready before any load")
	}
}

func TestContextArgsListOrderAndCurrent(t *testing.T) {
	a := contextRouter(t)
	loadContexts(t, a)
	s := typeLine(a, "ctx ")
	if !s.ArgsReady {
		t.Fatal("args not ready after load")
	}
	if got, want := values(s.Args), []string{"default", "wip", "work", "-", "new"}; !slices.Equal(got, want) {
		t.Fatalf("args = %v, want %v", got, want)
	}
	notes := map[string]string{}
	for _, x := range s.Args {
		notes[x.Value] = x.Note
		if x.Current != (x.Value == "work") {
			t.Errorf("%q: Current = %v, want only work to be current", x.Value, x.Current)
		}
	}
	if notes["work"] != "" || notes["default"] != "" || notes["wip"] != "" {
		t.Errorf("no context carries a note (the renderer labels the current one), got %v", notes)
	}
	if notes["-"] != "Previous context" || notes["new"] != "Create a context" {
		t.Errorf("notes for - and new = %q, %q", notes["-"], notes["new"])
	}
}

func TestContextArgsPrefixFiltersCaseInsensitively(t *testing.T) {
	a := contextRouter(t)
	loadContexts(t, a)
	s := typeLine(a, "ctx WO")
	if got, want := values(s.Args), []string{"work"}; !slices.Equal(got, want) {
		t.Errorf("prefix WO = %v, want %v", got, want)
	}
	s = typeLine(a, "ctx n")
	if got, want := values(s.Args), []string{"new"}; !slices.Equal(got, want) {
		t.Errorf("prefix n = %v, want %v", got, want)
	}
}

func TestContextArgsAfterNewListNothing(t *testing.T) {
	a := contextRouter(t)
	loadContexts(t, a)
	for _, line := range []string{"ctx new ", "ctx new x", "ctx new fo", "ctx NEW "} {
		if s := typeLine(a, line); len(s.Args) != 0 {
			t.Errorf("%q gave %v, want no line", line, values(s.Args))
		}
	}
}

// A complete "new" without a space is still a prefix: contexts that start with
// it stay visible next to the reserved word.
func TestContextArgsNewWithoutSpaceIsAPrefix(t *testing.T) {
	a := contextRouter(t)
	if err := config.SaveContext(testConfig(), "newton"); err != nil {
		t.Fatal(err)
	}
	loadContexts(t, a)
	s := typeLine(a, "ctx new")
	if got, want := values(s.Args), []string{"newton", "new"}; !slices.Equal(got, want) {
		t.Errorf("ctx new = %v, want %v", got, want)
	}
}

func TestContextLoadErrorClearsStaleArgLines(t *testing.T) {
	a := contextRouter(t)
	loadContexts(t, a)
	if s := typeLine(a, "ctx "); len(s.Args) == 0 {
		t.Fatal("precondition: no arg lines after a good load")
	}
	a.Update(contextNamesLoadedMsg{Err: errors.New("boom")})
	if len(a.suggestions.Args) != 0 || a.suggestions.ArgsReady {
		t.Errorf("after a failed load: args=%v ready=%v, want none and not ready",
			values(a.suggestions.Args), a.suggestions.ArgsReady)
	}
}

func TestContextLoadKeepsMovedSuggestionCursor(t *testing.T) {
	a := contextRouter(t)
	cmd := a.enterCommandMode()
	msg, ok := testutil.MsgOf[contextNamesLoadedMsg](cmd)
	if !ok {
		t.Fatal("no contextNamesLoadedMsg")
	}
	typeLine(a, "c")
	if len(a.suggestions.Entries) < 2 {
		t.Fatalf("need two command rows for %q, got %d", "c", len(a.suggestions.Entries))
	}
	a.sugCursor = 1
	a.Update(msg)
	if a.sugCursor != 1 {
		t.Errorf("sugCursor = %d after the load, want 1 (the user moved it)", a.sugCursor)
	}
}

func TestContextSnapshotAfterLoad(t *testing.T) {
	a := contextRouter(t)
	loadContexts(t, a)
	snap := a.snapshot()
	if !snap.Ready || snap.Current != "work" || !slices.Equal(snap.Names, []string{"default", "wip", "work"}) {
		t.Errorf("snapshot = %+v", snap)
	}
}

func TestContextLoadErrorPostsFooterAndStaysNotReady(t *testing.T) {
	a := contextRouter(t)
	_, cmd := a.Update(contextNamesLoadedMsg{Err: errors.New("boom")})
	post, ok := testutil.MsgOf[components.PostFooterMsg](cmd)
	if !ok {
		t.Fatal("no PostFooterMsg on a read error")
	}
	if post.Level != components.LevelError || post.Text != "Failed to list contexts — check logs" {
		t.Errorf("footer = level %v %q", post.Level, post.Text)
	}
	if a.snapshot().Ready {
		t.Error("snapshot ready after a failed load")
	}
	if s := typeLine(a, "ctx "); s.ArgsReady {
		t.Error("args ready after a failed load")
	}
}
