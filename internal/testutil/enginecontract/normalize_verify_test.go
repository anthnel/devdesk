package enginecontract_test

import (
	"strings"
	"testing"

	"github.com/anthnel/devdesk/internal/enginefixture"
	"github.com/anthnel/devdesk/internal/testutil/enginecontract"
)

// OC-09b2 (DEVDESK-807): a fixture recorded with fixed timestamps and durations is
// verified, with Tolerating(Durations, Timestamps), against the real output of another day.

func psOut(created, status string) []byte {
	return []byte("a1b2c3d4e5f6\tweb\talpine\trunning\t" + status + "\t" + created + "\t\n")
}

func TestAFixtureWithFixedValuesPassesAgainstARealOutputOfAnotherDay(t *testing.T) {
	argv := []string{"ps", "--all"}
	recordedRaw := enginefixture.Exchange{Argv: argv, Stdout: psOut("2026-11-23 08:00:00.123456789 +0000 UTC", "Up Less than a second (healthy)")}
	fixed, header := enginefixture.Redact(recordedRaw, enginefixture.Redaction{FixedTimestamps: true, FixedDurations: true})
	fixture := recorded("6.2.1", enginefixture.Rootless, "")
	fixture.Scenario, fixture.Name, fixture.Exchange = "ps-health", "ps-health--podman-6.2.1-rootless", fixed
	fixture.Meta.Redacted = header
	today := enginefixture.Exchange{Argv: argv, Stdout: psOut("2026-12-05 21:10:09.5 +0100 CET", "Up 12 seconds (healthy)")}
	check := func(cmp enginecontract.Comparer) *recT {
		rt := &recT{TB: t}
		rt.run(func() {
			verifySession().VerifyAgainst(rt, []enginefixture.Fixture{fixture}, "ps-health", today, enginefixture.Redaction{}, cmp)
		})
		return rt
	}

	if rt := check(enginecontract.Tolerating(enginecontract.Durations, enginecontract.Timestamps)); !rt.quiet() {
		t.Errorf("a real output of another day differs from the fixed fixture: %s", rt.text())
	}
	if rt := check(nil); len(rt.errs) == 0 {
		t.Error("with no Comparer the two agree: the test proves nothing")
	}
	// What the fixed values do not hide: a state, a health marker, a name.
	for _, changed := range []string{"Exited (0) 3 seconds ago (healthy)", "Up 12 seconds (unhealthy)"} {
		other := enginefixture.Exchange{Argv: argv, Stdout: psOut("2026-12-05 21:10:09 +0000 UTC", changed)}
		rt := &recT{TB: t}
		rt.run(func() {
			verifySession().VerifyAgainst(rt, []enginefixture.Fixture{fixture}, "ps-health", other, enginefixture.Redaction{},
				enginecontract.Tolerating(enginecontract.Durations, enginecontract.Timestamps))
		})
		if len(rt.errs) == 0 || !strings.Contains(strings.Join(rt.errs, "\n"), "stdout") {
			t.Errorf("status %q passed against the fixed fixture", changed)
		}
	}
}

func TestRecordWritesTheFixedValuesWhenTheScenarioDeclaresThem(t *testing.T) {
	s, out := recordingSession(t, enginecontract.Record)
	ex := enginefixture.Exchange{Argv: []string{"ps", "--all"}, Stdout: psOut("2026-11-27 17:45:10.987654321 +0000 UTC", "Up 4 seconds (healthy)")}

	rt := record(t, s, "containers", "ps-health", ex, enginefixture.Redaction{RunID: recRun, FixedTimestamps: true, FixedDurations: true})

	if !rt.quiet() {
		t.Fatalf("Record reported: %s", rt.text())
	}
	text := string(readOnly(t, out, "containers"))
	for _, want := range []string{"2026-01-01 00:00:00 +0000 UTC", "Up 5 seconds (healthy)", "durations, timestamps"} {
		if !strings.Contains(text, want) {
			t.Errorf("the fixture lacks %q:\n%s", want, text)
		}
	}
	if strings.Contains(text, "17:45:10") || strings.Contains(text, "Up 4 seconds") {
		t.Errorf("the fixture still holds a value of the recording day:\n%s", text)
	}
}
