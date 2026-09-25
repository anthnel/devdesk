package trust

import (
	"context"
	"errors"
	"fmt"
)

// Verifier checks a signature. The production one is cosign, run by
// internal/scan; tests give a table.
type Verifier interface {
	// Verify asks whether ref — pinned by digest — is signed the way rule says.
	// The error explains a Failed verdict, for the log; the verdict is the
	// answer. An error beside any other verdict wraps ErrUnproven: the verdict
	// was inferred from a failure the tool cannot tell apart from it, and is
	// neither stored nor stated as a fact.
	Verify(ctx context.Context, ref string, rule Rule) (Verdict, error)
	// Identities returns who the signatures on ref *claim* to be. They are
	// hints and nothing more: a claim is only believed once Verify has proven
	// it (see Continuity).
	Identities(ctx context.Context, ref string) ([]Identity, error)
}

// Result is one image's answer, with what a person needs to act on it.
type Result struct {
	Verdict  Verdict
	Decision Decision
	// Rule is what the verdict was measured against; for continuity, a rule
	// built from the identity of the image in use.
	Rule Rule
	// Err explains a Failed verdict, for the log — or an Unsigned one inferred
	// rather than proven (ErrUnproven).
	Err error
}

// Proven reports a verdict the verifier established, not one it inferred from a
// failure: only a proven verdict is cached or reported as a finding.
func (r Result) Proven() bool { return r.Err == nil }

// ErrNotPinned is a reference with no digest. A tag is only a name: verifying
// it and then using it lets the tag move in between, which is the attack.
var ErrNotPinned = errors.New("reference is not pinned by digest")

// ErrUnproven marks a verdict inferred from a failure: key mode reads every
// exit code it cannot place as Unsigned, fail-closed (§3.82), and a network
// failure exits the same way. The decision holds — it still blocks — but the
// verdict is asked again next time rather than kept, and it is not a finding.
var ErrUnproven = errors.New("verdict inferred, not proven")

// Evaluate answers for candidate, pinned by digest. current is the image in
// use — also pinned — for continuity, "" when there is none.
//
// The rule comes from the policy, then the built-in list; with no rule, the
// candidate must be signed by whoever signed current.
func Evaluate(ctx context.Context, p Policy, v Verifier, candidate, current string) Result {
	if Digest(candidate) == "" {
		return Result{Verdict: Failed, Decision: Decide(Failed, SourceContinuity), Err: ErrNotPinned}
	}
	if rule, ok := p.Lookup(candidate); ok {
		if rule.Mode == ModeNone {
			return Result{Verdict: NoPolicy, Decision: Allow, Rule: rule}
		}
		verdict, err := v.Verify(ctx, candidate, rule)
		return Result{Verdict: verdict, Decision: Decide(verdict, rule.Source), Rule: rule, Err: err}
	}
	if current == "" || Digest(current) == "" {
		return Result{Verdict: NoPolicy, Decision: Allow}
	}
	verdict, rule, err := Continuity(ctx, v, candidate, current)
	return Result{Verdict: verdict, Decision: Decide(verdict, SourceContinuity), Rule: rule, Err: err}
}

// Continuity is A: the candidate must be signed by an identity that signed the
// image in use.
//
// The identity is never read and believed. A permissive check passes as soon
// as *one* signature is valid, so an attacker could attach their own valid
// signature next to a bundle whose forged certificate names the expected
// identity; reading the certificate would then report Verified. So each
// claimed identity is first verified strictly on the image in use, and only an
// identity cosign has proven there is asked of the candidate.
//
// Every proven identity is asked, not only the first: an image signed by its
// publisher and by a distributor continues if the candidate carries either
// signature. When none verifies, a check that could not run outranks a
// mismatch — not knowing warns, where a mismatch would block.
func Continuity(ctx context.Context, v Verifier, candidate, current string) (Verdict, Rule, error) {
	claims, err := v.Identities(ctx, current)
	if err != nil {
		return Failed, Rule{}, fmt.Errorf("identities of %s: %w", current, err)
	}
	answered := false
	var verdict Verdict
	var rule Rule
	var verr error
	for _, id := range claims {
		r := Rule{
			Mode: ModeKeyless, Issuer: id.Issuer, Subject: id.Subject,
			Source: SourceContinuity, Origin: "continuity with " + current,
		}
		proven, err := v.Verify(ctx, current, r)
		if err != nil || proven != Verified {
			continue
		}
		got, err := v.Verify(ctx, candidate, r)
		if got == Verified {
			return Verified, r, err
		}
		if !answered || (got == Failed && verdict != Failed) {
			answered, verdict, rule, verr = true, got, r, err
		}
	}
	if !answered {
		// Nothing on the image in use that could be proven: nothing to continue.
		return NoPolicy, Rule{}, nil
	}
	return verdict, rule, verr
}

// Reason is the sentence a refusal or a warning shows, naming the rule so the
// reader knows what to change. Empty when there is nothing to say.
func (r Result) Reason() string {
	var what string
	switch r.Verdict {
	case IdentityMismatch:
		what = "Signed by an unexpected identity"
	case Unsigned:
		what = "Not signed"
		if r.Rule.Mode == ModeKey {
			what = "No verifiable signature from the expected key"
		}
	case Failed:
		what = "Signature could not be verified"
	default:
		return ""
	}
	if r.Rule.Origin == "" {
		return what
	}
	return what + " — " + r.Rule.Origin
}
