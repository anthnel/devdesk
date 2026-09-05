package mcp

import (
	"context"
	"fmt"

	sdk "github.com/modelcontextprotocol/go-sdk/mcp"

	"github.com/anthnel/devdesk/internal/jobs"
)

// ── the action tier ─────────────────────────────────────────────────────────
//
// §3.38 refused this outright: an agent that picks the wrong row in the TUI
// meets no modal, and there was nobody to raise one. §3.61 reverses it, and not
// by bringing the modal back — a confirmation raised by a tool call would block
// the agent on an event the user is not looking at, in a window they may not
// have open. **The class of action that would have needed one is simply not
// registered**, which is the shape of every other guarantee here: contextGetOut
// has no field for a credential, finding has no Match, expose is an allow-list.
// An action never registered cannot be wrongly confirmed.
//
// So nothing here deletes, prunes, kills or stops. Nothing creates or renames
// either, whose only undo is a delete that is not exposed — an action made
// irreversible by removing its inverse is worse than a destructive one owned up
// to.
//
// Every tool returns a job identifier and not a result. The work outlives the
// call: `jobs_get` is how it is followed, and `jobs_cancel` how a scan or a
// pull is cut short.

type actionOut struct {
	JobID int `json:"job_id" jsonschema:"the run this started; jobs_get follows it and jobs_cancel can stop it while it lasts"`
}

type workspaceScanIn struct {
	Paths []string `json:"paths,omitempty" jsonschema:"absolute repository paths as workspaces_list reports them; empty scans every repository this context lists"`
}

type workspaceSyncIn struct {
	Paths []string `json:"paths,omitempty" jsonschema:"absolute repository paths as workspaces_list reports them; empty syncs every repository this context lists"`
}

type imageScanIn struct {
	Images []string `json:"images,omitempty" jsonschema:"image references as images_list reports them; empty scans every image that has never been scanned"`
}

type imagePullIn struct {
	Image string `json:"image" jsonschema:"the image reference to pull, tag included"`
}

type jobsCancelIn struct {
	JobID int `json:"job_id" jsonschema:"the run to stop, from jobs_list"`
}

type jobsCancelOut struct {
	Stopped bool `json:"stopped"`
}

func registerWorkspaceScanStart(s *sdk.Server, env *Env) {
	sdk.AddTool(s, &sdk.Tool{
		Name:        "workspace_scan_start",
		Description: toolDescription("workspace_scan_start"),
	}, func(ctx context.Context, _ *sdk.CallToolRequest, in workspaceScanIn) (*sdk.CallToolResult, actionOut, error) {
		return startAction(ctx, env, Action{Tool: "workspace_scan_start", Targets: in.Paths})
	})
}

func registerWorkspaceSyncStart(s *sdk.Server, env *Env) {
	sdk.AddTool(s, &sdk.Tool{
		Name:        "workspace_sync_start",
		Description: toolDescription("workspace_sync_start"),
	}, func(ctx context.Context, _ *sdk.CallToolRequest, in workspaceSyncIn) (*sdk.CallToolResult, actionOut, error) {
		return startAction(ctx, env, Action{Tool: "workspace_sync_start", Targets: in.Paths})
	})
}

func registerImageScanStart(s *sdk.Server, env *Env) {
	sdk.AddTool(s, &sdk.Tool{
		Name:        "image_scan_start",
		Description: toolDescription("image_scan_start"),
	}, func(ctx context.Context, _ *sdk.CallToolRequest, in imageScanIn) (*sdk.CallToolResult, actionOut, error) {
		return startAction(ctx, env, Action{Tool: "image_scan_start", Targets: in.Images})
	})
}

func registerImagePullStart(s *sdk.Server, env *Env) {
	sdk.AddTool(s, &sdk.Tool{
		Name:        "image_pull_start",
		Description: toolDescription("image_pull_start"),
	}, func(ctx context.Context, _ *sdk.CallToolRequest, in imagePullIn) (*sdk.CallToolResult, actionOut, error) {
		return startAction(ctx, env, Action{Tool: "image_pull_start", Targets: []string{in.Image}})
	})
}

func registerJobsCancel(s *sdk.Server, env *Env) {
	sdk.AddTool(s, &sdk.Tool{
		Name:        "jobs_cancel",
		Description: toolDescription("jobs_cancel"),
	}, func(ctx context.Context, _ *sdk.CallToolRequest, in jobsCancelIn) (*sdk.CallToolResult, jobsCancelOut, error) {
		if env.Dispatch == nil {
			return nil, jobsCancelOut{}, ErrNoSession
		}
		if err := env.Dispatch.Cancel(ctx, jobs.JobID(in.JobID)); err != nil {
			return nil, jobsCancelOut{}, err
		}
		return nil, jobsCancelOut{Stopped: true}, nil
	})
}

// startAction is the one road every action tool takes, so the missing
// dispatcher and the shape of the answer are decided once.
//
// A refusal comes back as an error carrying the view's own sentence — the same
// one the header greys the shortcut with (Rule 130). One calculation, and now
// three readers: the header, the footer, and this.
func startAction(ctx context.Context, env *Env, act Action) (*sdk.CallToolResult, actionOut, error) {
	if env.Dispatch == nil {
		return nil, actionOut{}, ErrNoSession
	}
	id, err := env.Dispatch.Start(ctx, act)
	if err != nil {
		return nil, actionOut{}, fmt.Errorf("%s: %w", act.Tool, err)
	}
	return nil, actionOut{JobID: int(id)}, nil
}
