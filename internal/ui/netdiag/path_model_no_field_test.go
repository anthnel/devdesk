package netdiag

import (
	"reflect"
	"strings"
	"testing"

	"github.com/anthnel/devdesk/internal/ui/testutil"
)

// DEVDESK-471 step 3 (red phase): PathModel loses its target field. The target
// comes from the Diagnostics form only; nothing typed, pasted or pressed in the
// path model can change it or start a trace on something else. These tests name
// only symbols that exist before and after the change.

// TestPathModelHasNoTextField: no text input is held by the path model.
func TestPathModelHasNoTextField(t *testing.T) {
	typ := reflect.TypeOf(PathModel{})
	for i := range typ.NumField() {
		f := typ.Field(i)
		if strings.Contains(f.Type.String(), "textinput") {
			t.Errorf("PathModel.%s is a %s: the target field must be gone", f.Name, f.Type)
		}
	}
}

// TestPathNewModelIsReadyAndInert: a fresh model is in the ready state with no
// target, and neither typing nor pasting nor enter starts anything.
func TestPathNewModelIsReadyAndInert(t *testing.T) {
	pm, f := newPM(t)
	if pm.state != pathReady {
		t.Errorf("fresh state = %v, want the ready state", pm.state)
	}

	pm, _ = pmFeed(t, pm, testutil.Type("example.com")...)
	pm, _ = pmFeed(t, pm, testutil.Paste("example.org"))
	pm, cmd := pmFeed(t, pm, testutil.Key("enter"))

	if pm.running() || pm.gen != 0 || cmd != nil {
		t.Errorf("running=%v gen=%d cmd=%v: a model with no target must launch nothing", pm.running(), pm.gen, cmd != nil)
	}
	if pm.target.Host != "" {
		t.Errorf("target = %q, typing must not set one", pm.target.Host)
	}
	if _, ok := headerValue(pm, "Target"); ok {
		t.Error("header shows a Target nobody gave")
	}
	if f.resolveCalls != 0 || f.traceCalls != 0 {
		t.Errorf("engine called (resolve %d, trace %d)", f.resolveCalls, f.traceCalls)
	}
}

// TestPathLettersPasteAndEscNeverEditTheTarget: from the ready state and from the
// done state, esc opens no editing, letters and pastes are dropped, and enter
// traces again the target the form gave, unchanged.
func TestPathLettersPasteAndEscNeverEditTheTarget(t *testing.T) {
	const host = "example.com"
	cases := []struct {
		name  string
		setup func(t *testing.T) *PathModel
	}{
		{"ready", func(t *testing.T) *PathModel {
			pm, _ := newPM(t)
			pm.ready(pathTarget{Host: host})
			return pm
		}},
		{"done", func(t *testing.T) *PathModel {
			pm, _ := newPM(t)
			pm.ready(pathTarget{Host: host})
			pm, _ = pmFeed(t, pm, testutil.Key("enter"))
			pm, _ = pmFeed(t, pm, testutil.Key("esc")) // cancels the run
			if pm.state != pathDone {
				t.Fatalf("state = %v, want done after the cancel", pm.state)
			}
			return pm
		}},
	}
	for _, tc := range cases {
		t.Run(tc.name, func(t *testing.T) {
			pm := tc.setup(t)
			gen := pm.gen

			pm, _ = pmFeed(t, pm, testutil.Key("esc"))
			pm, _ = pmFeed(t, pm, testutil.Type("x.org")...)
			pm, cmd := pmFeed(t, pm, testutil.Paste("y.net"))
			if pm.running() || pm.gen != gen || cmd != nil {
				t.Fatalf("running=%v gen %d->%d cmd=%v: esc, letters and a paste must do nothing", pm.running(), gen, pm.gen, cmd != nil)
			}

			pm, cmd = pmFeed(t, pm, testutil.Key("enter"))
			if !pm.running() || pm.gen != gen+1 || cmd == nil {
				t.Fatalf("running=%v gen %d->%d cmd=%v, want enter to trace again", pm.running(), gen, pm.gen, cmd != nil)
			}
			if pm.target.Host != host {
				t.Errorf("target = %q, want %q: nothing typed may reach it", pm.target.Host, host)
			}
		})
	}
}
