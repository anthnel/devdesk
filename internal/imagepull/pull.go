// Package imagepull is the one way DevDesk pulls an image (§3.82): resolve the
// tag to a digest, verify that digest's signature, pull the digest, then name
// it the way it was asked for.
//
// Every pull goes through Pull — the registry browser, G, the MCP — and a test
// holds that no other package calls docker.PullImageContext. A second path
// would be a pull nobody verified.
package imagepull

import (
	"context"
	"fmt"
	"log"
	"strings"

	"github.com/anthnel/devdesk/internal/trust"
)

// Deps are the calls a pull makes, injected so the sequence runs without an
// engine, a registry or cosign.
type Deps struct {
	// Enabled is scan.image_verification. Off pulls the tag as it always did.
	Enabled bool
	// Policy loads ~/.devdesk/trust.yaml. Read on every pull, so an edit takes
	// effect without restarting.
	Policy func() (trust.Policy, error)
	// Verifier checks a signature — cosign, cached.
	Verifier trust.Verifier
	// Digest is the digest the registry says ref's tag points to now.
	Digest func(ref string) (string, error)
	// Current is the local image ref names, pinned by digest ("" when there is
	// none): the image continuity compares with.
	Current func(ref string) string
	// Pull fetches a reference; Tag names source as target.
	Pull func(ctx context.Context, ref string) error
	Tag  func(source, target string) error
}

// BlockedError is a pull refused because of what the signature check found.
// Its message is the Result's reason, which names the rule.
type BlockedError struct{ Result trust.Result }

func (e *BlockedError) Error() string { return e.Result.Reason() }

// Pull verifies, then pulls. The Result says what the check found; a Warn is
// carried there, for the caller to show. An error is either a *BlockedError or
// the engine's own failure.
func Pull(ctx context.Context, ref string, d Deps) (trust.Result, error) {
	if !d.Enabled {
		return trust.Result{}, d.Pull(ctx, ref)
	}
	policy, err := d.Policy()
	if err != nil {
		// A policy that cannot be read is not an empty one: the rules it holds
		// would be dropped in silence, which is the weakening the strict parse
		// exists to prevent. Refused, naming the file.
		res := trust.Result{Verdict: trust.Failed, Decision: trust.Block, Err: err,
			Rule: trust.Rule{Source: trust.SourceUser, Origin: "trust.yaml is invalid: " + err.Error()}}
		return res, &BlockedError{Result: res}
	}

	pinned := ref
	if trust.Digest(ref) == "" {
		digest, err := d.Digest(ref)
		if err != nil {
			return pullUnresolved(ctx, ref, policy, err, d)
		}
		pinned = trust.Repository(ref) + "@" + digest
	}

	res := trust.Evaluate(ctx, policy, d.Verifier, pinned, d.Current(ref))
	if res.Err != nil {
		log.Printf("ERROR [imagepull] verify %s: %v", pinned, res.Err)
	}
	if res.Decision == trust.Block {
		return res, &BlockedError{Result: res}
	}
	if err := d.Pull(ctx, pinned); err != nil {
		return res, err
	}
	if pinned == ref {
		return res, nil
	}
	// The digest is pulled nameless; the tag it was asked for points at it
	// only once named.
	return res, d.Tag(pinned, ref)
}

// pullUnresolved is a tag whose digest the registry did not give. Nothing can
// be verified — so it is a Failed verdict, decided by the rule that would have
// applied: a user rule refuses, anything else pulls the tag and warns.
func pullUnresolved(ctx context.Context, ref string, policy trust.Policy, cause error, d Deps) (trust.Result, error) {
	log.Printf("ERROR [imagepull] resolve %s: %v", ref, cause)
	res := trust.Result{Verdict: trust.Failed, Decision: trust.Decide(trust.Failed, trust.SourceContinuity),
		Err: fmt.Errorf("resolve %s: %w", ref, cause)}
	if rule, ok := policy.Lookup(ref); ok {
		if rule.Mode == trust.ModeNone {
			return trust.Result{Rule: rule}, d.Pull(ctx, ref)
		}
		res.Rule, res.Decision = rule, trust.Decide(trust.Failed, rule.Source)
	}
	if res.Decision == trust.Block {
		return res, &BlockedError{Result: res}
	}
	return res, d.Pull(ctx, ref)
}

// localDigest picks, among an image's repo digests, the one of ref's own
// repository — an image pulled under two names carries both.
func localDigest(ref string, repoDigests []string) string {
	repo := trust.Repository(ref)
	for _, rd := range repoDigests {
		name, digest, ok := strings.Cut(rd, "@")
		if ok && trust.Repository(name) == repo {
			return repo + "@" + digest
		}
	}
	return ""
}
