package scan

import (
	"strings"
	"testing"
	"unicode/utf8"
)

// The scanners themselves are subprocesses. What is testable without one is the
// part that carries the defects: turning tool JSON into findings, and building
// the argument lists that decide what the tool is actually asked to do.

// ── Trivy output ─────────────────────────────────────────────────────────────

// One report can carry all three kinds at once; each has to land under its own
// source, because that is what CountFindings dispatches on.
func TestTrivyOutputIsSplitByKind(t *testing.T) {
	findings, err := parseTrivyOutput([]byte(`{
	  "Results": [{
	    "Target": "go.mod",
	    "Vulnerabilities": [
	      {"VulnerabilityID":"CVE-1","Severity":"CRITICAL","PkgName":"golang.org/x/net","InstalledVersion":"0.1.0"}
	    ],
	    "Licenses": [
	      {"Name":"GPL-3.0","PkgName":"libfoo","Category":"restricted","Severity":"HIGH","FilePath":"vendor/libfoo"}
	    ],
	    "Misconfigurations": [
	      {"AVDID":"AVD-DS-0002","Title":"root user","Severity":"MEDIUM","CauseMetadata":{"StartLine":7}}
	    ],
	    "Secrets": [
	      {"RuleID":"aws-secret-access-key","Category":"AWS","Severity":"CRITICAL","Title":"AWS key","StartLine":3,"Match":"AKIAIOSFODNN7EXAMPLE"}
	    ]
	  }]
	}`))
	if err != nil {
		t.Fatalf("parsing failed: %v", err)
	}

	bySource := map[string]Finding{}
	for _, f := range findings {
		bySource[f.Source] = f
	}
	for _, want := range []string{SourceTrivy, SourceTrivyLicense, SourceTrivyMisconfig, SourceTrivySecret} {
		if _, ok := bySource[want]; !ok {
			t.Errorf("no finding with source %q in %v", want, bySource)
		}
	}

	if got := bySource[SourceTrivy].File; got != "go.mod" {
		t.Errorf("the vulnerability's file is %q, want the result target go.mod", got)
	}
	if got := bySource[SourceTrivyLicense].File; got != "vendor/libfoo" {
		t.Errorf("the license's file is %q, want its own FilePath", got)
	}
	if got := bySource[SourceTrivyMisconfig].Line; got != 7 {
		t.Errorf("the misconfiguration is at line %d, want 7 from CauseMetadata", got)
	}
	if got := bySource[SourceTrivySecret].Line; got != 3 {
		t.Errorf("the secret is at line %d, want 3 from StartLine", got)
	}
}

// Trivy's report has carried Secrets since the struct was declared, and nothing
// read them: a secret Trivy found was parsed and dropped. Same shape as D27 —
// declared, populated, read by nothing, and silent about it.
func TestATrivySecretIsReadAndItsValueMasked(t *testing.T) {
	findings, err := parseTrivyOutput([]byte(`{
	  "Results": [{
	    "Target": "app/.env",
	    "Secrets": [
	      {"RuleID":"aws-secret-access-key","Category":"AWS","Severity":"CRITICAL","Title":"AWS key","StartLine":3,"Match":"AKIAIOSFODNN7EXAMPLE"}
	    ]
	  }]
	}`))
	if err != nil {
		t.Fatalf("parsing failed: %v", err)
	}
	if len(findings) != 1 {
		t.Fatalf("%d findings, want the one secret", len(findings))
	}

	secret := findings[0]
	if secret.Source != SourceTrivySecret {
		t.Errorf("Source = %q, want %q — the source is what puts it in the Secrets tab",
			secret.Source, SourceTrivySecret)
	}
	if Categorize(secret) != CategorySecret {
		t.Error("a Trivy secret is not categorised as a secret")
	}
	if secret.ID != "aws-secret-access-key" || secret.File != "app/.env" || secret.Severity != SeverityCritical {
		t.Errorf("the secret does not describe itself: %+v", secret)
	}
	// The same guarantee as for Gitleaks: the result is written to disk and read
	// back by the inventory, so the secret must not be in it.
	if strings.Contains(secret.Match, "AKIAIOSFODNN7EXAMPLE") {
		t.Errorf("Match = %q, want the value masked", secret.Match)
	}
}

// The primary advisory link comes first and is not repeated when it also
// appears in References — the details pane lists them in order.
func TestAdvisoryLinksAreDeduplicated(t *testing.T) {
	findings, err := parseTrivyOutput([]byte(`{
	  "Results": [{"Target":"go.mod","Vulnerabilities":[{
	    "VulnerabilityID":"CVE-1","Severity":"HIGH",
	    "PrimaryURL":"https://avd.aquasec.com/CVE-1",
	    "References":["https://avd.aquasec.com/CVE-1","https://nvd.nist.gov/CVE-1"]
	  }]}]
	}`))
	if err != nil {
		t.Fatalf("parsing failed: %v", err)
	}

	refs := findings[0].References
	if len(refs) != 2 {
		t.Fatalf("references = %v, want the primary URL once plus the advisory", refs)
	}
	if refs[0] != "https://avd.aquasec.com/CVE-1" {
		t.Errorf("the first reference is %q, want the primary URL", refs[0])
	}
}

// A fix command is offered only when there is a version to move to; suggesting
// "update to " would be worse than saying nothing.
func TestAFixCommandNeedsAFixedVersion(t *testing.T) {
	findings, err := parseTrivyOutput([]byte(`{
	  "Results": [{"Target":"go.mod","Vulnerabilities":[
	    {"VulnerabilityID":"CVE-1","Severity":"HIGH","PkgName":"libfoo","FixedVersion":"1.2.3"},
	    {"VulnerabilityID":"CVE-2","Severity":"HIGH","PkgName":"libbar"}
	  ]}]
	}`))
	if err != nil {
		t.Fatalf("parsing failed: %v", err)
	}

	if got := findings[0].FixCommand; !strings.Contains(got, "1.2.3") {
		t.Errorf("FixCommand = %q, want it to name the fixed version", got)
	}
	if got := findings[1].FixCommand; got != "" {
		t.Errorf("FixCommand = %q for a vulnerability with no fix, want empty", got)
	}
}

// Trivy exits non-zero when it finds something, so an empty report is a normal
// outcome and must not read as a failure.
func TestAnEmptyTrivyReportYieldsNoFindings(t *testing.T) {
	findings, err := parseTrivyOutput([]byte(`{"Results":[]}`))
	if err != nil {
		t.Fatalf("parsing an empty report failed: %v", err)
	}
	if len(findings) != 0 {
		t.Errorf("an empty report produced %d findings", len(findings))
	}
}

func TestUnparseableTrivyOutputIsReported(t *testing.T) {
	if _, err := parseTrivyOutput([]byte("Error: DB download failed")); err == nil {
		t.Error("non-JSON output parsed without error")
	}
}

func TestSeverityParsingIsCaseInsensitiveAndDefaultsToUnknown(t *testing.T) {
	tests := map[string]SeverityLevel{
		"CRITICAL": SeverityCritical,
		"critical": SeverityCritical,
		"High":     SeverityHigh,
		"MEDIUM":   SeverityMedium,
		"low":      SeverityLow,
		"":         SeverityUnknown,
		"BOGUS":    SeverityUnknown,
	}

	for in, want := range tests {
		if got := parseSeverity(in); got != want {
			t.Errorf("parseSeverity(%q) = %q, want %q", in, got, want)
		}
	}
}

// ── Gitleaks output ──────────────────────────────────────────────────────────

// Every secret is high severity, and the fingerprint has to survive: it is what
// the ignore file is keyed on.
func TestGitleaksFindingsCarryTheirFingerprint(t *testing.T) {
	findings, err := parseGitleaksOutput([]byte(`[{
	  "RuleID":"aws-access-token","Description":"AWS Access Token",
	  "StartLine":42,"File":"config/prod.env",
	  "Secret":"AKIAIOSFODNN7EXAMPLE",
	  "Fingerprint":"config/prod.env:aws-access-token:42"
	}]`))
	if err != nil {
		t.Fatalf("parsing failed: %v", err)
	}
	if len(findings) != 1 {
		t.Fatalf("got %d findings, want 1", len(findings))
	}

	f := findings[0]
	if f.Severity != SeverityHigh {
		t.Errorf("severity = %q, want every secret to be high", f.Severity)
	}
	if f.Fingerprint != "config/prod.env:aws-access-token:42" {
		t.Errorf("fingerprint = %q; the ignore file is keyed on it", f.Fingerprint)
	}
	if f.Line != 42 || f.File != "config/prod.env" {
		t.Errorf("located at %s:%d, want config/prod.env:42", f.File, f.Line)
	}
	if !strings.Contains(f.Title, "aws-access-token") {
		t.Errorf("title = %q, want it to name the rule", f.Title)
	}
}

// The secret itself must never reach the finding: the results table and the
// cached report on disk would both hold it in clear.
func TestTheSecretItselfIsNeverKept(t *testing.T) {
	findings, err := parseGitleaksOutput(
		[]byte(`[{"RuleID":"generic","Secret":"AKIAIOSFODNN7EXAMPLE","File":"x"}]`))
	if err != nil {
		t.Fatalf("parsing failed: %v", err)
	}

	if strings.Contains(findings[0].Match, "IOSFODNN7EXAM") {
		t.Errorf("the finding carries the secret: %q", findings[0].Match)
	}
}

func TestGitleaksMasksShortAndLongSecrets(t *testing.T) {
	tests := []struct {
		name   string
		secret string
		want   string
	}{
		{"short enough to be all mask", "abc", "****"},
		{"exactly the threshold", "12345678", "****"},
		{"long enough to show its ends", "AKIAIOSFODNN7EXAMPLE", "AKIA****MPLE"},
	}

	for _, tt := range tests {
		t.Run(tt.name, func(t *testing.T) {
			if got := maskSecret(tt.secret); got != tt.want {
				t.Errorf("maskSecret(%q) = %q, want %q", tt.secret, got, tt.want)
			}
		})
	}
}

// D20, recorded not fixed: maskSecret slices by byte, so a secret whose 4th or
// 4th-from-last byte falls inside a multi-byte rune is cut in half and the mask
// renders as a replacement character.
//
// This test asserts the current behaviour and must fail when D20 is fixed.
func TestMaskingSplitsAMultiByteSecret(t *testing.T) {
	if got := maskSecret("日本語のパスワードです"); utf8.ValidString(got) {
		t.Errorf("maskSecret produced valid UTF-8 (%q) — D20 is fixed, delete this "+
			"test and its backlog entry", got)
	}
}

func TestEmptyGitleaksOutputYieldsNoFindings(t *testing.T) {
	findings, err := parseGitleaksOutput([]byte(`[]`))
	if err != nil {
		t.Fatalf("parsing an empty report failed: %v", err)
	}
	if len(findings) != 0 {
		t.Errorf("an empty report produced %d findings", len(findings))
	}
}

func TestUnparseableGitleaksOutputIsReported(t *testing.T) {
	if _, err := parseGitleaksOutput([]byte("panic: no config")); err == nil {
		t.Error("non-JSON output parsed without error")
	}
}

// The class and ecosystem of a package decide what fixes it, so the parser
// records them on vulnerabilities — and only there.
func TestTrivyVulnerabilitiesCarryTheirClassAndEcosystem(t *testing.T) {
	findings, err := parseTrivyOutput([]byte(`{
	  "Results": [
	    {"Target":"alpine 3.18","Class":"os-pkgs","Type":"alpine","Vulnerabilities":[
	      {"VulnerabilityID":"CVE-1","Severity":"HIGH","PkgName":"openssl","InstalledVersion":"3.1.0-r0","FixedVersion":"3.1.4-r0"}]},
	    {"Target":"app/go.mod","Class":"lang-pkgs","Type":"gomod","Vulnerabilities":[
	      {"VulnerabilityID":"CVE-2","Severity":"HIGH","PkgName":"golang.org/x/net","InstalledVersion":"0.10.0","FixedVersion":"0.17.0"}]},
	    {"Target":"Dockerfile","Class":"config","Type":"dockerfile","Misconfigurations":[
	      {"AVDID":"DS002","Severity":"HIGH"}]}
	  ]
	}`))
	if err != nil {
		t.Fatalf("parsing failed: %v", err)
	}
	if len(findings) != 3 {
		t.Fatalf("got %d findings, want 3", len(findings))
	}
	if f := findings[0]; f.Class != ClassOSPackages || f.Ecosystem != "alpine" {
		t.Errorf("os finding: class %q ecosystem %q", f.Class, f.Ecosystem)
	}
	if f := findings[0]; f.FixCommand != "apk upgrade openssl" {
		t.Errorf("os finding FixCommand = %q", f.FixCommand)
	}
	if f := findings[1]; f.Class != ClassLangPackages || f.Ecosystem != "gomod" {
		t.Errorf("lang finding: class %q ecosystem %q", f.Class, f.Ecosystem)
	}
	if f := findings[1]; f.FixCommand != "go get golang.org/x/net@v0.17.0" {
		t.Errorf("lang finding FixCommand = %q", f.FixCommand)
	}
	if f := findings[2]; f.Class != "" || f.Ecosystem != "" {
		t.Errorf("a misconfiguration must not carry a package class, got %q / %q", f.Class, f.Ecosystem)
	}
}

// Trivy lists one fixed version per maintained branch. The command moves to the
// one on the installed major line — the smallest change that clears the CVE.
func TestTheFixCommandFollowsTheInstalledMajorLine(t *testing.T) {
	findings, err := parseTrivyOutput([]byte(`{
	  "Results": [{"Target":"package-lock.json","Class":"lang-pkgs","Type":"npm","Vulnerabilities":[
	    {"VulnerabilityID":"CVE-1","Severity":"HIGH","PkgName":"semver","InstalledVersion":"6.3.0","FixedVersion":"5.7.2, 6.3.1, 7.5.2"}]}]
	}`))
	if err != nil {
		t.Fatalf("parsing failed: %v", err)
	}
	if got, want := findings[0].FixCommand, "npm install semver@6.3.1"; got != want {
		t.Errorf("FixCommand = %q, want %q", got, want)
	}
	// The finding still carries Trivy's whole list.
	if got := findings[0].FixedIn; got != "5.7.2, 6.3.1, 7.5.2" {
		t.Errorf("FixedIn = %q, want Trivy's list untouched", got)
	}
}

// §3.78 — a misconfiguration is the one finding whose exact extent is known:
// Trivy reports the block it faults, not just where it starts. The parser used
// to keep StartLine and drop the rest, which left a caller able to read the
// problem and unable to replace it.
func TestAMisconfigurationCarriesItsSpanMessageAndStatus(t *testing.T) {
	findings, err := parseTrivyOutput([]byte(`{
	  "Results": [
	    {"Target":"Dockerfile","Class":"config","Type":"dockerfile","Misconfigurations":[
	      {"AVDID":"DS002","ID":"DS002","Title":"Image user should not be root",
	       "Description":"Running containers with root user can lead to escalation.",
	       "Message":"Specify at least 1 USER command",
	       "Resolution":"Add USER command","Severity":"HIGH","Status":"FAIL",
	       "PrimaryURL":"https://avd.aquasec.com/misconfig/ds002",
	       "CauseMetadata":{"StartLine":3,"EndLine":7}}]}
	  ]
	}`))
	if err != nil {
		t.Fatalf("parsing failed: %v", err)
	}
	if len(findings) != 1 {
		t.Fatalf("got %d findings, want 1", len(findings))
	}
	f := findings[0]
	if f.Line != 3 || f.EndLine != 7 {
		t.Errorf("span = %d..%d, want 3..7", f.Line, f.EndLine)
	}
	// Message is this instance's wording, Description the rule's generic text.
	// Collapsing the two would lose whichever one it kept.
	if f.Message != "Specify at least 1 USER command" {
		t.Errorf("Message = %q", f.Message)
	}
	if f.Description == f.Message {
		t.Error("Message and Description are the same string; the instance's wording was lost")
	}
	if f.Status != "FAIL" {
		t.Errorf("Status = %q, want FAIL", f.Status)
	}
}

// §3.80 — the dialect a misconfiguration comes from is Trivy's Result.Type,
// and it was decoded then dropped. A KSV rule and a DS rule are otherwise told
// apart only by guessing from the file name.
func TestAMisconfigurationCarriesItsDialect(t *testing.T) {
	findings, err := parseTrivyOutput([]byte(`{
	  "Results": [
	    {"Target":"deploy/app.yaml","Class":"config","Type":"kubernetes","Misconfigurations":[
	      {"AVDID":"KSV-0017","Type":"Kubernetes Security Check","Severity":"HIGH",
	       "CauseMetadata":{"StartLine":20,"EndLine":24}}]},
	    {"Target":"chart/templates/deploy.yaml","Class":"config","Type":"helm","Misconfigurations":[
	      {"AVDID":"KSV-0017","Type":"Helm Security Check","Severity":"HIGH"}]}
	  ]
	}`))
	if err != nil {
		t.Fatalf("parsing failed: %v", err)
	}
	if len(findings) != 2 {
		t.Fatalf("got %d findings, want 2", len(findings))
	}
	// The result's type, not the misconfiguration's label.
	if got := findings[0].IaCType; got != "kubernetes" {
		t.Errorf("IaCType = %q, want kubernetes", got)
	}
	if got := findings[1].IaCType; got != "helm" {
		t.Errorf("IaCType = %q, want helm", got)
	}
}

// An old cached result, and a rule that reports a point rather than a span,
// both arrive with EndLine at zero. Zero reads as "unknown" — never as line
// zero, which does not exist — the same convention Class and Ecosystem follow.
func TestAMisconfigurationWithNoSpanReportsZeroNotALine(t *testing.T) {
	findings, err := parseTrivyOutput([]byte(`{
	  "Results": [
	    {"Target":"Dockerfile","Class":"config","Type":"dockerfile","Misconfigurations":[
	      {"AVDID":"DS026","Severity":"LOW","CauseMetadata":{"StartLine":0,"EndLine":0}}]}
	  ]
	}`))
	if err != nil {
		t.Fatalf("parsing failed: %v", err)
	}
	if len(findings) != 1 {
		t.Fatalf("got %d findings, want 1", len(findings))
	}
	if f := findings[0]; f.Line != 0 || f.EndLine != 0 {
		t.Errorf("span = %d..%d, want 0..0 meaning unknown", f.Line, f.EndLine)
	}
}
