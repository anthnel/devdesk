package app

import (
	"testing"

	tea "github.com/charmbracelet/bubbletea"

	"github.com/anthnel/devdesk/internal/ui/testutil"
)

// editingView stands in for any view with a focused text input. It records the
// keys the router forwards to it, which is how the tests tell "the view got the
// keystroke" apart from "the router swallowed it".
type editingView struct {
	editing  bool
	received []string
}

func (v *editingView) Init() tea.Cmd { return nil }

func (v *editingView) Update(msg tea.Msg) (tea.Model, tea.Cmd) {
	if key, ok := msg.(tea.KeyMsg); ok {
		v.received = append(v.received, key.String())
	}
	return v, nil
}

func (v *editingView) View() string       { return "" }
func (v *editingView) InEditMode() bool   { return v.editing }
func (v *editingView) keysSeen() []string { return v.received }

// The point of the binding: a focused text input cannot claim it. Typing a URL
// like https://trivy-server:4954 needs ":" to stay an ordinary character, so
// ctrl+p is the only key that can be authoritative.
func TestAltColonEntersCommandModeFromInsideATextField(t *testing.T) {
	view := &editingView{editing: true}
	a := router(t, view)

	a.handleKeyMsg(testutil.Key(commandModeKey))

	if !a.commandMode {
		t.Fatal("ctrl+p did not open the command line while the view was editing")
	}
	if _, drawn := findPaletteFrame(screen(a)); !drawn {
		t.Error("ctrl+p opened the palette but nothing is drawn")
	}
	if len(view.keysSeen()) != 0 {
		t.Errorf("ctrl+p was forwarded to the view as %v; the router must consume it", view.keysSeen())
	}
}

func TestAltColonEntersCommandModeWithNothingFocused(t *testing.T) {
	a := router(t, &editingView{editing: false})

	a.handleKeyMsg(testutil.Key(commandModeKey))

	if !a.commandMode {
		t.Error("ctrl+p did not open the command line")
	}
	if _, drawn := findPaletteFrame(screen(a)); !drawn {
		t.Error("ctrl+p opened the palette but nothing is drawn")
	}
}

// A bare ":" keeps its old, conditional behaviour so muscle memory survives.
func TestBareColonStillDependsOnTheView(t *testing.T) {
	t.Run("nothing focused, it opens the command line", func(t *testing.T) {
		view := &editingView{editing: false}
		a := router(t, view)

		a.handleKeyMsg(testutil.Key(":"))

		if !a.commandMode {
			t.Error(": did not open the command line with nothing focused")
		}
		if len(view.keysSeen()) != 0 {
			t.Errorf(": was forwarded to the view as %v as well as opening the command line", view.keysSeen())
		}
	})

	t.Run("editing, it reaches the field", func(t *testing.T) {
		view := &editingView{editing: true}
		a := router(t, view)

		a.handleKeyMsg(testutil.Key(":"))

		if a.commandMode {
			t.Error(": opened the command line while a text field was focused")
		}
		if got := view.keysSeen(); len(got) != 1 || got[0] != ":" {
			t.Errorf("the view received %v, want the \":\" it needs to type a URL", got)
		}
	})
}

// Opening the palette changes no height: it is drawn over the view, so neither
// ctrl+p nor ":" asks for a re-layout (the view must not even receive a
// WindowSizeMsg), and the viewport keeps the height it had.
func TestOpeningThePaletteDoesNotResize(t *testing.T) {
	for _, key := range []string{commandModeKey, ":"} {
		t.Run(key, func(t *testing.T) {
			view := &fakeView{}
			a := router(t, view)
			height := a.viewport.Height
			received := len(view.received)

			_, cmd := a.handleKeyMsg(testutil.Key(key))

			if !a.commandMode {
				t.Fatalf("%s did not open the palette", key)
			}
			if requestsResize(cmd) {
				t.Errorf("opening the palette with %s requested a resize", key)
			}
			if a.viewport.Height != height || len(view.received) != received {
				t.Errorf("opening changed the layout: viewport %d -> %d, view messages %d -> %d",
					height, a.viewport.Height, received, len(view.received))
			}
		})
	}
}

// The command line always opens empty; a leftover query from the previous
// invocation would be executed by the next enter.
func TestEnteringCommandModeClearsTheInput(t *testing.T) {
	a := router(t, &editingView{editing: false})
	a.commandInput.SetValue("security")

	a.handleKeyMsg(testutil.Key(commandModeKey))

	if got := a.commandInput.Value(); got != "" {
		t.Errorf("the command line opened holding %q", got)
	}
}
