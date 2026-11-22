package remediation

import (
	"strings"

	"github.com/anthnel/devdesk/internal/scan"
)

// The Pod Security Standards profile of a Kubernetes finding (DEVDESK-686,
// S-01). Trivy writes the link to the standard, with a #baseline or #restricted
// anchor, in the References of the rules that implement it; the table below is
// that anchor written down, so it is known without a scan that kept the
// references (a cached result) and can be corrected where Trivy is not enough
// (KSV-0104). TestThePodSecurityTableMatchesTrivy keeps it honest against a
// recorded Trivy output; a rule the table lacks falls back on the anchor.

// Profile is a Pod Security Standards profile.
type Profile string

const (
	ProfileBaseline   Profile = "baseline"
	ProfileRestricted Profile = "restricted"
)

// Covers reports whether target requires p: restricted covers baseline.
func (target Profile) Covers(p Profile) bool {
	return target == ProfileRestricted || target == p
}

// PSS is the Pod Security profile of a finding. FromTrivy is set when the
// anchor was read in References and the rule is not in the table.
type PSS struct {
	Profile   Profile
	FromTrivy bool
}

// pssLinkBase is the page every anchor sits on.
const pssLinkBase = "https://kubernetes.io/docs/concepts/security/pod-security-standards/#"

// ruleSeccomp is KSV-0104, the one rule whose profile depends on the manifest.
var ruleSeccomp = RuleKey("KSV-0104")

// pssProfiles maps RuleKey to the profile Trivy's checks anchor. KSV-0117
// (ports below 1024) and KSV-0119 (NET_RAW) link the standard without an
// anchor, so they name no profile and are left out on purpose.
var pssProfiles = map[string]Profile{
	RuleKey("KSV-0002"): ProfileBaseline,
	RuleKey("KSV-0008"): ProfileBaseline,
	RuleKey("KSV-0009"): ProfileBaseline,
	RuleKey("KSV-0010"): ProfileBaseline,
	RuleKey("KSV-0017"): ProfileBaseline,
	RuleKey("KSV-0022"): ProfileBaseline,
	RuleKey("KSV-0023"): ProfileBaseline,
	RuleKey("KSV-0024"): ProfileBaseline,
	RuleKey("KSV-0025"): ProfileBaseline,
	RuleKey("KSV-0026"): ProfileBaseline,
	RuleKey("KSV-0027"): ProfileBaseline,
	RuleKey("KSV-0103"): ProfileBaseline,
	RuleKey("KSV-0104"): ProfileBaseline,

	RuleKey("KSV-0001"): ProfileRestricted,
	RuleKey("KSV-0012"): ProfileRestricted,
	RuleKey("KSV-0028"): ProfileRestricted,
	RuleKey("KSV-0030"): ProfileRestricted,
	RuleKey("KSV-0105"): ProfileRestricted,
	RuleKey("KSV-0106"): ProfileRestricted,
	RuleKey("KSV-0121"): ProfileRestricted,
}

// PodSecurity returns the profile that requires the rule behind f, and false
// when the finding is not a Pod Security one: not a Trivy misconfiguration, a
// rule that names no profile, or no rule at all.
//
// KSV-0104 is the exception to the table: Trivy anchors it #baseline, but it
// fires on a missing seccomp profile too, which only the restricted profile
// requires. Its SeccompOrigin decides; an unknown origin counts as baseline,
// so nothing is hidden without proof.
func PodSecurity(f scan.Finding) (PSS, bool) {
	if f.Source != scan.SourceTrivyMisconfig {
		return PSS{}, false
	}
	key := RuleKey(f.ID)
	if p, ok := pssProfiles[key]; ok {
		if key == ruleSeccomp && f.SeccompOrigin == "absent" {
			p = ProfileRestricted
		}
		return PSS{Profile: p}, true
	}
	if p, ok := pssAnchor(f.References); ok {
		return PSS{Profile: p, FromTrivy: true}, true
	}
	return PSS{}, false
}

// pssAnchor reads the profile out of a reference list: the link to the
// standard with a known anchor.
func pssAnchor(refs []string) (Profile, bool) {
	for _, r := range refs {
		anchor, ok := strings.CutPrefix(r, pssLinkBase)
		if !ok {
			continue
		}
		if p := Profile(anchor); p == ProfileBaseline || p == ProfileRestricted {
			return p, true
		}
	}
	return "", false
}
