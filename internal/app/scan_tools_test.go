package app

import (
	"testing"

	"github.com/anthnel/devdesk/internal/command"
	"github.com/anthnel/devdesk/internal/scan"
	"github.com/anthnel/devdesk/internal/shared"
)

// The detection is the router's, once for every view (§3.86). These tests never
// run the detection Cmd — it probes the machine — but feed its answer back.

func detected(gen int) scanToolsDetectedMsg {
	return scanToolsDetectedMsg{gen: gen, report: scan.Report{
		Tools: map[scan.ToolID]scan.ToolStatus{scan.ToolTrivy: {Available: true}},
	}}
}

// An answer is kept in shared.State and handed to every view held, on screen
// or not.
func TestADetectionReachesEveryView(t *testing.T) {
	dashboard, ws := &fakeView{}, &fakeView{}
	a := router(t, dashboard)
	a.views[command.ViewWorkspaces] = ws

	_ = a.detectScanTools()
	a.Update(detected(a.toolsGen))

	if a.sharedState.Tools == nil || !a.sharedState.Tools.Available(scan.ToolTrivy) {
		t.Fatalf("shared Tools = %+v, want the detection", a.sharedState.Tools)
	}
	for name, v := range map[string]*fakeView{"dashboard": dashboard, "workspaces": ws} {
		msg, ok := receivedOf[shared.ScanToolsMsg](v)
		if !ok || msg.Report != a.sharedState.Tools {
			t.Errorf("%s did not receive the detection", name)
		}
	}
}

// A detection that is not the latest is dropped: a slow one started before a
// source change must not land after the next and show the old state.
func TestAStaleDetectionIsDropped(t *testing.T) {
	dashboard := &fakeView{}
	a := router(t, dashboard)

	_ = a.detectScanTools()
	stale := a.toolsGen
	_ = a.detectScanTools()
	a.Update(detected(stale))

	if a.sharedState.Tools != nil {
		t.Error("a stale detection was kept")
	}
	if _, ok := receivedOf[shared.ScanToolsMsg](dashboard); ok {
		t.Error("a stale detection was broadcast")
	}
}

// A context switch forgets the answer — the views say "not known yet" rather
// than show another context's — and retires what is in flight.
func TestForgettingTheToolsRetiresTheDetectionInFlight(t *testing.T) {
	a := router(t, &fakeView{})
	_ = a.detectScanTools()
	inFlight := a.toolsGen
	a.Update(detected(inFlight))

	a.forgetScanTools()
	a.Update(detected(inFlight))

	if a.sharedState.Tools != nil {
		t.Error("the previous context's detection survived the switch")
	}
}

// ctrl+r on the dashboard asks; the router detects.
func TestADetectionRequestStartsOne(t *testing.T) {
	a := router(t, &fakeView{})
	before := a.toolsGen

	_, cmd := a.Update(shared.ScanToolsDetectRequestMsg{})

	if cmd == nil || a.toolsGen != before+1 {
		t.Errorf("the request started no detection (gen %d → %d)", before, a.toolsGen)
	}
}

// A saved configuration re-detects only when it moved a tool: a filter changes
// what a scan reports, never where a tool runs from.
func TestOnlyASavedToolLocationRedetects(t *testing.T) {
	a := router(t, &fakeView{})
	_ = a.detectScanTools()

	a.config.Scan.Tools.Trivy.IgnoreUnfixed = true
	a.config.Scan.Categories.CI.Enabled = true
	if cmd := a.redetectIfToolsMoved(false); cmd != nil {
		t.Error("a filter and a category re-detected")
	}
	if cmd := a.redetectIfToolsMoved(true); cmd == nil {
		t.Error("an engine change did not re-detect")
	}
	a.config.Scan.Tools.Helm.Image = "mirror/helm"
	if cmd := a.redetectIfToolsMoved(false); cmd == nil {
		t.Error("a new image did not re-detect")
	}
}

// A view built after the detection landed receives it from the router.
func TestAViewBuiltLaterIsHandedTheDetection(t *testing.T) {
	a := router(t, &fakeView{})
	_ = a.detectScanTools()
	a.Update(detected(a.toolsGen))

	late := &fakeView{}
	a.views[command.ViewTemplates] = late
	a.sendScanToolsTo(command.ViewTemplates)

	if msg, ok := receivedOf[shared.ScanToolsMsg](late); !ok || msg.Report == nil {
		t.Error("a view built after the detection never received it")
	}
}
