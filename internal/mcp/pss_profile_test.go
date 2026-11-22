package mcp

import (
	"strings"
	"testing"

	"github.com/anthnel/devdesk/internal/scan"
)

// DEVDESK-686 (S-01), criterion 7: scan_result gives a Pod Security finding its
// effective profile in pss_profile, and a finding outside Pod Security has no
// such field at all (an absent field, not an empty one).

const pssAnchorRestricted = "https://kubernetes.io/docs/concepts/security/pod-security-standards/#restricted"

func k8sFinding(id, title string, sev scan.SeverityLevel) scan.Finding {
	return scan.Finding{
		ID: id, Title: title, Severity: sev, Source: scan.SourceTrivyMisconfig,
		File: "deploy.yaml", Line: 3, EndLine: 9, IaCType: "kubernetes",
	}
}

func TestScanResultCarriesThePodSecurityProfileOfAFinding(t *testing.T) {
	fakeCacheHome(t)
	stubImages(t, "app:1.0")

	unconfined := k8sFinding("KSV-0104", "Seccomp policies disabled", scan.SeverityMedium)
	unconfined.SeccompOrigin = "unconfined"
	absent := k8sFinding("KSV-0104", "Seccomp policies disabled", scan.SeverityMedium)
	absent.SeccompOrigin = "absent"
	absent.File = "other.yaml"
	unknownOrigin := k8sFinding("KSV-0104", "Seccomp policies disabled", scan.SeverityMedium)
	unknownOrigin.File = "legacy.yaml"
	fromTrivy := k8sFinding("KSV-9999", "A rule the table lacks", scan.SeverityLow)
	fromTrivy.References = []string{"https://avd.example/KSV-9999", pssAnchorRestricted}

	storeImageResult(t, "app:1.0", &scan.Result{
		Target:         "app:1.0",
		MisconfigCount: 9,
		Findings: []scan.Finding{
			k8sFinding("KSV-0017", "Privileged", scan.SeverityHigh),
			k8sFinding("KSV-0030", "Runtime/Default Seccomp profile not set", scan.SeverityLow),
			k8sFinding("KSV-0003", "Default capabilities not dropped", scan.SeverityLow),
			unconfined, absent, unknownOrigin, fromTrivy,
			{ID: "DS002", Title: "Image user should not be root", Severity: "HIGH", Source: scan.SourceTrivyMisconfig, File: "Dockerfile", IaCType: "dockerfile"},
			{ID: "CVE-1", Severity: "HIGH", Source: scan.SourceTrivy},
		},
	})

	var out scanResultOut
	callTool(t, connect(t, testEnv(nil)), "scan_result", map[string]any{"target": "app:1.0"}, &out)

	type key struct{ id, file string }
	want := map[key]string{
		{"KSV-0017", "deploy.yaml"}: "baseline",
		{"KSV-0030", "deploy.yaml"}: "restricted",
		{"KSV-0003", "deploy.yaml"}: "", // not Pod Security
		// The effective profile of DevDesk, which the filter also uses: a seccomp
		// profile that is absent is a restricted requirement, an explicit
		// Unconfined or an unknown origin a baseline one.
		{"KSV-0104", "deploy.yaml"}: "baseline",
		{"KSV-0104", "other.yaml"}:  "restricted",
		{"KSV-0104", "legacy.yaml"}: "baseline",
		{"KSV-9999", "deploy.yaml"}: "restricted", // read from Trivy's anchor
		{"DS002", "Dockerfile"}:     "",
		{"CVE-1", ""}:               "",
	}
	if len(out.Findings) != len(want) {
		t.Fatalf("got %d findings, want %d", len(out.Findings), len(want))
	}
	for _, f := range out.Findings {
		w, ok := want[key{f.ID, f.File}]
		if !ok {
			t.Errorf("unexpected finding %s in %s", f.ID, f.File)
			continue
		}
		if f.PSSProfile != w {
			t.Errorf("finding %s in %q has pss_profile %q, want %q", f.ID, f.File, f.PSSProfile, w)
		}
	}
}

// An absent field, not an empty string: the serialised answer of a result with
// no Pod Security finding never names the key.
func TestScanResultHasNoPodSecurityFieldOutsidePodSecurity(t *testing.T) {
	fakeCacheHome(t)
	stubImages(t, "app:1.0")
	storeImageResult(t, "app:1.0", &scan.Result{
		Target:         "app:1.0",
		MisconfigCount: 2,
		Findings: []scan.Finding{
			k8sFinding("KSV-0003", "Default capabilities not dropped", scan.SeverityLow),
			{ID: "DS002", Title: "Image user should not be root", Severity: "HIGH", Source: scan.SourceTrivyMisconfig, File: "Dockerfile", IaCType: "dockerfile"},
			{ID: "CVE-1", Severity: "HIGH", Source: scan.SourceTrivy},
		},
	})

	raw := callToolRaw(t, connect(t, testEnv(nil)), "scan_result", map[string]any{"target": "app:1.0"})
	if strings.Contains(raw, "pss_profile") {
		t.Errorf("the answer names pss_profile although no finding is a Pod Security one:\n%s", raw)
	}
}

// The field is there, with its value, in what crosses the pipe.
func TestTheSerialisedPodSecurityProfileIsNamed(t *testing.T) {
	fakeCacheHome(t)
	stubImages(t, "app:1.0")
	storeImageResult(t, "app:1.0", &scan.Result{
		Target:         "app:1.0",
		MisconfigCount: 1,
		Findings:       []scan.Finding{k8sFinding("KSV-0017", "Privileged", scan.SeverityHigh)},
	})

	raw := callToolRaw(t, connect(t, testEnv(nil)), "scan_result", map[string]any{"target": "app:1.0"})
	if !strings.Contains(raw, `pss_profile`) || !strings.Contains(raw, `baseline`) {
		t.Errorf("the answer lacks pss_profile baseline:\n%s", raw)
	}
}
