package app

import (
	"testing"

	tea "github.com/charmbracelet/bubbletea"

	"github.com/anthnel/devdesk/internal/command"
	ociresources "github.com/anthnel/devdesk/internal/ui/oci_resources"
	"github.com/anthnel/devdesk/internal/ui/testutil"
)

// commanding returns a router with the command line open, which is the state
// every test here starts from.
func commanding(t *testing.T, view tea.Model) *App {
	t.Helper()
	a := router(t, view)
	a.enterCommandMode()
	return a
}

// typeCommand types s into the open command line, one keypress at a time, so
// the completion engine sees each prefix the way it would from a real user.
func typeCommand(t *testing.T, a *App, s string) {
	t.Helper()
	for _, msg := range testutil.Type(s) {
		a.handleKeyMsg(msg.(tea.KeyMsg))
	}
}

// ── Executing ────────────────────────────────────────────────────────────────

func TestQuitCommandQuits(t *testing.T) {
	a := commanding(t, &fakeView{})
	typeCommand(t, a, "quit")

	cmd := feedKey(t, a, testutil.Key("enter"))

	if _, ok := testutil.MsgOf[tea.QuitMsg](cmd); !ok {
		t.Errorf(":quit produced %T, want a quit", testutil.Msg(cmd))
	}
}

// A view command switches, closes the command line and builds the view on
// first use.
func TestAViewCommandSwitchesAndBuildsTheViewLazily(t *testing.T) {
	a := commanding(t, &fakeView{})
	typeCommand(t, a, "status")

	feedKey(t, a, testutil.Key("enter"))

	if a.currentView != command.ViewStatus {
		t.Errorf("current view = %s, want the status view", a.currentView)
	}
	if a.commandMode {
		t.Error("the command line stayed open after the command ran")
	}
	if _, built := a.views[command.ViewStatus]; !built {
		t.Error("the status view was never built")
	}
}

// Every view the parser can name has to be buildable, or the command silently
// leaves the user on the previous view.
func TestEveryNamedViewCanBeReached(t *testing.T) {
	views := []struct {
		input string
		want  command.ViewType
	}{
		{"dashboard", command.ViewDashboard},
		{"status", command.ViewStatus},
		{"gitlab-auth", command.ViewGitlabAuth},
		{"gitlab-explorer", command.ViewGitlabExplorer},
		{"workspaces", command.ViewWorkspaces},
		{"security", command.ViewSecurity},
		{"containers", command.ViewContainers},
		{"oci-resources", command.ViewOCIResources},
		{"netdiag", command.ViewNetdiag},
	}

	for _, tt := range views {
		t.Run(tt.input, func(t *testing.T) {
			a := commanding(t, &fakeView{})
			typeCommand(t, a, tt.input)

			feedKey(t, a, testutil.Key("enter"))

			if a.currentView != tt.want {
				t.Fatalf("current view = %s, want %s", a.currentView, tt.want)
			}
			if _, built := a.views[tt.want]; !built {
				t.Errorf("%s was named but never built", tt.want)
			}
		})
	}
}

// Switching to workspaces drops the existing model so it cannot reopen stuck in
// selection mode; the OCI view is kept — it holds scan results — and reset.
func TestSwitchingResetsTheBrowsersThatCanBeStuckInSelectionMode(t *testing.T) {
	t.Run("workspaces is rebuilt", func(t *testing.T) {
		a := commanding(t, &fakeView{})
		stale := &fakeView{}
		a.views[command.ViewWorkspaces] = stale
		typeCommand(t, a, "workspaces")

		feedKey(t, a, testutil.Key("enter"))

		if a.views[command.ViewWorkspaces] == tea.Model(stale) {
			t.Error("the workspaces view was reused and may still be in selection mode")
		}
	})

	t.Run("oci-resources is reset in place", func(t *testing.T) {
		a := commanding(t, &fakeView{})
		oci := &fakeView{}
		a.views[command.ViewOCIResources] = oci
		typeCommand(t, a, "oci-resources")

		feedKey(t, a, testutil.Key("enter"))

		if _, ok := receivedOf[ociresources.ResetSelectionMsg](oci); !ok {
			t.Error("the OCI view was not taken out of selection mode")
		}
		if a.views[command.ViewOCIResources] != tea.Model(oci) {
			t.Error("the OCI view was rebuilt, losing its scan results")
		}
	})
}

// ":context work" creates the context when it does not exist yet, which is the
// documented way to start a new one.
func TestContextCommandCreatesAnUnknownContext(t *testing.T) {
	a := commanding(t, &fakeView{})
	typeCommand(t, a, "context phase5")

	cmd := feedKey(t, a, testutil.Key("enter"))

	done, ok := testutil.MsgOf[ContextSwitchCompleteMsg](cmd)
	if !ok {
		t.Fatalf("the switch produced %T, want a completed switch", testutil.Msg(cmd))
	}
	if done.ContextName != "phase5" {
		t.Errorf("switched to %q, want phase5", done.ContextName)
	}
	if !done.Created {
		t.Error("the context was not reported as created")
	}
	if done.Config == nil {
		t.Error("the switch carried no configuration")
	}
}

// A name that cannot be a directory has to be refused before anything is
// written.
func TestContextCommandRefusesAnInvalidName(t *testing.T) {
	a := commanding(t, &fakeView{})
	typeCommand(t, a, "context ../escape")

	cmd := feedKey(t, a, testutil.Key("enter"))

	if _, ok := testutil.MsgOf[ContextSwitchErrorMsg](cmd); !ok {
		t.Errorf("an invalid context name produced %T, want an error", testutil.Msg(cmd))
	}
}

// Bare ":context" opens the picker instead of switching blindly.
func TestBareContextCommandListsThem(t *testing.T) {
	a := commanding(t, &fakeView{})
	typeCommand(t, a, "context")

	cmd := feedKey(t, a, testutil.Key("enter"))

	list, ok := testutil.MsgOf[ContextListMsg](cmd)
	if !ok {
		t.Fatalf(":context produced %T, want the context list", testutil.Msg(cmd))
	}
	if list.Current == "" {
		t.Error("the list does not say which context is current")
	}
}

// `:theme` is gone: the theme is a setting, and the configuration view owns it.
// The command opened a picker that wrote app.theme behind the settings form's
// back, which is one setting with two writers again.
func TestThemeIsNoLongerACommand(t *testing.T) {
	a := commanding(t, &fakeView{})
	typeCommand(t, a, "theme")

	feedKey(t, a, testutil.Key("enter"))

	if !a.commandMode {
		t.Error("an unknown command closed the command line instead of leaving it open to correct")
	}
}

// An unrecognised command leaves the line open with the text intact, so it can
// be corrected rather than retyped.
func TestAnUnknownCommandKeepsTheLineOpen(t *testing.T) {
	a := commanding(t, &fakeView{})
	typeCommand(t, a, "zzz")
	a.completionSuggestions = nil // nothing matched

	feedKey(t, a, testutil.Key("enter"))

	if !a.commandMode {
		t.Error("an unknown command closed the command line")
	}
	if got := a.commandInput.Value(); got != "zzz" {
		t.Errorf("the line holds %q, want the rejected text back", got)
	}
}

// ── Completion ───────────────────────────────────────────────────────────────

func TestTypingNarrowsTheSuggestions(t *testing.T) {
	a := commanding(t, &fakeView{})

	typeCommand(t, a, "sec")

	if len(a.completionSuggestions) == 0 {
		t.Fatal("typing sec produced no suggestions")
	}
	for _, s := range a.completionSuggestions {
		if len(s.Text) < 3 || s.Text[:3] != "sec" {
			t.Errorf("suggestion %q does not start with the typed prefix", s.Text)
		}
	}
}

func TestTabCyclesThroughTheSuggestionsAndWraps(t *testing.T) {
	a := commanding(t, &fakeView{})
	typeCommand(t, a, "s")
	total := len(a.completionSuggestions)
	if total < 2 {
		t.Skipf("only %d suggestion(s) for \"s\"; nothing to cycle", total)
	}

	for i := 1; i < total; i++ {
		feedKey(t, a, testutil.Key("tab"))
		if a.completionIndex != i {
			t.Fatalf("after %d tabs the index is %d, want %d", i, a.completionIndex, i)
		}
	}

	feedKey(t, a, testutil.Key("tab"))
	if a.completionIndex != 0 {
		t.Errorf("index = %d after cycling past the last suggestion, want it back at 0", a.completionIndex)
	}
}

func TestTabDoesNothingWithoutSuggestions(t *testing.T) {
	a := commanding(t, &fakeView{})
	typeCommand(t, a, "zzz")

	feedKey(t, a, testutil.Key("tab"))

	if a.completionIndex != 0 {
		t.Errorf("index = %d with nothing to cycle, want 0", a.completionIndex)
	}
}

// Enter runs the highlighted suggestion, not the half-typed text — that is what
// makes tab-then-enter work.
func TestEnterRunsTheHighlightedSuggestion(t *testing.T) {
	a := commanding(t, &fakeView{})
	typeCommand(t, a, "cont")
	if len(a.completionSuggestions) == 0 {
		t.Skip("no suggestion for \"cont\"")
	}
	chosen := command.ParseCommand(a.completionSuggestions[0].Text)
	if chosen.Type != command.CommandView {
		t.Skipf("the first suggestion for \"cont\" is %v, not a view", chosen.Type)
	}

	feedKey(t, a, testutil.Key("enter"))

	if a.currentView != chosen.View {
		t.Errorf("current view = %s, want the highlighted %s", a.currentView, chosen.View)
	}
}

// Editing after tabbing must restart the cycle: the index would otherwise point
// into a list that no longer contains the same commands.
func TestEditingTheLineResetsTheCycle(t *testing.T) {
	a := commanding(t, &fakeView{})
	typeCommand(t, a, "s")
	feedKey(t, a, testutil.Key("tab"))
	if a.completionIndex == 0 {
		t.Skip("tab did not move the index")
	}

	typeCommand(t, a, "e")

	if a.completionIndex != 0 {
		t.Errorf("index = %d after typing another character, want 0", a.completionIndex)
	}
}

// ── Leaving ──────────────────────────────────────────────────────────────────

func TestEscapeClosesTheCommandLineAndClearsCompletion(t *testing.T) {
	a := commanding(t, &fakeView{})
	typeCommand(t, a, "sec")

	cmd := feedKey(t, a, testutil.Key("esc"))

	if a.commandMode {
		t.Error("esc did not close the command line")
	}
	if a.completionSuggestions != nil {
		t.Errorf("%d suggestion(s) survived the close", len(a.completionSuggestions))
	}
	if _, ok := testutil.MsgOf[tea.WindowSizeMsg](cmd); !ok {
		t.Error("closing the command line did not ask for a re-layout")
	}
}

// While the command line is open it owns the keyboard: a key that would
// otherwise act on the table must not reach it.
func TestTheCommandLineOwnsTheKeyboardWhileOpen(t *testing.T) {
	view := &fakeView{}
	a := commanding(t, view)

	typeCommand(t, a, "d")
	feedKey(t, a, testutil.Key("ctrl+d"))

	if view.sawKey("d") || view.sawKey("ctrl+d") {
		t.Errorf("the view received %v from behind the command line", view.keysSeen())
	}
}
