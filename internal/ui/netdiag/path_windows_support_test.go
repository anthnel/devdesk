package netdiag

import (
	"errors"
	"strings"
	"testing"

	"github.com/anthnel/devdesk/internal/netcheck"
	"github.com/anthnel/devdesk/internal/nettrace"
	"github.com/anthnel/devdesk/internal/ui/keymap"
	"github.com/anthnel/devdesk/internal/ui/testutil"
	"github.com/anthnel/devdesk/internal/wslenv"
)

// DEVDESK-492 (story DEVDESK-486): the path trace runs on native Windows, so the
// button, H, enter and ctrl+o are no longer greyed there and the "needs Linux"
// refusal is gone. These tests build the engine's supported() the way
// systemPathSupport does on a Windows host whose engine is available.

// windowsSupported is pathSupport as systemPathSupport calls it on native
// Windows when nettrace.Supported() answers nil.
func windowsSupported() error { return pathSupport("windows", wslenv.KindNone, nil) }

func TestPathSupportWindowsFollowsTheEngine(t *testing.T) {
	if err := pathSupport("windows", wslenv.KindNone, nil); err != nil {
		t.Fatalf("pathSupport(windows, none, nil) = %v, want nil", err)
	}
	engineErr := errors.New("raw sockets are filtered")
	if err := pathSupport("windows", wslenv.KindNone, engineErr); !errors.Is(err, engineErr) {
		t.Errorf("pathSupport(windows, none, engineErr) = %v, want the engine's error", err)
	}
	// An engine that refuses is not a platform refusal: no "needs Linux" text.
	err := pathSupport("windows", wslenv.KindNone, nettrace.ErrUnsupported)
	if got := pathUnsupportedReason(err); strings.Contains(got, "Linux") || strings.Contains(got, "WSL2") {
		t.Errorf("Windows reason = %q, want no mention of Linux or WSL2", got)
	}
}

// Only the platforms that still refuse keep a reason, and none sends Windows to WSL2.
func TestPathSupportRefusalsKeepTheirReasons(t *testing.T) {
	if got := pathUnsupportedReason(pathSupport("darwin", wslenv.KindNone, nil)); got != reasonPathMacOS {
		t.Errorf("macOS reason = %q, want %q", got, reasonPathMacOS)
	}
	if got := pathUnsupportedReason(pathSupport("linux", wslenv.KindWSL1, nil)); got != reasonPathWSL1 {
		t.Errorf("WSL1 reason = %q, want %q", got, reasonPathWSL1)
	}
	for _, goos := range []string{"darwin", "freebsd", "plan9"} {
		got := pathUnsupportedReason(pathSupport(goos, wslenv.KindNone, nil))
		if strings.Contains(got, "Windows") && goos != "darwin" {
			t.Errorf("%s reason = %q, must not mention Windows", goos, got)
		}
	}
	const want = "The path trace is not available on plan9"
	if got := pathUnsupportedReason(pathSupport("plan9", wslenv.KindNone, nil)); got != want {
		t.Errorf("generic OS reason = %q, want %q", got, want)
	}
}

func TestNoReasonSendsWindowsToWSL2(t *testing.T) {
	for _, err := range []error{
		pathSupport("windows", wslenv.KindNone, nil),
		pathSupport("windows", wslenv.KindNone, nettrace.ErrUnsupported),
	} {
		if err != nil && strings.Contains(pathUnsupportedReason(err), "use WSL2") {
			t.Errorf("reason %q still says use WSL2 for Windows", pathUnsupportedReason(err))
		}
	}
}

func TestWindowsTraceFormButtonIsNotGreyed(t *testing.T) {
	m, _ := traceFormModel(t, true)
	m.pathModel.engine.supported = windowsSupported
	m = typeInto(t, m, "example.com")

	if !m.traceable().Enabled() {
		t.Fatalf("traceable() = %+v, want enabled on Windows", m.traceable())
	}
	m.focusedField = fieldTraceButton
	m = feed(t, m, testutil.Key("enter"))

	if strings.Contains(m.footer.Text(), "Linux") {
		t.Errorf("footer = %q, want no refusal on Windows", m.footer.Text())
	}
	if m.state != StateTrace || !m.pathModel.running() {
		t.Errorf("state=%v running=%v after enter on Trace Path, want StateTrace and a started trace", m.state, m.pathModel.running())
	}
}

func TestWindowsHOnTheRouteRowIsNotGreyed(t *testing.T) {
	m, _ := routeResults(t, "example.com")
	m.pathModel.engine.supported = windowsSupported
	m = selectCheck(t, m, netcheck.CheckRoute)

	if sc := m.GetShortcuts(); !testutil.ShortcutEnabled(sc, keymap.Diagnose) {
		t.Errorf("H is greyed on Windows: %v", testutil.ShortcutKeys(sc))
	}
	m = feed(t, m, testutil.Key(keymap.Diagnose))
	if m.state != StateTrace {
		t.Errorf("state = %v after H, want StateTrace", m.state)
	}
}

func TestWindowsTraceLevelKeysAreNotGreyed(t *testing.T) {
	pm := pmWith(t, runningFake())
	pm.engine.supported = windowsSupported
	pm.ready(pathTarget{Host: "1.1.1.1"})

	sc := pmShortcuts(pm)
	for _, key := range []string{"enter", "ctrl+o"} {
		if !testutil.HasShortcut(sc, key) || !testutil.ShortcutEnabled(sc, key) {
			t.Errorf("%s is greyed in the trace level on Windows", key)
		}
	}
	if !pm.launchable().Enabled() {
		t.Errorf("launchable() = %+v, want enabled", pm.launchable())
	}

	pm, _ = pmFeed(t, pm, testutil.Key("ctrl+o"))
	if !pm.pickerOpen() {
		t.Error("ctrl+o did not open the source selector on Windows")
	}
	if strings.Contains(pm.footer.Text(), "Windows") {
		t.Errorf("footer = %q, want no platform refusal", pm.footer.Text())
	}
}

func TestWindowsEnterStartsTheTrace(t *testing.T) {
	pm := pmWith(t, runningFake())
	pm.engine.supported = windowsSupported
	gen := pm.gen

	pm, _ = launched(t, pm, "1.1.1.1")

	if !pm.running() || pm.gen == gen {
		t.Errorf("running=%v gen %d->%d, want a started trace", pm.running(), gen, pm.gen)
	}
}

// Native Windows is not WSL: the source row stays "this machine", no WSL2 label.
func TestWindowsSourceRowStaysThisMachine(t *testing.T) {
	steps := withWSL(pathSteps(nil, wslRoute, []nettrace.Hop{wslHop1, wslHop2}), wslenv.Info{})
	src := wslStep(steps, stepSource)
	if got := src.addressCell(); got != sourceLabel {
		t.Errorf("source cell = %q, want %q", got, sourceLabel)
	}
	for _, cell := range wslCells(steps) {
		if strings.Contains(cell, "WSL") || strings.Contains(cell, "Windows host") {
			t.Errorf("cell %q carries a WSL2 label on native Windows", cell)
		}
	}
}

// The help no longer lists Windows among the platforms that grey the trace out.
func TestPathHelpDoesNotGreyWindows(t *testing.T) {
	for _, s := range New(testConfig()).GetHelpContent().Sections {
		if s.Title != "Diagnostics Tab — Tracing the path" {
			continue
		}
		body := strings.Join(strings.Fields(s.Body), " ")
		if strings.Contains(body, "macOS, Windows and WSL1") {
			t.Error("help still says the trace is greyed on macOS, Windows and WSL1")
		}
		if !strings.Contains(body, "macOS and WSL1") {
			t.Error("help does not name macOS and WSL1 as the greyed platforms")
		}
		return
	}
	t.Fatal(`no section "Diagnostics Tab — Tracing the path"`)
}
