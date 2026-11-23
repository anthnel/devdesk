package security

import (
	"fmt"
	"strconv"

	tea "charm.land/bubbletea/v2"

	"github.com/anthnel/devdesk/internal/config"
	"github.com/anthnel/devdesk/internal/remediation"
	"github.com/anthnel/devdesk/internal/scan"
	sharedcomponents "github.com/anthnel/devdesk/internal/ui/components"
	"github.com/anthnel/devdesk/internal/ui/shortcut"
	"github.com/anthnel/devdesk/internal/ui/theme"
)

// The Pod Security profile on the Misconfigurations tab (DEVDESK-686, S-01):
// a label on every Pod Security finding, and a filter that hides what the
// target profile does not require. The label shows under every setting; the
// filter is off when the tab opens and p is greyed under all.

// pssFilterKey toggles the Pod Security filter.
const pssFilterKey = "p"

// pssToken is the filter's label in the bar.
const pssToken = "pod security"

// pssLabelSeparator sits between the label and the title in the Title cell.
const pssLabelSeparator = " · "

// Why `p` is greyed (Rule 130).
const (
	reasonPSSNotMisconfigTab = "The Pod Security filter belongs to the Misconfigurations tab"
	reasonPSSAll             = "scan.pss_profile is all: choose baseline or restricted in the configuration"
	reasonPSSInvalid         = "scan.pss_profile %q is not all, baseline or restricted, so all is used"
)

// pssFromTrivySuffix says the profile was read in Trivy's references for a rule
// the table does not know.
const pssFromTrivySuffix = " (from Trivy, no fix)"

// findingTokens are the severity tokens plus the Pod Security one.
func findingTokens() []sharedcomponents.FilterToken {
	return append(severityTokens(), sharedcomponents.FilterToken{Label: pssToken})
}

// matchesFinding is the findings table's token match for a target profile
// ("all", "baseline" or "restricted"). The severity tokens and the Pod Security
// one are separate questions: a finding must pass both.
func matchesFinding(target string) func(scan.Finding, map[string]bool) bool {
	return func(f scan.Finding, active map[string]bool) bool {
		if !matchesSeverity(f, active) {
			return false
		}
		return !active[pssToken] || !pssHides(f, target)
	}
}

// pssHides reports whether the target profile leaves the finding out: it is a
// Pod Security finding the target does not require. Under all, or a value that
// is not a profile, nothing is hidden.
func pssHides(f scan.Finding, target string) bool {
	t := remediation.Profile(target)
	if t != remediation.ProfileBaseline && t != remediation.ProfileRestricted {
		return false
	}
	pss, ok := remediation.PodSecurity(f)
	return ok && !t.Covers(pss.Profile)
}

// pssLabel is the label a finding carries, or "" outside Pod Security.
func pssLabel(f scan.Finding) string {
	pss, ok := remediation.PodSecurity(f)
	if !ok {
		return ""
	}
	if pss.FromTrivy {
		return string(pss.Profile) + pssFromTrivySuffix
	}
	return string(pss.Profile)
}

// titleWithLabel is the Title cell: the title, preceded by the label when the
// finding has one. Plain text (Rule 122).
func titleWithLabel(f scan.Finding) string {
	if label := pssLabel(f); label != "" {
		return label + pssLabelSeparator + f.Title
	}
	return f.Title
}

// canTogglePSS says whether `p` applies here.
func (m Model) canTogglePSS() shortcut.Availability {
	switch {
	case m.activeTab != TabMisconfig:
		return shortcut.Unavailable(reasonPSSNotMisconfigTab)
	case !m.config.Scan.PSSProfileValid():
		return shortcut.Unavailable(m.invalidPSSReason())
	case m.config.Scan.EffectivePSSProfile() == config.PSSProfileAll:
		return shortcut.Unavailable(reasonPSSAll)
	}
	return shortcut.Availability{}
}

func (m Model) invalidPSSReason() string {
	return fmt.Sprintf(reasonPSSInvalid, m.config.Scan.PSSProfile)
}

// togglePSS flips the Pod Security filter, or says why it cannot (Rule 130).
func (m Model) togglePSS() (tea.Model, tea.Cmd) {
	if a := m.canTogglePSS(); !a.Enabled() {
		return m, m.footer.Warn(a.Reason)
	}
	m.findingsTable.SetTokenActive(pssToken, !m.findingsTable.IsTokenActive(pssToken))
	m.findingsTable.GotoTop()
	m.refreshGroups()
	return m, nil
}

// enterPSSTab warns, on entering the Misconfigurations tab, that the profile in
// the configuration is not one DevDesk knows. Nothing is hidden meanwhile.
func (m *Model) enterPSSTab() tea.Cmd {
	if m.activeTab != TabMisconfig || m.config.Scan.PSSProfileValid() {
		return nil
	}
	return m.footer.Warn(m.invalidPSSReason())
}

// pssHeader is the header entry of the Misconfigurations tab: the profile, the
// state of the filter, and what it hides. Absent under all and under a value
// that is not in force.
func (m Model) pssHeader() (shortcut.HeaderInfo, bool) {
	if !m.showsResult() || m.result == nil || m.activeTab != TabMisconfig {
		return shortcut.HeaderInfo{}, false
	}
	target := m.config.Scan.EffectivePSSProfile()
	if target == config.PSSProfileAll {
		return shortcut.HeaderInfo{}, false
	}
	value := target + " · filter off"
	if m.findingsTable.IsTokenActive(pssToken) {
		// Counted over the whole tab, before the severity filter and the search:
		// those narrow the list, they do not make Trivy's answer any shorter.
		hidden, shown := pssCounts(m.filterFindingsByTab(), remediation.Profile(target))
		value = target + " · filter on · " + strconv.Itoa(hidden) + " hidden"
		if shown == 0 {
			// Said as what Trivy did, never as compliance.
			value += " · no violation detected by Trivy for " + target
		}
	}
	return shortcut.HeaderInfo{Key: "Pod Security", Value: value, Style: theme.HeaderValueStyle}, true
}

// pssCounts says how many Pod Security findings the target profile hides, and
// how many it keeps.
func pssCounts(findings []scan.Finding, target remediation.Profile) (hidden, shownPSS int) {
	for _, f := range findings {
		pss, ok := remediation.PodSecurity(f)
		switch {
		case !ok:
		case target.Covers(pss.Profile):
			shownPSS++
		default:
			hidden++
		}
	}
	return hidden, shownPSS
}
