package mcp

import (
	"context"

	sdk "github.com/modelcontextprotocol/go-sdk/mcp"
)

// ── forwards_list ───────────────────────────────────────────────────────────

// The forwards are the session's: the registry lives in the router and is
// reached through the Dispatcher, like the jobs. Unlike a job they are not a
// context's either — the forwards file is global and a switch leaves them open —
// so the answer carries no context and is declared machine-wide in
// TestEveryContextDependentAnswerSaysWhichContextServedIt.
//
// The tool only reads. Opening a forward binds a port toward a target the caller
// names, which is the one thing that makes forward_open an action to specify
// rather than plumbing (see .claude/plans/2026-09-21-mcp-new-features.md).

type forwardOut struct {
	ID        string `json:"id" jsonschema:"stable for as long as the forward exists; the local port is not, since it is reused once a forward closes"`
	Name      string `json:"name,omitempty" jsonschema:"the *.localhost host name of a named route; empty for a plain TCP forward"`
	LocalPort int    `json:"local_port" jsonschema:"the port a client connects to; for a named route it is the shared proxy's"`
	Address   string `json:"address" jsonschema:"where to connect on this machine, always on the loopback"`
	URL       string `json:"url,omitempty" jsonschema:"what to open for a named route; empty for a plain TCP forward"`
	Target    string `json:"target" jsonschema:"host:port the forward leads to"`
	State     string `json:"state" jsonschema:"live, paused or unbound; unbound means the port could not be bound and is retried at the next launch or by a toggle"`
	Active    int    `json:"active_connections"`
	Total     int64  `json:"total_connections" jsonschema:"connections carried since the forward was opened"`
	OpenedAt  string `json:"opened_at,omitempty" jsonschema:"RFC 3339"`
	LastError string `json:"last_error,omitempty" jsonschema:"why the forward is unbound, or the last failure it met"`
}

type forwardsListIn struct{}

type forwardsListOut struct {
	Forwards []forwardOut `json:"forwards" jsonschema:"in the order they were created"`
}

func registerForwardsList(s *sdk.Server, env *Env) {
	sdk.AddTool(s, &sdk.Tool{
		Name:        "forwards_list",
		Description: toolDescription("forwards_list"),
	}, func(ctx context.Context, _ *sdk.CallToolRequest, _ forwardsListIn) (*sdk.CallToolResult, forwardsListOut, error) {
		if env.Dispatch == nil {
			return nil, forwardsListOut{}, ErrNoSession
		}
		list, err := env.Dispatch.Forwards(ctx)
		if err != nil {
			return nil, forwardsListOut{}, err
		}

		out := make([]forwardOut, 0, len(list))
		for _, f := range list {
			out = append(out, forwardOut{
				ID:        f.ID,
				Name:      f.Name,
				LocalPort: f.LocalPort,
				Address:   f.Addr(),
				URL:       f.URL(),
				Target:    f.Target,
				State:     f.State.String(),
				Active:    f.Active,
				Total:     f.Total,
				OpenedAt:  stamp(f.Opened),
				LastError: f.LastErr,
			})
		}
		return nil, forwardsListOut{Forwards: out}, nil
	})
}
