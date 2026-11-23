package security

import (
	"github.com/anthnel/devdesk/internal/scan"
	"github.com/anthnel/devdesk/internal/ui/datatable"
)

// DEVDESK-754 (SC1, step 2): the pure half of "Group by fix". Nothing here
// touches the Model; the toggle, the level and the rendering come later.
//
// SKELETON (red phase): every function below compiles and returns the empty
// answer. Replace the bodies, keep the signatures.

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
func cveGroups(findings []scan.Finding) []fixGroup { return nil }

// visibleGroups keeps the groups with at least one member in visible (matched
// by sameFinding), in the order of all. A kept group keeps all its members.
func visibleGroups(all []fixGroup, visible []scan.Finding) []fixGroup { return nil }

// groupIndexOf is the index of the group that holds f, -1 if none.
func groupIndexOf(groups []fixGroup, f scan.Finding) int { return -1 }

// groupIndexByKey is the index of the group with key, else prev clamped into
// the list, -1 for an empty list.
func groupIndexByKey(groups []fixGroup, key groupKey, prev int) int { return -1 }

// nearestFinding is the index in visible the findings cursor goes to when the
// grouped list is left (AC4): the first visible member of g, else prev if it is
// still visible, else prevCursor clamped into the list, -1 if visible is empty.
func nearestFinding(visible []scan.Finding, g *fixGroup, prev *scan.Finding, prevCursor int) int {
	return -1
}

// cveGroupColumns describes the grouped list of the CVE tab: Severity, Package,
// Change, CVEs, Fix.
func cveGroupColumns() []datatable.Column[fixGroup] { return nil }
