package netdiag

import (
	"errors"
	"reflect"
	"strings"
	"testing"

	"github.com/anthnel/devdesk/internal/netcheck"
	"github.com/anthnel/devdesk/internal/nettrace"
	"github.com/anthnel/devdesk/internal/ui/components"
	"github.com/anthnel/devdesk/internal/ui/keymap"
	"github.com/anthnel/devdesk/internal/ui/testutil"
	"github.com/anthnel/devdesk/internal/wslenv"
)

// DEVDESK-454 (DEVDESK-334): the Path tab says up front why it cannot trace,
// with a reason that names the platform.

// platformOff is a pathEngine.supported answering with the macOS reason.
func platformOff() error { return &pathPlatformError{reason: reasonPathMacOS} }

// offFake returns a model whose engine refuses with the macOS reason, and its fake.
func offFake(t *testing.T) (*PathModel, *fakeEngine) {
	t.Helper()
	f := runningFake()
	pm := pmWith(t, f)
	pm.engine.supported = platformOff
	return pm, f
}

func TestPathSupport(t *testing.T) {
	generic := errors.New("socket option missing")
	tests := []struct {
		name       string
		goos       string
		kind       wslenv.Kind
		traceErr   error
		want       string // "" means nil
		wantUnsupp bool   // errors.Is(err, nettrace.ErrUnsupported)
	}{
		{"macOS", "darwin", wslenv.KindNone, nettrace.ErrUnsupported, reasonPathMacOS, true},
		{"Windows", "windows", wslenv.KindNone, nettrace.ErrUnsupported, reasonPathWindows, true},
		{"other OS", "freebsd", wslenv.KindNone, nettrace.ErrUnsupported, "The path trace needs Linux — not available on freebsd", true},
		{"WSL1", "linux", wslenv.KindWSL1, nil, reasonPathWSL1, true},
		{"WSL2", "linux", wslenv.KindWSL2, nil, "", false},
		{"native Linux", "linux", wslenv.KindNone, nil, "", false},
		{"Linux, generic trace error", "linux", wslenv.KindNone, generic, reasonPathUnsupported, false},
		{"WSL2, generic trace error", "linux", wslenv.KindWSL2, generic, reasonPathUnsupported, false},
	}
	for _, tt := range tests {
		t.Run(tt.name, func(t *testing.T) {
			err := pathSupport(tt.goos, tt.kind, tt.traceErr)
			if tt.want == "" {
				if err != nil {
					t.Fatalf("pathSupport(%q, %v, %v) = %v, want nil", tt.goos, tt.kind, tt.traceErr, err)
				}
				return
			}
			if err == nil {
				t.Fatalf("pathSupport(%q, %v, %v) = nil, want %q", tt.goos, tt.kind, tt.traceErr, tt.want)
			}
			if got := pathUnsupportedReason(err); got != tt.want {
				t.Errorf("reason = %q, want %q", got, tt.want)
			}
			if tt.wantUnsupp && !errors.Is(err, nettrace.ErrUnsupported) {
				t.Errorf("errors.Is(%v, nettrace.ErrUnsupported) = false", err)
			}
		})
	}
}

func TestPathPlatformErrorShape(t *testing.T) {
	err := &pathPlatformError{reason: "why"}
	if err.Error() != "why" {
		t.Errorf("Error() = %q, want %q", err.Error(), "why")
	}
	if !errors.Is(err, nettrace.ErrUnsupported) {
		t.Error("a platform error must unwrap to nettrace.ErrUnsupported")
	}
}

func TestPathUnsupportedReason(t *testing.T) {
	wrapped := errors.Join(errors.New("ctx"), &pathPlatformError{reason: reasonPathWindows})
	tests := []struct {
		name string
		err  error
		want string
	}{
		{"bare ErrUnsupported", nettrace.ErrUnsupported, reasonPathUnsupported},
		{"generic error", errors.New("boom"), reasonPathUnsupported},
		{"platform error", &pathPlatformError{reason: reasonPathMacOS}, reasonPathMacOS},
		{"wrapped platform error", wrapped, reasonPathWindows},
	}
	for _, tt := range tests {
		t.Run(tt.name, func(t *testing.T) {
			if got := pathUnsupportedReason(tt.err); got != tt.want {
				t.Errorf("pathUnsupportedReason() = %q, want %q", got, tt.want)
			}
		})
	}
}

func TestPathReasonsAreShortAndKeepTheGenericWording(t *testing.T) {
	if reasonPathUnsupported != "The path trace is not supported on this platform" {
		t.Errorf("reasonPathUnsupported = %q, want the existing generic sentence", reasonPathUnsupported)
	}
	for _, r := range []string{reasonPathMacOS, reasonPathWindows, reasonPathWSL1, reasonPathUnsupported} {
		if n := len([]rune(r)); n == 0 || n > 75 {
			t.Errorf("reason %q is %d characters, want 1 to 75", r, n)
		}
	}
}

func TestSystemPathSupportIsStable(t *testing.T) {
	if systemPathSupport == nil {
		t.Fatal("systemPathSupport is nil")
	}
	a, b := systemPathSupport(), systemPathSupport()
	if (a == nil) != (b == nil) {
		t.Error("systemPathSupport changed between two calls")
	}
}

// The reason is on screen as soon as the tab opens, derived on every frame.
func TestPathOpensWithTheReasonAtWarning(t *testing.T) {
	for _, state := range []pathState{pathEditing, pathDone} {
		pm, _ := offFake(t)
		pm.state = state

		st := pm.statusLine()
		if st.Text != reasonPathMacOS || st.Level != components.LevelWarning {
			t.Errorf("state %v: status = {%q %v}, want Warning %q", state, st.Text, st.Level, reasonPathMacOS)
		}
		if pm.errText != "" || pm.pickerOpen() {
			t.Errorf("state %v: errText=%q picker=%v, want neither", state, pm.errText, pm.pickerOpen())
		}
		if got := pmFooter(pm, 120); !strings.Contains(got, reasonPathMacOS) {
			t.Errorf("state %v: rendered footer = %q, want the reason", state, got)
		}
		// Derived, not timed: a footer expiry leaves it in place.
		pm.footer.Handle(components.ClearFooterMsg{ID: pm.footer.ID()})
		if got := pm.statusLine().Text; got != reasonPathMacOS {
			t.Errorf("state %v: status after a footer expiry = %q, want %q", state, got, reasonPathMacOS)
		}
	}
}

func TestPathSupportedPlatformHasNoStatusWhenEditing(t *testing.T) {
	pm, _ := newPM(t)
	if st := pm.statusLine(); st != (components.Status{}) {
		t.Errorf("status = %+v on a supported platform, want empty", st)
	}
}

func TestPathEnterIsRefusedWithThePlatformReason(t *testing.T) {
	pm, f := offFake(t)
	gen := pm.gen

	pm, _ = launched(t, pm, "1.1.1.1")

	if pm.running() || pm.gen != gen || !pm.InEditMode() {
		t.Errorf("running=%v gen=%d (was %d) edit=%v, want a refusal that stays editing", pm.running(), pm.gen, gen, pm.InEditMode())
	}
	if pm.footer.Level() != components.LevelWarning || pm.footer.Text() != reasonPathMacOS {
		t.Errorf("footer = {%v %q}, want Warning %q", pm.footer.Level(), pm.footer.Text(), reasonPathMacOS)
	}
	if f.resolveCalls != 0 || f.traceCalls != 0 {
		t.Errorf("engine reached (resolve %d, trace %d)", f.resolveCalls, f.traceCalls)
	}
	if !testutil.ShortcutDisabled(pm.shortcuts(), "enter") {
		t.Error("enter is not greyed")
	}
	if got := pm.launchable().Reason; got != reasonPathMacOS {
		t.Errorf("launchable().Reason = %q, want %q", got, reasonPathMacOS)
	}
}

func TestPathSourceKeyIsGreyedWithThePlatformReason(t *testing.T) {
	pm, f := offFake(t)
	if sc := pm.shortcuts(); !testutil.HasShortcut(sc, "ctrl+o") || !testutil.ShortcutDisabled(sc, "ctrl+o") {
		t.Error("ctrl+o must stay listed and be greyed")
	}

	pm, _ = pmFeed(t, pm, testutil.Key("ctrl+o"))

	if pm.pickerOpen() {
		t.Fatal("the selector opened")
	}
	if pm.footer.Level() != components.LevelWarning || pm.footer.Text() != reasonPathMacOS {
		t.Errorf("footer = {%v %q}, want Warning %q", pm.footer.Level(), pm.footer.Text(), reasonPathMacOS)
	}
	if f.osCalls+f.psCalls != 0 {
		t.Errorf("the container list was read (info %d, ps %d)", f.osCalls, f.psCalls)
	}
}

func TestPathHPrefillsTheHostAndEnterStaysGreyed(t *testing.T) {
	m, fe := routeResults(t, "example.com")
	m.pathModel.engine.supported = platformOff
	m = selectCheck(t, m, netcheck.CheckRoute)

	m = feed(t, m, testutil.Key(keymap.Diagnose))

	pm := m.pathModel
	if m.activeTab != tabPath || pm.input.Value() != "example.com" {
		t.Fatalf("tab=%d field=%q, want the Path tab prefilled", m.activeTab, pm.input.Value())
	}
	if !testutil.ShortcutDisabled(pm.shortcuts(), "enter") {
		t.Error("enter is not greyed after H")
	}
	if st := pm.statusLine(); st.Text != reasonPathMacOS || st.Level != components.LevelWarning {
		t.Errorf("status = {%q %v}, want Warning %q", st.Text, st.Level, reasonPathMacOS)
	}
	gen := pm.gen
	pm, _ = pmFeed(t, pm, testutil.Key("enter"))
	if pm.running() || pm.gen != gen || fe.traceCalls != 0 {
		t.Errorf("enter after H started a trace (running=%v gen=%d, was %d, traces %d)", pm.running(), pm.gen, gen, fe.traceCalls)
	}
}

// Rule 130: the platform changes the availability, never the key set.
func TestPathShortcutKeysDoNotDependOnThePlatform(t *testing.T) {
	ok, _ := newPM(t)
	off, _ := offFake(t)
	for _, state := range []pathState{pathEditing, pathDone} {
		ok.state, off.state = state, state
		if got, want := testutil.ShortcutKeys(off.shortcuts()), testutil.ShortcutKeys(ok.shortcuts()); !reflect.DeepEqual(got, want) {
			t.Errorf("state %v: keys %v unsupported, %v supported", state, got, want)
		}
	}
}
