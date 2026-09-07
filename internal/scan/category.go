package scan

// Where a finding goes: one single definition, for everyone.
//
// There used to be two, which did not say the same thing. `CountFindings`
// classified on `Source` alone and sent everything else to the severity
// counters; the security view classified on `Source` plus `PkgName` plus
// `Match`. Three entries told them apart — a trivy finding with no
// `PkgName`, an unknown source, a trivy finding carrying a `Match` — and
// each produced a finding counted in the header's CVE bar but absent from
// every tab, so invisible in the table. Same family as D24, D25 and D26:
// two copies of one rule, one place to update.
//
// Classification is now done on the source and nothing else, which required
// giving Trivy secrets their own source (`trivy-secret`) instead of being
// recognized by the presence of a `Match`.

// The sources a scanner can produce. A source missing from this list is a
// programming defect, not user input: `Categorize` classifies it as a
// vulnerability, which keeps it visible in the CVE tab rather than making it
// disappear.
const (
	SourceTrivy          = "trivy"           // vulnerabilities
	SourceTrivySecret    = "trivy-secret"    // secrets detected by Trivy
	SourceTrivyLicense   = "trivy-license"   // licenses
	SourceTrivyMisconfig = "trivy-misconfig" // IaC misconfigurations
	SourceGitleaks       = "gitleaks"        // secrets detected by Gitleaks
	SourcePlumber        = "plumber"         // pipeline security score (§3.42)
)

// Category is the family a finding belongs to: a tab in the results view,
// and a counter on `Result`.
type Category int

const (
	CategoryVulnerability Category = iota
	CategorySecret
	CategoryLicense
	CategoryMisconfiguration
	CategoryCIScore
)

// Categorize returns the family a finding belongs to.
//
// It is the only function that decides, and `Result.CountFindings` as well
// as the security view's tabs go through it.
func Categorize(f Finding) Category {
	switch f.Source {
	case SourceGitleaks, SourceTrivySecret:
		return CategorySecret
	case SourceTrivyLicense:
		return CategoryLicense
	case SourceTrivyMisconfig:
		return CategoryMisconfiguration
	case SourcePlumber:
		return CategoryCIScore
	default:
		return CategoryVulnerability
	}
}
