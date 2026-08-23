package mcp

import (
	"context"
	"strings"
	"time"

	"github.com/anthnel/devdesk/internal/netcheck"
	sdk "github.com/modelcontextprotocol/go-sdk/mcp"
)

// net_check is the one tool here that touches the network, and §3.38 keeps it
// deliberately: it *reads* the network without changing anything on the host,
// DevDesk already runs it on a keystroke with no confirmation, and its result is
// structured and deterministic — "the name resolves, the port accepts, the chain
// is incomplete at the intermediate" is something a model can act on, where the
// exit status of `curl` is not.
//
// The residual is stated rather than discovered: an agent can point it at any
// host, so it is a probe primitive as much as a diagnostic. That is what
// `mcp.enabled` is the decision about.
//
// It runs no container. The pipeline is pure Go — the route trace is the one
// probe in DevDesk that shells out, and it is not part of this.

const defaultCheckPort = 443

type netCheckIn struct {
	Host     string `json:"host" jsonschema:"the hostname or IP address to check"`
	Port     int    `json:"port,omitempty" jsonschema:"defaults to 443; the TLS stages only mean something on a port that speaks TLS"`
	Resolver string `json:"resolver,omitempty" jsonschema:"a nameserver to query as host or host:port; empty uses the system resolver, which is the answer that usually matters"`
}

type checkFact struct {
	Key   string `json:"key"`
	Value string `json:"value"`
}

type checkOut struct {
	ID      string `json:"id"`
	Stage   string `json:"stage" jsonschema:"resolve, reach, connect, tls or http"`
	Title   string `json:"title"`
	Verdict string `json:"verdict" jsonschema:"OK, WARN, FAIL, N/A or UNKNOWN"`

	// Summary states what was observed and never what to do about it; Means and
	// Do are the explanation layer, which is keyed on the check's id and reason.
	// Keeping them apart is what stops one statement of what happened being
	// restated in different words somewhere else.
	Summary string `json:"summary" jsonschema:"what was observed"`
	Means   string `json:"means" jsonschema:"why it matters"`
	Do      string `json:"do,omitempty" jsonschema:"the next action; absent when there is nothing to do, because a passing check that invents one teaches the reader to ignore the field"`

	// Reason discriminates the causes of a verdict that has several: a chain
	// FAIL is three different jobs, and the remedies diverge where the verdict
	// does not. Empty is the common case.
	Reason string `json:"reason,omitempty" jsonschema:"a stable sub-code such as self-signed, no-intermediates, untrusted-root, expired or partial-loss"`

	// Because is set only on a N/A that an upstream failure caused, and it names
	// the check that caused it. An N/A with no Because simply has no meaning
	// here — which is what separates "blocked" from "irrelevant", and an agent
	// reading a wall of N/A needs that distinction most.
	Because string `json:"because,omitempty" jsonschema:"the id of the check whose failure prevented this one; absent when the check is simply irrelevant to this target"`

	Facts []checkFact `json:"facts,omitempty"`
}

type netCheckOut struct {
	Host     string `json:"host"`
	Port     int    `json:"port"`
	Resolver string `json:"resolver,omitempty"`

	Verdict string     `json:"verdict" jsonschema:"the worst verdict among the checks that ran; UNKNOWN when none of them could look"`
	Checks  []checkOut `json:"checks" jsonschema:"in pipeline order: resolution, reachability, the TCP connect, the certificate, then the HTTP response"`
}

func registerNetCheck(s *sdk.Server, env *Env) {
	sdk.AddTool(s, &sdk.Tool{
		Name:        "net_check",
		Description: toolDescription("net_check"),
	}, func(ctx context.Context, _ *sdk.CallToolRequest, in netCheckIn) (*sdk.CallToolResult, netCheckOut, error) {
		target := netcheck.Target{
			Host:     strings.TrimSpace(in.Host),
			Port:     in.Port,
			Resolver: strings.TrimSpace(in.Resolver),
		}
		if target.Port == 0 {
			target.Port = defaultCheckPort
		}
		if err := target.Validate(); err != nil {
			return nil, netCheckOut{}, err
		}

		// The context is the client's, so cancelling the call actually stops the
		// probes. An unreachable host spends one timeout per stage, and the
		// dials are the only thing in this package that can take that long.
		out, err := runCheck(ctx, target, env)
		if err != nil {
			return nil, netCheckOut{}, err
		}
		return nil, out, nil
	})
}

// runCheck runs the pipeline with the served context's own dials.
//
// The settings come from `network:` rather than from a tool argument: they are
// the operator's calibration of this machine and this network — how long a probe
// waits, how many echo requests to send, how close to expiry counts as close —
// and a caller that could override them could also make the server hammer a host
// or hang on one. Normalized() fills in anything non-positive, so a hand-edited
// zero becomes the default rather than a dial with no deadline.
func runCheck(ctx context.Context, target netcheck.Target, env *Env) (netCheckOut, error) {
	settings := checkSettings(env)

	results, err := netcheck.Run(ctx, target, checkEnv(settings), settings)
	if err != nil {
		return netCheckOut{}, err
	}

	checks := results.All()
	out := netCheckOut{
		Host:     target.Host,
		Port:     target.Port,
		Resolver: target.Resolver,
		Verdict:  netcheck.Summarize(checks).String(),
		Checks:   make([]checkOut, 0, len(checks)),
	}

	for _, c := range checks {
		explanation := netcheck.Explain(c)
		entry := checkOut{
			ID:      string(c.ID),
			Stage:   string(c.Stage),
			Title:   c.Title,
			Verdict: c.Verdict.String(),
			Summary: explanation.Observed,
			Means:   explanation.Means,
			Do:      explanation.Do,
			Reason:  string(c.Reason),
			Because: string(c.Because),
		}
		for _, fact := range c.Facts {
			entry.Facts = append(entry.Facts, checkFact{Key: fact.Key, Value: fact.Value})
		}
		out.Checks = append(out.Checks, entry)
	}

	return out, nil
}

// checkEnv is netcheck.SystemEnv, indirected for the tests and nothing else —
// production never reassigns it.
//
// The seam is here rather than in netcheck, which deliberately takes its Env as
// a parameter so a fake is per-call and the package stays concurrency-safe. This
// tool builds the production one itself, so a test that must not touch the
// network has nowhere else to intervene.
var checkEnv = netcheck.SystemEnv

// checkSettings reads the context's network dials.
func checkSettings(env *Env) netcheck.Settings {
	network := env.Config.Network
	return netcheck.Settings{
		CheckTimeout:     time.Duration(network.CheckTimeout) * time.Second,
		PingCount:        network.PingCount,
		ExpiryWarnWindow: time.Duration(network.CertExpiryWarnDays) * 24 * time.Hour,
	}.Normalized()
}
