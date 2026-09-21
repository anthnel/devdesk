package app

import (
	"testing"

	tea "github.com/charmbracelet/bubbletea"

	"github.com/anthnel/devdesk/internal/command"
	"github.com/anthnel/devdesk/internal/ui/keymap"
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
		{"git-auth", command.ViewGitAuth},
		{"git-explorer", command.ViewGitExplorer},
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

	t.Run("oci-resources is left alone", func(t *testing.T) {
		a := commanding(t, &fakeView{})
		oci := &fakeView{}
		a.views[command.ViewOCIResources] = oci
		typeCommand(t, a, "oci-resources")

		feedKey(t, a, testutil.Key("enter"))

		// Only the workspaces view is ever lent now, so the images view has no
		// selection mode to be taken out of -- and it holds scan results, so it
		// must not be rebuilt either.
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

func TestTabOpensThePickerOnTheCurrentViewKeepingTheText(t *testing.T) {
	a := commanding(t, &fakeView{})
	a.currentView = command.ViewSecurity
	typeCommand(t, a, "zz")

	feedKey(t, a, testutil.Key("tab"))

	if !a.viewPicker {
		t.Fatal("tab did not open the view picker")
	}
	if got := a.viewPickerViews[a.viewPickerIdx]; got != string(command.ViewSecurity) {
		t.Errorf("selection = %s, want the current view", got)
	}
	if a.commandInput.Value() != "zz" {
		t.Errorf("typed text = %q, want it kept", a.commandInput.Value())
	}
}

func TestPickerIsCircular(t *testing.T) {
	a := commanding(t, &fakeView{})
	feedKey(t, a, testutil.Key("tab"))
	n := len(a.viewPickerViews)
	a.viewPickerIdx = 0

	feedKey(t, a, testutil.Key("left"))
	if a.viewPickerIdx != n-1 {
		t.Errorf("left from first = %d, want %d", a.viewPickerIdx, n-1)
	}
	feedKey(t, a, testutil.Key("right"))
	if a.viewPickerIdx != 0 {
		t.Errorf("right from last = %d, want 0", a.viewPickerIdx)
	}
}

func TestShiftTabMovesLeftInThePicker(t *testing.T) {
	a := commanding(t, &fakeView{})
	feedKey(t, a, testutil.Key("tab"))
	n := len(a.viewPickerViews)
	a.viewPickerIdx = 1

	feedKey(t, a, testutil.Key("shift+tab"))
	if a.viewPickerIdx != 0 {
		t.Errorf("shift+tab from 1 = %d, want 0", a.viewPickerIdx)
	}
	feedKey(t, a, testutil.Key("shift+tab"))
	if a.viewPickerIdx != n-1 {
		t.Errorf("shift+tab from first = %d, want %d", a.viewPickerIdx, n-1)
	}
}

func TestPickerListHasNoActionsOrRouterViews(t *testing.T) {
	a := commanding(t, &fakeView{})
	feedKey(t, a, testutil.Key("tab"))
	for _, name := range a.viewPickerViews {
		if name == "quit" || name == "context" || name == string(command.ViewViewer) {
			t.Errorf("picker lists %q", name)
		}
	}
}

func TestPickerEnterOpensTheSelectedView(t *testing.T) {
	a := commanding(t, &fakeView{})
	feedKey(t, a, testutil.Key("tab"))
	for i, name := range a.viewPickerViews {
		if name == string(command.ViewAbout) {
			a.viewPickerIdx = i
		}
	}

	feedKey(t, a, testutil.Key("enter"))

	if a.currentView != command.ViewAbout || a.commandMode || a.viewPicker {
		t.Errorf("view=%s commandMode=%v picker=%v, want about, both closed", a.currentView, a.commandMode, a.viewPicker)
	}
}

func TestPickerEscReturnsToTheCommandLine(t *testing.T) {
	a := commanding(t, &fakeView{})
	typeCommand(t, a, "ab")
	feedKey(t, a, testutil.Key("tab"))

	feedKey(t, a, testutil.Key("esc"))

	if a.viewPicker || !a.commandMode || a.commandInput.Value() != "ab" {
		t.Errorf("picker=%v commandMode=%v text=%q", a.viewPicker, a.commandMode, a.commandInput.Value())
	}
}

func TestPickerWindowKeepsTheSelectionVisible(t *testing.T) {
	names := []string{"aaaaaaaa", "bbbbbbbb", "cccccccc", "dddddddd", "eeeeeeee"}
	for sel := range names {
		start, end := pickerWindow(names, sel, 25)
		if sel < start || sel >= end {
			t.Errorf("sel %d outside window [%d,%d)", sel, start, end)
		}
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
	feedKey(t, a, testutil.Key(keymap.Delete))

	if view.sawKey("d") || view.sawKey("ctrl+d") {
		t.Errorf("the view received %v from behind the command line", view.keysSeen())
	}
}
