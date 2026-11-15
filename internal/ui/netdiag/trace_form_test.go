package netdiag

import (
	"strings"
	"testing"

	"github.com/anthnel/devdesk/internal/nettrace"
	"github.com/anthnel/devdesk/internal/ui/components"
	"github.com/anthnel/devdesk/internal/ui/testutil"
)

// DEVDESK-470 step 2: a Trace Path button next to Run Diagnostics. Focus alone
// never launches anything; only enter on the button does, and enter on a text
// field is always Run Diagnostics.

func traceFormModel(t *testing.T, supported bool) (*Model, *fakeEngine) {
	t.Helper()
	m := newTestModel(t)
	fe := &fakeEngine{}
	e := fe.engine()
	if !supported {
		e.supported = func() error { return nettrace.ErrUnsupported }
	}
	m.pathModel.engine = e
	return m, fe
}

func TestTraceFormFieldIndices(t *testing.T) {
	if fieldRunButton != 3 || fieldTraceButton != 4 || fieldCount != 5 {
		t.Fatalf("fields run=%d trace=%d count=%d, want 3, 4, 5", fieldRunButton, fieldTraceButton, fieldCount)
	}
}

func TestTraceFormVerticalNavigationLoopsOverFivePositions(t *testing.T) {
	m, _ := traceFormModel(t, true)
	want := []int{fieldPort, fieldDNSServer, fieldRunButton, fieldTraceButton, fieldTarget}
	for i, w := range want {
		m = feed(t, m, testutil.Key("down"))
		if m.focusedField != w {
			t.Fatalf("down #%d: focus = %d, want %d", i+1, m.focusedField, w)
		}
	}
	m = feed(t, m, testutil.Key("up"))
	if m.focusedField != fieldTraceButton {
		t.Fatalf("up from the target = %d, want the Trace Path button", m.focusedField)
	}
	m = feed(t, m, testutil.Key("up"))
	if m.focusedField != fieldRunButton {
		t.Fatalf("up from Trace Path = %d, want the Run Diagnostics button", m.focusedField)
	}
}

func TestTraceFormEnterOnRunDiagnosticsRunsTheChecksOnly(t *testing.T) {
	m, fe := traceFormModel(t, true)
	m = typeInto(t, m, "example.com")
	m.focusedField = fieldRunButton
	gen := m.pathModel.gen
	m = feed(t, m, testutil.Key("enter"))

	if m.state != StateRunning {
		t.Fatalf("state = %v, want StateRunning", m.state)
	}
	if m.pathModel.gen != gen {
		t.Errorf("pathModel.gen %d -> %d, Run Diagnostics must not start a trace", gen, m.pathModel.gen)
	}
	if fe.traceCalls != 0 {
		t.Errorf("traceCalls = %d, want 0", fe.traceCalls)
	}
}

func TestTraceFormFocusOnTracePathLaunchesNothing(t *testing.T) {
	m, fe := traceFormModel(t, true)
	m = typeInto(t, m, "example.com")
	gen := m.pathModel.gen
	for m.focusedField != fieldTraceButton {
		m = feed(t, m, testutil.Key("down"))
	}
	if m.state != StateInput || m.pathModel.gen != gen || fe.traceCalls != 0 || fe.resolveCalls != 0 {
		t.Errorf("state=%v gen %d->%d trace=%d resolve=%d, focus alone must launch nothing",
			m.state, gen, m.pathModel.gen, fe.traceCalls, fe.resolveCalls)
	}
}

func TestTraceFormEnterOnATextFieldAlwaysRunsDiagnostics(t *testing.T) {
	for _, field := range []int{fieldTarget, fieldPort, fieldDNSServer} {
		m, fe := traceFormModel(t, true)
		m = typeInto(t, m, "example.com")
		m.focusedField = field
		gen := m.pathModel.gen
		m = feed(t, m, testutil.Key("enter"))
		if m.state != StateRunning || m.pathModel.gen != gen || fe.traceCalls != 0 {
			t.Errorf("field %d: state=%v gen %d->%d trace=%d, want Run Diagnostics only",
				field, m.state, gen, m.pathModel.gen, fe.traceCalls)
		}
	}
}

func TestTraceFormTraceableMirrorsThePathModel(t *testing.T) {
	for _, supported := range []bool{true, false} {
		m, _ := traceFormModel(t, supported)
		got, want := m.traceable(), m.pathModel.launchable()
		if got != want {
			t.Errorf("supported=%v: traceable() = %+v, want %+v", supported, got, want)
		}
		if got.Enabled() != supported {
			t.Errorf("supported=%v: Enabled() = %v", supported, got.Enabled())
		}
	}
}

func TestTraceFormUnsupportedPlatformRefusesWithTheReason(t *testing.T) {
	m, fe := traceFormModel(t, false)
	m = typeInto(t, m, "example.com")
	m.focusedField = fieldTraceButton
	gen := m.pathModel.gen
	m = feed(t, m, testutil.Key("enter"))

	if m.state != StateInput {
		t.Errorf("state = %v, want StateInput", m.state)
	}
	if m.footer.Level() != components.LevelWarning || m.footer.Text() != reasonPathUnsupported {
		t.Errorf("footer = {%v %q}, want Warn %q", m.footer.Level(), m.footer.Text(), reasonPathUnsupported)
	}
	if m.pathModel.gen != gen || fe.traceCalls != 0 || fe.resolveCalls != 0 {
		t.Errorf("gen %d->%d trace=%d resolve=%d, nothing may be emitted", gen, m.pathModel.gen, fe.traceCalls, fe.resolveCalls)
	}

	// Run Diagnostics still starts on the same platform.
	m.focusedField = fieldRunButton
	m = feed(t, m, testutil.Key("enter"))
	if m.state != StateRunning {
		t.Errorf("Run Diagnostics state = %v, want StateRunning", m.state)
	}
}

// Rule 130: the platform changes the label and the greying, never the key set.
func TestTraceFormHeaderKeysDoNotDependOnThePlatform(t *testing.T) {
	ok, _ := traceFormModel(t, true)
	no, _ := traceFormModel(t, false)
	for _, field := range []int{fieldTarget, fieldRunButton, fieldTraceButton} {
		ok.focusedField, no.focusedField = field, field
		got := testutil.ShortcutKeys(no.GetShortcuts())
		want := testutil.ShortcutKeys(ok.GetShortcuts())
		if strings.Join(got, ",") != strings.Join(want, ",") {
			t.Errorf("field %d: keys %v unsupported, %v supported", field, got, want)
		}
	}
}

func TestTraceFormSupportedEnterOnTracePathIsNotRunDiagnostics(t *testing.T) {
	m, _ := traceFormModel(t, true)
	m = typeInto(t, m, "example.com")
	m.focusedField = fieldTraceButton
	gen := m.runGen
	m = feed(t, m, testutil.Key("enter"))
	if m.state == StateRunning || m.runGen != gen {
		t.Errorf("state=%v runGen %d->%d, Trace Path must not start the checks", m.state, gen, m.runGen)
	}
}

func TestTraceFormRendersBothButtonsOnOneLine(t *testing.T) {
	m, _ := traceFormModel(t, true)
	found := false
	for _, line := range strings.Split(m.Render(), "\n") {
		if strings.Contains(line, "Run Diagnostics") && strings.Contains(line, "Trace Path") {
			found = true
		}
	}
	if !found {
		t.Error("no line of the form shows both Run Diagnostics and Trace Path")
	}
}
