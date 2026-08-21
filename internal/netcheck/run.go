package netcheck

import (
	"context"
	"errors"
)

// errNotTLSConn is returned when a dialer hands back something that is not a
// TLS connection. It cannot happen with tls.Dialer, and is reported rather
// than asserted so a future dialer swap fails loudly.
var errNotTLSConn = errors.New("connection is not a TLS connection")

// checkTitles is the one place a check's human name is written. The skip path
// needs a title for a check that never ran, so it cannot come from the stage
// that would have produced it.
var checkTitles = map[CheckID]string{
	CheckResolve:      "DNS resolution",
	CheckReverseDNS:   "Reverse DNS",
	CheckICMP:         "ICMP echo",
	CheckTCP:          "TCP connect",
	CheckTLSHandshake: "TLS handshake",
	CheckTLSChain:     "Certificate chain",
	CheckTLSHostname:  "Hostname match",
	CheckTLSExpiry:    "Certificate expiry",
	CheckTLSVersion:   "TLS version",
	CheckHTTP:         "HTTP response",
}

// stage is one layer of the pipeline.
type stage struct {
	id        StageID
	dependsOn StageID
	// gate is the check whose verdict decides whether dependent stages run.
	// A stage that gates nothing leaves it empty — which is a decision, not an
	// omission, at both sites that use it.
	gate CheckID
	// produces is what run returns, declared so the skip path can name the
	// checks that never ran. TestEveryStageProducesWhatItDeclares keeps the two
	// in step.
	produces []CheckID
	run      func(ctx context.Context, t Target, env Env, prior *Results) []Check
}

// stages is the pipeline, in order.
//
// Two of the dependencies are worth reading twice:
//
//   - reach gates nothing. ICMP is filtered on a large share of hosts that are
//     perfectly reachable, so a silent ping must not cascade NotApplicable over
//     the TLS checks — which is the whole question the user came to ask.
//   - http depends on connect, not on tls. A broken chain must not hide whether
//     the service answers: "the app works, the certificate is what is wrong" is
//     useful, and the certificate problem has rows of its own.
func stages() []stage {
	return []stage{
		{
			id:       StageResolve,
			gate:     CheckResolve,
			produces: []CheckID{CheckResolve, CheckReverseDNS},
			run:      runResolve,
		},
		{
			id:        StageReach,
			dependsOn: StageResolve,
			produces:  []CheckID{CheckICMP},
			run:       runReach,
		},
		{
			id:        StageConnect,
			dependsOn: StageResolve,
			gate:      CheckTCP,
			produces:  []CheckID{CheckTCP},
			run:       runConnect,
		},
		{
			id:        StageTLS,
			dependsOn: StageConnect,
			produces: []CheckID{
				CheckTLSHandshake, CheckTLSChain,
				CheckTLSHostname, CheckTLSExpiry, CheckTLSVersion,
			},
			run: runTLS,
		},
		{
			id:        StageHTTP,
			dependsOn: StageConnect,
			produces:  []CheckID{CheckHTTP},
			run:       runHTTP,
		},
	}
}

// Run walks the pipeline and returns every check, in order.
//
// A stage whose dependency failed does not run: its checks come back
// NotApplicable carrying the check that blocked them. That is what turns seven
// red rows into one red row and a named point of rupture.
func Run(ctx context.Context, t Target, env Env) (Results, error) {
	var res Results
	if err := t.Validate(); err != nil {
		return res, err
	}

	all := stages()
	gateOf := make(map[StageID]CheckID, len(all))
	for _, s := range all {
		if s.gate != "" {
			gateOf[s.id] = s.gate
		}
	}

	for _, s := range all {
		if ctx.Err() != nil {
			addPlaceholders(&res, s, Unknown, "", "Cancelled before this check ran")
			continue
		}
		if blocker, ok := blockedBy(s, gateOf, res); ok {
			summary := "Skipped — " + checkTitles[blocker] + " did not succeed"
			addPlaceholders(&res, s, NotApplicable, blocker, summary)
			continue
		}
		for _, c := range s.run(ctx, t, env, &res) {
			res.add(c)
		}
	}
	return res, nil
}

// blockedBy reports whether s must be skipped, and which check blocked it.
//
// Fail and Unknown block. NotApplicable blocks only when it was itself caused
// by an upstream failure — which is precisely what Check.Because distinguishes,
// and the reason it is a field rather than a flag:
//
//   - a literal IP address makes the resolution check NotApplicable with no
//     Because, and everything below it is perfectly meaningful;
//   - a resolution that failed makes the TCP check NotApplicable *because* of
//     it, and the TLS checks below are just as unanswerable.
//
// The original cause is passed along rather than the immediate one. "Blocked by
// DNS resolution" is actionable; "blocked by a check that was itself blocked"
// sends the user hopping between rows to find the one thing that broke.
func blockedBy(s stage, gateOf map[StageID]CheckID, res Results) (CheckID, bool) {
	if s.dependsOn == "" {
		return "", false
	}
	gate, ok := gateOf[s.dependsOn]
	if !ok {
		return "", false
	}
	switch res.VerdictOf(gate) {
	case Fail, Unknown:
		return gate, true
	case NotApplicable:
		if c, found := res.Get(gate); found && c.Because != "" {
			return c.Because, true
		}
		return "", false
	default:
		return "", false
	}
}

// addPlaceholders records the checks a skipped stage would have produced, so a
// question that was never asked is visible as such rather than absent.
func addPlaceholders(res *Results, s stage, v Verdict, because CheckID, summary string) {
	for _, id := range s.produces {
		res.add(Check{
			ID:      id,
			Stage:   s.id,
			Title:   checkTitles[id],
			Verdict: v,
			Summary: summary,
			Because: because,
		})
	}
}

// newCheck starts a check with its declared title, so no stage spells one out.
func newCheck(id CheckID, s StageID, v Verdict, summary string) Check {
	return Check{ID: id, Stage: s, Title: checkTitles[id], Verdict: v, Summary: summary}
}
