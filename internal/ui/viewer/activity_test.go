package viewer

import (
	"go/parser"
	"go/token"
	"os"
	"path/filepath"
	"strconv"
	"strings"
	"testing"

	"github.com/charmbracelet/x/ansi"

	"github.com/anthnel/devdesk/internal/config"
	"github.com/anthnel/devdesk/internal/jobs"
	"github.com/anthnel/devdesk/internal/ui/indicator"
	"github.com/anthnel/devdesk/internal/ui/keymap"
	"github.com/anthnel/devdesk/internal/ui/testutil"
	viewerpkg "github.com/anthnel/devdesk/internal/viewer"
)

// DEVDESK-370: a load is reported by the shared activity dot, driven by the
// router's blink phase, not by a spinner of this view's own.

// wantsBlink asks through an interface assertion so the suite compiles (and
// fails on the assertion) until the view implements it.
func wantsBlink(m Model) bool {
	b, ok := any(m).(interface{ WantsBlink() bool })
	return ok && b.WantsBlink()
}

func wantLoading(t *testing.T, m Model, name, what string) {
	t.Helper()
	st := m.status()
	if want := "Loading " + name + "..."; st.Text != want || !st.Activity {
		t.Errorf("%s: status() = %+v, want Text %q, Activity and no Spinner", what, st, want)
	}
	if !wantsBlink(m) {
		t.Errorf("%s: WantsBlink() = false while loading", what)
	}
}

func TestViewerImplementsWantsBlink(t *testing.T) {
	if _, ok := any(Model{}).(interface{ WantsBlink() bool }); !ok {
		t.Error("viewer.Model has no WantsBlink() bool, so the router never arms the blink chain for it")
	}
}

func TestViewerActivityWhileLoadingThenEnds(t *testing.T) {
	m := NewWithSource(config.Default(), fakeSource{name: "notes.txt", content: "hello\n"})
	wantLoading(t, m, "notes.txt", "a new model")

	m, _ = step(t, m, DocumentLoadedMsg{Name: "notes.txt", Content: []byte("hello\n")})
	if st := m.status(); st.Text != "" || st.Activity {
		t.Errorf("status() = %+v after DocumentLoadedMsg, want empty", st)
	}
	if wantsBlink(m) {
		t.Error("WantsBlink() = true after the document loaded")
	}
}

func TestViewerActivityEndsOnLoadError(t *testing.T) {
	m := NewWithSource(config.Default(), fakeSource{name: "big.txt"})
	m, _ = step(t, m, DocumentLoadedMsg{Name: "big.txt", Err: viewerpkg.ErrTooLarge})
	if m.status().Activity || wantsBlink(m) {
		t.Errorf("a failed load keeps the dot: %+v", m.status())
	}
}

func TestViewerActivityReloadShowsTheDot(t *testing.T) {
	m := logModel(t, "a\nb\n")
	if m.status().Activity || wantsBlink(m) {
		t.Fatalf("fixture error: a loaded model reports activity: %+v", m.status())
	}
	m, _ = step(t, m, testutil.Key("ctrl+r"))
	wantLoading(t, m, "logs · api", "ctrl+r")
}

// Regression: the toggle used to start a load with no spinner chain behind it.
func TestViewerActivityTimestampToggleShowsTheDot(t *testing.T) {
	m := logModel(t, "a\nb\n")
	m, _ = step(t, m, testutil.Key("t"))
	wantLoading(t, m, "logs · api", "t")
}

// Regression: the pager's return reloads the document with no chain behind it.
func TestViewerActivityPagerReturnShowsTheDot(t *testing.T) {
	m := logModel(t, "a\nb\n")
	m, _ = step(t, m, PagerExitMsg{})
	wantLoading(t, m, "logs · api", "PagerExitMsg")
}

func TestViewerActivityPagerErrorStillReloads(t *testing.T) {
	m := logModel(t, "a\nb\n")
	m, _ = step(t, m, PagerExitMsg{Err: os.ErrNotExist})
	wantLoading(t, m, "logs · api", "PagerExitMsg with an error")
	m, _ = step(t, m, DocumentLoadedMsg{Name: "logs · api", Content: []byte("a\n")})
	if wantsBlink(m) {
		t.Error("WantsBlink() = true after the reload came back")
	}
}

func TestViewerFollowShowsNoDot(t *testing.T) {
	m := logModel(t, "a\nb\n")
	m, _ = step(t, m, testutil.Key(keymap.Fetch))
	if st := m.status(); st.Activity {
		t.Errorf("following shows an indicator: %+v", st)
	}
	if wantsBlink(m) {
		t.Error("WantsBlink() = true while following")
	}
}

func TestViewerInitLoadsOnlyTheDocument(t *testing.T) {
	m := NewWithSource(config.Default(), fakeSource{name: "notes.txt", content: "hello\n"})
	msgs := resolve(t, m.Init())
	if len(msgs) != 1 {
		t.Fatalf("Init yielded %d messages, want exactly one DocumentLoadedMsg: %#v", len(msgs), msgs)
	}
	if _, ok := msgs[0].(DocumentLoadedMsg); !ok {
		t.Errorf("Init yielded %T, want DocumentLoadedMsg", msgs[0])
	}
}

func TestViewerFooterFollowsTheBlinkPhase(t *testing.T) {
	m := NewWithSource(config.Default(), fakeSource{name: "notes.txt"})
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
	for _, r := range f {
		if r >= 0x2800 && r <= 0x28FF {
			t.Errorf("footer holds a braille rune %q: %q", r, f)
		}
	}
}

// No spinner of the view's own: neither bubbles/spinner nor a spinner.Model.
func TestViewerHasNoSpinner(t *testing.T) {
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
				t.Errorf("%s imports %s: report the load with the activity dot", name, p)
			}
		}
		if strings.Contains(string(src), "spinner.Model") {
			t.Errorf("%s uses spinner.Model", name)
		}
	}
}
