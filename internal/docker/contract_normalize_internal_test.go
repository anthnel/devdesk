package docker

import (
	"strings"
	"testing"

	"github.com/anthnel/devdesk/internal/enginefixture"
	"github.com/anthnel/devdesk/internal/testutil/enginecontract"
)

// OC-09b2 (DEVDESK-807): c.normalize(kinds...) declares that the output of this contract carries
// timestamps and/or durations the recording fixes (Redaction.FixedTimestamps, FixedDurations), so a
// fixture does not change at every recording. Only a recording applies it: verifying compares the real
// output with the Comparer of the contract. Never global: a contract that does not declare it records
// what the engine printed. The names of these tests do not start with "TestContract" (guard G5, R3).
//
//	func (c *contract) normalize(kinds ...enginecontract.Tolerance)

func sessionIn(mode enginecontract.Mode) *enginecontract.Session {
	s := labSession(enginefixture.Podman, labRunID)
	s.Mode = mode
	return s
}

func TestNormalizeFixesTheValuesOfARecordingOnly(t *testing.T) {
	t.Setenv(enginecontract.SentinelEnv, sentinelID)
	for _, tt := range []struct {
		mode             enginecontract.Mode
		timestamps, durs bool
	}{{enginecontract.Record, true, true}, {enginecontract.Verify, false, false}} {
		c := newContract(&contractT{TB: t}, sessionIn(tt.mode))
		c.normalize(enginecontract.Durations, enginecontract.Timestamps)

		r := c.redaction()

		if r.FixedTimestamps != tt.timestamps || r.FixedDurations != tt.durs {
			t.Errorf("mode %s: FixedTimestamps = %v, FixedDurations = %v, want %v and %v", tt.mode, r.FixedTimestamps, r.FixedDurations, tt.timestamps, tt.durs)
		}
	}
}

func TestNormalizeIsDeclaredKindByKind(t *testing.T) {
	t.Setenv(enginecontract.SentinelEnv, sentinelID)
	c := newContract(&contractT{TB: t}, sessionIn(enginecontract.Record))
	c.normalize(enginecontract.Timestamps)

	r := c.redaction()

	if !r.FixedTimestamps || r.FixedDurations {
		t.Errorf("FixedTimestamps = %v, FixedDurations = %v, want only the timestamps", r.FixedTimestamps, r.FixedDurations)
	}
}

func TestNothingIsFixedWithoutADeclaration(t *testing.T) {
	t.Setenv(enginecontract.SentinelEnv, sentinelID)
	c := newContract(&contractT{TB: t}, sessionIn(enginecontract.Record))

	if r := c.redaction(); r.FixedTimestamps || r.FixedDurations {
		t.Errorf("Redaction = %+v, want nothing fixed: a contract that does not declare it records what the engine printed", r)
	}
}

// A health marker is not a clock: it is judged, never fixed. A kind that is not one is a mistake.
func TestNormalizeRefusesWhatIsNotATimestampOrADuration(t *testing.T) {
	for _, kinds := range [][]enginecontract.Tolerance{{enginecontract.Health}, {0}, nil, {enginecontract.Durations, enginecontract.Health}} {
		ct := &contractT{TB: t}
		c := newContract(ct, sessionIn(enginecontract.Record))

		ended := ct.run(func() { c.normalize(kinds...) })

		if !ended || len(ct.fatals) != 1 || !strings.Contains(ct.fatals[0], "normalize") {
			t.Errorf("normalize(%v): ended = %v, fatals = %q, want one Fatalf", kinds, ended, ct.fatals)
		}
	}
}
