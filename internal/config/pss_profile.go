package config

import (
	"log"
	"slices"
)

// The Pod Security profile a context aims at (DEVDESK-686, S-01).
//
// The names are stated here and in internal/remediation, which config must not
// import; TestTheProfileNamesMatchTheConfig (internal/remediation) holds the two
// in step.
const (
	// PSSProfileAll is the default: nothing is hidden by the filter.
	PSSProfileAll = "all"
	// PSSProfileBaseline aims at the baseline profile.
	PSSProfileBaseline = "baseline"
	// PSSProfileRestricted aims at the restricted profile, which includes baseline.
	PSSProfileRestricted = "restricted"
)

// PSSProfiles is the set scan.pss_profile cycles through in the configuration view.
func PSSProfiles() []string {
	return []string{PSSProfileAll, PSSProfileBaseline, PSSProfileRestricted}
}

// PSSProfileValid reports whether scan.pss_profile is absent or one of the set.
func (c ScanConfig) PSSProfileValid() bool {
	return c.PSSProfile == "" || slices.Contains(PSSProfiles(), c.PSSProfile)
}

// EffectivePSSProfile is the profile in force: the value when it is valid and
// non-empty, all otherwise.
func (c ScanConfig) EffectivePSSProfile() string {
	if c.PSSProfile == "" || !c.PSSProfileValid() {
		return PSSProfileAll
	}
	return c.PSSProfile
}

// warnInvalidPSSProfile logs a scan.pss_profile outside the set. The file is
// left alone: a typo is not silently rewritten.
func warnInvalidPSSProfile(c ScanConfig) {
	if !c.PSSProfileValid() {
		log.Printf("WARN [config] scan.pss_profile %q is not %s, %s or %s, using %s",
			c.PSSProfile, PSSProfileAll, PSSProfileBaseline, PSSProfileRestricted, PSSProfileAll)
	}
}
