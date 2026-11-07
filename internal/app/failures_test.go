package app

import (
	"errors"
	"fmt"
	"io/fs"
	"os"
	"path/filepath"
	"reflect"
	"testing"

	tea "github.com/charmbracelet/bubbletea"

	"github.com/anthnel/devdesk/internal/command"
	"github.com/anthnel/devdesk/internal/jobs"
	ociresources "github.com/anthnel/devdesk/internal/ui/oci_resources"
	"github.com/anthnel/devdesk/internal/ui/security"
	"github.com/anthnel/devdesk/internal/ui/testutil"
	"github.com/anthnel/devdesk/internal/ui/workspaces"
)

// failureRouter holds a fake view for each view the failure tests route to.
func failureRouter(t *testing.T) (*App, map[command.ViewType]*fakeView) {
	t.Helper()
	testutil.FastTimers(t, &jobSpinnerInterval)
	held := map[command.ViewType]*fakeView{
		command.ViewWorkspaces:   {},
		command.ViewSecurity:     {},
		command.ViewOCIResources: {},
	}
	a := router(t, &fakeView{})
	for v, fv := range held {
		a.views[v] = fv
	}
	return a, held
}

func startScan(a *App, origin command.ViewType, targets ...string) {
	a.Update(jobs.StartMsg{Run: jobs.NewRun(jobs.KindScan, origin, "", "x", targets...)})
}

func failWorkspaceScan(a *App, repo string) {
	a.Update(workspaces.WorkspaceScanCompleteMsg{RepoPath: repo, Error: errors.New("boom")})
}

func failImageScan(a *App, image string) {
	a.Update(ociresources.ImageScanFinishedMsg{ImageName: image, Err: errors.New("boom")})
}

func remembered(a *App, kind jobs.Kind, target, contextName string) bool {
	_, ok := jobs.FailureFor(a.jobs.Failures(), kind, target, contextName)
	return ok
}

// Criterion 6: a view is handed a copy of the memory, never the router's state.
func TestAFailureReachesEveryHeldViewAsACopy(t *testing.T) {
	a, held := failureRouter(t)
	startScan(a, command.ViewWorkspaces, "/w/app")
	failWorkspaceScan(a, "/w/app")

	for v, fv := range held {
		got, ok := jobs.FailureFor(lastJobs(t, fv).Failures, jobs.KindScan, "/w/app", "default")
		if !ok {
			t.Errorf("%s was not handed the failure", v)
			continue
		}
		if got.Detail == "" {
			t.Errorf("%s was handed a failure without a reason", v)
		}
	}

	seen := lastJobs(t, held[command.ViewSecurity]).Failures
	seen[0].Detail = "mutated"
	seen[0].Target = "/elsewhere"
	_ = append(seen[:0], seen[1:]...)

	stored := a.jobs.Failures()
	if len(stored) != 1 || stored[0].Detail == "mutated" || stored[0].Target != "/w/app" {
		t.Errorf("a view wrote through its copy: registry holds %+v", stored)
	}
}

// Criterion 8: ws and :sec name one repository by one key, however spelled.
func TestARetryFromAnotherViewClearsTheFailure(t *testing.T) {
	for _, retry := range []string{"/w/app", "/w/app/"} {
		t.Run(retry, func(t *testing.T) {
			a, _ := failureRouter(t)
			startScan(a, command.ViewWorkspaces, "/w/app")
			failWorkspaceScan(a, "/w/app")
			if !remembered(a, jobs.KindScan, "/w/app", "default") {
				t.Fatal("the failure was not remembered")
			}
			if !remembered(a, jobs.KindScan, "/w/app/", "default") {
				t.Error("the same repository written with a trailing slash is another key")
			}

			startScan(a, command.ViewSecurity, retry)
			if remembered(a, jobs.KindScan, "/w/app", "default") {
				t.Error("a retry from :sec did not clear the failure ws remembered")
			}
		})
	}
}

// A failure seen by :sec is cleared by ws as well, the other direction.
func TestAFailureFromSecIsClearedByWorkspaces(t *testing.T) {
	a, _ := failureRouter(t)
	startScan(a, command.ViewSecurity, "/w/app")
	a.Update(security.InventoryScanFinishedMsg{Name: "/w/app", Err: errors.New("boom")})
	if !remembered(a, jobs.KindScan, "/w/app/", "default") {
		t.Fatal("ws would not find the failure :sec posed")
	}
	startScan(a, command.ViewWorkspaces, "/w/app/")
	if remembered(a, jobs.KindScan, "/w/app", "default") {
		t.Error("a retry from ws did not clear it")
	}
}

// Criterion 8: an image is its raw reference in OCI and in :sec; two spellings
// are two targets.
func TestAnImageFailureIsSharedByOCIAndSec(t *testing.T) {
	a, _ := failureRouter(t)
	startScan(a, command.ViewOCIResources, "nginx:1.27")
	failImageScan(a, "nginx:1.27")
	if !remembered(a, jobs.KindScan, "nginx:1.27", "default") {
		t.Fatal("the image failure was not remembered")
	}

	startScan(a, command.ViewSecurity, "docker.io/library/nginx:1.27")
	if !remembered(a, jobs.KindScan, "nginx:1.27", "default") {
		t.Error("another spelling of the image cleared the failure: spellings are not normalized")
	}

	startScan(a, command.ViewSecurity, "nginx:1.27")
	if remembered(a, jobs.KindScan, "nginx:1.27", "default") {
		t.Error("a :sec scan of the same reference did not clear the OCI failure")
	}
}

func TestTwoSpellingsOfAnImageAreTwoKeys(t *testing.T) {
	a, _ := failureRouter(t)
	startScan(a, command.ViewOCIResources, "nginx", "docker.io/library/nginx")
	failImageScan(a, "nginx")
	failImageScan(a, "docker.io/library/nginx")

	got := a.jobs.Failures()
	if len(got) != 2 {
		t.Fatalf("remembered %d failures, want one per spelling: %+v", len(got), got)
	}
	if got[0].Target == got[1].Target {
		t.Errorf("both spellings share the key %q", got[0].Target)
	}
}

func TestAnImageIsNotARepository(t *testing.T) {
	a, held := failureRouter(t)
	startScan(a, command.ViewOCIResources, "nginx:1.27")
	startScan(a, command.ViewWorkspaces, "/w/app")
	failImageScan(a, "nginx:1.27")

	failures := lastJobs(t, held[command.ViewWorkspaces]).Failures
	if len(jobs.FailuresOn(failures, "default", "/w/app")) != 0 {
		t.Error("an image failure shows on a repository")
	}
	if len(jobs.FailuresOn(failures, "default", "nginx:1.27")) != 1 {
		t.Error("the image failure is missing")
	}

	failWorkspaceScan(a, "/w/app")
	if len(a.jobs.Failures()) != 2 {
		t.Errorf("repository and image share a key: %+v", a.jobs.Failures())
	}
}

// Criterion 3: a view that removes a target outside the registry says so.
func TestForgetTargetClearsInTheCurrentContextOnly(t *testing.T) {
	a, held := failureRouter(t)
	startScan(a, command.ViewOCIResources, "nginx:1.27")
	failImageScan(a, "nginx:1.27")

	// The same failure in another context, posed straight in the registry.
	id := a.jobs.Start(scanRun("prod", "nginx:1.27"))
	a.jobs.Advance(id, "nginx:1.27", jobs.ItemFailed, "boom")
	if !remembered(a, jobs.KindScan, "nginx:1.27", "prod") {
		t.Fatal("the prod failure was not posed")
	}

	a.Update(jobs.ForgetTargetMsg{Target: "nginx:1.27"})

	if remembered(a, jobs.KindScan, "nginx:1.27", "default") {
		t.Error("the failure survived the removal of its target")
	}
	if !remembered(a, jobs.KindScan, "nginx:1.27", "prod") {
		t.Error("forgetting in default dropped the prod failure")
	}
	// The broadcast reaches the fake views inside Update (jobsChanged returns
	// no Cmd of its own with no open run), so the views are what proves it.
	if got := lastJobs(t, held[command.ViewWorkspaces]).Failures; len(jobs.FailuresOn(got, "default", "nginx:1.27")) != 0 {
		t.Error("the views were not told")
	}

	if _, again := a.Update(jobs.ForgetTargetMsg{Target: "nginx:1.27"}); again != nil {
		t.Error("forgetting what is already gone should return no command")
	}
}

func TestForgetTargetCommandBuildsTheMessage(t *testing.T) {
	msg, ok := testutil.Msg(jobs.ForgetTarget("nginx:1.27")).(jobs.ForgetTargetMsg)
	if !ok || msg.Target != "nginx:1.27" {
		t.Errorf("ForgetTarget built %#v", msg)
	}
}

// Criterion 4: A fails, B does not show it, A finds it again.
func TestAFailureSurvivesAContextRoundTrip(t *testing.T) {
	a, _ := failureRouter(t)
	startScan(a, command.ViewWorkspaces, "/w/app")
	failWorkspaceScan(a, "/w/app")

	switchTo := func(name string) {
		a.Update(ContextSwitchCompleteMsg{Config: testConfig(), ContextName: name, Secrets: a.sharedState.Secrets})
		// Views are rebuilt by the switch; hold a fake to read what they are handed.
		a.views[command.ViewWorkspaces] = &fakeView{}
		a.jobsChanged()
	}
	read := func() []jobs.Failure {
		return lastJobs(t, a.views[command.ViewWorkspaces].(*fakeView)).Failures
	}

	switchTo("prod")
	if a.currentContext != "prod" {
		t.Fatalf("currentContext = %q", a.currentContext)
	}
	if got := jobs.FailuresOn(read(), a.currentContext, "/w/app"); len(got) != 0 {
		t.Errorf("prod shows default's failure: %+v", got)
	}
	if !remembered(a, jobs.KindScan, "/w/app", "default") {
		t.Fatal("the switch purged the memory")
	}

	switchTo("default")
	if _, ok := jobs.FailureFor(read(), jobs.KindScan, "/w/app", a.currentContext); !ok {
		t.Error("coming back to default lost its failure")
	}
}

// Criterion 3, second half: neither leaving a view nor ctrl+r clears it.
func TestAFailureSurvivesLeavingAndReopeningAView(t *testing.T) {
	a, held := failureRouter(t)
	startScan(a, command.ViewWorkspaces, "/w/app")
	failWorkspaceScan(a, "/w/app")

	a.switchView(command.ViewWorkspaces)
	a.switchView(command.ViewDashboard)
	a.switchView(command.ViewWorkspaces)

	if _, ok := jobs.FailureFor(lastJobs(t, held[command.ViewWorkspaces]).Failures, jobs.KindScan, "/w/app", "default"); !ok {
		t.Error("the re-entered view was not handed the failure")
	}

	a.Update(tea.KeyMsg{Type: tea.KeyCtrlR})
	if !remembered(a, jobs.KindScan, "/w/app", "default") {
		t.Error("ctrl+r cleared the failure")
	}
}

// Criterion 2: a new attempt, or a success, ends the failure.
func TestAStartOrADoneClearsTheFailure(t *testing.T) {
	a, held := failureRouter(t)
	startScan(a, command.ViewWorkspaces, "/w/app")
	failWorkspaceScan(a, "/w/app")

	startScan(a, command.ViewWorkspaces, "/w/app")
	if remembered(a, jobs.KindScan, "/w/app", "default") {
		t.Error("a new attempt left the previous failure")
	}
	if got := lastJobs(t, held[command.ViewSecurity]).Failures; len(got) != 0 {
		t.Errorf("the views were still told of it: %+v", got)
	}

	// A done clears on its own, without a Start: two attempts open on the same
	// target, the first fails, the second then succeeds. A Start before the
	// done would clear the failure by itself and prove nothing about done.
	startScan(a, command.ViewWorkspaces, "/w/app")
	failWorkspaceScan(a, "/w/app")
	if !remembered(a, jobs.KindScan, "/w/app", "default") {
		t.Fatal("the failure was not posed while another attempt is open")
	}
	a.Update(workspaces.WorkspaceScanCompleteMsg{RepoPath: "/w/app"})
	if remembered(a, jobs.KindScan, "/w/app", "default") {
		t.Error("a success left the failure")
	}
}

// homeListing records every path under root with its size and mtime.
func homeListing(t *testing.T, root string) []string {
	t.Helper()
	var out []string
	err := filepath.WalkDir(root, func(p string, d fs.DirEntry, err error) error {
		if err != nil {
			return err
		}
		info, err := d.Info()
		if err != nil {
			return err
		}
		out = append(out, fmt.Sprintf("%s %d %d", p, info.Size(), info.ModTime().UnixNano()))
		return nil
	})
	if err != nil {
		t.Fatalf("walk %s: %v", root, err)
	}
	return out
}

// Criterion 5: no failure is written to disk, and none survives a restart.
func TestFailuresAreNotWrittenToDisk(t *testing.T) {
	a, _ := failureRouter(t)
	home := os.Getenv("HOME")
	before := homeListing(t, home)

	startScan(a, command.ViewWorkspaces, "/w/app")
	failWorkspaceScan(a, "/w/app")
	startScan(a, command.ViewWorkspaces, "/w/app")
	failWorkspaceScan(a, "/w/app")
	startScan(a, command.ViewOCIResources, "nginx:1")
	failImageScan(a, "nginx:1")
	if !remembered(a, jobs.KindScan, "/w/app", "default") {
		t.Fatal("setup: the failure was not memorised, the check below proves nothing")
	}
	a.Update(jobs.ForgetTargetMsg{Target: "/w/app"})

	if after := homeListing(t, home); !reflect.DeepEqual(before, after) {
		t.Errorf("HOME changed after failures were memorised:\nbefore: %q\nafter:  %q", before, after)
	}

	// A restart: a new router. routerAt isolates HOME and takes a fresh one,
	// so this proves the memo lives in memory only, not in the old HOME.
	t.Setenv("HOME", home)
	fresh := routerAt(t, &fakeView{}, 180, 40)
	t.Setenv("HOME", home)
	if got := fresh.jobs.Failures(); len(got) != 0 {
		t.Errorf("a recreated router holds %d failure(s), want none: %+v", len(got), got)
	}
}
