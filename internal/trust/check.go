package trust

import (
	"context"
	"fmt"
	"log"
)

// CheckDeps are what a check needs beyond this package: the user's policy, a
// verifier, and the registry's answer for a tag. Injected, so this package
// stays free of the engine, the registry client and cosign.
type CheckDeps struct {
	// Policy loads ~/.devdesk/trust.yaml; read on every check, so an edit takes
	// effect without restarting.
	Policy func() (Policy, error)
	// Verifier checks a signature — cosign, cached.
	Verifier Verifier
	// Digest is the digest the registry says ref's tag points to now.
	Digest func(ref string) (string, error)
}

// Check answers for ref, as a tag or pinned. current is the image ref would
// replace, for continuity — pinned, or a tag resolved here to what it points to
// now; "" for none.
//
// pinned is the digest the verdict is about, "" when the registry would not
// give one: the pull then knows it has nothing verified to pull.
func Check(ctx context.Context, ref, current string, d CheckDeps) (res Result, pinned string) {
	policy, err := d.Policy()
	if err != nil {
		// A policy that cannot be read is not an empty one: the rules it holds
		// would be dropped in silence, which is the weakening the strict parse
		// exists to prevent. Refused, naming the file.
		return Result{Verdict: Failed, Decision: Block, Err: err,
			Rule: Rule{Source: SourceUser, Origin: "trust.yaml is invalid: " + err.Error()}}, ""
	}
	pinned = ref
	if Digest(ref) == "" {
		digest, err := d.Digest(ref)
		if err != nil {
			return unresolved(ref, policy, err), ""
		}
		pinned = Repository(ref) + "@" + digest
	}
	if current != "" && Digest(current) == "" {
		// Continuity compares with what the tag in use points to now; a
		// registry that will not say leaves nothing to continue.
		current = pin(current, d)
	}
	res = Evaluate(ctx, policy, d.Verifier, pinned, current)
	if res.Err != nil {
		log.Printf("ERROR [trust] verify %s: %v", pinned, res.Err)
	}
	return res, pinned
}

// unresolved is a tag whose digest the registry did not give. Nothing can be
// verified — so it is a Failed verdict, decided by the rule that would have
// applied: a user rule refuses, anything else goes ahead and warns.
func unresolved(ref string, policy Policy, cause error) Result {
	log.Printf("ERROR [trust] resolve %s: %v", ref, cause)
	res := Result{Verdict: Failed, Decision: Decide(Failed, SourceContinuity),
		Err: fmt.Errorf("resolve %s: %w", ref, cause)}
	if rule, ok := policy.Lookup(ref); ok {
		if rule.Mode == ModeNone {
			return Result{Rule: rule}
		}
		res.Rule, res.Decision = rule, Decide(Failed, rule.Source)
	}
	return res
}

// pin resolves a tag to its registry digest, "" when the registry will not say.
func pin(ref string, d CheckDeps) string {
	digest, err := d.Digest(ref)
	if err != nil {
		return ""
	}
	return Repository(ref) + "@" + digest
}
