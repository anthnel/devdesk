package security

import (
	"fmt"
	"reflect"
	"slices"
	"strings"
	"testing"

	tea "charm.land/bubbletea/v2"
	"github.com/charmbracelet/x/ansi"

	"github.com/anthnel/devdesk/internal/remediation"
	"github.com/anthnel/devdesk/internal/scan"
	sharedcomponents "github.com/anthnel/devdesk/internal/ui/components"
	"github.com/anthnel/devdesk/internal/ui/testutil"
)

// DEVDESK-686 (S-01), criteria 1 to 6: the Pod Security label on the
// Misconfigurations tab, the filter on `p`, its header and the standing warning
// of an invalid scan.pss_profile. Footer timers are never run: the footer is
// read through Text and Level.

const (
	pssPage       = "https://kubernetes.io/docs/concepts/security/pod-security-standards/"
	pssBaselineLn = pssPage + "#baseline"
	pssRestrictLn = pssPage + "#restricted"
)

// pssFinding is a Kubernetes misconfiguration the way a scan records it.
func pssFinding(id, title, file string, sev scan.SeverityLevel) scan.Finding {
	return scan.Finding{
		ID: id, Title: title, Severity: sev, Source: scan.SourceTrivyMisconfig,
		File: file, Line: 4, EndLine: 12, IaCType: "kubernetes",
	}
}

// pssFixtures: six Pod Security findings (three baseline, three restricted,
// one of them read from Trivy's anchor) and three that are not Pod Security.
// The severities are chosen so that the severity filter and the Pod Security
// one give different answers.
func pssFixtures() []scan.Finding {
	unconfined := pssFinding("KSV-0104", "Seccomp policies disabled", "a.yaml", scan.SeverityMedium)
	unconfined.SeccompOrigin = "unconfined"
	absent := pssFinding("KSV-0104", "Seccomp policies disabled", "b.yaml", scan.SeverityLow)
	absent.SeccompOrigin = "absent"
	fromTrivy := pssFinding("KSV-9999", "A rule the table lacks", "deploy.yaml", scan.SeverityHigh)
	fromTrivy.References = []string{"https://avd.example/KSV-9999", pssRestrictLn}
	noAnchor := pssFinding("KSV-0117", "Ports below 1024", "deploy.yaml", scan.SeverityLow)
	noAnchor.References = []string{pssPage}
	dockerfile := scan.Finding{
		ID: "DS002", Title: "Image user should not be root", Severity: scan.SeverityHigh,
		Source: scan.SourceTrivyMisconfig, File: "Dockerfile", IaCType: "dockerfile",
	}
	return []scan.Finding{
		pssFinding("KSV-0017", "Privileged", "deploy.yaml", scan.SeverityHigh),
		pssFinding("KSV-0022", "Non-default capabilities", "deploy.yaml", scan.SeverityCritical),
		pssFinding("KSV-0030", "Runtime/Default Seccomp profile not set", "deploy.yaml", scan.SeverityCritical),
		pssFinding("KSV-0003", "Default capabilities not dropped", "deploy.yaml", scan.SeverityCritical),
		unconfined, absent, fromTrivy, dockerfile, noAnchor,
	}
}

// pssKeys names findings by rule and file: KSV-0104 is there twice.
func pssKeys(findings []scan.Finding) []string {
	out := make([]string, 0, len(findings))
	for _, f := range findings {
		out = append(out, f.ID+"@"+f.File)
	}
	return out
}

func visibleKeys(m Model) []string { return pssKeys(m.findingsTable.Visible()) }

// Every key of the fixtures, in the order the scan reported them.
var allPSSKeys = []string{
	"KSV-0017@deploy.yaml", "KSV-0022@deploy.yaml", "KSV-0030@deploy.yaml", "KSV-0003@deploy.yaml",
	"KSV-0104@a.yaml", "KSV-0104@b.yaml", "KSV-9999@deploy.yaml", "DS002@Dockerfile", "KSV-0117@deploy.yaml",
}

// The restricted findings, which baseline leaves out.
var restrictedPSSKeys = []string{"KSV-0030@deploy.yaml", "KSV-0104@b.yaml", "KSV-9999@deploy.yaml"}

func without(keys []string, drop ...string) []string {
	var out []string
	for _, k := range keys {
		if !slices.Contains(drop, k) {
			out = append(out, k)
		}
	}
	return out
}

// pssModel is the results of findings with scan.pss_profile = profile, on the
// results state, laid out, on the CVE tab.
func pssModel(t *testing.T, profile string, findings []scan.Finding) Model {
	t.Helper()
	cfg := testConfig()
	cfg.Scan.PSSProfile = profile
	result := resultFixture()
	result.Findings = findings
	return feed(t, NewWithPreloadedResult(cfg, nil, result), tea.WindowSizeMsg{Width: 160, Height: 30})
}

// onMisconfig opens the Misconfigurations tab the way a user does: tab, three
// times.
func onMisconfig(t *testing.T, profile string, findings []scan.Finding) Model {
	t.Helper()
	m := pssModel(t, profile, findings)
	m = feed(t, m, testutil.Key("tab"), testutil.Key("tab"), testutil.Key("tab"))
	if m.activeTab != TabMisconfig {
		t.Fatalf("activeTab = %d, want the Misconfigurations tab", m.activeTab)
	}
	return m
}

func titleColumn(t *testing.T) int {
	t.Helper()
	for i, c := range findingColumns() {
		if c.Title == "Title" {
			return i
		}
	}
	t.Fatal("no Title column")
	return -1
}

// headerText is every header entry as "Key Value", ANSI removed.
func headerText(m Model) string {
	var parts []string
	for _, h := range m.GetHeaderInfo("ctx") {
		parts = append(parts, ansi.Strip(h.Key+" "+h.Value))
	}
	return strings.Join(parts, "\n")
}

func podSecurityHeader(m Model) (string, bool) {
	for _, h := range m.GetHeaderInfo("ctx") {
		if h.Key == "Pod Security" {
			return ansi.Strip(h.Value), true
		}
	}
	return "", false
}

func footerOf(m Model) (string, sharedcomponents.Level) { return m.footer.Text(), m.footer.Level() }

// ── The label (criterion 2) ──────────────────────────────────────────────────

func TestPSSLabel(t *testing.T) {
	byKey := map[string]scan.Finding{}
	for _, f := range pssFixtures() {
		byKey[f.ID+"@"+f.File] = f
	}
	tests := []struct{ key, want string }{
		{"KSV-0017@deploy.yaml", "baseline"},
		{"KSV-0022@deploy.yaml", "baseline"}, // Trivy's profile, even if stricter than the standard
		{"KSV-0030@deploy.yaml", "restricted"},
		{"KSV-0003@deploy.yaml", ""},                                // not Pod Security
		{"KSV-0104@a.yaml", "baseline"},                             // explicit Unconfined
		{"KSV-0104@b.yaml", "restricted"},                           // seccomp absent
		{"KSV-9999@deploy.yaml", "restricted (from Trivy, no fix)"}, // anchor outside the table
		{"DS002@Dockerfile", ""},
		{"KSV-0117@deploy.yaml", ""}, // links the standard without a profile
	}
	for _, tt := range tests {
		t.Run(tt.key, func(t *testing.T) {
			if got := pssLabel(byKey[tt.key]); got != tt.want {
				t.Errorf("pssLabel = %q, want %q", got, tt.want)
			}
		})
	}
}

// An unknown origin is a baseline finding: nothing is relabelled on a guess.
// A result cached before the references were kept still gets its label from
// the table.
func TestPSSLabelOfACachedFindingWithoutOrigin(t *testing.T) {
	f := pssFinding("KSV-0104", "Seccomp policies disabled", "old.yaml", scan.SeverityMedium)
	if got := pssLabel(f); got != "baseline" {
		t.Errorf("pssLabel = %q, want baseline", got)
	}
	f = pssFinding("KSV-0017", "Privileged", "old.yaml", scan.SeverityHigh)
	f.References = nil
	if got := pssLabel(f); got != "baseline" {
		t.Errorf("pssLabel without references = %q, want baseline", got)
	}
}

// Only a Trivy misconfiguration carries a label: a kubeconform schema finding
// or a CVE never does, whatever its ID.
func TestOnlyATrivyMisconfigurationIsLabelled(t *testing.T) {
	for _, src := range []string{scan.SourceKubeconform, scan.SourceTrivy, scan.SourceBuildContext} {
		f := pssFinding("KSV-0017", "Privileged", "deploy.yaml", scan.SeverityHigh)
		f.Source = src
		if got := pssLabel(f); got != "" {
			t.Errorf("source %q: pssLabel = %q, want none", src, got)
		}
	}
}

func TestTheTitleCellCarriesTheLabelAsAPrefix(t *testing.T) {
	cell := findingColumns()[titleColumn(t)].Cell
	tests := []struct {
		f    scan.Finding
		want string
	}{
		{pssFinding("KSV-0017", "Privileged", "d.yaml", scan.SeverityHigh), "baseline · Privileged"},
		{pssFinding("KSV-0030", "Seccomp not set", "d.yaml", scan.SeverityHigh), "restricted · Seccomp not set"},
		{pssFixtures()[6], "restricted (from Trivy, no fix) · A rule the table lacks"},
		// Outside Pod Security: the title as it was, no separator.
		{pssFinding("KSV-0003", "Capabilities not dropped", "d.yaml", scan.SeverityHigh), "Capabilities not dropped"},
		{scan.Finding{ID: "CVE-1", Title: "Flaw in libfoo", Source: scan.SourceTrivy}, "Flaw in libfoo"},
	}
	for _, tt := range tests {
		if got := cell(tt.f); got != tt.want {
			t.Errorf("Title cell of %s = %q, want %q", tt.f.ID, got, tt.want)
		}
	}
	// Rule 122: plain text, no escape sequence in a cell.
	for _, f := range pssFixtures() {
		if got := cell(f); got != ansi.Strip(got) {
			t.Errorf("Title cell of %s carries an escape sequence: %q", f.ID, got)
		}
	}
}

// The label is a prefix of the text, not a reason to sort differently: the sort
// stays on the title, the search sees the label.
func TestTheLabelDoesNotChangeTheTitleSortButIsSearchable(t *testing.T) {
	col := findingColumns()[titleColumn(t)]
	labelled := pssFinding("KSV-0017", "Aaa", "d.yaml", scan.SeverityHigh)
	plain := pssFinding("KSV-0003", "Zzz", "d.yaml", scan.SeverityHigh)
	if !col.Less(labelled, plain) || col.Less(plain, labelled) {
		t.Error("the Title sort follows the label instead of the title")
	}
	restricted := pssFinding("KSV-0030", "Seccomp not set", "d.yaml", scan.SeverityHigh)
	if !strings.Contains(col.Search(restricted), "restricted") {
		t.Errorf("the Title search text %q lacks the label, so /restricted finds nothing", col.Search(restricted))
	}
}

func TestTheLabelShowsInTheTableWhateverTheSetting(t *testing.T) {
	for _, profile := range []string{"", "all", "baseline", "restricted"} {
		t.Run("profile "+profile, func(t *testing.T) {
			m := onMisconfig(t, profile, pssFixtures())
			cell := findingColumns()[titleColumn(t)].Cell
			got := map[string]string{}
			for _, f := range m.findingsTable.Visible() {
				got[f.ID+"@"+f.File] = cell(f)
			}
			if !strings.HasPrefix(got["KSV-0017@deploy.yaml"], "baseline") {
				t.Errorf("KSV-0017 reads %q", got["KSV-0017@deploy.yaml"])
			}
			if !strings.HasPrefix(got["KSV-0030@deploy.yaml"], "restricted") {
				t.Errorf("KSV-0030 reads %q", got["KSV-0030@deploy.yaml"])
			}
			if strings.Contains(got["KSV-0003@deploy.yaml"], "·") {
				t.Errorf("KSV-0003 reads %q, want no label", got["KSV-0003@deploy.yaml"])
			}
			// And in what is drawn.
			if rendered := ansi.Strip(m.findingsTable.View()); !strings.Contains(rendered, "baseline · Privileged") {
				t.Errorf("the table does not draw the label:\n%s", rendered)
			}
		})
	}
}

// The details name the profile too, and say when it is Trivy's alone.
func TestTheDetailsNameThePodSecurityProfile(t *testing.T) {
	m := onMisconfig(t, "baseline", pssFixtures())
	content := func(f scan.Finding) string {
		m.selectedFinding = &f
		return ansi.Strip(m.buildDetailsContent())
	}
	byKey := map[string]scan.Finding{}
	for _, f := range pssFixtures() {
		byKey[f.ID+"@"+f.File] = f
	}

	got := content(byKey["KSV-0017@deploy.yaml"])
	if !strings.Contains(got, "Pod Security") || !strings.Contains(got, "baseline") {
		t.Errorf("details of KSV-0017 lack the profile:\n%s", got)
	}
	// DEVDESK-751: the profile is a reason, so it sits under Why, after the Fix block.
	if why, pod := strings.Index(got, "Why:"), strings.Index(got, "Pod Security"); why < 0 || pod < why {
		t.Errorf("the Pod Security label (at %d) must be under Why (at %d):\n%s", pod, why, got)
	}
	if got := content(byKey["KSV-9999@deploy.yaml"]); !strings.Contains(got, "restricted") || !strings.Contains(got, "from Trivy, no fix") {
		t.Errorf("details of KSV-9999 lack the profile or its origin:\n%s", got)
	}
	if got := content(byKey["KSV-0003@deploy.yaml"]); strings.Contains(got, "Pod Security") {
		t.Errorf("details of a rule outside Pod Security mention it:\n%s", got)
	}
}

// ── The setting alone changes nothing (criterion 1) ─────────────────────────

func TestWithoutAProfileTheListIsTheScanOrderAndNothingIsHidden(t *testing.T) {
	for _, profile := range []string{"", "all"} {
		t.Run("profile "+profile, func(t *testing.T) {
			m := onMisconfig(t, profile, pssFixtures())
			if got := visibleKeys(m); !reflect.DeepEqual(got, allPSSKeys) {
				t.Errorf("visible = %v, want the scan order %v", got, allPSSKeys)
			}
			if m.findingsTable.IsTokenActive(pssToken) {
				t.Error("the filter is on")
			}
			if _, ok := podSecurityHeader(m); ok {
				t.Errorf("the header speaks of Pod Security under %q:\n%s", profile, headerText(m))
			}
		})
	}
}

// Under all, `p` is greyed with the reason and the key never hides anything.
func TestUnderAllThePodSecurityFilterIsGreyedAndHidesNothing(t *testing.T) {
	for _, profile := range []string{"", "all"} {
		t.Run("profile "+profile, func(t *testing.T) {
			m := onMisconfig(t, profile, pssFixtures())
			if !testutil.HasShortcut(m.GetShortcuts(), pssFilterKey) {
				t.Fatal("p disappeared instead of being greyed (Rule 130)")
			}
			if !testutil.ShortcutDisabled(m.GetShortcuts(), pssFilterKey) {
				t.Error("p is offered under all")
			}
			if a := m.canTogglePSS(); a.Reason != reasonPSSAll {
				t.Errorf("reason = %q, want %q", a.Reason, reasonPSSAll)
			}
			m = feed(t, m, testutil.Key(pssFilterKey))
			if m.findingsTable.IsTokenActive(pssToken) {
				t.Error("p switched the filter on under all")
			}
			if got := visibleKeys(m); !reflect.DeepEqual(got, allPSSKeys) {
				t.Errorf("visible = %v after p, want everything", got)
			}
			text, level := footerOf(m)
			if text != reasonPSSAll || level != sharedcomponents.LevelWarning {
				t.Errorf("footer = %q (level %v), want the reason as a Warn", text, level)
			}
		})
	}
}

// ── The filter (criteria 3 and 4) ────────────────────────────────────────────

func TestTheFilterIsOffWhenTheTabOpens(t *testing.T) {
	for _, profile := range []string{"baseline", "restricted"} {
		t.Run(profile, func(t *testing.T) {
			m := onMisconfig(t, profile, pssFixtures())
			if m.findingsTable.IsTokenActive(pssToken) {
				t.Fatal("the filter is on at opening")
			}
			if got := visibleKeys(m); !reflect.DeepEqual(got, allPSSKeys) {
				t.Errorf("visible = %v, want everything, in the scan order", got)
			}
			// A result opened with its tab already on is no different.
			opened := pssModel(t, profile, pssFixtures())
			if opened.findingsTable.IsTokenActive(pssToken) {
				t.Error("the filter is on at construction")
			}
		})
	}
}

func TestPTogglesTheFilter(t *testing.T) {
	m := onMisconfig(t, "baseline", pssFixtures())
	if !testutil.ShortcutEnabled(m.GetShortcuts(), pssFilterKey) {
		t.Fatal("p is not offered on the Misconfigurations tab under baseline")
	}
	if a := m.canTogglePSS(); !a.Enabled() {
		t.Fatalf("canTogglePSS: %q", a.Reason)
	}
	m = feed(t, m, testutil.Key(pssFilterKey))
	if !m.findingsTable.IsTokenActive(pssToken) {
		t.Fatal("p did not switch the filter on")
	}
	m = feed(t, m, testutil.Key(pssFilterKey))
	if m.findingsTable.IsTokenActive(pssToken) {
		t.Error("a second p did not switch it off")
	}
	if got := visibleKeys(m); !reflect.DeepEqual(got, allPSSKeys) {
		t.Errorf("visible = %v, want everything again", got)
	}
}

func TestUnderBaselineTheFilterHidesTheRestrictedFindingsOnly(t *testing.T) {
	m := feed(t, onMisconfig(t, "baseline", pssFixtures()), testutil.Key(pssFilterKey))
	want := without(allPSSKeys, restrictedPSSKeys...)
	if got := visibleKeys(m); !reflect.DeepEqual(got, want) {
		t.Errorf("visible = %v, want %v", got, want)
	}
	// The pieces of criterion 3 spelled out: baseline stays, a KSV outside
	// Pod Security stays, restricted goes.
	for _, k := range []string{"KSV-0017@deploy.yaml", "KSV-0003@deploy.yaml", "DS002@Dockerfile", "KSV-0117@deploy.yaml"} {
		if !slices.Contains(visibleKeys(m), k) {
			t.Errorf("%s is hidden under baseline", k)
		}
	}
	if slices.Contains(visibleKeys(m), "KSV-0030@deploy.yaml") {
		t.Error("KSV-0030 (restricted) is shown under baseline")
	}
}

// restricted accumulates baseline: nothing is hidden.
func TestUnderRestrictedTheFilterHidesNothing(t *testing.T) {
	m := feed(t, onMisconfig(t, "restricted", pssFixtures()), testutil.Key(pssFilterKey))
	if !m.findingsTable.IsTokenActive(pssToken) {
		t.Fatal("the filter is off")
	}
	if got := visibleKeys(m); !reflect.DeepEqual(got, allPSSKeys) {
		t.Errorf("visible = %v, want everything", got)
	}
}

// Criterion 5: KSV-0104 on an explicit Unconfined is a baseline finding, on an
// absent seccomp profile a restricted one; KSV-0022 keeps the profile Trivy
// gives it.
func TestTheSeccompOriginDecidesWhereKSV0104Belongs(t *testing.T) {
	baseline := feed(t, onMisconfig(t, "baseline", pssFixtures()), testutil.Key(pssFilterKey))
	if !slices.Contains(visibleKeys(baseline), "KSV-0104@a.yaml") {
		t.Error("KSV-0104 on an explicit Unconfined is hidden under baseline")
	}
	if slices.Contains(visibleKeys(baseline), "KSV-0104@b.yaml") {
		t.Error("KSV-0104 on an absent seccomp profile is shown under baseline")
	}
	if !slices.Contains(visibleKeys(baseline), "KSV-0022@deploy.yaml") {
		t.Error("KSV-0022 is hidden under baseline, its profile in Trivy")
	}

	restricted := feed(t, onMisconfig(t, "restricted", pssFixtures()), testutil.Key(pssFilterKey))
	for _, k := range []string{"KSV-0104@a.yaml", "KSV-0104@b.yaml", "KSV-0030@deploy.yaml", "KSV-0022@deploy.yaml"} {
		if !slices.Contains(visibleKeys(restricted), k) {
			t.Errorf("%s is hidden under restricted", k)
		}
	}
}

// A KSV-0104 of unknown origin (cached before the origin was kept) is baseline:
// shown under baseline, hidden by nothing.
func TestAnUnknownSeccompOriginIsKeptUnderBaseline(t *testing.T) {
	unknown := pssFinding("KSV-0104", "Seccomp policies disabled", "old.yaml", scan.SeverityMedium)
	m := feed(t, onMisconfig(t, "baseline", []scan.Finding{unknown}), testutil.Key(pssFilterKey))
	if got := visibleKeys(m); !reflect.DeepEqual(got, []string{"KSV-0104@old.yaml"}) {
		t.Errorf("visible = %v, want the finding kept", got)
	}
}

// Leaving the tab, in either direction, puts the filter back to off.
func TestChangingTabSwitchesTheFilterOff(t *testing.T) {
	for _, away := range []string{"tab", "shift+tab"} {
		t.Run(away, func(t *testing.T) {
			m := feed(t, onMisconfig(t, "baseline", pssFixtures()), testutil.Key(pssFilterKey))
			if !m.findingsTable.IsTokenActive(pssToken) {
				t.Fatal("the filter is off before the test starts")
			}
			m = feed(t, m, testutil.Key(away))
			if m.findingsTable.IsTokenActive(pssToken) {
				t.Error("the filter is still on after leaving the tab")
			}
			back := "shift+tab"
			if away == "shift+tab" {
				back = "tab"
			}
			m = feed(t, m, testutil.Key(back))
			if m.activeTab != TabMisconfig {
				t.Fatalf("activeTab = %d, want the Misconfigurations tab", m.activeTab)
			}
			if m.findingsTable.IsTokenActive(pssToken) {
				t.Error("the filter is on when coming back")
			}
			if got := visibleKeys(m); !reflect.DeepEqual(got, allPSSKeys) {
				t.Errorf("visible = %v, want everything", got)
			}
		})
	}
}

// ── The filter next to the severity filter ───────────────────────────────────

// matchesSeverity reads "no token" as "everything", which is false as soon as
// the Pod Security token is active alone: the pitfall of the plan.
func TestThePodSecurityTokenAloneDoesNotHideEverything(t *testing.T) {
	m := feed(t, onMisconfig(t, "baseline", pssFixtures()), testutil.Key(pssFilterKey))
	if len(visibleKeys(m)) == 0 {
		t.Fatal("the Pod Security token alone hides every finding: matchesSeverity reads it as a severity")
	}
}

func TestSeverityAndPodSecurityFiltersCombine(t *testing.T) {
	// c alone, no Pod Security filter: every CRITICAL one.
	m := feed(t, onMisconfig(t, "baseline", pssFixtures()), testutil.Key("c"))
	want := []string{"KSV-0022@deploy.yaml", "KSV-0030@deploy.yaml", "KSV-0003@deploy.yaml"}
	if got := visibleKeys(m); !reflect.DeepEqual(got, want) {
		t.Errorf("c alone: visible = %v, want %v", got, want)
	}

	// c then p: the CRITICAL ones the profile still requires.
	both := feed(t, m, testutil.Key(pssFilterKey))
	want = []string{"KSV-0022@deploy.yaml", "KSV-0003@deploy.yaml"}
	if got := visibleKeys(both); !reflect.DeepEqual(got, want) {
		t.Errorf("c then p: visible = %v, want %v", got, want)
	}

	// p off again: the severity filter is still on.
	off := feed(t, both, testutil.Key(pssFilterKey))
	want = []string{"KSV-0022@deploy.yaml", "KSV-0030@deploy.yaml", "KSV-0003@deploy.yaml"}
	if got := visibleKeys(off); !reflect.DeepEqual(got, want) {
		t.Errorf("p off: visible = %v, want %v", got, want)
	}

	// p then c, the other order.
	other := feed(t, onMisconfig(t, "baseline", pssFixtures()), testutil.Key(pssFilterKey), testutil.Key("c"))
	want = []string{"KSV-0022@deploy.yaml", "KSV-0003@deploy.yaml"}
	if got := visibleKeys(other); !reflect.DeepEqual(got, want) {
		t.Errorf("p then c: visible = %v, want %v", got, want)
	}
}

// ── The header (criterion 4) ─────────────────────────────────────────────────

func TestTheHeaderSaysTheProfileTheStateAndTheHiddenCount(t *testing.T) {
	m := onMisconfig(t, "baseline", pssFixtures())
	value, ok := podSecurityHeader(m)
	if !ok {
		t.Fatalf("no Pod Security entry in the header:\n%s", headerText(m))
	}
	for _, want := range []string{"baseline", "filter off"} {
		if !strings.Contains(value, want) {
			t.Errorf("filter off: %q lacks %q", value, want)
		}
	}
	if strings.Contains(value, "hidden") {
		t.Errorf("filter off: %q counts hidden findings", value)
	}

	on := feed(t, m, testutil.Key(pssFilterKey))
	value, _ = podSecurityHeader(on)
	for _, want := range []string{"baseline", "filter on", "3 hidden"} {
		if !strings.Contains(value, want) {
			t.Errorf("filter on: %q lacks %q", value, want)
		}
	}

	restricted := feed(t, onMisconfig(t, "restricted", pssFixtures()), testutil.Key(pssFilterKey))
	value, ok = podSecurityHeader(restricted)
	if !ok {
		t.Fatal("no Pod Security entry under restricted")
	}
	for _, want := range []string{"restricted", "filter on", "0 hidden"} {
		if !strings.Contains(value, want) {
			t.Errorf("restricted: %q lacks %q", value, want)
		}
	}
}

// The entry belongs to the Misconfigurations tab, in the results and in the
// details of one of its findings.
func TestTheHeaderEntryBelongsToTheMisconfigurationsTab(t *testing.T) {
	cve := pssModel(t, "baseline", pssFixtures())
	if _, ok := podSecurityHeader(cve); ok {
		t.Error("the CVE tab speaks of Pod Security")
	}
	m := onMisconfig(t, "baseline", pssFixtures())
	details := feed(t, m, testutil.Key("enter"))
	if details.state != StateDetails {
		t.Fatalf("state = %v, want the details", details.state)
	}
	if value, ok := podSecurityHeader(details); !ok || !strings.Contains(value, "baseline") {
		t.Errorf("details header: %q (present %v), want the profile", value, ok)
	}
}

func TestNoViolationDetectedIsSaidOnlyWhenTheActiveFilterLeavesNone(t *testing.T) {
	const phrase = "no violation detected by Trivy for"
	restrictedOnly := []scan.Finding{
		pssFinding("KSV-0030", "Seccomp not set", "deploy.yaml", scan.SeverityHigh),
		pssFinding("KSV-0003", "Capabilities not dropped", "deploy.yaml", scan.SeverityHigh),
	}

	// baseline, filter on, only a restricted finding left to hide.
	m := feed(t, onMisconfig(t, "baseline", restrictedOnly), testutil.Key(pssFilterKey))
	if got := headerText(m); !strings.Contains(got, phrase+" baseline") {
		t.Errorf("header lacks %q:\n%s", phrase+" baseline", got)
	}

	// Filter off: the filter says nothing about what it would leave.
	off := onMisconfig(t, "baseline", restrictedOnly)
	if got := headerText(off); strings.Contains(got, phrase) {
		t.Errorf("filter off, yet the header says:\n%s", got)
	}

	// restricted, filter on, and no Pod Security finding at all.
	none := []scan.Finding{pssFinding("KSV-0003", "Capabilities not dropped", "deploy.yaml", scan.SeverityHigh)}
	r := feed(t, onMisconfig(t, "restricted", none), testutil.Key(pssFilterKey))
	if got := headerText(r); !strings.Contains(got, phrase+" restricted") {
		t.Errorf("header lacks %q:\n%s", phrase+" restricted", got)
	}

	// A baseline finding remains: nothing is concluded.
	kept := feed(t, onMisconfig(t, "baseline", pssFixtures()), testutil.Key(pssFilterKey))
	if got := headerText(kept); strings.Contains(got, phrase) {
		t.Errorf("a baseline finding remains, yet the header says:\n%s", got)
	}

	// The severity filter and the search narrow the list; they do not turn a
	// list that holds Pod Security findings into one that holds none.
	narrowed := feed(t, kept, testutil.Key("l")) // LOW: no baseline finding is LOW
	if got := headerText(narrowed); strings.Contains(got, phrase) {
		t.Errorf("the severity filter emptied the Pod Security findings, and the header concludes:\n%s", got)
	}
	searched := feed(t, kept, testutil.Key("/"))
	searched = feed(t, searched, testutil.Type("zzz-no-match")...)
	if got := headerText(searched); strings.Contains(got, phrase) {
		t.Errorf("a search matched nothing, and the header concludes:\n%s", got)
	}
}

// Never "compliant", in any state: Trivy is the only source, and a clean scan
// is no proof of compliance.
func TestTheHeaderNeverSaysCompliant(t *testing.T) {
	states := map[string]Model{
		"off":        onMisconfig(t, "baseline", pssFixtures()),
		"on":         feed(t, onMisconfig(t, "baseline", pssFixtures()), testutil.Key(pssFilterKey)),
		"restricted": feed(t, onMisconfig(t, "restricted", pssFixtures()), testutil.Key(pssFilterKey)),
		"none left": feed(t, onMisconfig(t, "baseline", []scan.Finding{
			pssFinding("KSV-0030", "Seccomp not set", "deploy.yaml", scan.SeverityHigh),
		}), testutil.Key(pssFilterKey)),
	}
	for name, m := range states {
		if got := strings.ToLower(headerText(m)); strings.Contains(got, "complian") {
			t.Errorf("%s: the header speaks of compliance:\n%s", name, got)
		}
	}
}

// ── The counts ───────────────────────────────────────────────────────────────

func TestPSSCounts(t *testing.T) {
	tests := []struct {
		target        remediation.Profile
		hidden, shown int
	}{
		{remediation.ProfileBaseline, 3, 3},
		{remediation.ProfileRestricted, 0, 6},
	}
	for _, tt := range tests {
		hidden, shown := pssCounts(pssFixtures(), tt.target)
		if hidden != tt.hidden || shown != tt.shown {
			t.Errorf("pssCounts(%s) = hidden %d, shown %d; want %d, %d", tt.target, hidden, shown, tt.hidden, tt.shown)
		}
	}
	if hidden, shown := pssCounts(nil, remediation.ProfileBaseline); hidden != 0 || shown != 0 {
		t.Errorf("pssCounts(nil) = %d, %d, want 0, 0", hidden, shown)
	}
}

// matchesFinding is the table's whole token match: Pod Security and severity.
func TestMatchesFinding(t *testing.T) {
	byKey := map[string]scan.Finding{}
	for _, f := range pssFixtures() {
		byKey[f.ID+"@"+f.File] = f
	}
	on := map[string]bool{pssToken: true}
	tests := []struct {
		name   string
		target string
		active map[string]bool
		key    string
		want   bool
	}{
		{"baseline, filter on, restricted", "baseline", on, "KSV-0030@deploy.yaml", false},
		{"baseline, filter on, baseline", "baseline", on, "KSV-0017@deploy.yaml", true},
		{"baseline, filter on, outside Pod Security", "baseline", on, "KSV-0003@deploy.yaml", true},
		{"baseline, filter on, a Dockerfile rule", "baseline", on, "DS002@Dockerfile", true},
		{"baseline, filter on, absent seccomp", "baseline", on, "KSV-0104@b.yaml", false},
		{"baseline, filter on, explicit Unconfined", "baseline", on, "KSV-0104@a.yaml", true},
		{"baseline, filter on, restricted by Trivy", "baseline", on, "KSV-9999@deploy.yaml", false},
		{"baseline, filter off", "baseline", nil, "KSV-0030@deploy.yaml", true},
		{"restricted, filter on", "restricted", on, "KSV-0030@deploy.yaml", true},
		{"all, filter on", "all", on, "KSV-0030@deploy.yaml", true},
		{"an invalid target, filter on", "strict", on, "KSV-0030@deploy.yaml", true},
		{"severity alone, kept", "baseline", map[string]bool{"critical": true}, "KSV-0030@deploy.yaml", true},
		{"severity alone, dropped", "baseline", map[string]bool{"critical": true}, "KSV-0017@deploy.yaml", false},
		{"both, hidden by the profile", "baseline", map[string]bool{pssToken: true, "critical": true}, "KSV-0030@deploy.yaml", false},
		{"both, hidden by the severity", "baseline", map[string]bool{pssToken: true, "critical": true}, "KSV-0017@deploy.yaml", false},
		{"both, kept", "baseline", map[string]bool{pssToken: true, "critical": true}, "KSV-0022@deploy.yaml", true},
	}
	for _, tt := range tests {
		t.Run(tt.name, func(t *testing.T) {
			if got := matchesFinding(tt.target)(byKey[tt.key], tt.active); got != tt.want {
				t.Errorf("matchesFinding(%q)(%s, %v) = %v, want %v", tt.target, tt.key, tt.active, got, tt.want)
			}
		})
	}
}

func TestThePodSecurityTokenIsOnTheBar(t *testing.T) {
	var labels []string
	for _, tok := range findingTokens() {
		labels = append(labels, tok.Label)
	}
	for _, want := range []string{"critical", "high", "medium", "low", pssToken} {
		if !slices.Contains(labels, want) {
			t.Errorf("the bar lacks the %q token; has %v", want, labels)
		}
	}
	if pssToken != "pod security" || pssFilterKey != "p" {
		t.Errorf("token %q on key %q", pssToken, pssFilterKey)
	}
}

// ── An invalid setting (criterion 6) ─────────────────────────────────────────

func TestAnInvalidProfileWarnsOnEnteringTheTabAndHidesNothing(t *testing.T) {
	want := fmt.Sprintf(reasonPSSInvalid, "strict")

	m := pssModel(t, "strict", pssFixtures())
	m = feed(t, m, testutil.Key("tab"))
	if text, _ := footerOf(m); text == want {
		t.Error("the warning is shown on a tab it does not concern")
	}
	m = feed(t, m, testutil.Key("tab"), testutil.Key("tab"))
	if m.activeTab != TabMisconfig {
		t.Fatalf("activeTab = %d", m.activeTab)
	}
	text, level := footerOf(m)
	if text != want || level != sharedcomponents.LevelWarning {
		t.Errorf("footer = %q (level %v), want %q as a Warn", text, level, want)
	}

	if got := visibleKeys(m); !reflect.DeepEqual(got, allPSSKeys) {
		t.Errorf("visible = %v, want nothing hidden", got)
	}
	if !testutil.HasShortcut(m.GetShortcuts(), pssFilterKey) || !testutil.ShortcutDisabled(m.GetShortcuts(), pssFilterKey) {
		t.Error("p is not greyed under an invalid setting")
	}
	if a := m.canTogglePSS(); a.Reason != want {
		t.Errorf("reason = %q, want %q", a.Reason, want)
	}
	if _, ok := podSecurityHeader(m); ok {
		t.Errorf("the header speaks of a profile that is not in force:\n%s", headerText(m))
	}

	// Pressing the key says why again, and hides nothing.
	m = feed(t, m, testutil.Key(pssFilterKey))
	if m.findingsTable.IsTokenActive(pssToken) {
		t.Error("p switched the filter on under an invalid setting")
	}
	if got := visibleKeys(m); !reflect.DeepEqual(got, allPSSKeys) {
		t.Errorf("visible = %v after p, want nothing hidden", got)
	}
	if text, level := footerOf(m); text != want || level != sharedcomponents.LevelWarning {
		t.Errorf("footer after p = %q (level %v), want the reason as a Warn", text, level)
	}
}

// A valid setting does not warn on entering the tab, and an absent one neither.
func TestAValidProfileDoesNotWarnOnEnteringTheTab(t *testing.T) {
	for _, profile := range []string{"", "all", "baseline", "restricted"} {
		m := onMisconfig(t, profile, pssFixtures())
		if text, _ := footerOf(m); strings.Contains(text, "scan.pss_profile") {
			t.Errorf("profile %q warns: %q", profile, text)
		}
	}
}

// ── Shortcuts and help (Rules 114, 130, 135) ─────────────────────────────────

func TestThePodSecurityKeyIsOnEveryTabAndGreyedWhereItDoesNotApply(t *testing.T) {
	m := pssModel(t, "baseline", pssFixtures())
	for tab := 0; tab < tabCount; tab++ {
		sc := m.GetShortcuts()
		if !testutil.HasShortcut(sc, pssFilterKey) {
			t.Errorf("tab %d: p is not in the column", tab)
		}
		if enabled := testutil.ShortcutEnabled(sc, pssFilterKey); enabled != (tab == TabMisconfig) {
			t.Errorf("tab %d: p enabled = %v, want only on the Misconfigurations tab", tab, enabled)
		}
		m = feed(t, m, testutil.Key("tab"))
	}

	cve := pssModel(t, "baseline", pssFixtures())
	if a := cve.canTogglePSS(); a.Reason != reasonPSSNotMisconfigTab {
		t.Errorf("reason on the CVE tab = %q, want %q", a.Reason, reasonPSSNotMisconfigTab)
	}
	cve = feed(t, cve, testutil.Key(pssFilterKey))
	if text, level := footerOf(cve); text != reasonPSSNotMisconfigTab || level != sharedcomponents.LevelWarning {
		t.Errorf("p on the CVE tab: footer = %q (level %v), want the reason as a Warn", text, level)
	}
	if cve.findingsTable.IsTokenActive(pssToken) {
		t.Error("p switched the filter on off the Misconfigurations tab")
	}
}

// Rule 130: the set of keys does not change with the state of the screen.
func TestTheShortcutKeysDoNotChangeWithTheProfile(t *testing.T) {
	want := testutil.ShortcutKeys(pssModel(t, "", pssFixtures()).GetShortcuts())
	if !slices.Contains(want, pssFilterKey) {
		t.Fatalf("keys %v lack p", want)
	}
	states := map[string]Model{
		"all":                   pssModel(t, "all", pssFixtures()),
		"baseline":              pssModel(t, "baseline", pssFixtures()),
		"restricted":            pssModel(t, "restricted", pssFixtures()),
		"invalid":               pssModel(t, "strict", pssFixtures()),
		"baseline, tab":         onMisconfig(t, "baseline", pssFixtures()),
		"baseline, filter on":   feed(t, onMisconfig(t, "baseline", pssFixtures()), testutil.Key(pssFilterKey)),
		"invalid, misconfig":    onMisconfig(t, "strict", pssFixtures()),
		"restricted, filter on": feed(t, onMisconfig(t, "restricted", pssFixtures()), testutil.Key(pssFilterKey)),
	}
	for name, m := range states {
		if got := testutil.ShortcutKeys(m.GetShortcuts()); !reflect.DeepEqual(got, want) {
			t.Errorf("%s: keys %v, want %v", name, got, want)
		}
	}
}

func TestThePodSecurityShortcutIsAnImperativeDescription(t *testing.T) {
	for _, s := range pssModel(t, "baseline", pssFixtures()).GetShortcuts() {
		if s.Key != pssFilterKey {
			continue
		}
		if s.Description != "Toggle Pod Security filter" {
			t.Errorf("description = %q", s.Description)
		}
		return
	}
	t.Fatal("no p shortcut")
}

func TestTheHelpExplainsThePodSecurityFilterAndLabel(t *testing.T) {
	help := pssModel(t, "baseline", pssFixtures()).GetHelpContent()
	found := false
	for _, kb := range help.KeyBindings {
		if kb.Key == pssFilterKey {
			found = true
			if !strings.Contains(kb.Description, "Pod Security") {
				t.Errorf("the p binding reads %q", kb.Description)
			}
		}
	}
	if !found {
		t.Error("no p key binding in the help (Rule 114)")
	}
	var all strings.Builder
	for _, s := range help.Sections {
		all.WriteString(s.Body + "\n")
	}
	for _, want := range []string{"baseline", "restricted", "scan.pss_profile", "from Trivy, no fix"} {
		if !strings.Contains(all.String(), want) {
			t.Errorf("no help section mentions %q", want)
		}
	}
}
