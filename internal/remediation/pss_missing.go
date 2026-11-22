package remediation

import (
	"fmt"

	"github.com/anthnel/devdesk/internal/scan"
)

// missingRule is what a Pod Security rule without a fix asks of the manifest:
// the rule as it is written for the user, the field with the value the profile
// expects, and what is worth knowing besides. Every entry was read in the source
// of the Trivy check (the checks bundle the recorded output was made with,
// testdata/pss/), none from memory of the standard.
//
// The text is read in a footer that keeps one line and cuts it with an ellipsis,
// 80 columns being the narrow case: need is what comes first and must fit after
// the prefix; detail comes last and may be the part that is cut.
type missingRule struct {
	id     string
	need   string
	detail string
}

// missingRules is keyed by RuleKey. It holds the rules of the Pod Security table
// that have no fix of their own; a rule with a fix (KSV-0017, KSV-0001,
// KSV-0008 to KSV-0010) is absent on purpose, so its confirmation diff is what
// the user sees. KSV-0030 and KSV-0104 have a fix since DEVDESK-688 and stay
// here: the text is shown only for a KSV-0104 Unconfined, which the fix never
// touches and greys in advance. Every other refusal of the seccomp fix shows its
// own ReasonSeccomp* reason at the press of the key, not this text. KSV-0104 is
// handled apart because its text depends on SeccompOrigin.
var missingRules = map[string]missingRule{
	RuleKey("KSV-0103"): {"KSV-0103", "set windowsOptions.hostProcess to false, or remove it", ""},
	RuleKey("KSV-0022"): {"KSV-0022", "remove capabilities.add", "Trivy's check is stricter than Pod Security, which allows a list of capabilities"},
	RuleKey("KSV-0023"): {"KSV-0023", "remove the hostPath entries of volumes", ""},
	RuleKey("KSV-0024"): {"KSV-0024", "remove hostPort from the container ports", ""},
	RuleKey("KSV-0002"): {"KSV-0002", "set the AppArmor annotation to runtime/default", "the annotation is container.apparmor.security.beta.kubernetes.io/<container>"},
	RuleKey("KSV-0025"): {"KSV-0025", "set seLinuxOptions.type to container_t, no role or user", "container_init_t and container_kvm_t are allowed too; any seLinuxOptions.role or user fails"},
	RuleKey("KSV-0026"): {"KSV-0026", "keep sysctls names in the allowed list", "such as kernel.shm_rmid_forced, in securityContext.sysctls"},
	RuleKey("KSV-0027"): {"KSV-0027", "remove procMount", "even Default fails"},
	RuleKey("KSV-0028"): {"KSV-0028", "limit volumes to allowed types (emptyDir, not nfs)", ""},
	RuleKey("KSV-0012"): {"KSV-0012", "set runAsNonRoot to true", "on the pod, or on every container"},
	RuleKey("KSV-0105"): {"KSV-0105", "set runAsUser to a value greater than 0", ""},
	RuleKey("KSV-0106"): {"KSV-0106", "capabilities.drop needs ALL, add only NET_BIND_SERVICE", ""},
	RuleKey("KSV-0121"): {"KSV-0121", "do not mount a hostPath on /, /boot, /dev, /etc, /lib, /proc, /sys, /usr or /var/lib/docker", ""},
	RuleKey("KSV-0030"): {"KSV-0030", "set seccompProfile.type to RuntimeDefault", "on the pod or on the container"},
}

// seccompNeed is the text of KSV-0104 for each origin of the finding.
func seccompNeed(origin string) (need, detail string) {
	switch origin {
	case "unconfined":
		return "set seccompProfile.type: RuntimeDefault, not Unconfined", ""
	case "absent":
		return "set seccompProfile.type to RuntimeDefault", "seccompProfile is not set"
	default:
		return "set seccompProfile", ""
	}
}

// MissingFor is the text of the refusal for a Pod Security finding DevDesk has
// no safe fix for (DEVDESK-689, S-04): the field to set and the value the
// finding's profile expects. The boolean is false when there is nothing to say,
// that is when f is not a Pod Security finding, or when its rule has a fix of
// its own.
//
// A rule whose profile was read in Trivy (not in the table) gets a text that
// says so and names no field: DevDesk knows nothing else about it.
func MissingFor(f scan.Finding) (string, bool) {
	pss, ok := PodSecurity(f)
	if !ok {
		return "", false
	}
	if pss.FromTrivy {
		return fmt.Sprintf("%s: the %s Pod Security profile comes from Trivy. DevDesk has no fix and no text for this rule; see the check's description in Trivy", f.ID, pss.Profile), true
	}
	key := RuleKey(f.ID)
	if key == ruleSeccomp {
		need, detail := seccompNeed(f.SeccompOrigin)
		return missingText(pss.Profile, "KSV-0104", need, detail), true
	}
	r, ok := missingRules[key]
	if !ok {
		return "", false
	}
	return missingText(pss.Profile, r.id, r.need, r.detail), true
}

// missingText puts the rule, its profile and what it asks in one sentence, the
// need right after a short prefix, then the fact that the finding has no fix,
// then the detail. It does not offer a fix: the finding has none.
func missingText(p Profile, id, need, detail string) string {
	text := fmt.Sprintf("%s (%s): %s (no built-in fix)", id, p, need)
	if detail != "" {
		text += " — " + detail
	}
	return text
}
