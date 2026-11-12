package status

import (
	"go/parser"
	"go/token"
	"os"
	"path/filepath"
	"strconv"
	"strings"
	"testing"
	"time"

	"github.com/charmbracelet/x/ansi"

	"github.com/anthnel/devdesk/internal/jobs"
	"github.com/anthnel/devdesk/internal/ui/indicator"
	"github.com/anthnel/devdesk/internal/ui/testutil"
)

// DEVDESK-369 T1: the check is reported by the shared activity dot, driven by
// the router's blink phase, not by a spinner of this view's own.

var checkingActivity = struct {
	Text     string
	Activity bool
}{"Checking components...", true}

// wantsBlink asks the view whether it wants the router's blink chain. Through an
// interface assertion, so the suite compiles (and fails on the assertion) until
// the view implements it.
func wantsBlink(m Model) bool {
	b, ok := any(m).(interface{ WantsBlink() bool })
	return ok && b.WantsBlink()
}

func wantChecking(t *testing.T, m Model, what string) {
	t.Helper()
	st := m.status()
	if st.Text != checkingActivity.Text || !st.Activity {
		t.Errorf("%s: status() = %+v, want Text %q and Activity", what, st, checkingActivity.Text)
	}
}

func TestStatusViewImplementsWantsBlink(t *testing.T) {
	if _, ok := any(Model{}).(interface{ WantsBlink() bool }); !ok {
		t.Error("status.Model has no WantsBlink() bool, so the router never arms the blink chain for it")
	}
}

func TestStatusActivityFirstCheckShowsTheDot(t *testing.T) {
	m := newTestModel(t, monitorConfigs()...)
	wantChecking(t, m, "a new model, before the first check answers")
	if !wantsBlink(m) {
		t.Error("WantsBlink() = false while the first check runs")
	}
}

func TestStatusActivityManualRefreshShowsTheDot(t *testing.T) {
	m := loadedModel(t)
	if m.status().Activity || wantsBlink(m) {
		t.Fatalf("fixture error: a loaded model reports activity: %+v", m.status())
	}
	m, _ = step(t, m, testutil.Key("ctrl+r"))
	wantChecking(t, m, "ctrl+r")
	if !wantsBlink(m) {
		t.Error("WantsBlink() = false after ctrl+r")
	}
}

func TestStatusActivityConfigReloadShowsTheDot(t *testing.T) {
	home := t.TempDir()
	t.Setenv("HOME", home)
	t.Setenv("USERPROFILE", home)
	m := loadedModel(t)
	m, _ = step(t, m, ComponentSavedMsg{Success: true})
	wantChecking(t, m, "a config reload")
}

func TestStatusActivityAutoRefreshShowsNoDot(t *testing.T) {
	m := loadedModel(t)
	m.autoRefresh = true
	m.nextCheck = time.Now().Add(-time.Minute)
	m, _ = step(t, m, TickMsg{})
	if !m.checking {
		t.Fatal("fixture error: the due tick started no check")
	}
	if st := m.status(); st.Activity || wantsBlink(m) {
		t.Errorf("an auto-refresh shows the dot: status() = %+v", st)
	}
}

func TestStatusActivityEndsWithTheCheck(t *testing.T) {
	m := newTestModel(t, monitorConfigs()...)
	m, _ = step(t, m, CheckCompleteMsg{Components: checkResults(), Timestamp: time.Now()})
	if st := m.status(); st.Text != "" || st.Activity {
		t.Errorf("status() = %+v after CheckCompleteMsg, want empty", st)
	}
	if wantsBlink(m) {
		t.Error("WantsBlink() = true after the check ended")
	}
}

func TestStatusFooterFollowsTheBlinkPhase(t *testing.T) {
	m := newTestModel(t, monitorConfigs()...)
	on, off := indicator.Glyph(indicator.Running, true), indicator.Glyph(indicator.Running, false)

	m, _ = step(t, m, jobs.FrameMsg{Blink: false})
	f := ansi.Strip(m.RenderFooter(120))
	if strings.Count(f, off) != 1 || strings.Contains(f, on) {
		t.Errorf("Blink:false footer = %q, want exactly one %s", f, off)
	}
	m, _ = step(t, m, jobs.FrameMsg{Blink: true})
	f = ansi.Strip(m.RenderFooter(120))
	if strings.Count(f, on) != 1 || strings.Contains(f, off) {
		t.Errorf("Blink:true footer = %q, want exactly one %s", f, on)
	}
}

// No spinner of the view's own: the package imports neither bubbles/spinner nor
// uses a spinner.Model.
func TestStatusViewHasNoSpinner(t *testing.T) {
	files, err := filepath.Glob("*.go")
	if err != nil {
		t.Fatal(err)
	}
	for _, name := range files {
		if strings.HasSuffix(name, "_test.go") {
			continue
		}
		src, err := os.ReadFile(name)
		if err != nil {
			t.Fatal(err)
		}
		file, err := parser.ParseFile(token.NewFileSet(), name, src, parser.ImportsOnly)
		if err != nil {
			t.Fatal(err)
		}
		for _, imp := range file.Imports {
			if p, _ := strconv.Unquote(imp.Path.Value); strings.HasSuffix(p, "/spinner") {
				t.Errorf("%s imports %s: report the check with the activity dot", name, p)
			}
		}
		if strings.Contains(string(src), "spinner.Model") {
			t.Errorf("%s uses spinner.Model", name)
		}
	}
}
