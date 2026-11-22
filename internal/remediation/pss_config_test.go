package remediation

import (
	"testing"

	"github.com/anthnel/devdesk/internal/config"
)

// config states the profile names and cannot import this package, so a test is
// the only thing that keeps the two in step (the arrangement of
// TestTheTrackNamesMatchTheConfig). Every name scan.pss_profile offers is "all"
// or a Profile of the table, and the other way round.
func TestTheProfileNamesMatchTheConfig(t *testing.T) {
	if string(ProfileBaseline) != config.PSSProfileBaseline || string(ProfileRestricted) != config.PSSProfileRestricted {
		t.Errorf("profile constants %q / %q differ from the config's %q / %q",
			ProfileBaseline, ProfileRestricted, config.PSSProfileBaseline, config.PSSProfileRestricted)
	}
	known := map[string]bool{string(ProfileBaseline): true, string(ProfileRestricted): true}
	offered := map[string]bool{}
	for _, name := range config.PSSProfiles() {
		offered[name] = true
		if name == config.PSSProfileAll {
			continue
		}
		if !known[name] {
			t.Errorf("config offers %q, which is not a Profile of the table", name)
		}
	}
	if !offered[config.PSSProfileAll] {
		t.Errorf("config does not offer %q", config.PSSProfileAll)
	}
	for name := range known {
		if !offered[name] {
			t.Errorf("Profile %q is not offered by config.PSSProfiles()", name)
		}
	}
	// The table itself only ever names a known profile.
	for rule, p := range pssProfiles {
		if !known[string(p)] {
			t.Errorf("rule %s has profile %q, not offered by the config", rule, p)
		}
	}
}
