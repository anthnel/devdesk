package configuration

import (
	"testing"

	"github.com/anthnel/devdesk/internal/config"
	"github.com/anthnel/devdesk/internal/ui/testutil"
)

// The container engine is settled on blur, like the forge and the secret
// backend. Cycling auto → docker → podman must re-resolve once, on the way out
// — not once per keypress, and not on the way back to where it started (§3.67).

func TestTheEngineFieldSettlesOnBlur(t *testing.T) {
	m := newModel(t)
	f := fieldNamed(t, containerEngineLabel)

	if !m.settlesOnBlur(f) {
		t.Error("the engine field does not settle on blur; cycling it would re-resolve once per keypress")
	}
}

// Cycling alone writes nothing: the field is committed when focus leaves it.
func TestCyclingTheEngineDoesNotPersist(t *testing.T) {
	m := focusOn(t, newModel(t), containerEngineLabel)
	before := m.config.App.ContainerEngine

	updated, cmd := m.cycleField(1)
	next := updated.(Model)

	if next.config.App.ContainerEngine == before {
		t.Fatalf("cycling left the value at %q", before)
	}
	if cmd != nil {
		t.Error("cycling the engine returned a command; it is settled on blur, not on every keypress")
	}
}

// Leaving the field with the value it arrived with changes nothing — otherwise
// cycling all the way round and back would re-resolve for no reason.
func TestLeavingTheEngineUnchangedResolvesNothing(t *testing.T) {
	m := focusOn(t, newModel(t), containerEngineLabel)

	next, cmd, ok := m.commitFocused()

	if !ok {
		t.Fatal("commitFocused refused a value it had not changed")
	}
	if cmd != nil {
		t.Error("leaving the engine as it was still asked the router to re-resolve")
	}
	if next.config.App.ContainerEngine != m.config.App.ContainerEngine {
		t.Error("commitFocused changed a value nobody cycled")
	}
}

// Leaving it on a different value tells the router, through the one flag it
// acts on. Without EngineChanged the views are rebuilt against the old engine.
func TestLeavingTheEngineChangedTellsTheRouter(t *testing.T) {
	m := focusOn(t, newModel(t), containerEngineLabel)
	updated, _ := m.cycleField(1)
	m = updated.(Model)

	_, cmd, ok := m.commitFocused()

	if !ok {
		t.Fatal("commitFocused refused the cycled value")
	}
	if cmd == nil {
		t.Fatal("leaving the engine on a new value persisted nothing")
	}
	msg, found := testutil.MsgOf[ConfigSavedMsg](cmd)
	if !found {
		t.Fatal("no ConfigSavedMsg; the router is never told")
	}
	if !msg.EngineChanged {
		t.Error("ConfigSavedMsg.EngineChanged is false; the router will rebuild the views against the old engine")
	}
}

// The cycled values are config's, so there is one list and the view cannot
// offer an engine no shape answers to.
func TestTheEngineFieldOffersWhatConfigDeclares(t *testing.T) {
	f := fieldNamed(t, containerEngineLabel)

	want := config.ContainerEngines()
	if len(f.Options) != len(want) {
		t.Fatalf("the field offers %v, config declares %v", f.Options, want)
	}
	for i := range want {
		if f.Options[i] != want[i] {
			t.Errorf("option %d = %q, want %q", i, f.Options[i], want[i])
		}
	}
}
