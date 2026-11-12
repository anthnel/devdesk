package security

import (
	"go/ast"
	"go/parser"
	"go/token"
	"os"
	"path/filepath"
	"reflect"
	"strings"
	"testing"
	"time"

	tea "charm.land/bubbletea/v2"
	"github.com/charmbracelet/x/ansi"

	"github.com/anthnel/devdesk/internal/jobs"
	"github.com/anthnel/devdesk/internal/ui/testutil"
)

// DEVDESK-372: the :sec inventory load and the remediation reads use the shared
// activity dot (Status.Activity, WantsBlink, jobs.FrameMsg) instead of a
// spinner.Model of the view's own. Timers are never run.

// wantsBlink reads the router's BlinkingView contract off the model: a model
// without the method asks for nothing, which is what these tests must catch.
func wantsBlink(t *testing.T, m Model) bool {
	t.Helper()
	b, ok := any(m).(interface{ WantsBlink() bool })
	if !ok {
		t.Fatal("security.Model has no WantsBlink() bool: the router never arms the blink chain for it")
	}
	return b.WantsBlink()
}

// loadingModel is a fresh view: the inventory is still being read.
func loadingModel(t *testing.T) Model {
	t.Helper()
	m := feed(t, New(testConfig(), nil), tea.WindowSizeMsg{Width: 160, Height: 30})
	if !m.inventoryLoading {
		t.Fatal("the fixture is meant to be loading")
	}
	return m
}

func dots(s string) int { return strings.Count(s, "●") + strings.Count(s, "○") }

// ── Inventory ────────────────────────────────────────────────────────────────

func TestTheInventoryLoadIsTheActivityDot(t *testing.T) {
	m := loadingModel(t)
	st := m.inventoryStatus()
	if !st.Activity {
		t.Fatalf("status Activity=%v, want Activity only", st.Activity)
	}
	if st.Text != "Loading scan inventory..." {
		t.Errorf("text = %q", st.Text)
	}
	if !wantsBlink(t, m) {
		t.Error("a loading inventory does not ask for the blink")
	}
	m = feed(t, m, jobs.FrameMsg{Blink: true})
	if got := ansi.Strip(infoLine(m, 200)); dots(got) != 1 || !strings.Contains(got, "●") || hasBraille(got) {
		t.Errorf("want exactly one ● and no braille, got %q", got)
	}
	m = feed(t, m, jobs.FrameMsg{Blink: false})
	if got := ansi.Strip(infoLine(m, 200)); dots(got) != 1 || !strings.Contains(got, "○") {
		t.Errorf("want exactly one ○ in the off phase, got %q", got)
	}
}

func TestTheDotEndsWithTheInventoryLoad(t *testing.T) {
	m := feed(t, loadingModel(t), InventoryLoadedMsg{})
	if m.inventoryLoading || m.status().Activity || wantsBlink(t, m) {
		t.Errorf("loaded: loading=%v activity=%v wantsBlink=%v", m.inventoryLoading, m.status().Activity, wantsBlink(t, m))
	}
	if got := infoLine(m, 200); dots(ansi.Strip(got)) != 0 {
		t.Errorf("a dot after the load: %q", ansi.Strip(got))
	}
}

// A load and a scan together are one dot, and the load text wins over the jobs line.
func TestALoadAndAScanShowOneDotAndTheLoadText(t *testing.T) {
	m := footerModel(t, []jobs.Run{batch()}, nil)
	m = selectTarget(t, m, ftOther) // an idle row: no glyph of its own
	m, _ = step(t, m, testutil.Key("ctrl+r"))
	if !m.inventoryLoading {
		t.Fatal("ctrl+r did not start a load")
	}
	m = feed(t, m, jobs.FrameMsg{Blink: true})
	got := ansi.Strip(infoLine(m, 200))
	if n := dots(got); n != 1 {
		t.Errorf("%d dots, want exactly one: %q", n, got)
	}
	if !strings.Contains(got, "Loading scan inventory...") || strings.Contains(got, "Scanning —") {
		t.Errorf("the load text must replace the jobs line: %q", got)
	}
	if st := m.inventoryStatus(); !st.Activity {
		t.Errorf("status Activity=%v", st.Activity)
	}
}

// Same texts as before: a scan alone is still the progress line with the dot.
func TestInventoryStatusPrecedenceKeepsItsTexts(t *testing.T) {
	scanOnly := footerModel(t, []jobs.Run{batch()}, nil)
	scanOnly.inventoryLoading = false
	scanOnly = selectTarget(t, scanOnly, ftOther)
	if st := scanOnly.inventoryStatus(); !st.Activity || !strings.Contains(st.Text, "1/3") {
		t.Errorf("scan alone: %+v", st)
	}
	if !wantsBlink(t, scanOnly) {
		t.Error("a scan with the cursor on an idle row does not ask for the blink")
	}
	idle := footerModel(t, nil, nil)
	idle.inventoryLoading = false
	if st := idle.inventoryStatus(); st.Activity || st.Text != "" || wantsBlink(t, idle) {
		t.Errorf("nothing running: %+v wantsBlink=%v", st, wantsBlink(t, idle))
	}
}

// The failed row's glyph is fixed: during a ctrl+r its detail stays, and the
// footer carries no moving dot beside it.
func TestAFailedRowDuringAReloadKeepsItsFixedGlyph(t *testing.T) {
	f := secFailure(jobs.KindScan, secCtx, ftRepo, time.Date(2026, 11, 1, 10, 0, 0, 0, time.UTC))
	m := selectTarget(t, footerModel(t, nil, []jobs.Failure{f}), ftRepo)
	m, _ = step(t, m, testutil.Key("ctrl+r"))
	if wantsBlink(t, m) {
		t.Error("WantsBlink is true while the footer draws no dot (failed row's fixed glyph)")
	}
	for _, blink := range []bool{true, false} {
		got := ansi.Strip(infoLine(feed(t, m, jobs.FrameMsg{Blink: blink}), 200))
		for _, w := range []string{"Loading scan inventory...", "last scan failed", ftRepo, "⊗"} {
			if !strings.Contains(got, w) {
				t.Errorf("blink=%v: footer lacks %q: %q", blink, w, got)
			}
		}
		if dots(got) != 0 {
			t.Errorf("blink=%v: a moving dot beside the failed row's glyph: %q", blink, got)
		}
	}
}

// ── Remediation ──────────────────────────────────────────────────────────────

func TestReadingTheDockerfilesIsTheActivityDot(t *testing.T) {
	m := scannedModel(t)
	for range TabRemediation {
		m = feed(t, m, testutil.Key("tab"))
	}
	if m.remediation.phase != phaseLoading {
		t.Fatalf("phase = %d, want loading", m.remediation.phase)
	}
	st := m.status()
	if !st.Activity || st.Text != "Reading Dockerfiles and listing tags..." {
		t.Errorf("status = %+v", st)
	}
	if !wantsBlink(t, m) {
		t.Error("reading the Dockerfiles does not ask for the blink")
	}
	m = feed(t, m, jobs.FrameMsg{Blink: true})
	if got := ansi.Strip(m.RenderFooter(200)); dots(got) != 1 || hasBraille(got) {
		t.Errorf("want one dot and no braille: %q", got)
	}
}

func TestScanningTheBaseImagesIsTheActivityDotUntilTheyAnswer(t *testing.T) {
	m := onRemediation(t, baseEntries(), nil)
	if st := m.status(); st.Activity || wantsBlink(t, m) {
		t.Fatalf("before the scan: %+v", st)
	}
	m, _ = step(t, m, testutil.Key("S"))
	st := m.status()
	if !st.Activity || !strings.Contains(st.Text, "4 image") {
		t.Fatalf("status = %+v", st)
	}
	if !wantsBlink(t, m) {
		t.Error("scanning images does not ask for the blink")
	}
	m.remediation.scanning = map[string]bool{}
	if m.status().Activity || wantsBlink(t, m) {
		t.Error("the dot outlives the last image scan")
	}
}

func TestTheDockerfilesReadEndsTheDot(t *testing.T) {
	m := onRemediation(t, baseEntries(), nil)
	if m.status().Activity || wantsBlink(t, m) {
		t.Errorf("the read answered: status=%+v", m.status())
	}
}

func TestAScanOnAnotherTabDoesNotAskForTheBlink(t *testing.T) {
	m := scannedModel(t)
	if m.activeTab != TabCVE {
		t.Fatalf("activeTab = %d, want the CVE tab", m.activeTab)
	}
	m.remediation.scanning = map[string]bool{"img:1": true}
	if !m.remediationBusy() {
		t.Fatal("fixture: remediation should be busy")
	}
	if wantsBlink(t, m) {
		t.Error("an image scan of the remediation tab blinks a footer on the CVE tab")
	}
}

// ── No spinner of its own ────────────────────────────────────────────────────

func TestTheModelHoldsNoSpinner(t *testing.T) {
	if _, ok := reflect.TypeOf(Model{}).FieldByName("spinner"); ok {
		t.Error("Model still carries a spinner.Model")
	}
}

func TestNoLoadCmdCarriesASpinnerTick(t *testing.T) {
	m := footerModel(t, nil, nil) // loaded: the reload starts a load of its own
	m.inventoryLoading = false
	_, cmd := m.reloadInventory()
	if cmd == nil {
		t.Fatal("reloadInventory returned no Cmd")
	}
	if _, batch := cmd().(tea.BatchMsg); batch {
		t.Error("reloadInventory batches its load with something else (a spinner tick)")
	}

	r := scannedModel(t)
	r.inventoryLoading = false // the load would keep a spinner chain alive on its own
	if r.remediation.phase != phaseIdle {
		t.Fatalf("phase = %d, want idle before the tab is entered", r.remediation.phase)
	}
	rc := r.enterRemediation()
	if rc == nil {
		t.Fatal("enterRemediation returned no Cmd")
	}
	if _, batch := rc().(tea.BatchMsg); batch {
		t.Error("enterRemediation batches its read with something else (a spinner tick)")
	}
}

// The package keeps none of the spinner's pieces: the import, its chain
// helpers, its tick case.
func TestThePackageHasNoSpinnerChain(t *testing.T) {
	files, err := filepath.Glob("*.go")
	if err != nil || len(files) == 0 {
		t.Fatalf("no source read (%v)", err)
	}
	banned := map[string]bool{"spinnerTickIfIdle": true, "spinnerAlive": true, "handleSpinnerTick": true, "SetSpinnerFrame": true}
	for _, f := range files {
		if strings.HasSuffix(f, "_test.go") {
			continue
		}
		src, err := os.ReadFile(f)
		if err != nil {
			t.Fatal(err)
		}
		file, err := parser.ParseFile(token.NewFileSet(), f, src, 0)
		if err != nil {
			t.Fatal(err)
		}
		for _, imp := range file.Imports {
			if strings.Contains(imp.Path.Value, "bubbles/v2/spinner") {
				t.Errorf("%s imports the bubbles spinner", f)
			}
		}
		ast.Inspect(file, func(n ast.Node) bool {
			if id, ok := n.(*ast.Ident); ok && banned[id.Name] {
				t.Errorf("%s still uses %s", f, id.Name)
			}
			return true
		})
	}
}
