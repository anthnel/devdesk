package security

import (
	"slices"
	"testing"

	"github.com/anthnel/devdesk/internal/remediation"
	"github.com/anthnel/devdesk/internal/scan"
)

// A finding of the file with no line (Line <= 0) cannot be placed outside the
// scope, so it counts as inside it: the safe side, a "still reported" at worst,
// never a "fixed" that was not measured (DEVDESK-688).
func TestStillReportedCountsAFindingWithNoLineInTheScope(t *testing.T) {
	scope := remediation.Scope{Rules: []string{"KSV-0030", "KSV-0104"}, Line: 10, EndLine: 20}
	for _, line := range []int{0, -1} {
		f := scan.Finding{ID: "KSV-0104", Source: scan.SourceTrivyMisconfig, File: "deploy.yaml", Line: line}
		v := misconfigVerify{Rule: "KSV-0030", File: "deploy.yaml", Scope: scope}
		got := stillReported(&scan.Result{Findings: []scan.Finding{f}}, v)
		if !slices.Equal(got, []string{"KSV-0104"}) {
			t.Errorf("Line %d: stillReported = %v, want [KSV-0104]", line, got)
		}
	}
}
