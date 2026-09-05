package mcp

import (
	"context"
	"fmt"
	"time"

	sdk "github.com/modelcontextprotocol/go-sdk/mcp"

	"github.com/anthnel/devdesk/internal/jobs"
)

// ── jobs_list, jobs_get ─────────────────────────────────────────────────────

// These are the first tools here that cannot read the disk, and they are the
// reason §3.61 puts the server inside the TUI rather than beside it. A scan
// that has finished is in the scan cache; a scan that is *running* exists only
// in jobs.Registry, in the router's model, for the life of the session (D8 of
// §3.58). A headless process cannot answer "is it still going".
//
// They go through the Dispatcher, which sends a message and waits — the model
// is in this process, and reading it from a tool handler's goroutine would be
// the data race Rule 110 exists to forbid.
//
// There is no `jobs_cancel` yet. Cancelling is an action, and §3.61's action
// tier comes with the rest of it; what is here is the reading half.

type jobItemOut struct {
	Target  string `json:"target" jsonschema:"the repository path, image reference or group path this item is about"`
	Display string `json:"display,omitempty" jsonschema:"what a screen prints for it, when that differs from the target — a registry alias applied, a path shortened"`
	State   string `json:"state" jsonschema:"queued, running, done, skipped or failed"`
	Detail  string `json:"detail,omitempty" jsonschema:"what the state alone cannot say: why it failed, or that the target was already there"`
}

type jobOut struct {
	ID   int    `json:"id" jsonschema:"the identifier jobs_get takes"`
	Kind string `json:"kind" jsonschema:"scan, sync, clone, pull, create or delete"`
	// State is the run's, derived from its items rather than counted alongside
	// them — which is why it cannot disagree with the counts below.
	State string `json:"state" jsonschema:"queued, running, done, failed or cancelled"`
	Label string `json:"label" jsonschema:"what the run is called for a human, such as the directory or registry it is about"`
	// Context is the run's own, stamped when it was admitted and never re-read.
	// A run started in one context and still going after the user switched
	// belongs to the context it was launched in, and says so here — otherwise
	// an agent reads a result believing it speaks about the context now on
	// screen (§3.61).
	Context string `json:"context" jsonschema:"the DevDesk context this run was launched in, which is not necessarily the one on screen now"`
	View    string `json:"view" jsonschema:"the screen the run was started from"`
	// Open says the target list is still being discovered — a clone whose walk
	// is still finding repositories. An open run is never finished, however its
	// items stand.
	Open      bool   `json:"open" jsonschema:"true while the run is still discovering what it has to do, so the counts below are not yet a total"`
	Cancelled bool   `json:"cancelled" jsonschema:"someone asked for this run to stop; it is how a stopped run is told from one that simply finished"`
	Total     int    `json:"total"`
	Done      int    `json:"done" jsonschema:"items that will not change state again, failed and skipped included"`
	StartedAt string `json:"started_at" jsonschema:"RFC 3339"`
	EndedAt   string `json:"ended_at,omitempty" jsonschema:"RFC 3339, absent while the run is still going"`
	// Items is filled by jobs_get and left out by jobs_list: a scan of every
	// image on a machine is one run with a hundred targets, and a listing that
	// carried them all would answer "what is running" with a page of paths.
	Items []jobItemOut `json:"items,omitempty"`
}

type jobsListIn struct {
	RunningOnly bool `json:"running_only,omitempty" jsonschema:"return only the runs that have not settled, which is what answers whether anything is still in flight"`
}

type jobsListOut struct {
	Jobs []jobOut `json:"jobs"`
}

type jobsGetIn struct {
	JobID int `json:"job_id" jsonschema:"the id from jobs_list"`
}

type jobsGetOut struct {
	Job jobOut `json:"job"`
}

func registerJobsList(s *sdk.Server, env *Env) {
	sdk.AddTool(s, &sdk.Tool{
		Name:        "jobs_list",
		Description: toolDescription("jobs_list"),
	}, func(ctx context.Context, _ *sdk.CallToolRequest, in jobsListIn) (*sdk.CallToolResult, jobsListOut, error) {
		runs, err := sessionJobs(ctx, env)
		if err != nil {
			return nil, jobsListOut{}, err
		}

		out := make([]jobOut, 0, len(runs))
		for _, run := range runs {
			if in.RunningOnly && run.Finished() {
				continue
			}
			out = append(out, summarise(run))
		}
		return nil, jobsListOut{Jobs: out}, nil
	})
}

func registerJobsGet(s *sdk.Server, env *Env) {
	sdk.AddTool(s, &sdk.Tool{
		Name:        "jobs_get",
		Description: toolDescription("jobs_get"),
	}, func(ctx context.Context, _ *sdk.CallToolRequest, in jobsGetIn) (*sdk.CallToolResult, jobsGetOut, error) {
		runs, err := sessionJobs(ctx, env)
		if err != nil {
			return nil, jobsGetOut{}, err
		}

		for _, run := range runs {
			if int(run.ID) != in.JobID {
				continue
			}
			job := summarise(run)
			job.Items = make([]jobItemOut, 0, len(run.Items))
			for _, item := range run.Items {
				job.Items = append(job.Items, jobItemOut{
					Target:  item.Target,
					Display: item.Display,
					State:   string(item.State),
					Detail:  item.Detail,
				})
			}
			return nil, jobsGetOut{Job: job}, nil
		}

		// An id nobody knows is an error, not an empty job. Runs live for the
		// session and the registry keeps only the last twenty settled ones, so
		// "gone" is a real answer and reading it as "not started" is D20.
		return nil, jobsGetOut{}, fmt.Errorf("no job %d in this session — runs are not saved across restarts, and the registry keeps only the last %d settled ones", in.JobID, jobs.MaxFinishedRuns)
	})
}

// sessionJobs is the one place these two tools reach the router, so the missing
// dispatcher is refused once rather than at each call site.
func sessionJobs(ctx context.Context, env *Env) ([]jobs.Run, error) {
	if env.Dispatch == nil {
		return nil, ErrNoSession
	}
	return env.Dispatch.Jobs(ctx)
}

func summarise(run jobs.Run) jobOut {
	return jobOut{
		ID:        int(run.ID),
		Kind:      string(run.Kind),
		State:     string(run.State()),
		Label:     run.Label,
		Context:   run.Context,
		View:      string(run.Origin),
		Open:      run.Open(),
		Cancelled: run.Cancelled(),
		Total:     run.Total(),
		Done:      run.Done(),
		StartedAt: stamp(run.StartedAt),
		EndedAt:   stamp(run.EndedAt),
	}
}

// stamp renders a time, and renders the zero one as nothing at all. A run that
// has not ended has no end, and 0001-01-01T00:00:00Z is a date an agent would
// have to know to disbelieve.
func stamp(t time.Time) string {
	if t.IsZero() {
		return ""
	}
	return t.Format(time.RFC3339)
}
