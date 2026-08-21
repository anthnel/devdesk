package netcheck

import (
	"context"
	"errors"
	"net"
	"testing"
	"time"
)

func target() Target { return Target{Host: "example.com", Port: 443} }

func runWith(t *testing.T, env Env, tg Target) []Check {
	t.Helper()
	res, err := Run(context.Background(), tg, env)
	if err != nil {
		t.Fatalf("Run: %v", err)
	}
	return res.All()
}

// --- contracts ---------------------------------------------------------------

// TestUnknownIsTheZeroVerdict states structurally what SecretVerdict's *bool
// states elsewhere: a check nobody filled in must not read as a pass.
func TestUnknownIsTheZeroVerdict(t *testing.T) {
	var v Verdict
	if v != Unknown {
		t.Fatalf("zero Verdict is %v, want Unknown — a check nobody answered must not read as OK", v)
	}
	var c Check
	if c.Verdict != Unknown {
		t.Fatalf("zero Check.Verdict is %v, want Unknown", c.Verdict)
	}
}

// TestEveryStageProducesWhatItDeclares keeps the `produces` list honest. The
// skip path names checks from that list alone, so a stage that grew a check
// without declaring it would go missing precisely when something upstream
// failed — the moment the list matters most.
func TestEveryStageProducesWhatItDeclares(t *testing.T) {
	// Two shapes per stage, because a stage's check set must not depend on how
	// its probe went: the TLS stage returns five checks whether the handshake
	// succeeded or not, and that is the property being pinned.
	state, roots := buildChain(t, chainOpts{dnsNames: []string{"example.com"}, withIntermediate: true})

	for _, s := range stages() {
		for _, env := range []Env{
			fakeEnv{roots: roots, handshake: handshakeReturning(state)},
			fakeEnv{
				resolve: func(context.Context, string, string) ([]net.IP, error) { return nil, errors.New("nxdomain") },
				ping:    func(context.Context, string, int) (PingStats, error) { return PingStats{}, errors.New("no socket") },
				dial:    func(context.Context, string) (time.Duration, error) { return 0, errors.New("refused") },
				head:    func(context.Context, string) (HTTPResult, error) { return HTTPResult{}, errors.New("eof") },
			},
		} {
			var prior Results
			got := idsOf(s.run(context.Background(), target(), env, &prior))
			if len(got) != len(s.produces) {
				t.Fatalf("stage %q produced %v, declares %v", s.id, got, s.produces)
			}
			declared := make(map[CheckID]bool, len(s.produces))
			for _, id := range s.produces {
				declared[id] = true
			}
			for _, id := range got {
				if !declared[id] {
					t.Fatalf("stage %q produced undeclared check %q", s.id, id)
				}
			}
		}
	}
}

// TestEveryCheckHasATitleAndAStage catches drift in both directions: a check
// that no stage produces, and a check no title names.
func TestEveryCheckHasATitleAndAStage(t *testing.T) {
	produced := map[CheckID]bool{}
	for _, s := range stages() {
		for _, id := range s.produces {
			if produced[id] {
				t.Fatalf("check %q is produced by more than one stage", id)
			}
			produced[id] = true
			if checkTitles[id] == "" {
				t.Errorf("check %q has no title", id)
			}
		}
	}
	for id := range checkTitles {
		if !produced[id] {
			t.Errorf("check %q has a title but no stage produces it", id)
		}
	}
}

// TestAGateBelongsToTheStageThatProducesIt stops a dependency pointing at a
// check the named stage never emits — the cascade would then never fire.
func TestAGateBelongsToTheStageThatProducesIt(t *testing.T) {
	for _, s := range stages() {
		if s.gate == "" {
			continue
		}
		found := false
		for _, id := range s.produces {
			if id == s.gate {
				found = true
			}
		}
		if !found {
			t.Errorf("stage %q gates on %q, which it does not produce", s.id, s.gate)
		}
	}
}

// --- the cascade -------------------------------------------------------------

func TestAFailedResolutionBlocksEverythingBelowItAndNamesItself(t *testing.T) {
	checks := runWith(t, fakeEnv{
		resolve: func(context.Context, string, string) ([]net.IP, error) {
			return nil, errors.New("no such host")
		},
	}, target())

	if got := verdictOf(t, checks, CheckResolve); got != Fail {
		t.Fatalf("resolve verdict = %v, want Fail", got)
	}
	for _, id := range []CheckID{CheckTCP, CheckTLSChain, CheckHTTP} {
		c := checkNamed(t, checks, id)
		if c.Verdict != NotApplicable {
			t.Errorf("%s verdict = %v, want NotApplicable", id, c.Verdict)
		}
		if c.Because != CheckResolve {
			t.Errorf("%s blamed %q, want %q", id, c.Because, CheckResolve)
		}
	}
}

// TestASilentPingDoesNotBlockTheCertificateChecks is the reason the reach stage
// gates nothing. ICMP is filtered on a large share of reachable hosts, and a
// cascade from it would suppress exactly the question this view exists for.
func TestASilentPingDoesNotBlockTheCertificateChecks(t *testing.T) {
	state, roots := buildChain(t, chainOpts{dnsNames: []string{"example.com"}, withIntermediate: true})
	checks := runWith(t, fakeEnv{
		roots:     roots,
		handshake: handshakeReturning(state),
		ping: func(_ context.Context, _ string, count int) (PingStats, error) {
			return PingStats{Sent: count, Received: 0}, nil
		},
	}, target())

	if got := verdictOf(t, checks, CheckICMP); got != Warn {
		t.Fatalf("icmp verdict = %v, want Warn — a filtered echo is not a failure", got)
	}
	if got := verdictOf(t, checks, CheckTLSChain); got != OK {
		t.Fatalf("tls-chain verdict = %v, want OK — a silent ping must not suppress it", got)
	}
}

// TestAnUnprobeableIcmpIsUnknownNotDown separates "we could not look" from
// "the host is down": on a machine without the privilege to open an ICMP
// socket, reporting down blames the target for a local restriction.
func TestAnUnprobeableIcmpIsUnknownNotDown(t *testing.T) {
	checks := runWith(t, fakeEnv{
		ping: func(context.Context, string, int) (PingStats, error) {
			return PingStats{}, errors.New("operation not permitted")
		},
	}, target())

	if got := verdictOf(t, checks, CheckICMP); got != Unknown {
		t.Fatalf("icmp verdict = %v, want Unknown", got)
	}
}

func TestARefusedPortBlocksTlsAndHttpButNotIcmp(t *testing.T) {
	checks := runWith(t, fakeEnv{
		dial: func(context.Context, string) (time.Duration, error) {
			return 0, errors.New("connection refused")
		},
	}, target())

	if got := verdictOf(t, checks, CheckICMP); got != OK {
		t.Errorf("icmp verdict = %v, want OK — it does not depend on the port", got)
	}
	for _, id := range []CheckID{CheckTLSHandshake, CheckHTTP} {
		c := checkNamed(t, checks, id)
		if c.Verdict != NotApplicable || c.Because != CheckTCP {
			t.Errorf("%s = %v because %q, want NotApplicable because %q", id, c.Verdict, c.Because, CheckTCP)
		}
	}
}

// TestALiteralAddressBlocksNothing pins that NotApplicable is not a failure:
// a literal IP makes the resolution check meaningless, and everything below it
// is perfectly meaningful.
func TestALiteralAddressBlocksNothing(t *testing.T) {
	checks := runWith(t, fakeEnv{}, Target{Host: "10.2.3.4", Port: 443})

	resolve := checkNamed(t, checks, CheckResolve)
	if resolve.Verdict != NotApplicable {
		t.Fatalf("resolve verdict = %v, want NotApplicable", resolve.Verdict)
	}
	if resolve.Because != "" {
		t.Errorf("resolve blamed %q; nothing failed, so nothing should be blamed", resolve.Because)
	}
	if got := verdictOf(t, checks, CheckTCP); got != OK {
		t.Errorf("tcp verdict = %v, want OK — a literal address must not cascade", got)
	}
}

func TestACancelledRunReportsUnknownRatherThanFailure(t *testing.T) {
	ctx, cancel := context.WithCancel(context.Background())
	cancel()

	res, err := Run(ctx, target(), fakeEnv{})
	if err != nil {
		t.Fatalf("Run: %v", err)
	}
	for _, c := range res.All() {
		if c.Verdict != Unknown {
			t.Fatalf("%s = %v on a cancelled run, want Unknown", c.ID, c.Verdict)
		}
	}
}

func TestRunRejectsATargetItCannotActdOn(t *testing.T) {
	for _, tc := range []struct {
		name string
		tg   Target
	}{
		{"no host", Target{Port: 443}},
		{"port zero", Target{Host: "example.com"}},
		{"port too high", Target{Host: "example.com", Port: 70000}},
	} {
		t.Run(tc.name, func(t *testing.T) {
			if _, err := Run(context.Background(), tc.tg, fakeEnv{}); err == nil {
				t.Fatal("want an error, got none")
			}
		})
	}
}

// --- Summarize ---------------------------------------------------------------

func TestSummarizeIsTheOnlyAggregate(t *testing.T) {
	for _, tc := range []struct {
		name string
		in   []Verdict
		want Verdict
	}{
		{"all ok", []Verdict{OK, OK}, OK},
		{"a warning shows", []Verdict{OK, Warn}, Warn},
		{"a failure outranks a warning", []Verdict{Warn, Fail, OK}, Fail},
		{"unknown outranks a warning", []Verdict{Warn, Unknown}, Unknown},
		{"a failure outranks unknown", []Verdict{Unknown, Fail}, Fail},
		{"not-applicable never colours the headline", []Verdict{OK, NotApplicable}, OK},
		{"nothing but not-applicable is unknown", []Verdict{NotApplicable, NotApplicable}, Unknown},
		{"no checks at all is unknown", nil, Unknown},
	} {
		t.Run(tc.name, func(t *testing.T) {
			checks := make([]Check, 0, len(tc.in))
			for _, v := range tc.in {
				checks = append(checks, Check{Verdict: v})
			}
			if got := Summarize(checks); got != tc.want {
				t.Fatalf("Summarize = %v, want %v", got, tc.want)
			}
		})
	}
}

func TestVerdictNamesAreShortEnoughForATableCell(t *testing.T) {
	for _, v := range []Verdict{Unknown, NotApplicable, OK, Warn, Fail} {
		if n := len(v.String()); n == 0 || n > 7 {
			t.Errorf("%d renders as %q (%d chars)", int(v), v.String(), n)
		}
	}
}

// TestRunStepWalksTheSamePipelineAsRun pins the factoring: Run and RunStep share
// runStage, so the skip rule cannot mean one thing to a batch caller and another
// to a view chaining the stages through messages.
func TestRunStepWalksTheSamePipelineAsRun(t *testing.T) {
	env := fakeEnv{
		dial: func(context.Context, string) (time.Duration, error) {
			return 0, errors.New("connection refused")
		},
	}

	whole, err := Run(context.Background(), target(), env)
	if err != nil {
		t.Fatalf("Run: %v", err)
	}

	var piecewise Results
	for _, id := range Steps() {
		piecewise = RunStep(context.Background(), target(), env, id, piecewise)
	}

	if len(piecewise.All()) != len(whole.All()) {
		t.Fatalf("piecewise produced %d checks, Run produced %d", len(piecewise.All()), len(whole.All()))
	}
	for i, c := range whole.All() {
		got := piecewise.All()[i]
		if got.ID != c.ID || got.Verdict != c.Verdict || got.Because != c.Because {
			t.Errorf("check %d: piecewise %s/%s/%s, Run %s/%s/%s",
				i, got.ID, got.Verdict, got.Because, c.ID, c.Verdict, c.Because)
		}
	}
}

// TestRunStepDoesNotWriteIntoTheResultsItWasGiven is the property a Bubble Tea
// caller depends on: the previous value is held by the model while the next
// stage runs on a goroutine, so a shared backing array would be a data race on
// the one thing Rule 110 exists to prevent.
func TestRunStepDoesNotWriteIntoTheResultsItWasGiven(t *testing.T) {
	first := RunStep(context.Background(), target(), fakeEnv{}, StageResolve, Results{})
	snapshot := len(first.All())

	RunStep(context.Background(), target(), fakeEnv{}, StageReach, first)
	RunStep(context.Background(), target(), fakeEnv{}, StageConnect, first)

	if len(first.All()) != snapshot {
		t.Fatalf("the earlier Results grew from %d to %d checks", snapshot, len(first.All()))
	}
}

// TestEveryStageIsNamedForAProgressLine — a stage the user waits on with no
// label is the mute spinner this indirection exists to avoid.
func TestEveryStageIsNamedForAProgressLine(t *testing.T) {
	for _, id := range Steps() {
		if StageTitle(id) == "" {
			t.Errorf("stage %q has no title", id)
		}
	}
	if len(Steps()) != len(stages()) {
		t.Errorf("Steps() reports %d stages, the pipeline has %d", len(Steps()), len(stages()))
	}
}
