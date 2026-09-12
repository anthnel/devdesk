// Package netcheck answers one question about one target — is this host
// reachable, and is its TLS chain sound — and reports it as a list of checks
// carrying verdicts.
//
// A check is a question that got an answer, not a binary that ran. The
// distinction is the point: one handshake produces four checks (chain,
// hostname, expiry, version), and reachability is answered by two probes. A
// row named after a tool cannot express either, which is why the exit code of
// `nc` and the exit code of `curl` used to render as the same red FAIL.
//
// Nothing here shells out. DNS, ICMP, TCP, TLS and HTTP are answered from this
// process, so they answer for the machine DevDesk runs on. The Docker runner
// they replaced used --network host, which on Docker Desktop is the
// docker-desktop VM's namespace — a different resolver, a different routing
// table, and no knowledge of a VPN the user is actually on.
package netcheck

import (
	"fmt"
	"strings"
	"time"
)

// Verdict is what a check concluded.
//
// The zero value is Unknown, deliberately: a check that was never filled in
// must not read as a pass. That is the same reasoning behind
// scan.Result.SecretVerdict returning a *bool — "nobody looked" displayed as a
// clean result is a defect, not a nuance.
type Verdict int

const (
	// Unknown means the check could not look. It is not a failure and not a
	// pass; it is the absence of an observation.
	Unknown Verdict = iota
	// NotApplicable means the check has no meaning for this target, or an
	// upstream failure prevented it. The two are told apart by Check.Because.
	NotApplicable
	// OK means the objective is met.
	OK
	// Warn means the objective is met but something about it is off.
	Warn
	// Fail means the objective is not met.
	Fail
)

// String implements fmt.Stringer. The words are what the UI shows, so they are
// English US (Rule 129) and short enough for a table cell.
func (v Verdict) String() string {
	switch v {
	case NotApplicable:
		return "N/A"
	case OK:
		return "OK"
	case Warn:
		return "WARN"
	case Fail:
		return "FAIL"
	default:
		return "UNKNOWN"
	}
}

// severity ranks verdicts for Summarize. Fail outranks Unknown because a known
// failure is more actionable than an absent observation; Unknown outranks Warn
// because not having looked is worse than having looked and found a blemish.
// NotApplicable ranks lowest: it never colours a headline.
func (v Verdict) severity() int {
	switch v {
	case Fail:
		return 4
	case Unknown:
		return 3
	case Warn:
		return 2
	case OK:
		return 1
	default:
		return 0
	}
}

// CheckID identifies one question. It is stable: the explanation table in PR 2
// and any serialization are keyed on it.
type CheckID string

const (
	CheckResolve      CheckID = "resolve"
	CheckReverseDNS   CheckID = "reverse-dns"
	CheckRoute        CheckID = "route"
	CheckICMP         CheckID = "icmp"
	CheckTCP          CheckID = "tcp"
	CheckTLSHandshake CheckID = "tls-handshake"
	CheckTLSChain     CheckID = "tls-chain"
	CheckTLSHostname  CheckID = "tls-hostname"
	CheckTLSExpiry    CheckID = "tls-expiry"
	CheckTLSVersion   CheckID = "tls-version"
	CheckHTTP         CheckID = "http"
)

// StageID identifies a layer of the pipeline.
type StageID string

const (
	StageResolve StageID = "resolve"
	StageRoute   StageID = "route"
	StageReach   StageID = "reach"
	StageConnect StageID = "connect"
	StageTLS     StageID = "tls"
	StageHTTP    StageID = "http"
)

// Fact is one observed value, shown in a check's detail pane.
type Fact struct {
	Key   string
	Value string
}

// Phase is one named, non-overlapping span within a check's Duration — a
// waterfall row. Phases are sequential and sum to Duration; a check with
// nothing to break down (most of them: one dial, one handshake) leaves this
// nil rather than a single phase covering the whole thing, which would say
// nothing a bar chart couldn't already say better as a number.
type Phase struct {
	Name     string
	Duration time.Duration
}

// Check is one answered question.
type Check struct {
	ID      CheckID
	Stage   StageID
	Title   string
	Verdict Verdict
	// Summary is the one line the results table shows. It states what was
	// observed, never what to do about it — the advice belongs to the
	// explanation layer, which is keyed on ID.
	Summary string
	Facts   []Fact
	// Duration is how long the observation behind this check took, when
	// timed. Zero means untimed — a check that answers instantly (DNS
	// resolution) or one nothing has instrumented yet.
	Duration time.Duration
	// Phases breaks Duration down into a waterfall, when there is more than
	// one span worth showing separately (today: the HTTP check's DNS,
	// connect, TLS, server wait and content transfer). Nil elsewhere.
	Phases []Phase
	// Because is set only when Verdict is NotApplicable *and* the cause was an
	// upstream failure. Empty on a NotApplicable that simply has no meaning
	// here, which is what separates "blocked" from "irrelevant".
	Because CheckID
	// Reason is a stable sub-code for a verdict that has more than one cause.
	//
	// It exists because the remedies differ where the verdict does not: a chain
	// that fails because the leaf is self-signed, because the server sent no
	// intermediates, and because the root is unknown are one Fail with three
	// different things to go and do. Summary already says which in prose;
	// Reason is what an explanation — or a serialization — can switch on.
	//
	// Empty is the common case: most verdicts have exactly one cause.
	Reason Reason
}

// Reason discriminates the causes of a verdict that has several. It is stable:
// the guidance table is keyed on it.
type Reason string

const (
	// DNS resolution
	//
	// "Server misbehaving" is Go's net.DNSError wording for a resolver that
	// answered but with something the client could not use — not a timeout,
	// not NXDOMAIN. It is worth telling apart because the remedy is neither
	// "check the spelling" nor "check the network": it is almost always the
	// resolver itself, commonly a router's built-in DNS proxy choking on the
	// EDNS0 queries Go's resolver sends (which this pipeline must use to
	// reach a resolver the user names — see resolverFor in env.go).
	ReasonServerMisbehaving Reason = "server-misbehaving"

	// Certificate chain
	ReasonSelfSigned      Reason = "self-signed"
	ReasonNoIntermediates Reason = "no-intermediates"
	ReasonUntrustedRoot   Reason = "untrusted-root"
	ReasonChainIncomplete Reason = "chain-incomplete"

	// Certificate validity
	ReasonExpired     Reason = "expired"
	ReasonNotYetValid Reason = "not-yet-valid"

	// ICMP
	ReasonNoReply     Reason = "no-reply"
	ReasonPartialLoss Reason = "partial-loss"

	// TLS handshake
	ReasonNotTLS Reason = "not-tls"

	// Local route
	ReasonPartialRoute Reason = "partial-route"
)

// fact appends an observed value, skipping empty ones so a detail pane never
// shows a key with nothing beside it.
func (c *Check) fact(key, value string) {
	if strings.TrimSpace(value) == "" {
		return
	}
	c.Facts = append(c.Facts, Fact{Key: key, Value: value})
}

// Target is what is being checked.
type Target struct {
	Host string
	Port int
	// Resolver is the nameserver to query, "host" or "host:port". Empty means
	// the system resolver — which is the answer the user actually cares about,
	// so it is the default rather than a fallback.
	Resolver string
}

// Addr returns the host:port form used to dial.
func (t Target) Addr() string {
	return fmt.Sprintf("%s:%d", t.Host, t.Port)
}

// Validate rejects a target the stages cannot act on. The view validates too,
// but this is a package boundary and external data is not trusted at one.
func (t Target) Validate() error {
	if strings.TrimSpace(t.Host) == "" {
		return fmt.Errorf("target is required")
	}
	if t.Port < 1 || t.Port > 65535 {
		return fmt.Errorf("port must be a number between 1 and 65535")
	}
	return nil
}

// Results holds the checks a run produced, in pipeline order.
//
// It is a type rather than a []Check because a stage needs to read what came
// before it — the HTTP stage picks its scheme from the handshake's outcome —
// and because the order is meaningful and must not be re-derived by a caller.
type Results struct {
	checks []Check
	index  map[CheckID]int
}

func (r *Results) add(c Check) {
	if r.index == nil {
		r.index = make(map[CheckID]int)
	}
	if i, ok := r.index[c.ID]; ok {
		r.checks[i] = c
		return
	}
	r.index[c.ID] = len(r.checks)
	r.checks = append(r.checks, c)
}

// clone returns a deep-enough copy: the slice and the index are fresh, so
// adding to the copy cannot write into the original's backing array. It is what
// makes RunStep safe to call on a goroutine while the caller still holds the
// previous value.
func (r Results) clone() Results {
	out := Results{
		checks: make([]Check, len(r.checks)),
		index:  make(map[CheckID]int, len(r.index)),
	}
	copy(out.checks, r.checks)
	for k, v := range r.index {
		out.index[k] = v
	}
	return out
}

// ResultsOf assembles a Results from checks that are already known.
//
// It exists for the callers that receive checks rather than produce them: a
// view driving the pipeline through messages, a test describing a situation,
// and — later — anything reading a serialized run back.
func ResultsOf(checks ...Check) Results {
	var r Results
	for _, c := range checks {
		r.add(c)
	}
	return r
}

// All returns the checks in pipeline order.
func (r Results) All() []Check { return r.checks }

// Get returns one check by ID.
func (r Results) Get(id CheckID) (Check, bool) {
	i, ok := r.index[id]
	if !ok {
		return Check{}, false
	}
	return r.checks[i], true
}

// VerdictOf returns a check's verdict, or Unknown when it was never produced —
// which is the honest answer for a question nobody asked.
func (r Results) VerdictOf(id CheckID) Verdict {
	c, ok := r.Get(id)
	if !ok {
		return Unknown
	}
	return c.Verdict
}

// Summarize folds the checks into the one verdict the header shows.
//
// It is the only thing that computes an aggregate. scan.Categorize and
// SecretVerdict both exist because two rules answered one question and
// disagreed; the symptom was identical each time — something counted in one
// place and absent from another.
func Summarize(checks []Check) Verdict {
	worst := OK
	seen := false
	for _, c := range checks {
		if c.Verdict == NotApplicable {
			continue
		}
		seen = true
		if c.Verdict.severity() > worst.severity() {
			worst = c.Verdict
		}
	}
	if !seen {
		return Unknown
	}
	return worst
}
