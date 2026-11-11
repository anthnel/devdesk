package explorer

import (
	tea "charm.land/bubbletea/v2"
	"testing"

	"github.com/anthnel/devdesk/internal/shared"
	"github.com/anthnel/devdesk/internal/ui/keymap"
	"github.com/anthnel/devdesk/internal/ui/testutil"
)

// DEVDESK-341 step 7 review: WantsBlink arms the router's blink chain, so it
// must follow what the footer shows (status().Activity), never the raw flags.

func unauthenticatedModel(t *testing.T) Model {
	t.Helper()
	return feed(t, New(testConfig(), &shared.State{IsAuthenticated: false}), tea.WindowSizeMsg{Width: 160, Height: 30})
}

func TestUnauthenticatedExplorerDoesNotWantBlink(t *testing.T) {
	m := unauthenticatedModel(t)
	if st := m.status(); st.Activity {
		t.Errorf("a signed-out explorer shows an activity dot: %+v", st)
	}
	if m.WantsBlink() {
		t.Error("a signed-out explorer arms the blink chain with nothing to show")
	}
}

// With refreshing > 0 the clone, selection and creation screens show no
// activity line, so they must not ask for blink frames either.
func TestWantsBlinkMatchesTheStatusWhileRefreshingUnderAnotherMode(t *testing.T) {
	cases := map[string]func(t *testing.T) Model{
		"cloning": cloningModel,
		"selecting": func(t *testing.T) Model {
			return feed(t, drilledModel(t), testutil.Key(keymap.Clone))
		},
		"creating": func(t *testing.T) Model {
			return feed(t, loadedModel(t), testutil.Key(keymap.New))
		},
	}
	modes := map[string]ViewMode{"cloning": ModeCloning, "selecting": ModeSelecting, "creating": ModeCreatingProject}
	for name, build := range cases {
		t.Run(name, func(t *testing.T) {
			m := build(t)
			if m.mode != modes[name] {
				t.Fatalf("mode = %v, want %v", m.mode, modes[name])
			}
			m.refreshing = 1
			if got, want := m.WantsBlink(), m.status().Activity; got != want {
				t.Errorf("WantsBlink() = %v, status().Activity = %v", got, want)
			}
			// Only the clone and selection lines replace the load line; the
			// creation form shows it until a validation message takes over.
			if name != "creating" && m.WantsBlink() {
				t.Error("this screen shows no activity dot but wants blink frames")
			}
		})
	}
}

func TestWantsBlinkEqualsStatusActivityOnUsualStates(t *testing.T) {
	states := map[string]Model{
		"first load":   newTestModel(t),
		"loaded":       loadedModel(t),
		"refreshing":   refreshingModel(t),
		"drilled":      drilledModel(t),
		"unauth":       unauthenticatedModel(t),
		"selecting":    feed(t, drilledModel(t), testutil.Key(keymap.Clone)),
		"creating":     feed(t, loadedModel(t), testutil.Key(keymap.New)),
		"cloning":      cloningModel(t),
		"reloading":    func() Model { m := loadedModel(t); m.loading = true; return m }(),
		"error":        func() Model { m := refreshingModel(t); m.error = "boom"; return m }(),
		"error+load":   func() Model { m := newTestModel(t); m.error = "boom"; return m }(),
		"selecting+rf": func() Model { m := feed(t, drilledModel(t), testutil.Key(keymap.Clone)); m.refreshing = 2; return m }(),
	}
	for name, m := range states {
		t.Run(name, func(t *testing.T) {
			if got, want := m.WantsBlink(), m.status().Activity; got != want {
				t.Errorf("WantsBlink() = %v, status().Activity = %v", got, want)
			}
		})
	}
}
