package scan

import "testing"

// A stage that never ran says nothing, and "nothing found" is the one thing it
// must not be mistaken for. Same contract as SecretVerdict, one category over.
func TestMisconfigVerdictSaysNobodyLookedRatherThanClean(t *testing.T) {
	var unscanned Result
	if got := unscanned.MisconfigVerdict(); got != nil {
		t.Errorf("MisconfigVerdict = %+v with no stage run, want nil", got)
	}

	clean := Result{MisconfigScanned: true}
	got := clean.MisconfigVerdict()
	if got == nil {
		t.Fatal("MisconfigVerdict = nil after a stage that found nothing, want a summary")
	}
	if got.Count != 0 || got.Worst != "" {
		t.Errorf("a clean summary = %+v, want a zero count and no severity", got)
	}
}

// The count, the worst severity and what nothing could read travel together:
// the caches keep the summary, not the result it came from.
func TestMisconfigVerdictCarriesTheWorstSeverityAndWhatWasNotRendered(t *testing.T) {
	r := Result{
		MisconfigScanned: true,
		K8sUnrendered:    []string{"charts/api", "overlays/prod"},
		Findings: []Finding{
			{Source: SourceTrivyMisconfig, Severity: SeverityLow},
			{Source: SourceTrivyMisconfig, Severity: SeverityHigh},
			{Source: SourceKubeconform, Severity: SeverityMedium},
			// A vulnerability must not reach these counters, and must not
			// decide their colour either.
			{Source: SourceTrivy, Severity: SeverityCritical},
		},
	}
	r.CountFindings()

	got := r.MisconfigVerdict()
	if got == nil {
		t.Fatal("MisconfigVerdict = nil, want a summary")
	}
	if got.Count != 3 {
		t.Errorf("Count = %d, want 3 — the CRITICAL is a CVE, not a misconfiguration", got.Count)
	}
	if got.Worst != SeverityHigh {
		t.Errorf("Worst = %q, want HIGH — the CRITICAL belongs to another category", got.Worst)
	}
	if got.Unrendered != 2 {
		t.Errorf("Unrendered = %d, want 2", got.Unrendered)
	}
}

// CountFindings is called more than once on the same result, so the worst
// severity has to be reset with the counters rather than accumulated.
func TestRecountingDoesNotKeepAWorstSeverityThatIsGone(t *testing.T) {
	r := Result{
		MisconfigScanned: true,
		Findings:         []Finding{{Source: SourceTrivyMisconfig, Severity: SeverityCritical}},
	}
	r.CountFindings()
	r.Findings = []Finding{{Source: SourceTrivyMisconfig, Severity: SeverityLow}}
	r.CountFindings()

	if r.MisconfigWorst != SeverityLow {
		t.Errorf("MisconfigWorst = %q after a recount, want LOW", r.MisconfigWorst)
	}
}

// UNKNOWN is a severity the tool did not state. It must rank below LOW, and
// above the empty value that means "there are none" — otherwise a finding with
// no severity would either outrank a real one or fail to register at all.
func TestWorseSeverityRanksUnknownBetweenNothingAndLow(t *testing.T) {
	tests := []struct {
		a, b, want SeverityLevel
	}{
		{"", SeverityUnknown, SeverityUnknown},
		{SeverityUnknown, SeverityLow, SeverityLow},
		{SeverityLow, SeverityUnknown, SeverityLow},
		{SeverityHigh, SeverityCritical, SeverityCritical},
		{SeverityCritical, SeverityMedium, SeverityCritical},
	}
	for _, tt := range tests {
		if got := WorseSeverity(tt.a, tt.b); got != tt.want {
			t.Errorf("WorseSeverity(%q, %q) = %q, want %q", tt.a, tt.b, got, tt.want)
		}
	}
}

// The nil summary is the state "nobody looked", and every reader goes through
// these three accessors rather than unwrapping the pointer itself.
func TestTheAccessorsReadANilSummaryAsZero(t *testing.T) {
	var none *MisconfigSummary
	if none.Total() != 0 || none.WorstSeverity() != "" || none.UnrenderedCount() != 0 {
		t.Error("a nil summary must read as zero everywhere")
	}
}
