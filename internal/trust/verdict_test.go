package trust

import "testing"

// §3.82's table, cell by cell: the pull, the remediation tab and the scan all
// read Decide, so a cell changed here changes all three.
func TestDecideIsTheTableOfTheBacklog(t *testing.T) {
	for _, c := range []struct {
		verdict Verdict
		source  Source
		want    Decision
	}{
		{IdentityMismatch, SourceUser, Block},
		{IdentityMismatch, SourceBuiltin, Block},
		{IdentityMismatch, SourceContinuity, Block},
		{Unsigned, SourceUser, Block},
		{Unsigned, SourceBuiltin, Block},
		{Unsigned, SourceContinuity, Warn},
		{Failed, SourceUser, Block},
		{Failed, SourceBuiltin, Warn},
		{Failed, SourceContinuity, Warn},
		{Verified, SourceUser, Allow},
		{Verified, SourceBuiltin, Allow},
		{Verified, SourceContinuity, Allow},
		{NoPolicy, SourceUser, Allow},
		{NoPolicy, SourceContinuity, Allow},
	} {
		if got := Decide(c.verdict, c.source); got != c.want {
			t.Errorf("Decide(%v, %v) = %v, want %v", c.verdict, c.source, got, c.want)
		}
	}
}

func TestAVerdictReadsBackFromItsName(t *testing.T) {
	for _, v := range []Verdict{Verified, IdentityMismatch, Unsigned, Failed} {
		if got := parseVerdict(v.String()); got != v {
			t.Errorf("parseVerdict(%q) = %v", v.String(), got)
		}
	}
	if parseVerdict("garbage") != NoPolicy {
		t.Error("an unknown name must read as NoPolicy, which is never stored")
	}
}
