package trust

import (
	"context"
	"path/filepath"
	"testing"
	"time"
)

type countingVerifier struct {
	verdict Verdict
	calls   int
}

func (c *countingVerifier) Verify(context.Context, string, Rule) (Verdict, error) {
	c.calls++
	return c.verdict, nil
}

func (c *countingVerifier) Identities(context.Context, string) ([]Identity, error) { return nil, nil }

func cachedAt(t *testing.T, v Verdict) (*countingVerifier, Verifier, *time.Time) {
	t.Helper()
	inner := &countingVerifier{verdict: v}
	now := time.Date(2026, 9, 25, 12, 0, 0, 0, time.UTC)
	store := FileStore{Path: filepath.Join(t.TempDir(), "signature-verdicts.json")}
	return inner, Cached(inner, store, func() time.Time { return now }), &now
}

var cacheRule = Rule{Mode: ModeKeyless, Issuer: "i", Subject: "s"}

func TestAVerifiedDigestIsNotAskedAgainForADay(t *testing.T) {
	inner, v, now := cachedAt(t, Verified)
	ctx := context.Background()
	_, _ = v.Verify(ctx, candidate, cacheRule)
	*now = now.Add(23 * time.Hour)
	if got, _ := v.Verify(ctx, candidate, cacheRule); got != Verified || inner.calls != 1 {
		t.Errorf("verdict %v after %d calls, want the stored one after 1", got, inner.calls)
	}
	*now = now.Add(2 * time.Hour)
	_, _ = v.Verify(ctx, candidate, cacheRule)
	if inner.calls != 2 {
		t.Errorf("calls = %d: a day-old verdict must be asked again", inner.calls)
	}
}

func TestUnsignedIsAskedAgainSooner(t *testing.T) {
	inner, v, now := cachedAt(t, Unsigned)
	ctx := context.Background()
	_, _ = v.Verify(ctx, candidate, cacheRule)
	*now = now.Add(7 * time.Hour)
	_, _ = v.Verify(ctx, candidate, cacheRule)
	if inner.calls != 2 {
		t.Errorf("calls = %d: a signature may have been added since", inner.calls)
	}
}

// A block under a user rule must not outlast the network coming back.
func TestAFailureIsNeverStored(t *testing.T) {
	inner, v, _ := cachedAt(t, Failed)
	ctx := context.Background()
	_, _ = v.Verify(ctx, candidate, cacheRule)
	_, _ = v.Verify(ctx, candidate, cacheRule)
	if inner.calls != 2 {
		t.Errorf("calls = %d, want every failure asked again", inner.calls)
	}
}

func TestEditingTheRuleMissesTheCache(t *testing.T) {
	inner, v, _ := cachedAt(t, Verified)
	ctx := context.Background()
	_, _ = v.Verify(ctx, candidate, cacheRule)
	other := cacheRule
	other.Subject = "someone-else"
	_, _ = v.Verify(ctx, candidate, other)
	if inner.calls != 2 {
		t.Errorf("calls = %d: a verdict answered one identity, not another", inner.calls)
	}
	// Moving the rule or renaming its file is not editing what it asks.
	moved := cacheRule
	moved.Origin, moved.Match = "elsewhere:9", "x/*"
	_, _ = v.Verify(ctx, candidate, moved)
	if inner.calls != 2 {
		t.Errorf("calls = %d: the origin is not part of the question", inner.calls)
	}
}

func TestTheStoreSurvivesARestart(t *testing.T) {
	path := filepath.Join(t.TempDir(), "signature-verdicts.json")
	at := time.Date(2026, 9, 25, 12, 0, 0, 0, time.UTC)
	if err := (FileStore{Path: path}).Set("k", IdentityMismatch, at); err != nil {
		t.Fatal(err)
	}
	v, got, ok := FileStore{Path: path}.Get("k")
	if !ok || v != IdentityMismatch || !got.Equal(at) {
		t.Errorf("Get = %v, %v, %v", v, got, ok)
	}
}
