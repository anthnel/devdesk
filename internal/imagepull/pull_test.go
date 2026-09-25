package imagepull

import (
	"context"
	"errors"
	"slices"
	"strings"
	"testing"

	"github.com/anthnel/devdesk/internal/trust"
)

// verifier answers every question with one verdict and records them.
type verifier struct {
	verdict trust.Verdict
	asked   []string
}

func (v *verifier) Verify(_ context.Context, ref string, _ trust.Rule) (trust.Verdict, error) {
	v.asked = append(v.asked, ref)
	return v.verdict, nil
}

func (v *verifier) Identities(context.Context, string) ([]trust.Identity, error) { return nil, nil }

// engine records the engine calls a pull makes.
type engine struct{ calls []string }

func deps(v *verifier, e *engine, rules ...trust.Rule) Deps {
	return Deps{
		Enabled:  true,
		Policy:   func() (trust.Policy, error) { return trust.Policy{Rules: rules}, nil },
		Verifier: v,
		Digest:   func(string) (string, error) { return "sha256:new", nil },
		Current:  func(string) string { return "" },
		Pull: func(_ context.Context, ref string) error {
			e.calls = append(e.calls, "pull "+ref)
			return nil
		},
		Tag: func(source, target string) error {
			e.calls = append(e.calls, "tag "+source+" "+target)
			return nil
		},
	}
}

var userKeyRule = trust.Rule{Match: "registry.corp.example/**", Mode: trust.ModeKey, Source: trust.SourceUser, Origin: "trust.yaml:3"}

// The digest that was verified is the one pulled: pulling the tag would let it
// move in between, which is the attack.
func TestTheVerifiedDigestIsWhatIsPulledThenTagged(t *testing.T) {
	v, e := &verifier{verdict: trust.Verified}, &engine{}
	res, err := Pull(context.Background(), "registry.corp.example/app:1.2", deps(v, e, userKeyRule))
	if err != nil || res.Verdict != trust.Verified {
		t.Fatalf("res %+v, err %v", res, err)
	}
	want := []string{
		"pull registry.corp.example/app@sha256:new",
		"tag registry.corp.example/app@sha256:new registry.corp.example/app:1.2",
	}
	if !slices.Equal(e.calls, want) {
		t.Errorf("engine calls = %q, want %q", e.calls, want)
	}
	if !slices.Equal(v.asked, []string{"registry.corp.example/app@sha256:new"}) {
		t.Errorf("verified %q", v.asked)
	}
}

func TestABlockedPullNeverReachesTheEngine(t *testing.T) {
	v, e := &verifier{verdict: trust.Unsigned}, &engine{}
	res, err := Pull(context.Background(), "registry.corp.example/app:1.2", deps(v, e, userKeyRule))
	var blocked *BlockedError
	if !errors.As(err, &blocked) || res.Decision != trust.Block {
		t.Fatalf("res %+v, err %v", res, err)
	}
	if len(e.calls) != 0 {
		t.Errorf("a refused pull ran: %q", e.calls)
	}
	if !strings.Contains(err.Error(), "trust.yaml:3") {
		t.Errorf("the refusal %q does not name the rule", err)
	}
}

func TestAWarnedPullGoesAheadCarryingTheReason(t *testing.T) {
	// No rule, and a local image whose identity proves out: continuity, whose
	// Unsigned warns.
	v, e := &verifier{verdict: trust.Unsigned}, &engine{}
	d := deps(v, e)
	d.Current = func(string) string { return "registry.example/app@sha256:old" }
	d.Verifier = continuityVerifier{}
	res, err := Pull(context.Background(), "registry.example/app:1.2", d)
	if err != nil || res.Decision != trust.Warn || len(e.calls) != 2 {
		t.Errorf("res %+v, err %v, calls %q", res, err, e.calls)
	}
}

// continuityVerifier proves the image in use and finds the candidate unsigned.
type continuityVerifier struct{}

func (continuityVerifier) Verify(_ context.Context, ref string, _ trust.Rule) (trust.Verdict, error) {
	if strings.HasSuffix(ref, "@sha256:old") {
		return trust.Verified, nil
	}
	return trust.Unsigned, nil
}

func (continuityVerifier) Identities(context.Context, string) ([]trust.Identity, error) {
	return []trust.Identity{{Issuer: "i", Subject: "s"}}, nil
}

func TestOffPullsTheTagAsItAlwaysDid(t *testing.T) {
	v, e := &verifier{}, &engine{}
	d := deps(v, e, userKeyRule)
	d.Enabled = false
	if _, err := Pull(context.Background(), "registry.corp.example/app:1.2", d); err != nil {
		t.Fatal(err)
	}
	if !slices.Equal(e.calls, []string{"pull registry.corp.example/app:1.2"}) || len(v.asked) != 0 {
		t.Errorf("calls %q, asked %q", e.calls, v.asked)
	}
}

func TestAPinnedReferenceIsNeitherResolvedNorRetagged(t *testing.T) {
	v, e := &verifier{verdict: trust.Verified}, &engine{}
	d := deps(v, e, userKeyRule)
	d.Digest = func(string) (string, error) { t.Error("a pinned reference was resolved"); return "", nil }
	if _, err := Pull(context.Background(), "registry.corp.example/app@sha256:abc", d); err != nil {
		t.Fatal(err)
	}
	if !slices.Equal(e.calls, []string{"pull registry.corp.example/app@sha256:abc"}) {
		t.Errorf("calls %q", e.calls)
	}
}

// A policy that cannot be read is not an empty one.
func TestAnInvalidPolicyRefusesThePull(t *testing.T) {
	v, e := &verifier{verdict: trust.Verified}, &engine{}
	d := deps(v, e)
	d.Policy = func() (trust.Policy, error) { return trust.Policy{}, errors.New("line 7: field isuer not found") }
	_, err := Pull(context.Background(), "python:3.12", d)
	var blocked *BlockedError
	if !errors.As(err, &blocked) || len(e.calls) != 0 {
		t.Fatalf("err %v, calls %q", err, e.calls)
	}
	if !strings.Contains(err.Error(), "isuer") {
		t.Errorf("the refusal %q does not say what is wrong with the file", err)
	}
}

func TestADigestTheRegistryWillNotGive(t *testing.T) {
	unresolved := func(d Deps) Deps {
		d.Digest = func(string) (string, error) { return "", errors.New("registry unreachable") }
		return d
	}
	t.Run("under a user rule, refused", func(t *testing.T) {
		e := &engine{}
		_, err := Pull(context.Background(), "registry.corp.example/app:1.2", unresolved(deps(&verifier{}, e, userKeyRule)))
		var blocked *BlockedError
		if !errors.As(err, &blocked) || len(e.calls) != 0 {
			t.Errorf("err %v, calls %q", err, e.calls)
		}
	})
	t.Run("with no rule, the tag is pulled with a warning", func(t *testing.T) {
		e := &engine{}
		res, err := Pull(context.Background(), "python:3.12", unresolved(deps(&verifier{}, e)))
		if err != nil || res.Decision != trust.Warn || !slices.Equal(e.calls, []string{"pull python:3.12"}) {
			t.Errorf("res %+v, err %v, calls %q", res, err, e.calls)
		}
	})
	t.Run("expect none, pulled as asked", func(t *testing.T) {
		e := &engine{}
		none := trust.Rule{Match: "registry.corp.example/**", Mode: trust.ModeNone, Source: trust.SourceUser}
		res, err := Pull(context.Background(), "registry.corp.example/app:1.2", unresolved(deps(&verifier{}, e, none)))
		if err != nil || res.Decision != trust.Allow || len(e.calls) != 1 {
			t.Errorf("res %+v, err %v, calls %q", res, err, e.calls)
		}
	})
}

func TestTheLocalDigestIsTheOneOfTheSameRepository(t *testing.T) {
	digests := []string{"mirror.example/python@sha256:aaa", "python@sha256:bbb"}
	if got := LocalDigest("docker.io/library/python:3.12", digests); got != "docker.io/library/python@sha256:bbb" {
		t.Errorf("localDigest = %q", got)
	}
	if got := LocalDigest("ghcr.io/x/y:1", digests); got != "" {
		t.Errorf("localDigest = %q, want none", got)
	}
}

func TestCheckComparesACandidateWithWhatTheTagInUsePointsTo(t *testing.T) {
	d := deps(nil, &engine{})
	d.Verifier = continuityVerifier{}
	d.Digest = func(ref string) (string, error) {
		if strings.HasSuffix(ref, ":1.1") {
			return "sha256:old", nil
		}
		return "sha256:new", nil
	}
	res := Check(context.Background(), "registry.example/app:1.2", "registry.example/app:1.1", d)
	if res.Verdict != trust.Unsigned || res.Rule.Source != trust.SourceContinuity || res.Decision != trust.Warn {
		t.Errorf("res = %+v", res)
	}
	d.Enabled = false
	if res := Check(context.Background(), "registry.example/app:1.2", "", d); res.Verdict != trust.NoPolicy || res.Decision != trust.Allow {
		t.Errorf("off answered %+v", res)
	}
}

// An update to a newer tag continues the image it replaces: the target's own
// name holds nothing locally yet.
func TestReplacingContinuesTheReplacedImage(t *testing.T) {
	d := deps(&verifier{}, &engine{})
	if got := d.Replacing("docker.io/library/node@sha256:old").Current("node:20.11.4"); got != "docker.io/library/node@sha256:old" {
		t.Errorf("current = %q", got)
	}
	if got := d.Replacing("").Current("node:20.11.4"); got != "" {
		t.Errorf("a local build replaced: current = %q, want the target's own", got)
	}
}
