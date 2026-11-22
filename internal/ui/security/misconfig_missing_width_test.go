package security

import (
	"strings"
	"testing"

	tea "charm.land/bubbletea/v2"

	"github.com/anthnel/devdesk/internal/scan"
	"github.com/anthnel/devdesk/internal/ui/testutil"
)

// DEVDESK-689 review: the footer keeps one line and truncates it with an
// ellipsis (FooterMessage.View), so at 80 columns the end of a long refusal is
// lost. What the manifest needs, the field and the value, comes first and must
// survive; the secondary details may be cut.

// footerWidth is the width the review measured at.
const footerWidth = 80

// visibleCase is one finding and the words that must still be on the footer
// line at footerWidth: the field and the value the profile expects.
type visibleCase struct {
	name    string
	f       scan.Finding
	profile string
	visible []string
}

// KSV-0030 and KSV-0104 absent are no longer here: they have a fix since
// DEVDESK-688 and their key is lit. KSV-0104 on an explicit Unconfined stays a
// refusal, greyed from the finding alone.
func visibleCases() []visibleCase {
	unconfined := missingFinding("KSV-0104", pssBaselineLn)
	unconfined.SeccompOrigin = "unconfined"
	return []visibleCase{
		{"KSV-0103", missingFinding("KSV-0103"), "baseline", []string{"windowsOptions.hostProcess", "false"}},
		{"KSV-0022", missingFinding("KSV-0022"), "baseline", []string{"remove", "capabilities.add"}},
		{"KSV-0023", missingFinding("KSV-0023"), "baseline", []string{"remove", "hostPath", "volumes"}},
		{"KSV-0024", missingFinding("KSV-0024"), "baseline", []string{"remove", "hostPort"}},
		{"KSV-0002", missingFinding("KSV-0002"), "baseline", []string{"AppArmor annotation", "runtime/default"}},
		{"KSV-0025", missingFinding("KSV-0025"), "baseline", []string{"seLinuxOptions.type", "container_t", "role", "user"}},
		{"KSV-0026", missingFinding("KSV-0026"), "baseline", []string{"sysctls", "allowed list"}},
		{"KSV-0027", missingFinding("KSV-0027"), "baseline", []string{"remove", "procMount"}},
		{"KSV-0028", missingFinding("KSV-0028"), "restricted", []string{"volumes", "emptyDir", "nfs"}},
		{"KSV-0012", missingFinding("KSV-0012"), "restricted", []string{"runAsNonRoot", "to true"}},
		{"KSV-0105", missingFinding("KSV-0105"), "restricted", []string{"runAsUser", "greater than 0"}},
		{"KSV-0106", missingFinding("KSV-0106"), "restricted", []string{"capabilities.drop", "ALL", "NET_BIND_SERVICE"}},
		{"KSV-0121", missingFinding("KSV-0121"), "restricted", []string{"hostPath", "/, /boot, /dev, /etc"}},
		{"KSV-0104 unconfined", unconfined, "baseline", []string{"seccompProfile.type", "Unconfined", "RuntimeDefault"}},
	}
}

// The footer line the view really renders after the key, at 80 columns, still
// shows the field and the value of the rule, right after a short prefix.
func TestTheRefusalKeepsItsFieldAndValueAt80Columns(t *testing.T) {
	for _, tc := range visibleCases() {
		t.Run(tc.name, func(t *testing.T) {
			m, _ := hostModel(t, missingManifest, tc.f)
			m = feed(t, m, tea.WindowSizeMsg{Width: footerWidth, Height: 30})
			m, _ = step(t, m, testutil.Key(writeRemediationKey))

			line := strings.TrimSpace(plain(infoLine(m, footerWidth)))
			prefix := tc.f.ID + " (" + tc.profile + "): "
			if !strings.HasPrefix(line, prefix) {
				t.Errorf("footer = %q, want it to start with %q", line, prefix)
			}
			for _, want := range tc.visible {
				if !strings.Contains(line, want) {
					t.Errorf("footer at %d columns lost %q: %q", footerWidth, want, line)
				}
			}
		})
	}
}
