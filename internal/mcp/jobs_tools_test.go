package mcp

import (
	"context"
	"strings"
	"testing"
	"time"

	sdk "github.com/modelcontextprotocol/go-sdk/mcp"

	"github.com/anthnel/devdesk/internal/command"
	"github.com/anthnel/devdesk/internal/jobs"
)

// fakeSession stands in for the running TUI. The real dispatcher is one
// p.Send away from Update and is tested in internal/app; what these tests
// check is the shape of the answer, which is this package's business.
type fakeSession struct {
	runs []jobs.Run
	err  error

	// started records what an action tool asked for, and startedID is what it
	// gets back.
	started   []Action
	startedID jobs.JobID
	startErr  error

	cancelled []jobs.JobID
	cancelErr error
}

func (f *fakeSession) Jobs(context.Context) ([]jobs.Run, error) { return f.runs, f.err }

func (f *fakeSession) Start(_ context.Context, act Action) (jobs.JobID, error) {
	f.started = append(f.started, act)
	return f.startedID, f.startErr
}

func (f *fakeSession) Cancel(_ context.Context, id jobs.JobID) error {
	f.cancelled = append(f.cancelled, id)
	return f.cancelErr
}

func sessionEnv(runs []jobs.Run) *Env {
	env := testEnv(nil)
	env.Dispatch = &fakeSession{runs: runs}
	return env
}

// buildRun makes a run the way the registry hands one out: through Start, so
// the identifier and the derived state are the registry's rather than a
// literal a test made up.
func buildRun(t *testing.T, run jobs.Run) []jobs.Run {
	t.Helper()
	r := jobs.New()
	r.Start(run)
	return r.Snapshot()
}

// toolError calls a tool that is expected to refuse, and returns what it said.
// The shared callTool fails the test on a tool error, which is right for the
// tools that answer — these two have refusals worth reading.
func toolError(t *testing.T, env *Env, name string, args map[string]any) string {
	t.Helper()

	cs := connect(t, env)
	res, err := cs.CallTool(context.Background(), &sdk.CallToolParams{Name: name, Arguments: args})
	if err != nil {
		t.Fatalf("CallTool %s: %v", name, err)
	}
	if !res.IsError {
		t.Fatalf("CallTool %s answered where a refusal was expected: %+v", name, res.StructuredContent)
	}

	var sb strings.Builder
	for _, c := range res.Content {
		if text, ok := c.(*sdk.TextContent); ok {
			sb.WriteString(text.Text)
		}
	}
	return sb.String()
}

// A running scan exists only in the session — the cache holds the finished
// ones. This is the answer a headless server could not give, and the reason
// §3.61 moved the server into the TUI.
func TestJobsListReportsTheWorkInFlight(t *testing.T) {
	runs := buildRun(t, jobs.Run{
		Kind:      jobs.KindScan,
		Origin:    command.ViewWorkspaces,
		Context:   "work",
		Label:     "~/work",
		StartedAt: time.Date(2026, 9, 5, 10, 0, 0, 0, time.UTC),
		Items: []jobs.Item{
			{Target: "/repo/a", State: jobs.ItemDone},
			{Target: "/repo/b", State: jobs.ItemRunning},
		},
	})

	var out jobsListOut
	callTool(t, connect(t, sessionEnv(runs)), "jobs_list", nil, &out)

	if len(out.Jobs) != 1 {
		t.Fatalf("got %d jobs, want 1", len(out.Jobs))
	}
	job := out.Jobs[0]
	if job.Kind != "scan" || job.State != "running" {
		t.Errorf("kind/state = %q/%q, want scan/running", job.Kind, job.State)
	}
	if job.Total != 2 || job.Done != 1 {
		t.Errorf("done %d/%d, want 1/2", job.Done, job.Total)
	}
	// The run's own context, not the one on screen: a run started in one and
	// still going after a switch belongs to the one it was launched in (§3.61).
	if job.Context != "work" {
		t.Errorf("context = %q, want the one the run was launched in", job.Context)
	}
	if job.StartedAt != "2026-09-05T10:00:00Z" {
		t.Errorf("started_at = %q, want RFC 3339", job.StartedAt)
	}
	// A run still going has no end, and the zero time must not be rendered as
	// a date an agent would have to know to disbelieve.
	if job.EndedAt != "" {
		t.Errorf("ended_at = %q for a running job, want it absent", job.EndedAt)
	}
	// The listing answers "what is running", not "with which hundred targets".
	if job.Items != nil {
		t.Error("jobs_list carried the item list; that is what jobs_get is for")
	}
}

func TestJobsListCanNarrowToWhatIsStillGoing(t *testing.T) {
	r := jobs.New()
	r.Start(jobs.Run{Kind: jobs.KindScan, Label: "finished", Items: []jobs.Item{{Target: "a", State: jobs.ItemDone}}})
	r.Start(jobs.Run{Kind: jobs.KindSync, Label: "going", Items: []jobs.Item{{Target: "b", State: jobs.ItemRunning}}})

	var out jobsListOut
	callTool(t, connect(t, sessionEnv(r.Snapshot())), "jobs_list", map[string]any{"running_only": true}, &out)

	if len(out.Jobs) != 1 || out.Jobs[0].Label != "going" {
		t.Fatalf("running_only returned %+v, want only the unsettled run", out.Jobs)
	}
}

// jobs_get is where the targets are, with the detail that says why one failed.
func TestJobsGetCarriesEveryTargetAndItsReason(t *testing.T) {
	runs := buildRun(t, jobs.Run{
		Kind:  jobs.KindClone,
		Label: "team/infra",
		Items: []jobs.Item{
			{Target: "team/infra/api", Display: "api", State: jobs.ItemDone},
			{Target: "team/infra/web", State: jobs.ItemFailed, Detail: "authentication failed"},
		},
	})

	var out jobsGetOut
	callTool(t, connect(t, sessionEnv(runs)), "jobs_get", map[string]any{"job_id": int(runs[0].ID)}, &out)

	if len(out.Job.Items) != 2 {
		t.Fatalf("got %d items, want 2", len(out.Job.Items))
	}
	if out.Job.Items[0].Display != "api" {
		t.Errorf("display = %q, want the shortened form a screen prints", out.Job.Items[0].Display)
	}
	if out.Job.Items[1].Detail != "authentication failed" {
		t.Errorf("detail = %q, want the reason the item failed", out.Job.Items[1].Detail)
	}
}

// Runs live for the session and the registry keeps only the last twenty settled
// ones, so an unknown id is a real answer. Returning an empty job would be an
// absence read as an emptiness, which is D20.
func TestAnUnknownJobIsAnErrorAndNotAnEmptyJob(t *testing.T) {
	msg := toolError(t, sessionEnv(nil), "jobs_get", map[string]any{"job_id": 42})

	if !strings.Contains(msg, "42") {
		t.Errorf("the refusal does not name the id asked for: %s", msg)
	}
}

// A server built without a link to the session refuses with a sentence rather
// than panicking inside a tool handler.
func TestAToolThatNeedsTheSessionRefusesWithoutOne(t *testing.T) {
	msg := toolError(t, testEnv(nil), "jobs_list", nil)

	if !strings.Contains(msg, "running DevDesk session") {
		t.Errorf("the refusal does not say what is missing: %s", msg)
	}
}
