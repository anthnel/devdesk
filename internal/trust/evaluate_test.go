package trust

import (
	"context"
	"errors"
	"strings"
	"testing"
)

// fakeVerifier answers from a table keyed by reference and identity subject
// (or "key" for key mode), and records every question.
type fakeVerifier struct {
	verdicts   map[string]Verdict
	identities map[string][]Identity
	identErr   error
	asked      []string
}

func (f *fakeVerifier) Verify(_ context.Context, ref string, rule Rule) (Verdict, error) {
	who := rule.Subject
	if rule.Mode == ModeKey {
		who = "key"
	}
	f.asked = append(f.asked, ref+" "+who)
	v, ok := f.verdicts[ref+" "+who]
	if !ok {
		return Unsigned, nil
	}
	return v, nil
}

func (f *fakeVerifier) Identities(_ context.Context, ref string) ([]Identity, error) {
	return f.identities[ref], f.identErr
}

const (
	current   = "registry.example/app@sha256:old"
	candidate = "registry.example/app@sha256:new"
)

func TestAnUnpinnedReferenceIsNeverVerified(t *testing.T) {
	f := &fakeVerifier{}
	r := Evaluate(context.Background(), Policy{}, f, "gcr.io/distroless/static:nonroot", "")
	if r.Verdict != Failed || !errors.Is(r.Err, ErrNotPinned) {
		t.Errorf("result = %+v", r)
	}
	if len(f.asked) != 0 {
		t.Errorf("a tag was verified: %v — it can move between the check and the pull", f.asked)
	}
}

func TestExpectNoneAsksNothing(t *testing.T) {
	f := &fakeVerifier{}
	p := Policy{Rules: []Rule{{Match: "registry.example/*", Mode: ModeNone, Source: SourceUser}}}
	r := Evaluate(context.Background(), p, f, candidate, current)
	if r.Verdict != NoPolicy || r.Decision != Allow || len(f.asked) != 0 {
		t.Errorf("result = %+v, asked %v", r, f.asked)
	}
}

func TestAUserRuleDecidesWithTheUserSource(t *testing.T) {
	f := &fakeVerifier{}
	p := Policy{Rules: []Rule{{Match: "registry.example/*", Mode: ModeKey, Source: SourceUser, Origin: "trust.yaml:3"}}}
	r := Evaluate(context.Background(), p, f, candidate, current)
	if r.Verdict != Unsigned || r.Decision != Block {
		t.Errorf("result = %+v", r)
	}
	if want := "No verifiable signature from the expected key — trust.yaml:3"; r.Reason() != want {
		t.Errorf("reason = %q, want %q", r.Reason(), want)
	}
	// A rule is looked up before continuity: the image in use is not asked.
	for _, q := range f.asked {
		if strings.HasPrefix(q, current) {
			t.Errorf("continuity ran under a rule: %v", f.asked)
		}
	}
}

func TestNoRuleAndNothingInUseIsNoPolicy(t *testing.T) {
	r := Evaluate(context.Background(), Policy{}, &fakeVerifier{}, candidate, "")
	if r.Verdict != NoPolicy || r.Decision != Allow {
		t.Errorf("result = %+v", r)
	}
}

// The attack continuity must not fall for: the image in use carries a bundle
// whose certificate names the expected identity but does not verify.
func TestContinuityNeverBelievesAClaimedIdentity(t *testing.T) {
	forged := Identity{Issuer: "https://token.actions.githubusercontent.com", Subject: "trusted-release"}
	f := &fakeVerifier{
		identities: map[string][]Identity{current: {forged}},
		verdicts: map[string]Verdict{
			current + " trusted-release":   IdentityMismatch, // the claim does not prove out
			candidate + " trusted-release": Verified,
		},
	}
	r := Evaluate(context.Background(), Policy{}, f, candidate, current)
	if r.Verdict != NoPolicy {
		t.Fatalf("verdict = %v: an unproven claim was used", r.Verdict)
	}
	for _, q := range f.asked {
		if strings.HasPrefix(q, candidate) {
			t.Errorf("the candidate was asked about an identity nobody proved: %v", f.asked)
		}
	}
}

func TestContinuityAsksTheCandidateForTheProvenIdentity(t *testing.T) {
	proven := Identity{Issuer: "https://accounts.google.com", Subject: "release@example"}
	f := &fakeVerifier{
		identities: map[string][]Identity{current: {{Issuer: "x", Subject: "claimed-only"}, proven}},
		verdicts: map[string]Verdict{
			current + " release@example": Verified,
		},
	}
	r := Evaluate(context.Background(), Policy{}, f, candidate, current)
	// The candidate is unsigned: under continuity that warns, it does not block.
	if r.Verdict != Unsigned || r.Decision != Warn || r.Rule.Source != SourceContinuity {
		t.Errorf("result = %+v", r)
	}
	if !strings.Contains(r.Reason(), "continuity with "+current) {
		t.Errorf("reason %q does not say what it continued", r.Reason())
	}
}

func TestContinuityThatCannotReadIdentitiesFails(t *testing.T) {
	f := &fakeVerifier{identErr: errors.New("registry unreachable")}
	r := Evaluate(context.Background(), Policy{}, f, candidate, current)
	if r.Verdict != Failed || r.Decision != Warn {
		t.Errorf("result = %+v", r)
	}
}

func TestAMismatchUnderContinuityBlocks(t *testing.T) {
	proven := Identity{Issuer: "i", Subject: "s"}
	f := &fakeVerifier{
		identities: map[string][]Identity{current: {proven}},
		verdicts: map[string]Verdict{
			current + " s":   Verified,
			candidate + " s": IdentityMismatch,
		},
	}
	if r := Evaluate(context.Background(), Policy{}, f, candidate, current); r.Decision != Block {
		t.Errorf("result = %+v", r)
	}
}

// An image signed by its publisher and by a distributor continues when the
// candidate carries either signature — not only the first one listed.
func TestContinuityAsksEveryProvenIdentity(t *testing.T) {
	f := &fakeVerifier{
		identities: map[string][]Identity{current: {{Issuer: "i", Subject: "distributor"}, {Issuer: "i", Subject: "publisher"}}},
		verdicts: map[string]Verdict{
			current + " distributor":   Verified,
			current + " publisher":     Verified,
			candidate + " distributor": IdentityMismatch,
			candidate + " publisher":   Verified,
		},
	}
	r := Evaluate(context.Background(), Policy{}, f, candidate, current)
	if r.Verdict != Verified || r.Decision != Allow || r.Rule.Subject != "publisher" {
		t.Errorf("result = %+v", r)
	}
}

// Not knowing warns; a mismatch would block. When no identity verifies, a
// check that could not run is what is said.
func TestContinuityPrefersAFailureToAMismatch(t *testing.T) {
	f := &fakeVerifier{
		identities: map[string][]Identity{current: {{Issuer: "i", Subject: "a"}, {Issuer: "i", Subject: "b"}}},
		verdicts: map[string]Verdict{
			current + " a":   Verified,
			current + " b":   Verified,
			candidate + " a": IdentityMismatch,
			candidate + " b": Failed,
		},
	}
	r := Evaluate(context.Background(), Policy{}, f, candidate, current)
	if r.Verdict != Failed || r.Decision != Warn {
		t.Errorf("result = %+v", r)
	}
	f.verdicts[candidate+" b"] = IdentityMismatch
	if r := Evaluate(context.Background(), Policy{}, f, candidate, current); r.Verdict != IdentityMismatch || r.Rule.Subject != "a" {
		t.Errorf("both mismatch: result = %+v, want the first proven identity's", r)
	}
}
