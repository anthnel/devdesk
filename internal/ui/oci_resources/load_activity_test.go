package ociresources

import (
	"errors"
	"go/parser"
	"go/token"
	"os"
	"path/filepath"
	"reflect"
	"strings"
	"testing"

	"github.com/charmbracelet/x/ansi"

	"github.com/anthnel/devdesk/internal/jobs"
	"github.com/anthnel/devdesk/internal/ui/testutil"
)

// DEVDESK-374: the view's own loads (the three tabs, the tag search, the
// network inspection) show the shared activity dot, driven by WantsBlink.
// ctrl+r on a table that already has rows is silent (decision Q1).

// loadState is one situation of the view and what its footer must say about it.
type loadState struct {
	name  string
	model func(*testing.T) Model
	text  string // the load label, "" when nothing is loading
}

func searching(t *testing.T) Model {
	t.Helper()
	m := resultsModel(t, jobs.FrameMsg{Blink: true})
	m.registryBrowser.pendingSearches = 1
	return m
}

func inspecting(t *testing.T) Model {
	t.Helper()
	m := feed(t, loadedModel(t), testutil.Key("tab"))
	m = feed(t, m, testutil.Key("enter"))
	if m.networkInspectForm == nil {
		t.Fatal("enter on a network did not open the inspection")
	}
	return m
}

func tabLoading(tabs int) func(*testing.T) Model {
	return func(t *testing.T) Model {
		t.Helper()
		m := newTestModel(t)
		for range tabs {
			m = feed(t, m, testutil.Key("tab"))
		}
		return m
	}
}

func ctrlROn(tabs int) func(*testing.T) Model {
	return func(t *testing.T) Model {
		t.Helper()
		m := loadedModel(t)
		for range tabs {
			m = feed(t, m, testutil.Key("tab"))
		}
		return feed(t, m, testutil.Key("ctrl+r"))
	}
}

func loadStates() []loadState {
	return []loadState{
		{"images tab", tabLoading(0), "Loading images..."},
		{"networks tab", tabLoading(1), "Loading networks..."},
		{"volumes tab", tabLoading(2), "Loading volumes..."},
		{"tag search", searching, "Searching registries..."},
		{"network inspection", inspecting, "Loading containers..."},
	}
}

func currentStatus(m Model) (text string, activity, spinner bool) {
	st := m.status()
	if m.registryBrowser != nil || m.networkInspectForm != nil {
		st = m.formStatus()
	}
	return st.Text, st.Activity, st.Spinner
}

func TestEveryOwnLoadShowsTheDotWithTheSameText(t *testing.T) {
	for _, s := range loadStates() {
		t.Run(s.name, func(t *testing.T) {
			m := s.model(t)
			text, activity, spin := currentStatus(m)
			if text != s.text || !activity || spin {
				t.Errorf("status = {%q Activity=%v Spinner=%v}, want {%q Activity=true Spinner=false}", text, activity, spin, s.text)
			}
			if !m.WantsBlink() {
				t.Error("a loading view does not ask for the blink (the dot would stay fixed)")
			}
		})
	}
}

func TestEveryOwnLoadFollowsTheBlinkPhase(t *testing.T) {
	for _, s := range loadStates() {
		t.Run(s.name, func(t *testing.T) {
			for _, p := range []struct {
				blink      bool
				want, deny string
			}{{false, "○", "●"}, {true, "●", "○"}} {
				m := feed(t, s.model(t), jobs.FrameMsg{Blink: p.blink})
				got := plainOciInfoLine(m, 200)
				if !strings.Contains(got, s.text) || strings.Count(got, p.want) != 1 || strings.Contains(got, p.deny) || hasBraille(got) {
					t.Errorf("blink=%v: want the text, one %s, no %s, no braille: %q", p.blink, p.want, p.deny, got)
				}
			}
		})
	}
}

// Q1: a refresh with rows on screen is silent, on every tab.
func TestCtrlROnAFilledTableIsSilent(t *testing.T) {
	for i, name := range []string{"images", "networks", "volumes"} {
		t.Run(name, func(t *testing.T) {
			m := feed(t, ctrlROn(i)(t), jobs.FrameMsg{Blink: true})
			st := m.status()
			if st.Activity || strings.HasPrefix(st.Text, "Loading") {
				t.Errorf("status = %+v, want no dot and no load text", st)
			}
			if m.WantsBlink() {
				t.Error("a silent refresh asks for the blink")
			}
			if got := plainOciInfoLine(m, 200); strings.ContainsAny(got, "●○") || strings.Contains(got, "Loading") {
				t.Errorf("the footer says something during ctrl+r: %q", got)
			}
		})
	}
}

// Only the tab on screen counts.
func TestOnlyTheTabOnScreenAsksForTheBlink(t *testing.T) {
	m := feed(t, newTestModel(t), ImagesListMsg{Images: imageFixtures()}) // networks still loading
	if m.WantsBlink() {
		t.Error("the Images tab is loaded and the Networks load still blinks the dot")
	}
}

func TestEndOfALoadStopsTheBlink(t *testing.T) {
	ends := []struct {
		name  string
		model func(*testing.T) Model
		msgs  []interface{}
	}{
		{"images", tabLoading(0), []interface{}{ImagesListMsg{}, ImagesListMsg{Err: errors.New("down")}}},
		{"networks", tabLoading(1), []interface{}{NetworksListMsg{}, NetworksListMsg{Err: errors.New("down")}}},
		{"volumes", tabLoading(2), []interface{}{VolumesListMsg{}, VolumesListMsg{Err: errors.New("down")}}},
		{"tag search", searching, []interface{}{MultiRegistryTagsLoadedMsg{EntryKey: "prod", RegistryURL: "registry.example.com", Repo: "api", Tags: []string{"v1"}}}},
		{"inspection", inspecting, []interface{}{
			NetworkInspectLoadedMsg{NetworkID: "net11111", NetworkName: "bridge"},
			NetworkInspectLoadedMsg{NetworkID: "net11111", Err: errors.New("down")},
		}},
	}
	for _, e := range ends {
		for i, msg := range e.msgs {
			t.Run(e.name, func(t *testing.T) {
				m := e.model(t)
				if !m.WantsBlink() {
					t.Fatalf("fixture %d: not blinking before the end", i)
				}
				m = feed(t, m, msg)
				if m.WantsBlink() {
					t.Error("still asks for the blink after the load ended")
				}
				if got := plainOciInfoLine(m, 200); strings.ContainsAny(got, "●○") || strings.Contains(got, "Searching") || strings.Contains(got, "Loading") {
					t.Errorf("the footer still shows the load: %q", got)
				}
			})
		}
	}
}

// Regression of the frozen glyph: the inspection form's load used to be a
// spinner nobody ticked.
func TestNetworkInspectionLoadShowsTheDot(t *testing.T) {
	st := inspecting(t).formStatus()
	if st.Text != "Loading containers..." || !st.Activity || st.Spinner {
		t.Errorf("formStatus = %+v", st)
	}
}

// A load and a job together: one dot, not two.
func TestALoadAndAJobShowOneDot(t *testing.T) {
	run := ociRun(jobs.KindScan, ociCtx, jobs.ItemRunning, imgRef)
	m := feed(t, newTestModel(t), jobs.ChangedMsg{Runs: []jobs.Run{run}, Context: ociCtx}, jobs.FrameMsg{Blink: true})
	if got := plainOciInfoLine(m, 200); strings.Count(got, "●") != 1 || strings.Contains(got, "○") {
		t.Errorf("want exactly one ●, got %q", got)
	}
}

// A row detail carrying a glyph is the only mark: no dot beside it.
func TestARowDetailGlyphGetsNoExtraDot(t *testing.T) {
	m := ociSnapshot(t, ociCtx, []jobs.Run{ociRun(jobs.KindScan, ociCtx, jobs.ItemRunning, imgRef)}, nil)
	m.loading = false
	m = feed(t, selectImage(t, m, imgRef), jobs.FrameMsg{Blink: false})
	if got := plainOciInfoLine(m, 200); strings.Count(got, "○")+strings.Count(got, "●") != 1 {
		t.Errorf("want a single walking glyph, got %q", got)
	}
}

// Rule 139: the body is the table while it loads.
func TestTheBodyIsTheTableWhileLoading(t *testing.T) {
	m := newTestModel(t)
	body := ansi.Strip(m.Render())
	header := ""
	for _, line := range strings.Split(ansi.Strip(m.imageTable.View()), "\n") {
		if strings.TrimSpace(line) != "" {
			header = strings.TrimSpace(line)
			break
		}
	}
	if header == "" || !strings.Contains(body, header) {
		t.Errorf("the table header %q is missing from the loading body", header)
	}
	if strings.Contains(body, "Loading images") || hasBraille(body) {
		t.Error("the body carries the load")
	}
}

// The invariant that arms the router's chain: WantsBlink is true exactly when
// the footer carries the dot, with no job running.
func TestWantsBlinkIsTrueExactlyWhenTheFooterCarriesTheDot(t *testing.T) {
	type state struct {
		name  string
		model func(*testing.T) Model
		want  bool
	}
	states := []state{
		{"loaded", loadedModel, false},
		{"ctrl+r images", ctrlROn(0), false},
		{"ctrl+r networks", ctrlROn(1), false},
		{"ctrl+r volumes", ctrlROn(2), false},
		{"browser form", browsingModel, false},
		{"browser results", func(t *testing.T) Model { return resultsModel(t) }, false},
	}
	for _, s := range loadStates() {
		states = append(states, state{s.name, s.model, true})
	}
	for _, s := range states {
		t.Run(s.name, func(t *testing.T) {
			m := s.model(t)
			if m.jobs != nil {
				t.Fatal("fixture error: a job is running")
			}
			if m.WantsBlink() != s.want {
				t.Errorf("WantsBlink = %v, want %v", m.WantsBlink(), s.want)
			}
			if has := strings.ContainsAny(plainOciInfoLine(m, 200), "●○"); has != m.WantsBlink() {
				t.Errorf("footer carries the dot = %v but WantsBlink = %v: %q", has, m.WantsBlink(), plainOciInfoLine(m, 200))
			}
		})
	}
}

// The view holds no spinner of its own any more.
func TestOCIViewHasNoSpinner(t *testing.T) {
	files, err := filepath.Glob("*.go")
	if err != nil {
		t.Fatal(err)
	}
	for _, f := range files {
		if strings.HasSuffix(f, "_test.go") {
			continue
		}
		src, err := os.ReadFile(f)
		if err != nil {
			t.Fatal(err)
		}
		file, err := parser.ParseFile(token.NewFileSet(), f, src, parser.ImportsOnly)
		if err != nil {
			t.Fatal(err)
		}
		for _, imp := range file.Imports {
			if strings.HasSuffix(strings.Trim(imp.Path.Value, `"`), "/spinner") {
				t.Errorf("%s imports %s", f, imp.Path.Value)
			}
		}
		if strings.Contains(string(src), "spinner.Model") {
			t.Errorf("%s declares a spinner.Model", f)
		}
	}
	for _, c := range []struct {
		typ   reflect.Type
		field string
	}{
		{reflect.TypeOf(Model{}), "loadingRegs"},
		{reflect.TypeOf(Model{}), "spinner"},
		{reflect.TypeOf(RegistryBrowser{}), "spinner"},
	} {
		if _, ok := c.typ.FieldByName(c.field); ok {
			t.Errorf("%s still has the field %s", c.typ.Name(), c.field)
		}
	}
}
