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
	// Position is remediation.Fix.Position: it keeps two vulnerabilities that
	// name no package from sharing a key.
	Position int
	// The Misconfigurations tab's key (SC2). File is the raw path of the finding,
	// Rule is remediation.RuleKey of its ID, Fix is the state of the built-in fix and
	// Reason is the reason a declined fix gives, so that two instances the fix
	// declines for different reasons are two rows. Separate fields, never one joined
	// string, so that no two groups can share a key.
	File, Rule string
	Fix        fixState
	Reason     string
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
	// Misconfig marks a group of the Misconfigurations tab (SC2): Subject is then
	// the file as shown, Rule the ID of its first member, Change is empty and
	// Status is the state of the built-in fix its members share.
	Misconfig bool
	Rule      string
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
		key := groupKey{Ecosystem: fx.Ecosystem, Subject: fx.Package, Installed: fx.Installed, Unfixed: fx.Unfixed, Position: fx.Position}
		fx = displaySafe(fx)
		g := fixGroup{
			Key:      key,
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

// displaySafe is fx with the scan's text made safe for the terminal: the package,
// the versions and the candidates are shown as they are in the title, the footer
// and the cells, so none may carry an escape sequence, a control or a bidi mark.
// Each one removed is shown as "?" (visibleText, DEVDESK-766), so two packages
// that differ only by such a character do not look alike.
func displaySafe(fx remediation.Fix) remediation.Fix {
	fx.Package = visibleText(fx.Package)
	fx.Installed = visibleText(fx.Installed)
	fx.Target = visibleText(fx.Target)
	options := make([]string, len(fx.Options))
	for i, o := range fx.Options {
		options[i] = visibleText(o)
	}
	fx.Options = options
	return fx
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
		members := make(map[findingIdentity]struct{}, len(g.Members))
		for _, member := range g.Members {
			members[identityOf(member)] = struct{}{}
		}
		for i, f := range visible {
			if _, ok := members[identityOf(f)]; ok {
				return i
			}
		}
	}
	if prev != nil {
		want := identityOf(*prev)
		for i, f := range visible {
			if identityOf(f) == want {
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
