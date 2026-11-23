package security

import (
	"strings"
	"testing"

	tea "charm.land/bubbletea/v2"

	"github.com/anthnel/devdesk/internal/remediation"
	"github.com/anthnel/devdesk/internal/scan"
	"github.com/anthnel/devdesk/internal/ui/testutil"
)

// DEVDESK-751 (steps 1 and 2): the closed vocabulary of the Fix line of the
// finding detail, and the one computation it shares with the shortcut column
// (Rule 130). It replaces the DEVDESK-750 labels ("Fix available", "No fix
// available", "No built-in fix here", ...).

func misconfigOf(id string) scan.Finding {
	return scan.Finding{ID: id, Title: "A misconfiguration", Severity: scan.SeverityHigh,
		Source: scan.SourceTrivyMisconfig, File: "Dockerfile", Line: 1, EndLine: 3}
}

func unconfinedSeccomp() scan.Finding {
	f := misconfigOf("KSV-0104")
	f.File, f.SeccompOrigin = "pod.yaml", "unconfined"
	return f
}

// withResolution is f with a Resolution sentence, the guidance a scanner gives.
func withResolution(f scan.Finding) scan.Finding {
	f.Resolution = "Rotate the credential and remove it from the history."
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
	if _, ok := remediation.MissingFor(misconfigOf("KSV-0012")); !ok {
		t.Fatal("KSV-0012 must have a MissingFor text")
	}
	if _, ok := remediation.MissingFor(misconfigOf("KSV-0003")); ok {
		t.Fatal("KSV-0003 must have no MissingFor text: it is the no-guidance rule of these tests")
	}
	cve := scan.Finding{ID: "CVE-1", Source: scan.SourceTrivy, FixedIn: "1.2.3"}

	tests := []struct {
		name      string
		f         scan.Finding
		onDisk    bool
		want      fixStatus
		wantLabel string
		// loose compares State and Reason only: the plan does not say what
		// Effect and Warning hold for this row.
		loose bool
	}{
		{"built-in fix on disk", misconfigOf("DS002"), true,
			fixStatus{State: fixBuiltIn, Effect: builtIn.Title, Warning: builtIn.Warning}, labelBuiltInAvailable, false},
		{"built-in fix with a warning", misconfigOf("KSV-0009"), true,
			fixStatus{State: fixBuiltIn, Effect: hostNS.Title, Warning: hostNS.Warning}, labelBuiltInAvailable, false},
		{"catalog rule that declines this instance", declines, true,
			fixStatus{State: fixBuiltInDeclined, Reason: declineReason}, labelBuiltInUnavailable + ": " + declineReason, true},
		{"catalog rule on an image", misconfigOf("DS002"), false,
			fixStatus{State: fixCannotFixHere, Reason: reasonFileNotOnDisk}, labelCannotFixHere, true},
		{"rule with a MissingFor text on an image", misconfigOf("KSV-0012"), false,
			fixStatus{State: fixCannotFixHere, Reason: reasonFileNotOnDisk}, labelCannotFixHere, true},
		{"rule with no fix on an image", misconfigOf("KSV-0003"), false,
			fixStatus{State: fixCannotFixHere, Reason: reasonFileNotOnDisk}, labelCannotFixHere, true},
		{"rule with a MissingFor text", misconfigOf("KSV-0012"), true, fixStatus{State: fixManual}, labelManual, false},
		{"rule with no fix and a Resolution", withResolution(misconfigOf("KSV-0003")), true,
			fixStatus{State: fixManual}, labelManual, false},
		{"rule with a FixCommand and no catalog fix", func() scan.Finding {
			f := misconfigOf("KSV-0003")
			f.FixCommand = "Edit the manifest"
			return f
		}(), true, fixStatus{State: fixManual}, labelManual, false},
		{"rule with no fix and no text", misconfigOf("KSV-0003"), true, fixStatus{State: fixNoGuidance}, labelNoGuidance, false},
		{"render failure without a Resolution", scan.Finding{ID: scan.K8sRenderID, Source: scan.SourceKubeconform}, true,
			fixStatus{State: fixNoGuidance}, labelNoGuidance, false},
		{"render failure with a Resolution", withResolution(scan.Finding{ID: scan.K8sRenderID, Source: scan.SourceKustomize}), true,
			fixStatus{State: fixManual}, labelManual, false},
		{"CVE with a fixed version", cve, true,
			fixStatus{State: fixManual, Effect: "Fixed in 1.2.3"}, labelManual, false},
		{"CVE on an image with a fixed version", cve, false,
			fixStatus{State: fixManual, Effect: "Fixed in 1.2.3"}, labelManual, false},
		{"CVE without a fixed version", scan.Finding{ID: "CVE-2", Source: scan.SourceTrivy}, true,
			fixStatus{State: fixNoFixPublished}, labelNoFixPublished, false},
		{"gitleaks secret without guidance", scan.Finding{ID: "generic-api-key", Source: scan.SourceGitleaks}, true,
			fixStatus{State: fixNoGuidance}, labelNoGuidance, false},
		{"gitleaks secret with a Resolution", withResolution(scan.Finding{ID: "generic-api-key", Source: scan.SourceGitleaks}), true,
			fixStatus{State: fixManual}, labelManual, false},
		{"trivy secret without guidance", scan.Finding{ID: "aws-access-key-id", Source: scan.SourceTrivySecret}, true,
			fixStatus{State: fixNoGuidance}, labelNoGuidance, false},
		{"trivy secret with a Resolution", withResolution(scan.Finding{ID: "aws-access-key-id", Source: scan.SourceTrivySecret}), true,
			fixStatus{State: fixManual}, labelManual, false},
		{"license without guidance", scan.Finding{ID: "GPL-3.0", Source: scan.SourceTrivyLicense}, true,
			fixStatus{State: fixNoGuidance}, labelNoGuidance, false},
		{"license with a Resolution", withResolution(scan.Finding{ID: "GPL-3.0", Source: scan.SourceTrivyLicense}), true,
			fixStatus{State: fixManual}, labelManual, false},
		{"CI score without guidance", scan.Finding{ID: "PLUMBER-1", Source: scan.SourcePlumber}, true,
			fixStatus{State: fixNoGuidance}, labelNoGuidance, false},
		{"CI score with a Resolution", withResolution(scan.Finding{ID: "PLUMBER-1", Source: scan.SourcePlumber}), true,
			fixStatus{State: fixManual}, labelManual, false},
	}
	for _, tc := range tests {
		t.Run(tc.name, func(t *testing.T) {
			got := fixStatusOf(tc.f, tc.onDisk)
			if tc.loose {
				if got.State != tc.want.State || got.Reason != tc.want.Reason {
					t.Errorf("fixStatusOf(%q, onDisk=%v) = %+v, want state %v and reason %q",
						tc.f.ID, tc.onDisk, got, tc.want.State, tc.want.Reason)
				}
			} else if got != tc.want {
				t.Errorf("fixStatusOf(%q, onDisk=%v) = %+v, want %+v", tc.f.ID, tc.onDisk, got, tc.want)
			}
			if label := got.Label(); label != tc.wantLabel {
				t.Errorf("Label() = %q, want %q", label, tc.wantLabel)
			}
		})
	}
}

// Label is the closed vocabulary of AC1: the constants, and the one label that
// carries a reason after a colon.
func TestFixStatusLabel(t *testing.T) {
	tests := []struct {
		s    fixStatus
		want string
	}{
		{fixStatus{State: fixBuiltIn}, "Built-in fix available"},
		{fixStatus{State: fixBuiltInDeclined, Reason: "Seccomp is Unconfined"}, "Built-in fix unavailable: Seccomp is Unconfined"},
		{fixStatus{State: fixManual}, "Manual"},
		{fixStatus{State: fixNoFixPublished}, "No fix published"},
		{fixStatus{State: fixCannotFixHere, Reason: reasonFileNotOnDisk}, "Cannot fix here"},
		{fixStatus{State: fixNoGuidance}, "No guidance available"},
	}
	for _, tc := range tests {
		if got := tc.s.Label(); got != tc.want {
			t.Errorf("Label() of %+v = %q, want %q", tc.s, got, tc.want)
		}
	}
}

// statusFixtures are the findings the invariants run over: every misconfiguration
// of the agreement set, and one finding per other category, with and without
// guidance.
func statusFixtures() []scan.Finding {
	out := append([]scan.Finding{}, agreementFixtures()...)
	out = append(out,
		scan.Finding{ID: "CVE-1", Source: scan.SourceTrivy, FixedIn: "1.2.3"},
		scan.Finding{ID: "CVE-2", Source: scan.SourceTrivy},
		scan.Finding{ID: "generic-api-key", Source: scan.SourceGitleaks},
		withResolution(scan.Finding{ID: "generic-api-key", Source: scan.SourceGitleaks}),
		scan.Finding{ID: "GPL-3.0", Source: scan.SourceTrivyLicense},
		scan.Finding{ID: "PLUMBER-1", Source: scan.SourcePlumber},
		withResolution(misconfigOf("KSV-0003")),
		scan.Finding{ID: "X-1"},
	)
	return out
}

// Invariant 1: a label is one of the five constants, or "Built-in fix
// unavailable: " followed by a reason.
func TestEveryFixLabelIsInTheClosedVocabulary(t *testing.T) {
	closed := map[string]bool{
		"Built-in fix available": true, "Manual": true, "No fix published": true,
		"Cannot fix here": true, "No guidance available": true,
	}
	const declinedPrefix = "Built-in fix unavailable: "
	for _, f := range statusFixtures() {
		for _, onDisk := range []bool{true, false} {
			label := fixStatusOf(f, onDisk).Label()
			declined := strings.HasPrefix(label, declinedPrefix) && strings.TrimPrefix(label, declinedPrefix) != ""
			if !closed[label] && !declined {
				t.Errorf("%s (onDisk=%v): %q is outside the closed vocabulary", f.ID, onDisk, label)
			}
		}
	}
	for _, c := range []struct{ got, want string }{
		{labelBuiltInAvailable, "Built-in fix available"}, {labelBuiltInUnavailable, "Built-in fix unavailable"},
		{labelManual, "Manual"}, {labelNoFixPublished, "No fix published"},
		{labelCannotFixHere, "Cannot fix here"}, {labelNoGuidance, "No guidance available"},
	} {
		if c.got != c.want {
			t.Errorf("label constant = %q, want %q", c.got, c.want)
		}
	}
}

func TestHasGuidance(t *testing.T) {
	tests := []struct {
		name string
		f    scan.Finding
		want bool
	}{
		{"nothing", scan.Finding{ID: "X-1"}, false},
		{"a Resolution", scan.Finding{Resolution: "Do the thing"}, true},
		{"a FixCommand", scan.Finding{FixCommand: "go get x@v1"}, true},
		{"a MissingFor text", misconfigOf("KSV-0012"), true},
		{"a rule with no text", misconfigOf("KSV-0003"), false},
	}
	for _, tc := range tests {
		t.Run(tc.name, func(t *testing.T) {
			if got := hasGuidance(tc.f); got != tc.want {
				t.Errorf("hasGuidance = %v, want %v", got, tc.want)
			}
		})
	}
}

// The status carries words, never a key name: the key is the footer's business
// (Rules 130 and 134), and one that is greyed would make a hint a lie.
func TestFixStatusNamesNoKey(t *testing.T) {
	for _, f := range statusFixtures() {
		for _, onDisk := range []bool{true, false} {
			s := fixStatusOf(f, onDisk)
			for _, text := range []string{s.Label(), s.Reason, s.Effect, s.Warning} {
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

// Invariant 2: "Built-in fix available" is shown exactly when ctrl+o is enabled,
// on every misconfiguration, on a directory and on an image.
func TestFixStatusAndTheShortcutAgree(t *testing.T) {
	for _, f := range agreementFixtures() {
		for _, kind := range []scan.TargetType{scan.TargetDirectory, scan.TargetImage} {
			onDisk := kind == scan.TargetDirectory
			t.Run(f.ID+"/"+string(kind), func(t *testing.T) {
				m := misconfigModelFor(t, f, kind)
				enabled := m.canFixMisconfig().Enabled()
				// m.fixStatus is the Fix line of the detail: the same computation plus the
				// stored preview, of which a results state has none (DEVDESK-753).
				status := m.fixStatus(f)
				if want := fixStatusOf(f, onDisk); status != want {
					t.Errorf("fixStatus = %+v with no stored preview, fixStatusOf = %+v", status, want)
				}
				if want := status.State == fixBuiltIn; enabled != want {
					t.Errorf("canFixMisconfig enabled = %v, fixStatusOf = %+v (%q)", enabled, status, status.Label())
				}
				if enabled != (status.Label() == labelBuiltInAvailable) {
					t.Errorf("canFixMisconfig enabled = %v, label = %q", enabled, status.Label())
				}
				if !onDisk && status.State == fixBuiltIn {
					t.Errorf("an image target can never read %q", status.Label())
				}
			})
		}
	}
}

// Invariant 3: when ctrl+o is refused, the footer says the reason, and the Fix
// line says the same one: "Built-in fix unavailable: R" for a catalog rule that
// declines (R is the footer), "Cannot fix here" on an image (reasonFileNotOnDisk).
func TestTheFooterAfterCtrlOSaysTheReasonOfTheFixLine(t *testing.T) {
	refused := 0
	for _, f := range agreementFixtures() {
		for _, kind := range []scan.TargetType{scan.TargetDirectory, scan.TargetImage} {
			t.Run(f.ID+"/"+string(kind), func(t *testing.T) {
				m := misconfigModelFor(t, f, kind)
				a := m.canFixMisconfig()
				if a.Enabled() {
					return
				}
				refused++
				status := fixStatusOf(f, kind == scan.TargetDirectory)
				switch _, inCatalog := remediation.FixFor(f); {
				case kind == scan.TargetImage:
					if status.State != fixCannotFixHere || status.Reason != reasonFileNotOnDisk {
						t.Errorf("on an image the status is %+v, want Cannot fix here with %q", status, reasonFileNotOnDisk)
					}
				case inCatalog:
					if status.State != fixBuiltInDeclined || status.Reason != a.Reason {
						t.Errorf("a catalog rule that declines reads %+v, want Built-in fix unavailable with %q", status, a.Reason)
					}
				}
				m, _ = step(t, m, testutil.Key(writeRemediationKey))
				if footer := footerText(m, 400); !strings.Contains(footer, a.Reason) {
					t.Errorf("the footer after ctrl+o does not say %q:\n%s", a.Reason, footer)
				}
			})
		}
	}
	if refused == 0 {
		t.Fatal("no fixture is refused: the invariant is not exercised")
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
