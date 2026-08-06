package scan

import "testing"

// Categorize is the one rule that decides where a finding goes. It replaced two
// that disagreed: Result.CountFindings switched on Source alone, the security
// view's tabs on Source plus PkgName plus Match. The three rows marked below are
// the inputs they answered differently — each produced a finding counted in the
// header's CVE bar and shown in no tab at all.
func TestCategorizeCoversEverySourceAndFallsBackToVulnerability(t *testing.T) {
	tests := []struct {
		name    string
		finding Finding
		want    Category
	}{
		{"a vulnerability", Finding{Source: SourceTrivy, PkgName: "libfoo"}, CategoryVulnerability},
		{"a gitleaks secret", Finding{Source: SourceGitleaks, Match: "****"}, CategorySecret},
		{"a trivy secret", Finding{Source: SourceTrivySecret, Match: "****"}, CategorySecret},
		{"a licence", Finding{Source: SourceTrivyLicense}, CategoryLicense},
		{"a misconfiguration", Finding{Source: SourceTrivyMisconfig}, CategoryMisconfiguration},

		// The three that used to fall between the two rules.
		{"a vulnerability with no package", Finding{Source: SourceTrivy}, CategoryVulnerability},
		{"a trivy finding carrying a match", Finding{Source: SourceTrivy, Match: "****"}, CategoryVulnerability},
		{"an unknown source", Finding{Source: "grype"}, CategoryVulnerability},
	}

	for _, tt := range tests {
		t.Run(tt.name, func(t *testing.T) {
			if got := Categorize(tt.finding); got != tt.want {
				t.Errorf("Categorize(%+v) = %d, want %d", tt.finding, got, tt.want)
			}
		})
	}
}

// Every finding lands in exactly one counter, and the counters add up to the
// total. A finding in none of them is the failure that matters: it is invisible
// in the results table while still contributing to the header.
func TestEveryFindingIsCountedExactlyOnce(t *testing.T) {
	result := &Result{Findings: []Finding{
		{Source: SourceTrivy, Severity: SeverityCritical, PkgName: "libfoo"},
		{Source: SourceTrivy, Severity: SeverityHigh}, // no PkgName
		{Source: SourceGitleaks, Severity: SeverityHigh},
		{Source: SourceTrivySecret, Severity: SeverityCritical},
		{Source: SourceTrivyLicense, Severity: SeverityLow},
		{Source: SourceTrivyMisconfig, Severity: SeverityMedium},
		{Source: "grype", Severity: SeverityUnknown}, // a source nobody declared
	}}

	result.CountFindings()

	if got := result.TotalFindings(); got != len(result.Findings) {
		t.Errorf("TotalFindings() = %d over %d findings — one is counted twice or not at all",
			got, len(result.Findings))
	}
	if result.SecretCount != 2 {
		t.Errorf("SecretCount = %d, want both the gitleaks and the trivy secret", result.SecretCount)
	}
	// Three vulnerabilities: the one with a package, the one without, and the
	// unknown source. The last two used to be counted here and shown nowhere.
	if vulns := result.Counts.Critical + result.Counts.High + result.Counts.Unknown; vulns != 3 {
		t.Errorf("%d vulnerabilities counted by severity, want 3", vulns)
	}
	if result.LicenseCount != 1 || result.MisconfigCount != 1 {
		t.Errorf("licences = %d, misconfigurations = %d, want 1 each",
			result.LicenseCount, result.MisconfigCount)
	}
}
