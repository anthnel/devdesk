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
	want := map[string]reflect.Kind{"Frame": reflect.String, "RenderedFrame": reflect.String, "Blink": reflect.Bool}
	if typ.NumField() != len(want) {
		t.Errorf("FrameMsg has %d fields, want exactly %d", typ.NumField(), len(want))
	}
	for name, kind := range want {
		if f, ok := typ.FieldByName(name); !ok || f.Type.Kind() != kind {
			t.Errorf("FrameMsg.%s missing or not a %s", name, kind)
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
