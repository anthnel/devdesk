package jobs

import (
	"reflect"
	"testing"
)

// FrameMsg carries no snapshot: a receiver cannot mistake it for a change of
// the registry, and the compiler finds every reader of the frame.
func TestAFrameMessageCarriesNoSnapshot(t *testing.T) {
	typ := reflect.TypeOf(FrameMsg{})
	for _, name := range []string{"Runs", "Failures", "Snapshot"} {
		if _, ok := typ.FieldByName(name); ok {
			t.Errorf("FrameMsg has a %s field; it must carry no snapshot", name)
		}
	}
	for i := 0; i < typ.NumField(); i++ {
		switch typ.Field(i).Type.Kind() {
		case reflect.Slice, reflect.Map, reflect.Pointer:
			t.Errorf("FrameMsg.%s is a %s; a frame holds values only", typ.Field(i).Name, typ.Field(i).Type.Kind())
		}
	}
}

// The frame left ChangedMsg: ChangedMsg is for changes of the registry only.
func TestAChangedMessageCarriesNoFrame(t *testing.T) {
	typ := reflect.TypeOf(ChangedMsg{})
	for _, name := range []string{"Frame", "RenderedFrame", "Blink"} {
		if _, ok := typ.FieldByName(name); ok {
			t.Errorf("ChangedMsg still has a %s field; it belongs to FrameMsg", name)
		}
	}
}

// DEVDESK-341 step 5 (red): the router animates no spinner, so FrameMsg carries
// the blink phase and nothing else.
func TestAFrameMessageCarriesOnlyTheBlinkPhase(t *testing.T) {
	typ := reflect.TypeOf(FrameMsg{})
	if typ.NumField() != 1 {
		t.Errorf("FrameMsg has %d fields, want exactly 1 (Blink)", typ.NumField())
	}
	for _, name := range []string{"Frame", "RenderedFrame"} {
		if _, ok := typ.FieldByName(name); ok {
			t.Errorf("FrameMsg still has a %s field", name)
		}
	}
	if f, ok := typ.FieldByName("Blink"); !ok || f.Type.Kind() != reflect.Bool {
		t.Error("FrameMsg.Blink is missing or not a bool")
	}
}
