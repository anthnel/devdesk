package security

import (
	"sort"
	"strconv"
	"strings"

	"charm.land/lipgloss/v2"

	"github.com/anthnel/devdesk/internal/remediation"
	"github.com/anthnel/devdesk/internal/scan"
	"github.com/anthnel/devdesk/internal/ui/datatable"
	"github.com/anthnel/devdesk/internal/ui/theme"
)

// DEVDESK-754 (SC1, step 2): the pure half of "Group by fix". Nothing here
// touches the Model; the toggle, the level and the rendering live elsewhere.

// groupKey identifies a group across two scans: what it is about, never its
// members or its target, so a re-scan finds the same row again.
type groupKey struct {
	Ecosystem, Subject, Installed string
	Unfixed                       bool
}

// fixGroup is one row of the grouped list.
type fixGroup struct {
	Key      groupKey
	Severity scan.SeverityLevel
	// Subject is the package, or the ID of a vulnerability that names none.
	Subject string
	// Change is the delta shown in the table, "1.1.1k -> 1.1.1w".
	Change string
	// Target is the one version the group moves to, "" when the versions could
	// not be ordered (several candidates) or none is published.
	Target string
	// Status is the Fix column, from fixStatusOf.
	Status fixStatus
	// Command is the group's command, "" when none is known or it is not plain
	// words (scan.PlainWords).
	Command string
	// Members are every finding of the group, not only the visible ones.
	Members []scan.Finding
}

// cveGroups groups the vulnerabilities of findings by the cause that clears
// them, from remediation.GroupAll. Worst severity first, then the most
// findings, then Subject, then Change.
func cveGroups(findings []scan.Finding) []fixGroup {
	fixes := remediation.GroupAll(findings)
	groups := make([]fixGroup, 0, len(fixes))
	for _, fx := range fixes {
		if len(fx.Members) == 0 {
			continue
		}
		g := fixGroup{
			Key:      groupKey{Ecosystem: fx.Ecosystem, Subject: fx.Package, Installed: fx.Installed, Unfixed: fx.Unfixed},
			Severity: fx.Severity,
			Subject:  fx.Package,
			Change:   fixChange(fx),
			Target:   fx.Target,
			Status:   fixStatusOf(fx.Members[0], false),
			Members:  fx.Members,
		}
		if fx.Command != "" && scan.PlainWords(fx.Command) {
			g.Command = fx.Command
		}
		groups = append(groups, g)
	}
	sort.SliceStable(groups, func(i, j int) bool {
		a, b := groups[i], groups[j]
		if ra, rb := severityRank(a.Severity), severityRank(b.Severity); ra != rb {
			return ra > rb
		}
		if len(a.Members) != len(b.Members) {
			return len(a.Members) > len(b.Members)
		}
		if a.Subject != b.Subject {
			return a.Subject < b.Subject
		}
		return a.Change < b.Change
	})
	return groups
}

// fixChange is the delta of a fix: the installed version, an arrow and what it
// moves to. An unfixed group moves to none; versions that could not be ordered
// are all listed, none preferred.
func fixChange(fx remediation.Fix) string {
	installed := fx.Installed
	if installed == "" {
		installed = "unknown"
	}
	switch {
	case fx.Unfixed:
		return installed + " -> none"
	case fx.Target != "":
		return installed + " -> " + fx.Target
	}
	var options []string
	for _, o := range fx.Options {
		for _, v := range strings.Split(o, ",") {
			if v = strings.TrimSpace(v); v != "" {
				options = append(options, v)
			}
		}
	}
	if len(options) == 0 {
		return installed + " -> none"
	}
	return installed + " -> " + strings.Join(options, " or ")
}

// findingIdentity is what sameFinding compares, as a map key.
type findingIdentity struct {
	source, rule, file, pkg, version, instance string
}

func identityOf(f scan.Finding) findingIdentity {
	return findingIdentity{f.Source, remediation.RuleKey(f.ID), f.File, f.PkgName, f.Version, instanceOf(f)}
}

// visibleGroups keeps the groups with at least one member in visible (matched
// by sameFinding), in the order of all. A kept group keeps all its members.
func visibleGroups(all []fixGroup, visible []scan.Finding) []fixGroup {
	shown := make(map[findingIdentity]struct{}, len(visible))
	for _, f := range visible {
		shown[identityOf(f)] = struct{}{}
	}
	kept := make([]fixGroup, 0, len(all))
	for _, g := range all {
		for _, f := range g.Members {
			if _, ok := shown[identityOf(f)]; ok {
				kept = append(kept, g)
				break
			}
		}
	}
	return kept
}

// groupIndexOf is the index of the group that holds f, -1 if none.
func groupIndexOf(groups []fixGroup, f scan.Finding) int {
	for i, g := range groups {
		for _, member := range g.Members {
			if sameFinding(member, f) {
				return i
			}
		}
	}
	return -1
}

// groupIndexByKey is the index of the group with key, else prev clamped into
// the list, -1 for an empty list.
func groupIndexByKey(groups []fixGroup, key groupKey, prev int) int {
	for i, g := range groups {
		if g.Key == key {
			return i
		}
	}
	return clampIndex(prev, len(groups))
}

// clampIndex puts i into [0, n), -1 when n is 0.
func clampIndex(i, n int) int {
	if n == 0 {
		return -1
	}
	return max(0, min(i, n-1))
}

// nearestFinding is the index in visible the findings cursor goes to when the
// grouped list is left (AC4): the first visible member of g, else prev if it is
// still visible, else prevCursor clamped into the list, -1 if visible is empty.
func nearestFinding(visible []scan.Finding, g *fixGroup, prev *scan.Finding, prevCursor int) int {
	if len(visible) == 0 {
		return -1
	}
	if g != nil && len(g.Members) > 0 {
		for i, f := range visible {
			for _, member := range g.Members {
				if sameFinding(member, f) {
					return i
				}
			}
		}
	}
	if prev != nil {
		for i, f := range visible {
			if sameFinding(*prev, f) {
				return i
			}
		}
	}
	return clampIndex(prevCursor, len(visible))
}

// cveGroupColumns describes the grouped list of the CVE tab: Severity, Package,
// Change, CVEs, Fix. Cells are plain text (Rule 122); only Severity is coloured,
// and it is also its word.
func cveGroupColumns() []datatable.Column[fixGroup] {
	return []datatable.Column[fixGroup]{
		{
			Title: "Severity", Sizing: datatable.SizingFixed, MinWidth: 10,
			Cell:  func(g fixGroup) string { return string(g.Severity) },
			Style: func(g fixGroup) lipgloss.Style { return theme.SeverityTextStyle(string(g.Severity)) },
			Less:  func(a, b fixGroup) bool { return severityRank(a.Severity) < severityRank(b.Severity) },
		},
		{
			Title: "Package", Sizing: datatable.SizingContent, MinWidth: 14,
			Cell: func(g fixGroup) string { return g.Subject },
			Less: func(a, b fixGroup) bool { return a.Subject < b.Subject },
		},
		{
			Title: "Change", Sizing: datatable.SizingContent, MinWidth: 16, Flex: 1,
			Cell: func(g fixGroup) string { return g.Change },
			Less: func(a, b fixGroup) bool { return a.Change < b.Change },
		},
		{
			Title: "CVEs", Sizing: datatable.SizingFixed, MinWidth: 5, DescFirst: true,
			Cell: func(g fixGroup) string { return strconv.Itoa(len(g.Members)) },
			Less: func(a, b fixGroup) bool { return len(a.Members) < len(b.Members) },
		},
		{
			Title: "Fix", Sizing: datatable.SizingContent, MinWidth: 16, MaxWidth: 24, Optional: true,
			Cell: func(g fixGroup) string { return g.Status.Label() },
			Less: func(a, b fixGroup) bool { return a.Status.Label() < b.Status.Label() },
		},
	}
}
