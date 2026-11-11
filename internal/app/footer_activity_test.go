package app

import (
	"fmt"
	"go/ast"
	"go/parser"
	"go/token"
	"os"
	"path/filepath"
	"sort"
	"strings"
	"testing"
	"time"

	tea "charm.land/bubbletea/v2"
	"github.com/charmbracelet/x/ansi"

	"github.com/anthnel/devdesk/internal/command"
	"github.com/anthnel/devdesk/internal/docker"
	gitlabforge "github.com/anthnel/devdesk/internal/forge/gitlab"
	"github.com/anthnel/devdesk/internal/forgeindex"
	"github.com/anthnel/devdesk/internal/jobs"
	"github.com/anthnel/devdesk/internal/template"
	"github.com/anthnel/devdesk/internal/ui/components"
	"github.com/anthnel/devdesk/internal/ui/forge/explorer"
	"github.com/anthnel/devdesk/internal/ui/indicator"
	ociresources "github.com/anthnel/devdesk/internal/ui/oci_resources"
	"github.com/anthnel/devdesk/internal/ui/testutil"
	"github.com/anthnel/devdesk/internal/ui/workspaces"
)

// DEVDESK-341 step 3: the footer's activity dot, guarded and accepted at the
// router. The seeds below put a view on screen with jobs work running and the
// cursor on an IDLE row (or on none), so no row detail carries a glyph and the
// footer must carry its own dot.

// renderedFrameName is the router's styled braille frame, which no footer reads
// any more. It is retired under internal/ui.
const renderedFrameName = "RenderedFrame"

// footerSeed puts a view on screen, jobs work running and no selected row busy.
type footerSeed func(*testing.T, *App)

// footerActivitySeeds are the seven views whose footer shows the shared activity
// dot (the router-driven sites of DEVDESK-341). Every one is checked for braille
// and for exactly one running glyph.
var footerActivitySeeds = map[command.ViewType]footerSeed{
	command.ViewDashboard:    seedDashboardActivity,
	command.ViewJobs:         seedJobsActivity,
	command.ViewWorkspaces:   seedWorkspacesActivity,
	command.ViewTemplates:    seedTemplatesActivity,
	command.ViewSecurity:     seedSecurityActivity,
	command.ViewOCIResources: seedOCIActivity,
	command.ViewGitExplorer:  seedExplorerActivity,
}

// startRunning starts run, puts item in the running state and returns its id.
func startRunning(t *testing.T, a *App, run jobs.Run, item string) {
	t.Helper()
	a.Update(jobs.StartMsg{Run: run})
	snap := a.jobs.Snapshot()
	if len(snap) == 0 || !a.jobs.Advance(snap[len(snap)-1].ID, item, jobs.ItemRunning, "") {
		t.Fatalf("the seed could not put %q in the running state: %+v", item, snap)
	}
	a.jobsChanged() // broadcast; the returned Cmd carries timers and is dropped
}

func seedDashboardActivity(t *testing.T, a *App) {
	t.Helper()
	startRunning(t, a, scanRun("default", "a"), "a")
	delete(a.views, command.ViewDashboard) // routerAt parks a bare view on the start view
	_ = a.switchView(command.ViewDashboard)
	a.resize(a.width, a.height)
}

// seedJobsActivity filters :jobs down to nothing, so no run is selected.
func seedJobsActivity(t *testing.T, a *App) {
	t.Helper()
	startRunning(t, a, scanRun("default", "a"), "a")
	testutil.Msgs(a.switchView(command.ViewJobs))
	a.resize(a.width, a.height)
	for _, msg := range append(append([]tea.Msg{testutil.Key("/")}, testutil.Type("zzzz")...), testutil.Key("enter")) {
		a.Update(msg)
	}
}

func seedWorkspacesActivity(t *testing.T, a *App) {
	t.Helper()
	dir := t.TempDir()
	for _, name := range []string{"api", "web"} {
		if err := os.MkdirAll(filepath.Join(dir, name, ".git"), 0o755); err != nil {
			t.Fatal(err)
		}
	}
	a.config.App.WorkspacesDir = dir
	web := filepath.Join(dir, "web")
	startRunning(t, a, jobs.NewRun(jobs.KindSync, command.ViewWorkspaces, "", "ws", web), web)
	openWorkspaces(t, a) // the cursor lands on the first row, api, which is idle
}

func seedTemplatesActivity(t *testing.T, a *App) {
	t.Helper()
	path, err := template.DefaultPath() // under the isolated HOME
	if err != nil {
		t.Fatal(err)
	}
	store, err := template.Open(path)
	if err != nil {
		t.Fatal(err)
	}
	for _, slug := range []string{"api", "web"} {
		err = store.Put(template.Entry{
			Slug: slug, Name: strings.ToUpper(slug[:1]) + slug[1:],
			Source: template.Source{Kind: template.KindGit, URL: "https://git.example.com/acme/" + slug + ".git", Ref: "main"},
		})
		if err != nil {
			t.Fatal(err)
		}
	}
	startRunning(t, a, jobs.NewRun(jobs.KindSync, command.ViewTemplates, "", "Web", "web").WithDisplay("web", "Web"), "web")
	_ = a.switchView(command.ViewTemplates)
	for _, msg := range testutil.Msgs(a.views[command.ViewTemplates].Init()) {
		a.Update(msg)
	}
	a.resize(a.width, a.height)
}

// seedSecurityActivity scans the image row while the cursor sits on the repo row.
func seedSecurityActivity(t *testing.T, a *App) {
	t.Helper()
	stubImageNames(t, secImage)
	seedSecCaches(t)
	startRunning(t, a, jobs.NewRun(jobs.KindScan, command.ViewSecurity, "", "inventory", secImage), secImage)
	openSec(t, a)
}

// seedOCIActivity scans redis:7 while the cursor sits on nginx:1.27.
func seedOCIActivity(t *testing.T, a *App) {
	t.Helper()
	t.Setenv("PATH", t.TempDir()) // no docker: a reload command must stay inert
	startRunning(t, a, jobs.NewRun(jobs.KindScan, command.ViewOCIResources, "", "images", "redis:7"), "redis:7")
	_ = a.switchView(command.ViewOCIResources)
	a.Update(ociresources.ImagesListMsg{Images: []docker.Image{
		{ID: "aaa1111", Repository: "nginx", Tag: "1.27"},
		{ID: "bbb2222", Repository: "redis", Tag: "7"},
	}})
	a.resize(a.width, a.height)
}

// footerBrailleProblem is empty when footer holds no braille pattern character
// (U+2800 to U+28FF), the alphabet of the router's retired spinner frame.
func footerBrailleProblem(footer string) string {
	for _, r := range ansi.Strip(footer) {
		if r >= 0x2800 && r <= 0x28FF {
			return fmt.Sprintf("the footer holds the braille frame %q: a migrated footer shows the activity dot", r)
		}
	}
	return ""
}

// forceRouterFrame makes the router's frame as non-empty as it can be and hands
// it to the view on screen: a regression that still wrote the frame into a footer
// would show a braille character here.
func forceRouterFrame(a *App, vt command.ViewType) {
	a.anim.frameIdx = 3
	a.anim.blinkOn = true
	_ = a.sendFrameTo(vt)
}

func viewFooter(t *testing.T, a *App, vt command.ViewType) string {
	t.Helper()
	fr, ok := a.views[vt].(footerRenderer)
	if !ok {
		t.Fatalf("view %q is a %T, which renders no footer", vt, a.views[vt])
	}
	return fr.RenderFooter(160)
}

func TestActivityGuardFooterBrailleLogic(t *testing.T) {
	if got := footerBrailleProblem("  1 job running"); got != "" {
		t.Errorf("a clean footer: %s", got)
	}
	if got := footerBrailleProblem("  ⠹ 1 job running"); got == "" {
		t.Error("a footer with a braille frame passed")
	}
	if got := footerBrailleProblem("  \x1b[31m⣿\x1b[0m x"); got == "" {
		t.Error("a styled braille frame passed")
	}
	if got := footerBrailleProblem("  ● ○ ◴ ⊗ ◐ ◕ ◉ ⊘ ⊖ ◌ text"); got != "" {
		t.Errorf("the activity alphabet is not braille: %s", got)
	}
}

// DEVDESK-341: no braille in a migrated footer, and the shared dot is there once.
func TestActivityGuardFooterHasNoBraille(t *testing.T) {
	// Every key is a view the activity guard already classifies: a migrated
	// table, or an exempt view with its documented reason (the dashboard).
	for vt := range footerActivitySeeds {
		if len(migratedTables[vt]) == 0 && exemptViews[vt] == "" {
			t.Errorf("view %q has a footer-dot seed but is neither migrated (migratedTables) nor exempt with a reason (exemptViews)", vt)
		}
	}
	run := indicator.Glyph(indicator.Running, true)
	for vt, seed := range footerActivitySeeds {
		t.Run(string(vt), func(t *testing.T) {
			a := routerAt(t, &bareView{}, 180, 40)
			seed(t, a)
			if a.currentView != vt {
				t.Fatalf("the seed left %q on screen, want %q", a.currentView, vt)
			}
			forceRouterFrame(a, vt)
			footer := viewFooter(t, a, vt)
			if msg := footerBrailleProblem(footer); msg != "" {
				t.Errorf("view %q: %s\n%s", vt, msg, ansi.Strip(footer))
			}
			if n := strings.Count(ansi.Strip(footer), run); n != 1 {
				t.Errorf("view %q: the footer holds %d running glyphs, want exactly 1\n%s", vt, n, ansi.Strip(footer))
			}
		})
	}
	// The views that keep a spinner of their own are out of scope while they load:
	// the seeds below are loaded and selected on a busy row.
	keys := make([]string, 0, len(migratedFooterSeeds))
	for vt := range migratedFooterSeeds {
		keys = append(keys, string(vt))
	}
	sort.Strings(keys)
	for _, k := range keys {
		vt := command.ViewType(k)
		t.Run("row/"+k, func(t *testing.T) {
			a := routerAt(t, &bareView{}, 180, 40)
			migratedFooterSeeds[vt](t, a)
			forceRouterFrame(a, vt)
			if msg := footerBrailleProblem(viewFooter(t, a, vt)); msg != "" {
				t.Errorf("view %q: %s", vt, msg)
			}
		})
	}
}

// footerFrameConsumers lists every package under internal/ui whose Update takes
// a jobs.FrameMsg, and what it does with it for the footer: the view whose seed
// in footerActivitySeeds proves the dot, or the reason the package is exempt.
// A package that starts handling jobs.FrameMsg fails the test below until it is
// written down here (DEVDESK-341, plan section 3).
var footerFrameConsumers = map[string]struct {
	View   command.ViewType // set: the footer shows the dot, seeded in footerActivitySeeds
	Exempt string           // set: why the footer keeps its own spinner or has none
}{
	"ui/dashboard":      {View: command.ViewDashboard},
	"ui/jobsview":       {View: command.ViewJobs},
	"ui/workspaces":     {View: command.ViewWorkspaces},
	"ui/templates":      {View: command.ViewTemplates},
	"ui/security":       {View: command.ViewSecurity},
	"ui/oci_resources":  {View: command.ViewOCIResources},
	"ui/forge/explorer": {View: command.ViewGitExplorer},
	"ui/netdiag":        {Exempt: "own spinner.Model feeds its Spinner status; the frame only drives table blink (plan 1.2)"},
	"ui/containers":     {Exempt: "own spinner.Model feeds its Spinner status; the frame only drives table blink (plan 1.2)"},
	"ui/agentusageview": {Exempt: "own spinner.Model feeds its Spinner status; the frame only drives table blink (plan 1.2)"},
}

// footerFrameConsumerProblems compares the packages found handling a
// jobs.FrameMsg with the classification table.
func footerFrameConsumerProblems(found map[string]bool, table map[string]struct {
	View   command.ViewType
	Exempt string
}, seeds map[command.ViewType]footerSeed) []string {
	var out []string
	for dir := range found {
		c, ok := table[dir]
		switch {
		case !ok:
			out = append(out, fmt.Sprintf("%s handles jobs.FrameMsg but is in neither footerActivitySeeds nor an exemption (footerFrameConsumers)", dir))
		case c.View != "" && c.Exempt != "":
			out = append(out, fmt.Sprintf("%s is both seeded and exempt", dir))
		case c.View == "" && c.Exempt == "":
			out = append(out, fmt.Sprintf("%s is classified without a view or a reason", dir))
		case c.View != "" && seeds[c.View] == nil:
			out = append(out, fmt.Sprintf("%s names view %q, which has no footerActivitySeeds entry", dir, c.View))
		}
	}
	for dir := range table {
		if !found[dir] {
			out = append(out, fmt.Sprintf("footerFrameConsumers lists %s, which no longer handles jobs.FrameMsg: drop the entry", dir))
		}
	}
	for vt := range seeds {
		listed := false
		for _, c := range table {
			listed = listed || c.View == vt
		}
		if !listed {
			out = append(out, fmt.Sprintf("footerActivitySeeds has view %q, which footerFrameConsumers does not map to a consumer", vt))
		}
	}
	sort.Strings(out)
	return out
}

func TestActivityGuardFrameConsumersAreClassifiedLogic(t *testing.T) {
	type entry = struct {
		View   command.ViewType
		Exempt string
	}
	seeds := map[command.ViewType]footerSeed{command.ViewDashboard: seedDashboardActivity}
	good := map[string]entry{"ui/dashboard": {View: command.ViewDashboard}, "ui/x": {Exempt: "own spinner"}}
	if p := footerFrameConsumerProblems(map[string]bool{"ui/dashboard": true, "ui/x": true}, good, seeds); len(p) != 0 {
		t.Errorf("a classified set reported %v", p)
	}
	if p := footerFrameConsumerProblems(map[string]bool{"ui/dashboard": true, "ui/x": true, "ui/new": true}, good, seeds); len(p) != 1 {
		t.Errorf("an unclassified consumer: %v", p)
	}
	if p := footerFrameConsumerProblems(map[string]bool{"ui/dashboard": true}, good, seeds); len(p) != 1 {
		t.Errorf("a stale exemption: %v", p)
	}
	if p := footerFrameConsumerProblems(map[string]bool{"ui/dashboard": true, "ui/x": true}, map[string]entry{"ui/dashboard": {View: command.ViewDashboard}, "ui/x": {}}, seeds); len(p) != 1 {
		t.Errorf("a consumer with neither view nor reason: %v", p)
	}
	if p := footerFrameConsumerProblems(map[string]bool{"ui/dashboard": true, "ui/x": true}, good, map[command.ViewType]footerSeed{command.ViewDashboard: seedDashboardActivity, command.ViewJobs: seedJobsActivity}); len(p) != 1 {
		t.Errorf("a seed no consumer maps to: %v", p)
	}
}

func TestActivityGuardFrameConsumersAreClassified(t *testing.T) {
	found := map[string]bool{}
	sourceFiles(t, func(rel string, _ *token.FileSet, file *ast.File) {
		if !strings.HasPrefix(rel, "ui/") {
			return
		}
		ast.Inspect(file, func(n ast.Node) bool {
			cc, ok := n.(*ast.CaseClause)
			if !ok {
				return true
			}
			for _, e := range cc.List {
				if sel, ok := e.(*ast.SelectorExpr); ok && sel.Sel.Name == "FrameMsg" {
					if x, ok := sel.X.(*ast.Ident); ok && x.Name == "jobs" {
						found[filepath.ToSlash(filepath.Dir(rel))] = true
					}
				}
			}
			return true
		})
	})
	if len(found) == 0 {
		t.Fatal("no `case jobs.FrameMsg` found under internal/ui: the scan reads nothing")
	}
	for _, p := range footerFrameConsumerProblems(found, footerFrameConsumers, footerActivitySeeds) {
		t.Error(p)
	}
}

// renderedFrameUses reports every identifier named RenderedFrame in file: a
// selector, a composite key, a field or a declaration.
func renderedFrameUses(fset *token.FileSet, file *ast.File) []finding {
	var out []finding
	ast.Inspect(file, func(n ast.Node) bool {
		if id, ok := n.(*ast.Ident); ok && id.Name == renderedFrameName {
			out = append(out, finding{Pos: fset.Position(id.Pos()), What: "reference to " + renderedFrameName})
		}
		return true
	})
	return out
}

func TestActivityGuardRenderedFrameLogic(t *testing.T) {
	tests := []struct {
		name, body string
		want       int
	}{
		{"selector", "func f(m M) string { return m.RenderedFrame }", 1},
		{"composite key", "var x = T{RenderedFrame: \"x\"}", 1},
		{"field", "type T struct{ RenderedFrame string }", 1},
		{"another name", "func f(m M) { _ = m.RenderedFrameCount; _ = m.Frame }", 0},
		{"a string or a comment is not a use", "// RenderedFrame\nvar s = \"RenderedFrame\"", 0},
	}
	for _, tt := range tests {
		t.Run(tt.name, func(t *testing.T) {
			fset := token.NewFileSet()
			file, err := parser.ParseFile(fset, "v.go", "package v\n"+tt.body, parser.ParseComments)
			if err != nil {
				t.Fatal(err)
			}
			if got := renderedFrameUses(fset, file); len(got) != tt.want {
				t.Errorf("findings = %v, want %d", got, tt.want)
			}
		})
	}
}

// DEVDESK-341: nothing under internal/ui reads the router's styled frame. The
// two remaining mentions, jobs.FrameMsg.RenderedFrame (internal/jobs/message.go)
// and its producer (internal/app/animation.go), are outside internal/ui on
// purpose: step 5 of the plan retires the field, and this guard does not cover
// them.
func TestActivityGuardNoRenderedFrameUnderUI(t *testing.T) {
	scanned := 0
	sourceFiles(t, func(rel string, fset *token.FileSet, file *ast.File) {
		if !strings.HasPrefix(rel, "ui/") {
			return
		}
		scanned++
		for _, f := range renderedFrameUses(fset, file) {
			t.Errorf("%s: %s under internal/ui (DEVDESK-341: a footer shows the activity dot, not the router's frame)", f.Pos, f.What)
		}
	})
	if scanned == 0 {
		t.Fatal("no source file found under internal/ui: the scan reads nothing")
	}
}

// --- Router acceptance -----------------------------------------------------

// dotRouter is the dashboard on screen with one job running. The animation chain
// is stubbed at an instant of phase "on" so no real timer is slept on.
func dotRouter(t *testing.T) *App {
	t.Helper()
	stubAnimationEvery(t, time.UnixMilli(0))
	a := routerAt(t, &bareView{}, 180, 40)
	seedDashboardActivity(t, a)
	if !a.anim.ticking {
		t.Fatal("a running job started no animation chain")
	}
	return a
}

// footerDots counts the running glyph and its hollow phase in the stripped footer.
func footerDots(footer string) (filled, hollow int) {
	s := ansi.Strip(footer)
	return strings.Count(s, indicator.Glyph(indicator.Running, true)), strings.Count(s, indicator.Glyph(indicator.Running, false))
}

func wantDot(t *testing.T, a *App, vt command.ViewType, filled, hollow int) {
	t.Helper()
	f := viewFooter(t, a, vt)
	if gf, gh := footerDots(f); gf != filled || gh != hollow {
		t.Errorf("footer has %d filled and %d hollow dots, want %d and %d: %q", gf, gh, filled, hollow, ansi.Strip(f))
	}
}

func TestFooterActivityDotBlinksWithTheRouterPhase(t *testing.T) {
	a := dotRouter(t)
	tickAt(a, 0) // 0 ms: on
	wantDot(t, a, command.ViewDashboard, 1, 0)
	tickAt(a, 500) // 500 ms: off
	wantDot(t, a, command.ViewDashboard, 0, 1)
	tickAt(a, 1000)
	wantDot(t, a, command.ViewDashboard, 1, 0)
}

func TestFooterActivityDotIsSteadyUnderStaticMotion(t *testing.T) {
	a := dotRouter(t)
	a.setMotionStatic(true)
	for _, ms := range []int64{0, 500, 1000, 1500} {
		tickAt(a, ms)
		wantDot(t, a, command.ViewDashboard, 1, 0)
	}
}

func TestFooterActivityDotFreezesWhenTheTerminalLosesFocus(t *testing.T) {
	a := dotRouter(t)
	tickAt(a, 500) // off phase
	wantDot(t, a, command.ViewDashboard, 0, 1)

	a.Update(tea.BlurMsg{})
	// The tick that was in flight lands after the blur, on an off phase: the
	// router pins the phase on, hands the view that last frame, and renews nothing.
	cmd := tickAt(a, 500)
	wantDot(t, a, command.ViewDashboard, 1, 0)
	if a.anim.ticking {
		t.Error("the animation chain is still alive after a blur")
	}
	if cmd != nil {
		for _, m := range testutil.Msgs(cmd) {
			if _, again := m.(animationTickMsg); again {
				t.Error("a blur left a renewed animation tick")
			}
		}
	}
	// Late ticks change nothing: the dot stays put.
	tickAt(a, 1500)
	wantDot(t, a, command.ViewDashboard, 1, 0)
}

func TestFooterActivityDotGoesWhenTheJobEnds(t *testing.T) {
	a := dotRouter(t)
	tickAt(a, 0)
	wantDot(t, a, command.ViewDashboard, 1, 0)

	snap := a.jobs.Snapshot()
	if !a.jobs.Advance(snap[0].ID, "a", jobs.ItemDone, "") {
		t.Fatal("the run could not be settled")
	}
	a.jobsChanged()
	f := ansi.Strip(viewFooter(t, a, command.ViewDashboard))
	if gf, gh := footerDots(f); gf+gh != 0 || strings.Contains(f, "job running") {
		t.Errorf("a settled run leaves a dot or its text: %q", f)
	}
}

// A footer error wins over the activity dot while the work goes on. :ws is the
// view: its failure event is a footer message, and a second repo stays running.
func TestFooterErrorBeatsTheActivityDot(t *testing.T) {
	a := routerAt(t, &bareView{}, 180, 40)
	seedWorkspacesActivity(t, a)
	forceRouterFrame(a, command.ViewWorkspaces)
	wantDot(t, a, command.ViewWorkspaces, 1, 0)

	api := filepath.Join(a.config.App.WorkspacesDir, "api")
	a.Update(workspaces.WorkspaceSyncCompleteMsg{RepoPath: api, Error: fmt.Errorf("boom")})

	f := ansi.Strip(viewFooter(t, a, command.ViewWorkspaces))
	if gf, gh := footerDots(f); gf+gh != 0 {
		t.Errorf("an error footer still carries a dot: %q", f)
	}
	if !strings.Contains(f, "Failed to sync") {
		t.Errorf("the error text is not in the footer: %q", f)
	}
}

// --- Forge explorer (DEVDESK-341 step 7) ------------------------------------

// indexedExplorerState gives the session a forge index, so opening the explorer
// puts the indexed roots on screen and re-reads them: "Updating from GitLab...".
func indexedExplorerState(t *testing.T, a *App) {
	t.Helper()
	entries := []forgeindex.Entry{
		{ID: "1", Path: "alpha", Name: "alpha", Kind: forgeindex.KindNamespace},
		{ID: "2", Path: "beta", Name: "beta", Kind: forgeindex.KindNamespace},
	}
	a.sharedState.ForgeIndex = forgeindex.New("https://gitlab.example.com", "u", time.Now(), entries, nil)
}

// openExplorerLoading opens the explorer authenticated and never answers its
// root request. With an index it is a refresh over the indexed rows, without
// one it is the first load.
func openExplorerLoading(t *testing.T, a *App, indexed bool) {
	t.Helper()
	testutil.FastTimers(t, &components.FooterMsgDuration)
	a.sharedState.Forge = gitlabforge.NewWithClient(nil, "https://gitlab.example.com")
	a.sharedState.IsAuthenticated = true
	if indexed {
		indexedExplorerState(t, a)
	}
	_ = a.switchView(command.ViewGitExplorer) // its Init would call the forge: dropped
	a.resize(a.width, a.height)
}

// seedExplorerActivity is the refresh over the indexed roots; the cursor is on a
// row with no activity of its own.
func seedExplorerActivity(t *testing.T, a *App) { openExplorerLoading(t, a, true) }

func TestFooterActivityExplorerFirstLoadShowsOneDot(t *testing.T) {
	a := routerAt(t, &bareView{}, 180, 40)
	openExplorerLoading(t, a, false)
	forceRouterFrame(a, command.ViewGitExplorer)
	footer := viewFooter(t, a, command.ViewGitExplorer)
	if !strings.Contains(ansi.Strip(footer), "Loading") {
		t.Fatalf("fixture error: the explorer is not on its first load: %q", ansi.Strip(footer))
	}
	if msg := footerBrailleProblem(footer); msg != "" {
		t.Error(msg)
	}
	wantDot(t, a, command.ViewGitExplorer, 1, 0)
}

func TestFooterActivityExplorerRefreshShowsOneDot(t *testing.T) {
	a := routerAt(t, &bareView{}, 180, 40)
	seedExplorerActivity(t, a)
	forceRouterFrame(a, command.ViewGitExplorer)
	footer := viewFooter(t, a, command.ViewGitExplorer)
	if !strings.Contains(ansi.Strip(footer), "Updating from GitLab...") {
		t.Fatalf("fixture error: the explorer is not refreshing: %q", ansi.Strip(footer))
	}
	if msg := footerBrailleProblem(footer); msg != "" {
		t.Error(msg)
	}
	wantDot(t, a, command.ViewGitExplorer, 1, 0)
}

// With no job running, the view's own WantsBlink is what arms the router's
// chain, at the 500 ms blink cadence.
func TestFooterActivityExplorerRefreshArmsTheBlinkChain(t *testing.T) {
	stubAnimationEvery(t, time.UnixMilli(0))
	a := routerAt(t, &bareView{}, 180, 40)
	seedExplorerActivity(t, a)
	if a.jobs.Running() != 0 {
		t.Fatal("fixture error: a job is running")
	}
	held, ok := a.views[command.ViewGitExplorer].(BlinkingView)
	if !ok || !held.WantsBlink() {
		t.Fatal("a refreshing explorer does not ask for the blink")
	}
	a.anim.ticking = false
	if a.ensureTick() == nil {
		t.Fatal("a refreshing explorer with no job got no animation chain")
	}
	if a.anim.interval != animationBlinkInterval {
		t.Errorf("chain interval = %v, want the blink interval %v", a.anim.interval, animationBlinkInterval)
	}
	tickAt(a, 500) // off phase
	wantDot(t, a, command.ViewGitExplorer, 0, 1)
	tickAt(a, 1000)
	wantDot(t, a, command.ViewGitExplorer, 1, 0)
}

func TestFooterActivityExplorerDotIsSteadyUnderStaticMotion(t *testing.T) {
	got := stubAnimationEvery(t, time.UnixMilli(0))
	a := routerAt(t, &bareView{}, 180, 40)
	seedExplorerActivity(t, a)
	a.setMotionStatic(true)
	a.anim.ticking = false
	*got = nil
	if cmd := a.ensureTick(); cmd != nil || len(*got) != 0 {
		t.Errorf("app.motion: static started a chain for a blink-only need (%d requests)", len(*got))
	}
	for _, ms := range []int64{0, 500, 1000} {
		tickAt(a, ms)
		wantDot(t, a, command.ViewGitExplorer, 1, 0)
	}
}

// The chain stops once the load is over: the answer settles the refresh, the
// dot goes, and the next tick is not renewed.
func TestFooterActivityExplorerChainStopsWhenTheLoadEnds(t *testing.T) {
	stubAnimationEvery(t, time.UnixMilli(0))
	a := routerAt(t, &bareView{}, 180, 40)
	seedExplorerActivity(t, a)
	a.anim.ticking = false
	startBlinkChain(t, a)
	tickAt(a, 0)
	if !a.anim.ticking {
		t.Fatal("the chain died while the explorer was still refreshing")
	}

	a.Update(explorer.RootGroupsLoadedMsg{Nodes: explorerRoots()})
	if held := a.views[command.ViewGitExplorer].(BlinkingView); held.WantsBlink() {
		t.Fatal("the explorer still asks for the blink after its answer")
	}
	if f := ansi.Strip(viewFooter(t, a, command.ViewGitExplorer)); strings.Contains(f, "Updating") {
		t.Errorf("the refresh line outlived the answer: %q", f)
	}
	wantDot(t, a, command.ViewGitExplorer, 0, 0)
	tickAt(a, 500)
	if a.anim.ticking {
		t.Error("the animation chain is still alive with nothing left to blink")
	}
}
