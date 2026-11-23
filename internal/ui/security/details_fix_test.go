package security

import (
	"strings"
	"testing"

	tea "charm.land/bubbletea/v2"

	"github.com/anthnel/devdesk/internal/remediation"
	"github.com/anthnel/devdesk/internal/scan"
	"github.com/anthnel/devdesk/internal/ui/testutil"
)

// DEVDESK-750 (step 3): the fix status line of the finding detail, and the one
// computation it shares with the shortcut column (Rule 130).

const (
	labelFixAvailable   = "Fix available"
	labelNoFixAvailable = "No fix available"
	labelBuiltIn        = "Built-in fix available"
	labelDeclined       = "No built-in fix for this instance"
	labelImage          = "No built-in fix here"
	labelNoBuiltIn      = "No built-in fix"
)

func misconfigOf(id string) scan.Finding {
	return scan.Finding{ID: id, Title: "A misconfiguration", Severity: scan.SeverityHigh,
		Source: scan.SourceTrivyMisconfig, File: "Dockerfile", Line: 1, EndLine: 3}
}

func unconfinedSeccomp() scan.Finding {
	f := misconfigOf("KSV-0104")
	f.File, f.SeccompOrigin = "pod.yaml", "unconfined"
	return f
}

func TestFixStatusOf(t *testing.T) {
	builtIn, ok := remediation.FixFor(misconfigOf("DS002"))
	if !ok {
		t.Fatal("DS002 is no longer in the catalog: pick another built-in rule")
	}
	hostNS, ok := remediation.FixFor(misconfigOf("KSV-0009"))
	if !ok || hostNS.Warning == "" {
		t.Fatal("KSV-0009 must be a catalog rule with a Warning")
	}
	declines := unconfinedSeccomp()
	declineReason := ""
	if rule, ok := remediation.FixFor(declines); ok && rule.Declines != nil {
		declineReason = rule.Declines(declines)
	}
	if declineReason == "" {
		t.Fatal("KSV-0104 unconfined must decline from the finding alone")
	}
	missing, ok := remediation.MissingFor(misconfigOf("KSV-0012"))
	if !ok {
		t.Fatal("KSV-0012 must have a MissingFor text")
	}

	tests := []struct {
		name       string
		f          scan.Finding
		onDisk     bool
		wantLabel  string
		wantDetail string
		wantWarn   string
	}{
		{"CVE with a fixed version", scan.Finding{ID: "CVE-1", Source: scan.SourceTrivy, FixedIn: "1.2.3"}, true,
			labelFixAvailable, "Fixed in 1.2.3", ""},
		{"CVE on an image with a fixed version", scan.Finding{ID: "CVE-1", Source: scan.SourceTrivy, FixedIn: "1.2.3"}, false,
			labelFixAvailable, "Fixed in 1.2.3", ""},
		{"CVE without a fixed version", scan.Finding{ID: "CVE-2", Source: scan.SourceTrivy}, true,
			labelNoFixAvailable, "No fixed version is published", ""},
		{"built-in fix on disk", misconfigOf("DS002"), true, labelBuiltIn, builtIn.Title, builtIn.Warning},
		{"built-in fix with a warning", misconfigOf("KSV-0009"), true, labelBuiltIn, hostNS.Title, hostNS.Warning},
		{"catalog rule that declines this instance", declines, true, labelDeclined, declineReason, ""},
		{"catalog rule on an image", misconfigOf("DS002"), false, labelImage, reasonFileNotOnDisk, ""},
		{"rule with a MissingFor text", misconfigOf("KSV-0012"), true, labelNoBuiltIn, missing, ""},
		{"rule with no fix and no text", misconfigOf("KSV-0003"), true, labelNoBuiltIn, remediation.ReasonNoFixForRule, ""},
		{"render failure", scan.Finding{ID: scan.K8sRenderID, Source: scan.SourceKubeconform}, true,
			labelNoBuiltIn, remediation.ReasonNoFixForRule, ""},
		{"gitleaks secret", scan.Finding{ID: "generic-api-key", Source: scan.SourceGitleaks}, true, "", "", ""},
		{"trivy secret", scan.Finding{ID: "aws-access-key-id", Source: scan.SourceTrivySecret}, true, "", "", ""},
		{"license", scan.Finding{ID: "GPL-3.0", Source: scan.SourceTrivyLicense}, true, "", "", ""},
		{"CI score", scan.Finding{ID: "PLUMBER-1", Source: scan.SourcePlumber}, true, "", "", ""},
	}
	for _, tc := range tests {
		t.Run(tc.name, func(t *testing.T) {
			got := fixStatusOf(tc.f, tc.onDisk)
			want := fixStatus{Label: tc.wantLabel, Detail: tc.wantDetail, Warning: tc.wantWarn}
			if got != want {
				t.Errorf("fixStatusOf(%q, onDisk=%v) = %+v, want %+v", tc.f.ID, tc.onDisk, got, want)
			}
		})
	}
}

// The status carries words, never a key name: the key is the footer's business
// (Rules 130 and 134), and one that is greyed would make a hint a lie.
func TestFixStatusNamesNoKey(t *testing.T) {
	findings := []scan.Finding{
		misconfigOf("DS002"), misconfigOf("KSV-0009"), unconfinedSeccomp(), misconfigOf("KSV-0012"),
		misconfigOf("KSV-0003"), {ID: "CVE-1", Source: scan.SourceTrivy, FixedIn: "1"},
	}
	for _, f := range findings {
		for _, onDisk := range []bool{true, false} {
			s := fixStatusOf(f, onDisk)
			for _, text := range []string{s.Label, s.Detail} {
				lower := strings.ToLower(text)
				for _, key := range []string{"ctrl+", "[o]", "[" + writeRemediationKey + "]"} {
					if strings.Contains(lower, key) {
						t.Errorf("%s (onDisk=%v): %q names the key %q", f.ID, onDisk, text, key)
					}
				}
			}
		}
	}
}

func TestFixAction(t *testing.T) {
	tests := []struct {
		name         string
		f            scan.Finding
		wantLabel    string
		wantText     string
		wantNoAction bool
	}{
		{"go command", scan.Finding{PkgName: "golang.org/x/net", Ecosystem: "gomod", FixCommand: "go get golang.org/x/net@v0.7.0"},
			"Run", "go get golang.org/x/net@v0.7.0", false},
		{"apt command", scan.Finding{PkgName: "curl", FixCommand: "apt-get install --only-upgrade curl"},
			"Run", "apt-get install --only-upgrade curl", false},
		{"instruction", scan.Finding{PkgName: "x", FixCommand: "Update x to 1.2"}, "", "Update x to 1.2", false},
		{"instruction for another package is not recognised, it is a command", scan.Finding{PkgName: "x", FixCommand: "Update y to 1.2"},
			"Run", "Update y to 1.2", false},
		{"empty", scan.Finding{PkgName: "x"}, "", "", true},
		{"nothing", scan.Finding{}, "", "", true},
	}
	for _, tc := range tests {
		t.Run(tc.name, func(t *testing.T) {
			label, text := fixAction(tc.f)
			if label != tc.wantLabel || text != tc.wantText {
				t.Errorf("fixAction = (%q, %q), want (%q, %q)", label, text, tc.wantLabel, tc.wantText)
			}
			if tc.wantNoAction && (label != "" || text != "") {
				t.Errorf("an empty FixCommand must give (\"\", \"\"), got (%q, %q)", label, text)
			}
		})
	}
}

// ── One computation (Rule 130) ───────────────────────────────────────────────

// misconfigModelFor opens the Misconfigurations tab on a result holding f, on a
// directory or on an image.
func misconfigModelFor(t *testing.T, f scan.Finding, kind scan.TargetType) Model {
	t.Helper()
	target := t.TempDir()
	if kind == scan.TargetImage {
		target = "app:1.0"
	}
	result := &scan.Result{Target: target, TargetType: kind, Findings: []scan.Finding{f}}
	m := feed(t, NewWithPreloadedResult(testConfig(), nil, result), tea.WindowSizeMsg{Width: 160, Height: 30})
	for range TabMisconfig {
		m = feed(t, m, testutil.Key("tab"))
	}
	if _, ok := m.findingsTable.Selected(); !ok {
		t.Fatalf("no row selected for %s", f.ID)
	}
	return m
}

func agreementFixtures() []scan.Finding {
	return []scan.Finding{
		misconfigOf("DS002"), misconfigOf("AVD-DS-0002"), misconfigOf("KSV-0001"), misconfigOf("KSV-0009"),
		unconfinedSeccomp(), misconfigOf("KSV-0030"), misconfigOf("KSV-0012"), misconfigOf("KSV-0003"),
		misconfigOf("DS026"), {ID: scan.K8sRenderID, Source: scan.SourceKubeconform, File: "k/base"},
	}
}

// The header's shortcut column and the detail's status say the same thing, on
// every misconfiguration, on a directory and on an image.
func TestFixStatusAndTheShortcutAgree(t *testing.T) {
	for _, f := range agreementFixtures() {
		for _, kind := range []scan.TargetType{scan.TargetDirectory, scan.TargetImage} {
			onDisk := kind == scan.TargetDirectory
			t.Run(f.ID+"/"+string(kind), func(t *testing.T) {
				m := misconfigModelFor(t, f, kind)
				enabled := m.canFixMisconfig().Enabled()
				status := fixStatusOf(f, onDisk)
				if want := status.Label == labelBuiltIn; enabled != want {
					t.Errorf("canFixMisconfig enabled = %v, fixStatusOf label = %q", enabled, status.Label)
				}
				if !onDisk && status.Label == labelBuiltIn {
					t.Errorf("an image target can never read %q", status.Label)
				}
			})
		}
	}
}

// misconfigFixAvailability is what canFixMisconfig now calls: same answer, same
// reason, same rule as the catalog gives.
func TestMisconfigFixAvailabilityIsTheShortcutComputation(t *testing.T) {
	for _, f := range agreementFixtures() {
		for _, kind := range []scan.TargetType{scan.TargetDirectory, scan.TargetImage} {
			t.Run(f.ID+"/"+string(kind), func(t *testing.T) {
				m := misconfigModelFor(t, f, kind)
				want := m.canFixMisconfig()
				rule, got := misconfigFixAvailability(f, kind == scan.TargetDirectory)
				if got.Enabled() != want.Enabled() || got.Reason != want.Reason {
					t.Errorf("misconfigFixAvailability = (enabled %v, %q), canFixMisconfig = (enabled %v, %q)",
						got.Enabled(), got.Reason, want.Enabled(), want.Reason)
				}
				if catalog, ok := remediation.FixFor(f); ok && rule.AVDID != catalog.AVDID {
					t.Errorf("rule = %q, the catalog says %q", rule.AVDID, catalog.AVDID)
				}
			})
		}
	}
}

func TestMisconfigFixAvailabilityReasons(t *testing.T) {
	tests := []struct {
		name       string
		f          scan.Finding
		onDisk     bool
		wantEnable bool
		wantReason string
	}{
		{"built-in on disk", misconfigOf("DS002"), true, true, ""},
		{"image target", misconfigOf("DS002"), false, false, reasonFileNotOnDisk},
		{"no catalog rule", misconfigOf("KSV-0003"), true, false, remediation.ReasonNoFixForRule},
	}
	for _, tc := range tests {
		t.Run(tc.name, func(t *testing.T) {
			_, got := misconfigFixAvailability(tc.f, tc.onDisk)
			if got.Enabled() != tc.wantEnable || got.Reason != tc.wantReason {
				t.Errorf("= (enabled %v, %q), want (%v, %q)", got.Enabled(), got.Reason, tc.wantEnable, tc.wantReason)
			}
		})
	}
}
