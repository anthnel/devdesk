package jobsview

import (
	"slices"
	"strings"
	"testing"

	"github.com/charmbracelet/x/ansi"

	"github.com/anthnel/devdesk/internal/command"
	"github.com/anthnel/devdesk/internal/jobs"
)

// DEVDESK-211, step 4. The terminal tab's progress bar has a textual
// equivalent in this view: a Failed count in the header (the "error" state) and
// a "stopping" suffix in the footer (the "warning" state). The footer reads the
// same jobs.Summarize the router reads.

// failureIn builds a remembered failure the way the registry would hand it out.
func failureIn(contextName, target string, seq uint64) jobs.Failure {
	return jobs.Failure{
		FailureKey: jobs.FailureKey{Kind: jobs.KindScan, Target: target, Context: contextName},
		Display:    target,
		Detail:     "boom",
		Seq:        seq,
	}
}

func withFailures(t *testing.T, runs []jobs.Run, failures ...jobs.Failure) Model {
	t.Helper()
	return feed(t, newModel(t), jobs.ChangedMsg{Runs: runs, Failures: failures, Context: testContext})
}

func headerValue(t *testing.T, m Model, key string) string {
	t.Helper()
	_, value, ok := findHeaderInfo(m, key)
	if !ok {
		t.Fatalf("GetHeaderInfo has no %q line", key)
	}
	return value
}

// stoppingRun returns a snapshot holding one run that was asked to stop while
// an item is still running, and the registry that made it so the test can settle
// it afterwards.
func stoppingRun(t *testing.T) (*jobs.Registry, jobs.JobID, []jobs.Run) {
	t.Helper()
	reg := jobs.New()
	r := jobs.NewRun(jobs.KindSync, command.ViewWorkspaces, testContext, "~/work", "~/work/a", "~/work/b")
	id := reg.Start(r)
	if !reg.Advance(id, "~/work/a", jobs.ItemRunning, "") {
		t.Fatal("the registry refused to run the first target")
	}
	if !reg.Cancel(id) {
		t.Fatal("the registry refused to cancel the run")
	}
	return reg, id, reg.Snapshot()
}

func TestFailedIsInTheHeaderAtZero(t *testing.T) {
	m := withFailures(t, nil)
	if got := headerValue(t, m, "Failed"); got != "0" {
		t.Errorf("Failed = %q with no failure, want \"0\"", got)
	}
}

func TestFailedCountsTheFailuresOfThisContextOnly(t *testing.T) {
	cases := []struct {
		name     string
		failures []jobs.Failure
		want     string
	}{
		{"one", []jobs.Failure{failureIn(testContext, "~/a", 1)}, "1"},
		{"several", []jobs.Failure{failureIn(testContext, "~/a", 1), failureIn(testContext, "~/b", 2), failureIn(testContext, "~/c", 3)}, "3"},
		{"two here and one elsewhere", []jobs.Failure{failureIn(testContext, "~/a", 1), failureIn("another-context", "~/b", 2), failureIn(testContext, "~/c", 3)}, "2"},
		{"only another context", []jobs.Failure{failureIn("another-context", "~/b", 1)}, "0"},
	}
	for _, tc := range cases {
		t.Run(tc.name, func(t *testing.T) {
			m := withFailures(t, nil, tc.failures...)
			if got := headerValue(t, m, "Failed"); got != tc.want {
				t.Errorf("Failed = %q, want %q", got, tc.want)
			}
		})
	}
}

// The reviewer's case: the registry keeps a failure after its run has been
// pruned, so the table can no longer show it. The header is then the only place
// :jobs can say it exists.
func TestFailedCountsAFailureWhoseRunIsGone(t *testing.T) {
	m := withFailures(t, []jobs.Run{run(1, jobs.KindScan, "~/ok", jobs.ItemDone)},
		failureIn(testContext, "~/pruned-run-target", 4))

	if got := len(m.runTable.Items()); got != 1 {
		t.Fatalf("the table holds %d runs, want only the surviving one", got)
	}
	if got := headerValue(t, m, "Failed"); got != "1" {
		t.Errorf("Failed = %q, want \"1\": the failure outlives its run", got)
	}
}

// A retry that succeeds, or a Forget, removes the failure from the next
// snapshot: the count follows the snapshot, it does not accumulate.
func TestFailedFollowsTheLatestSnapshot(t *testing.T) {
	m := withFailures(t, nil, failureIn(testContext, "~/a", 1), failureIn(testContext, "~/b", 2))
	m = feed(t, m, jobs.ChangedMsg{Failures: []jobs.Failure{failureIn(testContext, "~/b", 2)}, Context: testContext})
	if got := headerValue(t, m, "Failed"); got != "1" {
		t.Errorf("Failed = %q after one failure was cleared, want \"1\"", got)
	}
	m = feed(t, m, jobs.ChangedMsg{Context: testContext})
	if got := headerValue(t, m, "Failed"); got != "0" {
		t.Errorf("Failed = %q after the last failure was cleared, want \"0\"", got)
	}
}

// Rule 130's spirit: a header whose keys change with the state reads badly.
func TestTheHeaderKeysAreTheSameInEveryState(t *testing.T) {
	_, _, stopping := stoppingRun(t)
	states := map[string]Model{
		"empty":            newModel(t),
		"a run":            withRuns(t, run(1, jobs.KindScan, "~/work", jobs.ItemRunning)),
		"a failure":        withFailures(t, nil, failureIn(testContext, "~/a", 1)),
		"a stopping run":   withRuns(t, stopping...),
		"run and failures": withFailures(t, []jobs.Run{run(1, jobs.KindScan, "~/w", jobs.ItemQueued)}, failureIn(testContext, "~/a", 1), failureIn(testContext, "~/b", 2)),
	}
	keys := func(m Model) []string {
		var out []string
		for _, info := range m.GetHeaderInfo(testContext) {
			out = append(out, info.Key)
		}
		return out
	}
	want := keys(states["empty"])
	if !slices.Contains(want, "Failed") {
		t.Fatalf("header keys %v lack Failed", want)
	}
	for name, m := range states {
		if got := keys(m); !slices.Equal(got, want) {
			t.Errorf("%s: header keys = %v, want %v", name, got, want)
		}
	}
}

func TestTheFooterSaysStoppingWhileACancelledRunHasNotSettled(t *testing.T) {
	_, _, snapshot := stoppingRun(t)
	m := withRuns(t, snapshot...)

	if want := "1 job running · 1 stopping"; m.status().Text != want {
		t.Errorf("the footer says %q, want %q", m.status().Text, want)
	}
	if !strings.Contains(ansi.Strip(m.RenderFooter(120)), "1 job running · 1 stopping") {
		t.Errorf("the rendered footer lacks the stopping text:\n%s", m.RenderFooter(120))
	}
}

func TestTheFooterOnlyCountsTheRunsBeingStopped(t *testing.T) {
	_, _, snapshot := stoppingRun(t)
	snapshot = append(snapshot, run(9, jobs.KindScan, "~/other", jobs.ItemRunning))
	m := withRuns(t, snapshot...)

	if want := "2 jobs running · 1 stopping"; m.status().Text != want {
		t.Errorf("the footer says %q, want %q", m.status().Text, want)
	}
}

func TestTheFooterGoesQuietOnceTheStoppedRunSettles(t *testing.T) {
	reg, id, _ := stoppingRun(t)
	if !reg.Advance(id, "~/work/a", jobs.ItemFailed, jobs.CancelledDetail) {
		t.Fatal("the registry refused to settle the running target")
	}
	m := withRuns(t, reg.Snapshot()...)
	if got := m.status().Text; got != "" {
		t.Errorf("the footer says %q after the stopped run settled, want nothing", got)
	}
}

// No cancellation, no suffix: the plain count of the existing footer is kept.
func TestTheFooterHasNoStoppingSuffixWhenNothingIsStopping(t *testing.T) {
	m := withRuns(t, run(1, jobs.KindScan, "~/work", jobs.ItemRunning))
	if got := m.status().Text; strings.Contains(got, "stopping") || got != "1 job running" {
		t.Errorf("the footer says %q, want \"1 job running\"", got)
	}
}

// A failure is the header's business (Failed): the footer's count stays about
// what is running, whatever failed.
func TestAFailureDoesNotChangeTheFooterText(t *testing.T) {
	m := withFailures(t, nil, failureIn(testContext, "~/a", 1))
	if got := m.status().Text; got != "" {
		t.Errorf("the footer says %q with a failure and nothing running, want nothing", got)
	}
}

func TestTheHelpExplainsTheTerminalTab(t *testing.T) {
	content := newModel(t).GetHelpContent()
	var body string
	for _, s := range content.Sections {
		if strings.Contains(s.Title, "Terminal tab") {
			body = s.Body
		}
	}
	if body == "" {
		t.Fatal("the help has no \"Terminal tab\" section (Rule 114)")
	}
	lower := strings.ToLower(body)
	for _, want := range []string{"red", "failed", "opening this view"} {
		if !strings.Contains(lower, want) {
			t.Errorf("the Terminal tab help does not mention %q:\n%s", want, body)
		}
	}
}
