package remediation

import (
	"strings"
	"testing"

	"github.com/anthnel/devdesk/internal/scan"
)

// DEVDESK-689 (S-04): the text of the refusal for a Pod Security finding that
// has no safe fix. Every field and value asserted below was read in the source of
// the Trivy check (policies/kubernetes/policies/*.rego of the checks bundle
// the fixture was recorded with, sha256:b591e919...) or in the recorded output
// (testdata/pss/trivy-0.76.2.json); the comment on each row says which.
//
// The assertions are deliberately loose on wording and strict on content: a
// group lists acceptable spellings of one fact (any of them satisfies it), and
// every group must be satisfied. The profile name is always required, because
// the story asks for the value "expected for the profile of the finding".

// missingCase is one rule and what its refusal must carry.
type missingCase struct {
	name    string
	f       scan.Finding
	profile Profile
	// must lists groups of acceptable spellings; each group must match once.
	must [][]string
	// forbid lists words the text must not contain.
	forbid []string
}

// gone is how a "the field must not be there" value may be worded.
var gone = []string{"remove", "unset", "not set", "omit", "without", "drop the", "delete"}

func seccomp(origin string) scan.Finding {
	f := pssFinding("KSV-0104", pssBaselineLn)
	f.SeccompOrigin = origin
	return f
}

func missingCases() []missingCase {
	return []missingCase{
		// windows_host_process.rego: windowsOptions.hostProcess == true at the pod
		// or at a container.
		{name: "KSV-0103", f: pssFinding("KSV-0103"), profile: ProfileBaseline,
			must: [][]string{{"hostProcess"}, {"windowsOptions"}, append([]string{"false"}, gone...)}},
		// specific_capabilities_added.rego: any entry in capabilities.add fails
		// (allowed_caps is empty); the story says Trivy is stricter than the
		// standard, which allows a list of capabilities.
		{name: "KSV-0022", f: pssFinding("KSV-0022"), profile: ProfileBaseline,
			must: [][]string{{"capabilities.add"}, {"stricter"}, {"Pod Security"}, gone}},
		// hostpath_volumes_mounted.rego: any volume with a hostPath key fails.
		{name: "KSV-0023", f: pssFinding("KSV-0023"), profile: ProfileBaseline,
			must: [][]string{{"hostPath"}, {"volumes"}, gone}},
		// access_to_host_ports.rego: any ports[*].hostPort fails.
		{name: "KSV-0024", f: pssFinding("KSV-0024"), profile: ProfileBaseline,
			must: [][]string{{"hostPort"}, gone}},
		// apparmor_policy_disabled.rego: the annotation
		// container.apparmor.security.beta.kubernetes.io/<container> must be
		// runtime/default when present.
		{name: "KSV-0002", f: pssFinding("KSV-0002"), profile: ProfileBaseline,
			must: [][]string{{"container.apparmor.security.beta.kubernetes.io"}, {"runtime/default"}}},
		// selinux_custom_options_set.rego: seLinuxOptions.type among container_t,
		// container_init_t, container_kvm_t; role and user not set.
		{name: "KSV-0025", f: pssFinding("KSV-0025"), profile: ProfileBaseline,
			must: [][]string{{"seLinuxOptions"}, {"container_t"}}},
		// unsafe_sysctl_options_set.rego: spec.securityContext.sysctls names must
		// be in a version-dependent allow-list; kernel.shm_rmid_forced is in all of
		// them.
		{name: "KSV-0026", f: pssFinding("KSV-0026"), profile: ProfileBaseline,
			must: [][]string{{"sysctls"}, {"kernel.shm_rmid_forced"}}},
		// non_default_proc_masks_set.rego: the mere presence of procMount fails,
		// even with the value Default.
		{name: "KSV-0027", f: pssFinding("KSV-0027"), profile: ProfileBaseline,
			must: [][]string{{"procMount"}, gone}},
		// non_core_volume_types.rego: a list of disallowed types (nfs, iscsi, gcePersistentDisk...).
		// emptyDir is on neither the Trivy list nor the disallowed ones of the standard.
		{name: "KSV-0028", f: pssFinding("KSV-0028"), profile: ProfileRestricted,
			must: [][]string{{"volumes"}, {"emptyDir"}}},
		// runs_as_root.rego, fixture message: "should set
		// 'securityContext.runAsNonRoot' to true".
		{name: "KSV-0012", f: pssFinding("KSV-0012"), profile: ProfileRestricted,
			must:   [][]string{{"runAsNonRoot"}, {"true"}},
			forbid: []string{"ctrl+o"}},
		// runs_with_a_root_uid.rego, fixture message: "securityContext.runAsUser
		// should be set to a value greater than 0".
		{name: "KSV-0105", f: pssFinding("KSV-0105"), profile: ProfileRestricted,
			must: [][]string{{"runAsUser"}, {"greater than 0", "non-zero", "above 0", "> 0"}}},
		// drop_all_capabilities_only_add_net_bind_service.rego: capabilities.drop
		// must hold ALL, and capabilities.add may only hold NET_BIND_SERVICE. It is
		// about capabilities, not about runAsGroup.
		{name: "KSV-0106", f: pssFinding("KSV-0106"), profile: ProfileRestricted,
			must:   [][]string{{"capabilities.drop"}, {"ALL"}, {"NET_BIND_SERVICE"}},
			forbid: []string{"runAsGroup"}},
		// Kubernetes_resource_with_disallowed_volumes_mounted.rego: hostPath.path
		// equal to /, /boot, /dev, /etc, /lib, /proc, /sys, /usr or /var/lib/docker.
		{name: "KSV-0121", f: pssFinding("KSV-0121"), profile: ProfileRestricted,
			must: [][]string{{"hostPath"}, {"/etc", "/var/lib/docker"}}},
		// runtime_default_seccomp_profile_not_set.rego: Either Pod or Container
		// should set 'securityContext.seccompProfile.type' to 'RuntimeDefault'.
		{name: "KSV-0030", f: pssFinding("KSV-0030"), profile: ProfileRestricted,
			must: [][]string{{"seccompProfile"}, {"RuntimeDefault"}}},
		// seccomp_profile_unconfined.rego fires on an explicit Unconfined (baseline)...
		{name: "KSV-0104 unconfined", f: seccomp("unconfined"), profile: ProfileBaseline,
			must: [][]string{{"seccompProfile"}, {"Unconfined"}, {"RuntimeDefault"}}},
		// ... and on a seccomp type that is absent (restricted, see PodSecurity).
		{name: "KSV-0104 absent", f: seccomp("absent"), profile: ProfileRestricted,
			must: [][]string{{"seccompProfile"}, {"RuntimeDefault"}}},
		// An unknown origin counts as baseline in PodSecurity; the text still names the field.
		{name: "KSV-0104 unknown origin", f: seccomp(""), profile: ProfileBaseline,
			must: [][]string{{"seccompProfile"}}},
	}
}

// Criterion 1 and 3: every rule has a text of its own, naming the field and
// the value for the profile of the finding; none is empty or generic.
func TestMissingForEveryRuleWithoutAFix(t *testing.T) {
	seen := map[string]string{}
	for _, tc := range missingCases() {
		t.Run(tc.name, func(t *testing.T) {
			pss, ok := PodSecurity(tc.f)
			if !ok || pss.Profile != tc.profile {
				t.Fatalf("PodSecurity = %+v, %v; the case expects %s", pss, ok, tc.profile)
			}
			text, ok := MissingFor(tc.f)
			if !ok || strings.TrimSpace(text) == "" {
				t.Fatalf("MissingFor = %q, %v; want a text", text, ok)
			}
			if text == ReasonNoFixForRule {
				t.Errorf("the text is the generic refusal %q", text)
			}
			lower := strings.ToLower(text)
			if !strings.Contains(lower, string(tc.profile)) {
				t.Errorf("the text does not name the profile %q: %s", tc.profile, text)
			}
			for _, group := range tc.must {
				if !containsAny(lower, group) {
					t.Errorf("the text lacks any of %q: %s", group, text)
				}
			}
			for _, word := range tc.forbid {
				if strings.Contains(lower, strings.ToLower(word)) {
					t.Errorf("the text contains %q: %s", word, text)
				}
			}
			if other, dup := seen[text]; dup {
				t.Errorf("the text is the same as %s's: %s", other, text)
			}
			seen[text] = tc.name
		})
	}
	if len(missingCases()) != 17 {
		t.Errorf("%d cases, want the 13 rules without a fix + KSV-0030 + KSV-0104 in three origins", len(missingCases()))
	}
}

func containsAny(lowerText string, group []string) bool {
	for _, w := range group {
		if strings.Contains(lowerText, strings.ToLower(w)) {
			return true
		}
	}
	return false
}

// The table of the story: every rule of the Pod Security table has a text,
// except the five that have a fix of their own. A rule added to the table
// without a text fails here. The fixed set is written out, not read from the
// catalog, because S-03 will add KSV-0030 and KSV-0104 to the catalog and their
// text stays.
func TestEveryPodSecurityRuleWithoutAFixHasAText(t *testing.T) {
	hasFix := map[string]bool{}
	for _, id := range []string{"KSV-0017", "KSV-0001", "KSV-0008", "KSV-0009", "KSV-0010"} {
		hasFix[RuleKey(id)] = true
	}
	withText := 0
	for key := range pssProfiles {
		f := pssFinding(key)
		text, ok := MissingFor(f)
		if hasFix[key] {
			continue
		}
		withText++
		if !ok || strings.TrimSpace(text) == "" {
			t.Errorf("%s: no text", key)
		}
	}
	if withText != 15 {
		t.Errorf("%d rules without a fix in the table, want 15", withText)
	}
}

// Criterion 6: a rule that has a fix does not get the "what is missing" text;
// the confirmation diff (or the reason of its own decline) is what the user sees.
func TestMissingForSaysNothingOnARuleWithAFix(t *testing.T) {
	for _, id := range []string{"KSV-0017", "KSV-0001", "KSV-0008", "KSV-0009", "KSV-0010"} {
		f := pssFinding(id)
		if _, ok := FixFor(f); !ok {
			t.Fatalf("%s has no fix in the catalog: the premise of this test is wrong", id)
		}
		if text, ok := MissingFor(f); ok || text != "" {
			t.Errorf("%s: MissingFor = %q, %v; a rule with a fix has no missing text", id, text, ok)
		}
	}
}

// Criterion 4: the anchor is not in the table, the profile comes from Trivy.
// The text says so, says DevDesk has no fix and no text for the rule, and sends
// the reader to Trivy's description of the check; no field and no value.
func TestMissingForARuleOutsideTheTable(t *testing.T) {
	for _, profile := range []Profile{ProfileBaseline, ProfileRestricted} {
		t.Run(string(profile), func(t *testing.T) {
			f := pssFinding("KSV-9999", "https://avd.aquasec.com/misconfig/ksv-9999", pssBase+"#"+string(profile))
			if got, ok := PodSecurity(f); !ok || !got.FromTrivy {
				t.Fatalf("PodSecurity = %+v, %v; the premise is a profile read in Trivy", got, ok)
			}
			text, ok := MissingFor(f)
			if !ok {
				t.Fatal("no text for a Pod Security finding whose profile comes from Trivy")
			}
			lower := strings.ToLower(text)
			for _, want := range []string{"trivy", string(profile), "ksv-9999", "description"} {
				if !strings.Contains(lower, want) {
					t.Errorf("the text lacks %q: %s", want, text)
				}
			}
			if !containsAny(lower, []string{"no fix", "no built-in fix", "no correction", "no safe fix"}) {
				t.Errorf("the text does not say DevDesk has no fix: %s", text)
			}
			// Nothing is invented: no field, no value, no path from any other rule's text.
			for _, invented := range []string{
				"securityContext", "runAs", "seccomp", "capabilities", "hostPath", "hostPort",
				"volumes", "sysctls", "procMount", "seLinux", "apparmor", "windowsOptions",
				"privileged", "RuntimeDefault", "NET_BIND_SERVICE",
			} {
				if strings.Contains(lower, strings.ToLower(invented)) {
					t.Errorf("the text names %q for a rule DevDesk knows nothing about: %s", invented, text)
				}
			}
			for _, tc := range missingCases() {
				if other, _ := MissingFor(tc.f); other == text {
					t.Errorf("the text is the text of %s", tc.name)
				}
			}
		})
	}
}

// A finding that is not Pod Security has no such text: another KSV rule, a
// rule that links the standard without an anchor, another source, a Dockerfile rule.
func TestMissingForIsSilentOutsidePodSecurity(t *testing.T) {
	kubeconform := pssFinding("KSV-0012")
	kubeconform.Source = scan.SourceKubeconform
	vuln := pssFinding("CVE-2024-1", pssBaselineLn)
	vuln.Source = "trivy"
	tests := map[string]scan.Finding{
		"a KSV rule outside Pod Security": pssFinding("KSV-0003"),
		"a PSS link without an anchor":    pssFinding("KSV-0117", pssBase),
		"a kubeconform finding":           kubeconform,
		"a vulnerability":                 vuln,
		"a Dockerfile rule":               {ID: "AVD-DS-0026", Source: scan.SourceTrivyMisconfig},
	}
	for name, f := range tests {
		if text, ok := MissingFor(f); ok || text != "" {
			t.Errorf("%s: MissingFor = %q, %v; want nothing", name, text, ok)
		}
	}
}

// Both spellings of a rule id reach the same text, as for the catalog.
func TestMissingForReadsEitherSpellingOfTheRule(t *testing.T) {
	want, ok := MissingFor(pssFinding("KSV-0012"))
	if !ok {
		t.Fatal("no text for KSV-0012")
	}
	for _, id := range []string{"AVD-KSV-0012", "KSV012"} {
		if got, _ := MissingFor(pssFinding(id)); got != want {
			t.Errorf("%s: text %q, want the one of KSV-0012", id, got)
		}
	}
}
