package mcp

import (
	"context"
	"fmt"
	"strings"
	"time"

	sdk "github.com/modelcontextprotocol/go-sdk/mcp"

	"github.com/anthnel/devdesk/internal/config"
	"github.com/anthnel/devdesk/internal/status"
)

// ── monitors_status ─────────────────────────────────────────────────────────

// context_get says which monitors a context declares; it cannot say whether
// they are up. This is the answer to that, and it is the second tool here that
// touches the network.
//
// The argument for it is net_check's, one notch tighter: the targets come from
// the context's configuration and never from a call, so an agent cannot point
// it at a host the operator did not already list. What it can do is make the
// server probe them all, which is what `:status` does on every refresh. The
// `type` filter only narrows that.
//
// Nothing here writes: the Status view refreshes into its own model, and this
// runs a checker of its own on a copy of the configured list.

// monitorsDeadline bounds the whole call. Each monitor has a timeout of its own,
// but the SSL probe does not take a context, so a client that gives up cannot
// stop it — without a bound here a slow list would hold the handler until the
// last probe's own timeout. A var so a test can shorten it.
var monitorsDeadline = 30 * time.Second

// defaultMonitorTimeout is what config.Load fills in for status.timeout.
const defaultMonitorTimeout = 5 * time.Second

type monitorsStatusIn struct {
	Type string `json:"type,omitempty" jsonschema:"probe only the monitors of this type: http, https, icmp, dns or ssl; empty probes them all"`
}

type monitorStatusOut struct {
	Name           string `json:"name"`
	Type           string `json:"type" jsonschema:"http, https, icmp, dns or ssl"`
	Target         string `json:"target"`
	Status         string `json:"status" jsonschema:"OK, DOWN (unreachable), ERROR (reachable but wrong: a bad HTTP code, an expired certificate) or WARNING"`
	ResponseTimeMs int64  `json:"response_time_ms,omitempty"`
	Error          string `json:"error,omitempty" jsonschema:"why the status is not OK"`
	CheckedAt      string `json:"checked_at" jsonschema:"RFC 3339"`

	// The certificate fields exist for an ssl monitor only.
	CertState string `json:"cert_state,omitempty" jsonschema:"valid, to renew (inside the renewal window), expired, or error when nothing could be read; ssl monitors only"`
	DaysLeft  *int   `json:"days_left,omitempty" jsonschema:"ssl monitors only; negative once expired"`
	Expires   string `json:"expires,omitempty" jsonschema:"RFC 3339; ssl monitors only"`
	Issuer    string `json:"issuer,omitempty" jsonschema:"ssl monitors only"`
}

type monitorsStatusOut struct {
	Context  string             `json:"context" jsonschema:"the DevDesk context that served this answer, whose monitors these are"`
	Monitors []monitorStatusOut `json:"monitors"`
}

func registerMonitorsStatus(s *sdk.Server, env *Env) {
	sdk.AddTool(s, &sdk.Tool{
		Name:        "monitors_status",
		Description: toolDescription("monitors_status"),
	}, func(ctx context.Context, _ *sdk.CallToolRequest, in monitorsStatusIn) (*sdk.CallToolResult, monitorsStatusOut, error) {
		components, err := selectMonitors(env.Config.Status.Components, in.Type)
		if err != nil {
			return nil, monitorsStatusOut{}, err
		}

		results, err := probeMonitors(ctx, env.Config.Status.Timeout, components)
		if err != nil {
			return nil, monitorsStatusOut{}, err
		}

		out := monitorsStatusOut{Context: env.Context, Monitors: make([]monitorStatusOut, 0, len(results))}
		for _, r := range results {
			out.Monitors = append(out.Monitors, monitorFrom(r))
		}
		return nil, out, nil
	})
}

var monitorTypes = []string{
	string(status.TypeHTTP), string(status.TypeHTTPS), string(status.TypeICMP),
	string(status.TypeDNS), string(status.TypeSSL),
}

// selectMonitors narrows the list by type before anything is probed. A type
// that matches no monitor is an empty answer; a type that is not one at all is
// refused, since "nothing to check" and "a typo" must not look alike (D20).
func selectMonitors(all []config.ComponentConfig, kind string) ([]config.ComponentConfig, error) {
	kind = strings.ToLower(strings.TrimSpace(kind))
	if kind == "" {
		return all, nil
	}
	known := false
	for _, t := range monitorTypes {
		known = known || t == kind
	}
	if !known {
		return nil, fmt.Errorf("unknown monitor type %q — the types are: %s", kind, strings.Join(monitorTypes, ", "))
	}

	var out []config.ComponentConfig
	for _, c := range all {
		if status.EffectiveType(c) == kind {
			out = append(out, c)
		}
	}
	return out, nil
}

// probeMonitors runs the checks, and gives up on them at the deadline or when
// the client does. The checks that are still running are left to finish on
// their own timeouts — nothing waits for them, and nothing they return is read.
func probeMonitors(ctx context.Context, timeoutSeconds int, components []config.ComponentConfig) ([]status.ComponentStatus, error) {
	if len(components) == 0 {
		return nil, nil
	}
	timeout := time.Duration(timeoutSeconds) * time.Second
	if timeout <= 0 {
		timeout = defaultMonitorTimeout
	}

	ctx, cancel := context.WithTimeout(ctx, monitorsDeadline)
	defer cancel()

	done := make(chan []status.ComponentStatus, 1)
	go func() { done <- status.NewChecker(timeout).CheckAll(ctx, components) }()

	select {
	case r := <-done:
		return r, nil
	case <-ctx.Done():
		if err := ctx.Err(); err == context.DeadlineExceeded {
			return nil, fmt.Errorf("the monitors did not all answer within %s — nothing is reported rather than a partial list read as complete", monitorsDeadline)
		}
		return nil, ctx.Err()
	}
}

func monitorFrom(r status.ComponentStatus) monitorStatusOut {
	out := monitorStatusOut{
		Name:           r.Name,
		Type:           string(r.Type),
		Target:         r.Target,
		Status:         string(r.Status),
		ResponseTimeMs: r.ResponseTime.Milliseconds(),
		Error:          r.Error,
		CheckedAt:      stamp(r.Timestamp),
	}
	if r.Type == status.TypeSSL {
		out.CertState = string(status.CertStateOf(r))
		out.DaysLeft = r.SSLDaysLeft
		out.Issuer = r.SSLIssuer
		if r.SSLExpires != nil {
			out.Expires = stamp(*r.SSLExpires)
		}
	}
	return out
}
