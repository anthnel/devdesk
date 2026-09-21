package remediation

import (
	"reflect"
	"testing"

	"github.com/anthnel/devdesk/internal/scan"
)

func vuln(id, pkg, eco, class, installed, fixed string, sev scan.SeverityLevel) scan.Finding {
	return scan.Finding{
		ID: id, Source: scan.SourceTrivy, Severity: sev,
		PkgName: pkg, Ecosystem: eco, Class: class, Version: installed, FixedIn: fixed,
	}
}

func TestSummarizeSplitsByClass(t *testing.T) {
	findings := []scan.Finding{
		vuln("CVE-1", "openssl", "alpine", scan.ClassOSPackages, "3.1.0", "3.1.4", scan.SeverityHigh),
		vuln("CVE-2", "musl", "alpine", scan.ClassOSPackages, "1.2.3", "1.2.4", scan.SeverityLow),
		vuln("CVE-3", "lodash", "npm", scan.ClassLangPackages, "4.0.0", "4.17.21", scan.SeverityCritical),
		vuln("CVE-4", "old", "", "", "1.0.0", "1.0.1", scan.SeverityHigh),
		vuln("CVE-5", "nofix", "npm", scan.ClassLangPackages, "1.0.0", "", scan.SeverityHigh),
		{ID: "S1", Source: scan.SourceGitleaks},
		{ID: "L1", Source: scan.SourceTrivyLicense, PkgName: "x", FixedIn: "9"},
		{ID: "M1", Source: scan.SourceTrivyMisconfig},
	}
	got := Summarize(findings)
	want := Summary{BaseImage: 2, Dependencies: 1, Unclassified: 1, Unfixed: 1}
	if got != want {
		t.Errorf("Summarize = %+v, want %+v", got, want)
	}
	if got.Fixable() != 4 {
		t.Errorf("Fixable = %d, want 4", got.Fixable())
	}
}

func TestSummarizeOfNothing(t *testing.T) {
	if got := Summarize(nil); got != (Summary{}) {
		t.Errorf("Summarize(nil) = %+v, want zero", got)
	}
}

func TestGroupTakesTheHighestVersionAnyCVENeeds(t *testing.T) {
	fixes := Group([]scan.Finding{
		vuln("CVE-1", "golang.org/x/net", "gomod", scan.ClassLangPackages, "0.10.0", "0.17.0", scan.SeverityHigh),
		vuln("CVE-2", "golang.org/x/net", "gomod", scan.ClassLangPackages, "0.10.0", "0.23.0", scan.SeverityCritical),
		vuln("CVE-3", "golang.org/x/net", "gomod", scan.ClassLangPackages, "0.10.0", "0.17.0", scan.SeverityLow),
	})
	if len(fixes) != 1 {
		t.Fatalf("got %d fixes, want 1", len(fixes))
	}
	fix := fixes[0]
	if fix.Target != "0.23.0" {
		t.Errorf("Target = %q, want the highest, 0.23.0", fix.Target)
	}
	if fix.Command != "go get golang.org/x/net@v0.23.0" {
		t.Errorf("Command = %q", fix.Command)
	}
	if fix.Severity != scan.SeverityCritical {
		t.Errorf("Severity = %q, want the worst", fix.Severity)
	}
	if want := []string{"CVE-1", "CVE-2", "CVE-3"}; !reflect.DeepEqual(fix.CVEs, want) {
		t.Errorf("CVEs = %v, want %v", fix.CVEs, want)
	}
}

func TestGroupChoosesNothingWhenVersionsCannotBeOrdered(t *testing.T) {
	fixes := Group([]scan.Finding{
		vuln("CVE-1", "libfoo", "npm", scan.ClassLangPackages, "1.0.0", "1.0.1", scan.SeverityHigh),
		vuln("CVE-2", "libfoo", "npm", scan.ClassLangPackages, "1.0.0", "weird", scan.SeverityHigh),
	})
	if fixes[0].Target != "" || fixes[0].Command != "" {
		t.Errorf("unorderable versions produced Target %q / Command %q, want none", fixes[0].Target, fixes[0].Command)
	}
	if want := []string{"1.0.1", "weird"}; !reflect.DeepEqual(fixes[0].Options, want) {
		t.Errorf("Options = %v, want %v", fixes[0].Options, want)
	}
}

// One CVE listing several fixed versions Trivy cannot rank is not resolved by
// guessing one of them.
func TestGroupKeepsAnAmbiguousFixedListVisible(t *testing.T) {
	fixes := Group([]scan.Finding{
		vuln("CVE-1", "libfoo", "npm", scan.ClassLangPackages, "1.0.0", "abc, def", scan.SeverityHigh),
	})
	if fixes[0].Target != "" {
		t.Errorf("Target = %q, want none", fixes[0].Target)
	}
	if want := []string{"abc, def"}; !reflect.DeepEqual(fixes[0].Options, want) {
		t.Errorf("Options = %v, want %v", fixes[0].Options, want)
	}
}

func TestGroupKeepsTwoInstalledVersionsApart(t *testing.T) {
	fixes := Group([]scan.Finding{
		vuln("CVE-1", "semver", "npm", scan.ClassLangPackages, "5.0.0", "5.7.2", scan.SeverityHigh),
		vuln("CVE-1", "semver", "npm", scan.ClassLangPackages, "6.0.0", "6.3.1", scan.SeverityHigh),
	})
	if len(fixes) != 2 {
		t.Fatalf("got %d fixes, want one per installed version", len(fixes))
	}
}

func TestGroupKeepsTwoEcosystemsApart(t *testing.T) {
	fixes := Group([]scan.Finding{
		vuln("CVE-1", "openssl", "alpine", scan.ClassOSPackages, "3.0.0", "3.0.1", scan.SeverityHigh),
		vuln("CVE-2", "openssl", "pip", scan.ClassLangPackages, "3.0.0", "3.0.1", scan.SeverityHigh),
	})
	if len(fixes) != 2 {
		t.Fatalf("got %d fixes, want 2", len(fixes))
	}
}

func TestGroupSortsWorstSeverityFirstAndSkipsWhatIsUnfixable(t *testing.T) {
	fixes := Group([]scan.Finding{
		vuln("CVE-1", "a", "npm", scan.ClassLangPackages, "1.0.0", "1.0.1", scan.SeverityLow),
		vuln("CVE-2", "b", "npm", scan.ClassLangPackages, "1.0.0", "1.0.1", scan.SeverityCritical),
		vuln("CVE-3", "c", "npm", scan.ClassLangPackages, "1.0.0", "", scan.SeverityCritical),
		{ID: "S1", Source: scan.SourceGitleaks, PkgName: "d", FixedIn: "1"},
	})
	if len(fixes) != 2 {
		t.Fatalf("got %d fixes, want 2 (unfixed and non-vulnerabilities skipped)", len(fixes))
	}
	if fixes[0].Package != "b" || fixes[1].Package != "a" {
		t.Errorf("order = %s, %s; want b (critical) then a (low)", fixes[0].Package, fixes[1].Package)
	}
}

func TestGroupOfNothing(t *testing.T) {
	if got := Group(nil); len(got) != 0 {
		t.Errorf("Group(nil) = %v, want none", got)
	}
}
