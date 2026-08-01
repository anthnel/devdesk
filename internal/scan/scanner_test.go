package scan

import (
	"encoding/json"
	"testing"
)

func TestCountFindings_SeparatesSourcesByType(t *testing.T) {
	tests := []struct {
		name             string
		findings         []Finding
		wantCounts       SeverityCounts
		wantSecretCount  int
		wantLicenseCount int
		wantMisconfig    int
	}{
		{
			name:       "empty findings",
			findings:   []Finding{},
			wantCounts: SeverityCounts{},
		},
		{
			name: "trivy CVEs counted by severity",
			findings: []Finding{
				{Source: "trivy", Severity: SeverityCritical},
				{Source: "trivy", Severity: SeverityHigh},
				{Source: "trivy", Severity: SeverityHigh},
				{Source: "trivy", Severity: SeverityMedium},
				{Source: "trivy", Severity: SeverityLow},
				{Source: "trivy", Severity: SeverityUnknown},
			},
			wantCounts: SeverityCounts{Critical: 1, High: 2, Medium: 1, Low: 1, Unknown: 1},
		},
		{
			name: "gitleaks findings go to SecretCount",
			findings: []Finding{
				{Source: "gitleaks", Severity: SeverityCritical},
				{Source: "gitleaks", Severity: SeverityHigh},
			},
			wantCounts:      SeverityCounts{},
			wantSecretCount: 2,
		},
		{
			name: "trivy-license findings go to LicenseCount",
			findings: []Finding{
				{Source: "trivy-license", Severity: SeverityMedium},
				{Source: "trivy-license", Severity: SeverityLow},
			},
			wantCounts:       SeverityCounts{},
			wantLicenseCount: 2,
		},
		{
			name: "trivy-misconfig findings go to MisconfigCount",
			findings: []Finding{
				{Source: "trivy-misconfig", Severity: SeverityHigh},
			},
			wantCounts:    SeverityCounts{},
			wantMisconfig: 1,
		},
		{
			name: "mixed findings separated correctly",
			findings: []Finding{
				{Source: "trivy", Severity: SeverityCritical},
				{Source: "trivy", Severity: SeverityHigh},
				{Source: "gitleaks", Severity: SeverityCritical},
				{Source: "gitleaks", Severity: SeverityCritical},
				{Source: "trivy-license", Severity: SeverityMedium},
				{Source: "trivy-misconfig", Severity: SeverityHigh},
				{Source: "trivy-misconfig", Severity: SeverityHigh},
			},
			wantCounts:       SeverityCounts{Critical: 1, High: 1},
			wantSecretCount:  2,
			wantLicenseCount: 1,
			wantMisconfig:    2,
		},
	}

	for _, tt := range tests {
		t.Run(tt.name, func(t *testing.T) {
			r := &Result{Findings: tt.findings}
			r.CountFindings()

			if r.Counts != tt.wantCounts {
				t.Errorf("Counts = %+v, want %+v", r.Counts, tt.wantCounts)
			}
			if r.SecretCount != tt.wantSecretCount {
				t.Errorf("SecretCount = %d, want %d", r.SecretCount, tt.wantSecretCount)
			}
			if r.LicenseCount != tt.wantLicenseCount {
				t.Errorf("LicenseCount = %d, want %d", r.LicenseCount, tt.wantLicenseCount)
			}
			if r.MisconfigCount != tt.wantMisconfig {
				t.Errorf("MisconfigCount = %d, want %d", r.MisconfigCount, tt.wantMisconfig)
			}
		})
	}
}

func TestParseTrivyOutput_PopulatesRemediationFields(t *testing.T) {
	report := TrivyReport{
		Results: []TrivyResult{
			{
				Target: "package-lock.json",
				Class:  "lang-pkgs",
				Vulnerabilities: []TrivyVulnerability{
					{
						VulnerabilityID:  "CVE-2023-1234",
						PkgName:          "lodash",
						InstalledVersion: "4.17.20",
						FixedVersion:     "4.17.21",
						Severity:         "HIGH",
						Title:            "Prototype Pollution",
						Description:      "lodash is vulnerable to prototype pollution",
						PrimaryURL:       "https://nvd.nist.gov/vuln/detail/CVE-2023-1234",
						References:       []string{"https://nvd.nist.gov/vuln/detail/CVE-2023-1234", "https://github.com/lodash/lodash/issues/1"},
					},
				},
				Misconfigurations: []TrivyMisconfiguration{
					{
						AVDID:      "AVD-KSV-0001",
						Title:      "Container should not run as root",
						Desc:       "Running as root increases risk",
						Severity:   "MEDIUM",
						Resolution: "Set securityContext.runAsNonRoot to true",
						PrimaryURL: "https://avd.aquasec.com/misconfig/ksv001",
					},
				},
			},
		},
	}

	data, err := json.Marshal(report)
	if err != nil {
		t.Fatalf("failed to marshal test report: %v", err)
	}

	findings, err := parseTrivyOutput(data)
	if err != nil {
		t.Fatalf("parseTrivyOutput returned error: %v", err)
	}

	if len(findings) != 2 {
		t.Fatalf("expected 2 findings, got %d", len(findings))
	}

	vuln := findings[0]
	if vuln.FixCommand == "" {
		t.Error("expected FixCommand to be set for vulnerability with FixedVersion")
	}
	if len(vuln.References) == 0 {
		t.Error("expected References to be populated from PrimaryURL")
	}
	if vuln.References[0] != "https://nvd.nist.gov/vuln/detail/CVE-2023-1234" {
		t.Errorf("expected first reference to be PrimaryURL, got %q", vuln.References[0])
	}
	// PrimaryURL should not be duplicated
	count := 0
	for _, r := range vuln.References {
		if r == "https://nvd.nist.gov/vuln/detail/CVE-2023-1234" {
			count++
		}
	}
	if count > 1 {
		t.Error("PrimaryURL should not be duplicated in References")
	}

	misconf := findings[1]
	if misconf.Resolution == "" {
		t.Error("expected Resolution to be populated for misconfiguration")
	}
	if misconf.Resolution != "Set securityContext.runAsNonRoot to true" {
		t.Errorf("unexpected Resolution: %q", misconf.Resolution)
	}
	if len(misconf.References) == 0 {
		t.Error("expected References to be populated from misconfig PrimaryURL")
	}
	// Resolution must NOT be embedded in Description anymore
	if len(misconf.Description) > 0 && len(misconf.Description) > len("Running as root increases risk") {
		t.Errorf("Description should not contain Resolution, got: %q", misconf.Description)
	}
}

func TestTotalFindings_IncludesAllTypes(t *testing.T) {
	r := &Result{
		Counts:         SeverityCounts{Critical: 1, High: 2, Medium: 3, Low: 4, Unknown: 5},
		SecretCount:    6,
		LicenseCount:   7,
		MisconfigCount: 8,
	}
	want := 1 + 2 + 3 + 4 + 5 + 6 + 7 + 8 // 36
	if got := r.TotalFindings(); got != want {
		t.Errorf("TotalFindings() = %d, want %d", got, want)
	}
}
