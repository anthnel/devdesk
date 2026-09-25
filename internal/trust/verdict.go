package trust

// Verdict is what a verification found about one image.
type Verdict int

const (
	// NoPolicy is the absence of a question: no rule, no signature on the image
	// in use to compare with, or `expect: none`. Not a no — nothing to say.
	NoPolicy Verdict = iota
	// Verified is the expected identity or key having signed this digest.
	Verified
	// IdentityMismatch is a valid signature by someone else — the one verdict
	// that says the content was replaced, whoever declared the rule.
	IdentityMismatch
	// Unsigned is no signature — or, in key mode, none the expected key
	// verifies. Under a rule it is what a republished tag looks like: its new
	// digest carries none of the old signatures.
	Unsigned
	// Failed is a question that could not be asked: registry, blob store or
	// Sigstore unreachable, cosign missing, a timeout.
	Failed
)

func (v Verdict) String() string {
	switch v {
	case Verified:
		return "verified"
	case IdentityMismatch:
		return "identity-mismatch"
	case Unsigned:
		return "unsigned"
	case Failed:
		return "failed"
	}
	return "no-policy"
}

// parseVerdict reads String back, for the cache. Anything else is NoPolicy,
// which is never stored: an unreadable entry is a miss.
func parseVerdict(s string) Verdict {
	for _, v := range []Verdict{Verified, IdentityMismatch, Unsigned, Failed} {
		if v.String() == s {
			return v
		}
	}
	return NoPolicy
}

// Decision is what a verdict does to the action asking for it.
type Decision int

const (
	// Allow lets the pull or the choice go ahead.
	Allow Decision = iota
	// Warn lets it go ahead, and says why it might not have.
	Warn
	// Block refuses it, naming the rule.
	Block
)

// Decide is §3.82's table, and the only copy of it: the pull, the remediation
// tab and the scan finding all read it.
//
//	verdict            user (C)  built-in (B)  continuity (A)
//	IdentityMismatch   block     block         block
//	Unsigned           block     block         warn
//	Failed             block     warn          warn
//
// A rule someone wrote asks for a guarantee, so not knowing blocks there. A
// built-in rule is DevDesk's declaration, not the user's: a proxy that filters
// Sigstore must not stop every Chainguard pull of someone who configured
// nothing. Continuity declares nothing at all, and a publisher may stop signing.
//
// Key mode never reaches Failed through an exit code — the verifier classes it
// Unsigned, fail-closed (§3.82) — so a built-in key rule blocks where a keyless
// one would warn. That is the one deliberate exception to the table, and it
// lives in the verifier, not here.
func Decide(v Verdict, s Source) Decision {
	switch v {
	case IdentityMismatch:
		return Block
	case Unsigned:
		if s == SourceContinuity {
			return Warn
		}
		return Block
	case Failed:
		if s == SourceUser {
			return Block
		}
		return Warn
	}
	return Allow
}
